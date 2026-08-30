package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/store"
	stripe "github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/webhook"
)

// One Stripe *Client* for every request the coordinator makes (billing + payouts).
// The connectdemo package builds its own client for its self-contained sample.
func newStripeClient(key string) *stripe.Client {
	if key == "" {
		return nil
	}
	return stripe.NewClient(key)
}

func (s *Server) requireStripe(w http.ResponseWriter) bool {
	if s.stripe == nil {
		writeError(w, http.StatusNotImplemented, "billing_not_configured",
			"Stripe is not configured on this coordinator (set SC_STRIPE_SECRET_KEY)")
		return false
	}
	return true
}

// --- consumer: balance + prepaid top-up (Stripe Checkout) ---

func (s *Server) handleBalance(w http.ResponseWriter, r *http.Request) {
	keyID := keyIDFrom(r.Context())
	bal, err := s.store.CreditBalance(r.Context(), keyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", "could not read balance")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"key_id":        keyID,
		"credit_micros": bal,
		"credit_usd":    float64(bal) / 1_000_000,
		"enforced":      s.cfg.BillingEnforce,
	})
}

func (s *Server) handleCheckout(w http.ResponseWriter, r *http.Request) {
	if !s.requireStripe(w) {
		return
	}
	keyID := keyIDFrom(r.Context())
	var body struct {
		AmountUSD float64 `json:"amount_usd"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", "could not parse body")
		return
	}
	cents := int64(body.AmountUSD*100 + 0.5)
	if cents < 50 || cents > 50_000_000 {
		writeError(w, http.StatusBadRequest, "bad_amount", "amount_usd must be between 0.50 and 500000")
		return
	}
	base := s.cfg.PublicBaseURL
	sess, err := s.stripe.V1CheckoutSessions.Create(r.Context(), &stripe.CheckoutSessionCreateParams{
		Mode:              stripe.String("payment"),
		ClientReferenceID: stripe.String(keyID),
		SuccessURL:        stripe.String(base + "/billing/?topup=success"),
		CancelURL:         stripe.String(base + "/billing/?topup=cancel"),
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{{
			Quantity: stripe.Int64(1),
			PriceData: &stripe.CheckoutSessionCreateLineItemPriceDataParams{
				Currency:   stripe.String("usd"),
				UnitAmount: stripe.Int64(cents),
				ProductData: &stripe.CheckoutSessionCreateLineItemPriceDataProductDataParams{
					Name: stripe.String("Ayni inference credits"),
				},
			},
		}},
		Metadata: map[string]string{
			"key_id":        keyID,
			"credit_micros": strconv.FormatInt(cents*10_000, 10), // cents -> micro-USD
		},
	})
	if err != nil {
		s.log.Warn("stripe checkout create failed", "err", err)
		writeError(w, http.StatusBadGateway, "stripe_error", "could not create checkout session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": sess.URL, "session_id": sess.ID})
}

// handleStripeWebhook credits a balance when a Checkout payment completes. This is
// a v1 *snapshot* webhook (full event body), verified by the endpoint signing
// secret. IgnoreAPIVersionMismatch: endpoints are often pinned to an older
// api_version than the SDK expects; without this every event is rejected.
func (s *Server) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	if s.cfg.StripeWebhookSecret == "" {
		writeError(w, http.StatusNotImplemented, "webhook_not_configured", "no signing secret set")
		return
	}
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 128*1024))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_failed", "could not read body")
		return
	}
	event, err := webhook.ConstructEventWithOptions(payload, r.Header.Get("Stripe-Signature"),
		s.cfg.StripeWebhookSecret, webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true})
	if err != nil {
		s.log.Warn("stripe webhook rejected", "err", err,
			"sig_present", r.Header.Get("Stripe-Signature") != "")
		writeError(w, http.StatusBadRequest, "bad_signature", "signature verification failed")
		return
	}
	switch event.Type {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded":
		var cs stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &cs); err != nil {
			writeError(w, http.StatusBadRequest, "bad_event", "could not parse session")
			return
		}
		if cs.PaymentStatus != stripe.CheckoutSessionPaymentStatusPaid {
			writeJSON(w, http.StatusOK, map[string]any{"ignored": "not paid"})
			return
		}
		keyID := cs.ClientReferenceID
		if keyID == "" {
			keyID = cs.Metadata["key_id"]
		}
		micros, _ := strconv.ParseInt(cs.Metadata["credit_micros"], 10, 64)
		if micros == 0 {
			micros = cs.AmountTotal * 10_000 // cents -> micro-USD
		}
		if keyID == "" || micros <= 0 {
			writeJSON(w, http.StatusOK, map[string]any{"ignored": "missing key_id/amount"})
			return
		}
		// Idempotent on the session id.
		if err := s.store.AddCredit(context.WithoutCancel(r.Context()), keyID, micros, "topup", cs.ID, ""); err != nil {
			s.log.Warn("credit topup failed", "err", err)
			writeError(w, http.StatusInternalServerError, "credit_failed", "could not apply credit")
			return
		}
		s.log.Info("credits topped up", "key_id", keyID, "usd", float64(micros)/1e6)
	}
	writeJSON(w, http.StatusOK, map[string]any{"received": true})
}

// --- provider payout accounts: V2 recipient accounts keyed by the provider's
//     X25519 static identity (static_pk) ---

type connectBody struct {
	StaticPK string `json:"static_pk"`
	Email    string `json:"email"` // required by the V2 recipient configuration
}

// createV2RecipientAccount mirrors internal/connectdemo: platform owns pricing and
// fees; the connected account only needs to *receive* transfers. A contact email
// is mandatory when a recipient configuration is supplied.
func (s *Server) createV2RecipientAccount(ctx context.Context, label, email string) (string, error) {
	acct, err := s.stripe.V2CoreAccounts.Create(ctx, &stripe.V2CoreAccountCreateParams{
		DisplayName:  stripe.String(label),
		ContactEmail: stripe.String(email),
		Dashboard:    stripe.String("express"),
		Identity: &stripe.V2CoreAccountCreateIdentityParams{
			Country: stripe.String("us"), // PLACEHOLDER: collect the real country
		},
		Defaults: &stripe.V2CoreAccountCreateDefaultsParams{
			Responsibilities: &stripe.V2CoreAccountCreateDefaultsResponsibilitiesParams{
				FeesCollector:   stripe.String("application"),
				LossesCollector: stripe.String("application"),
			},
		},
		Configuration: &stripe.V2CoreAccountCreateConfigurationParams{
			Recipient: &stripe.V2CoreAccountCreateConfigurationRecipientParams{
				Capabilities: &stripe.V2CoreAccountCreateConfigurationRecipientCapabilitiesParams{
					StripeBalance: &stripe.V2CoreAccountCreateConfigurationRecipientCapabilitiesStripeBalanceParams{
						StripeTransfers: &stripe.V2CoreAccountCreateConfigurationRecipientCapabilitiesStripeBalanceStripeTransfersParams{
							Requested: stripe.Bool(true),
						},
					},
				},
			},
		},
	})
	if err != nil {
		return "", err
	}
	return acct.ID, nil
}

func (s *Server) v2OnboardingLink(ctx context.Context, acctID string) (string, error) {
	base := s.cfg.PublicBaseURL
	al, err := s.stripe.V2CoreAccountLinks.Create(ctx, &stripe.V2CoreAccountLinkCreateParams{
		Account: stripe.String(acctID),
		UseCase: &stripe.V2CoreAccountLinkCreateUseCaseParams{
			Type: stripe.String("account_onboarding"),
			AccountOnboarding: &stripe.V2CoreAccountLinkCreateUseCaseAccountOnboardingParams{
				Configurations: []*string{stripe.String("recipient")},
				RefreshURL:     stripe.String(base + "/connect/onboard/refresh?accountId=" + acctID),
				ReturnURL:      stripe.String(base + "/connect/onboard/return?accountId=" + acctID),
			},
		},
	})
	if err != nil {
		return "", err
	}
	return al.URL, nil
}

// v2TransfersActive reports whether the account's stripe_transfers capability is
// live, plus the requirements status. Always read fresh from the API.
func (s *Server) v2AccountReady(ctx context.Context, acctID string) (ready bool, requirements string, err error) {
	acct, err := s.stripe.V2CoreAccounts.Retrieve(ctx, acctID, &stripe.V2CoreAccountRetrieveParams{
		Include: []*string{
			stripe.String("configuration.recipient"),
			stripe.String("requirements"),
		},
	})
	if err != nil {
		return false, "", err
	}
	if c := acct.Configuration; c != nil && c.Recipient != nil && c.Recipient.Capabilities != nil &&
		c.Recipient.Capabilities.StripeBalance != nil && c.Recipient.Capabilities.StripeBalance.StripeTransfers != nil {
		ready = string(c.Recipient.Capabilities.StripeBalance.StripeTransfers.Status) == "active"
	}
	if acct.Requirements != nil && acct.Requirements.Summary != nil && acct.Requirements.Summary.MinimumDeadline != nil {
		requirements = string(acct.Requirements.Summary.MinimumDeadline.Status)
	}
	return ready, requirements, nil
}

func (s *Server) adminPayoutConnect(w http.ResponseWriter, r *http.Request) {
	if !s.requireStripe(w) {
		return
	}
	var b connectBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b); err != nil ||
		strings.TrimSpace(b.StaticPK) == "" || !strings.Contains(b.Email, "@") {
		writeError(w, http.StatusBadRequest, "bad_request", "static_pk and a contact email are required")
		return
	}

	existing, ok, _ := s.store.GetPayoutAccount(r.Context(), b.StaticPK)
	acctID := ""
	if ok && existing.StripeAccount != "" {
		acctID = existing.StripeAccount
	} else {
		id, err := s.createV2RecipientAccount(r.Context(), "Ayni provider "+shortPK(b.StaticPK), b.Email)
		if err != nil {
			s.log.Warn("stripe v2 account create failed", "err", err)
			writeError(w, http.StatusBadGateway, "stripe_error", "could not create connect account: "+err.Error())
			return
		}
		acctID = id
		_ = s.store.UpsertPayoutAccount(r.Context(), store.PayoutAccount{
			StaticPK: b.StaticPK, StripeAccount: acctID, Status: "pending",
		})
	}

	link, err := s.v2OnboardingLink(r.Context(), acctID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "stripe_error", "could not create onboarding link: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account_id": acctID, "onboarding_url": link})
}

func (s *Server) adminPayoutRefresh(w http.ResponseWriter, r *http.Request) {
	if !s.requireStripe(w) {
		return
	}
	var b connectBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b); err != nil || b.StaticPK == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "static_pk required")
		return
	}
	pa, ok, _ := s.store.GetPayoutAccount(r.Context(), b.StaticPK)
	if !ok {
		writeError(w, http.StatusNotFound, "no_account", "no connect account for that identity")
		return
	}
	ready, reqs, err := s.v2AccountReady(r.Context(), pa.StripeAccount)
	if err != nil {
		writeError(w, http.StatusBadGateway, "stripe_error", "could not fetch account: "+err.Error())
		return
	}
	pa.Status = "pending"
	if ready {
		pa.Status = "enabled"
	}
	_ = s.store.UpsertPayoutAccount(r.Context(), pa)
	writeJSON(w, http.StatusOK, map[string]any{
		"account_id": pa.StripeAccount, "status": pa.Status,
		"ready_to_receive": ready, "requirements": reqs,
	})
}

func (s *Server) adminPayoutsPending(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.AccruedByProvider(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_failed", "could not read accruals")
		return
	}
	type line struct {
		StaticPK string  `json:"static_pk"`
		Jobs     int     `json:"jobs"`
		OwedUSD  float64 `json:"owed_usd"`
		Account  string  `json:"stripe_account"`
		Status   string  `json:"status"`
		Payable  bool    `json:"payable"`
	}
	var out []line
	for _, a := range rows {
		pa, ok, _ := s.store.GetPayoutAccount(r.Context(), a.StaticPK)
		l := line{StaticPK: a.StaticPK, Jobs: a.Jobs, OwedUSD: float64(a.OwedMicros) / 1e6}
		if ok {
			l.Account, l.Status = pa.StripeAccount, pa.Status
			l.Payable = pa.Status == "enabled"
		}
		out = append(out, l)
	}
	writeJSON(w, http.StatusOK, map[string]any{"pending": out})
}

// adminPayoutsRun pays providers their accrued share via Stripe Transfers
// (separate charges & transfers: the money is already in the platform balance
// from consumer credit top-ups). Dry-run by default; ?commit=1 moves money.
func (s *Server) adminPayoutsRun(w http.ResponseWriter, r *http.Request) {
	if !s.requireStripe(w) {
		return
	}
	commit := r.URL.Query().Get("commit") == "1"
	minUSD := 0.0
	if v := r.URL.Query().Get("min_usd"); v != "" {
		minUSD, _ = strconv.ParseFloat(v, 64)
	}
	minMicros := int64(minUSD * 1_000_000)

	rows, err := s.store.AccruedByProvider(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_failed", "could not read accruals")
		return
	}
	type result struct {
		StaticPK  string  `json:"static_pk"`
		AmountUSD float64 `json:"amount_usd"`
		Account   string  `json:"stripe_account"`
		Transfer  string  `json:"stripe_transfer,omitempty"`
		Skipped   string  `json:"skipped,omitempty"`
	}
	var results []result
	var totalMicros int64
	for _, a := range rows {
		res := result{StaticPK: a.StaticPK, AmountUSD: float64(a.OwedMicros) / 1e6}
		if a.OwedMicros < minMicros {
			res.Skipped = "below min"
			results = append(results, res)
			continue
		}
		pa, ok, _ := s.store.GetPayoutAccount(r.Context(), a.StaticPK)
		if !ok || pa.Status != "enabled" {
			res.Skipped = "no enabled payout account"
			results = append(results, res)
			continue
		}
		res.Account = pa.StripeAccount
		if !commit {
			results = append(results, res)
			totalMicros += a.OwedMicros
			continue
		}
		cents := a.OwedMicros / 10_000
		tr, terr := s.stripe.V1Transfers.Create(context.WithoutCancel(r.Context()), &stripe.TransferCreateParams{
			Amount:      stripe.Int64(cents),
			Currency:    stripe.String("usd"),
			Destination: stripe.String(pa.StripeAccount),
			Description: stripe.String("Ayni provider payout"),
		})
		if terr != nil {
			s.log.Warn("stripe transfer failed", "err", terr, "acct", pa.StripeAccount)
			_, _ = s.store.RecordPayoutAndSettle(context.WithoutCancel(r.Context()), store.PayoutRecord{
				StaticPK: a.StaticPK, StripeAccount: pa.StripeAccount,
				AmountMicros: a.OwedMicros, State: "failed",
			})
			res.Skipped = "transfer failed: " + terr.Error()
			results = append(results, res)
			continue
		}
		_, _ = s.store.RecordPayoutAndSettle(context.WithoutCancel(r.Context()), store.PayoutRecord{
			StaticPK: a.StaticPK, StripeAccount: pa.StripeAccount,
			AmountMicros: a.OwedMicros, StripeTransfer: tr.ID, State: "created",
		})
		res.Transfer = tr.ID
		results = append(results, res)
		totalMicros += a.OwedMicros
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"dry_run":   !commit,
		"min_usd":   minUSD,
		"count":     len(results),
		"total_usd": float64(totalMicros) / 1e6,
		"results":   results,
		"note":      fmt.Sprintf("pass ?commit=1 to move money (%d transfers)", len(results)),
	})
}

func shortPK(pk string) string {
	if len(pk) > 8 {
		return pk[:8]
	}
	return pk
}
