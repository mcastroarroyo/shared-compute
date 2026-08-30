package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/store"
	stripe "github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/account"
	"github.com/stripe/stripe-go/v81/accountlink"
	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/stripe/stripe-go/v81/transfer"
	"github.com/stripe/stripe-go/v81/webhook"
)

var stripeOnce sync.Once

func (s *Server) stripeReady() bool {
	if s.cfg.StripeSecretKey == "" {
		return false
	}
	stripeOnce.Do(func() { stripe.Key = s.cfg.StripeSecretKey })
	return true
}

func (s *Server) requireStripe(w http.ResponseWriter) bool {
	if !s.stripeReady() {
		writeError(w, http.StatusNotImplemented, "billing_not_configured",
			"Stripe is not configured on this coordinator")
		return false
	}
	return true
}

// --- consumer: balance + top-up ---

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
	p := &stripe.CheckoutSessionParams{
		Mode:              stripe.String(string(stripe.CheckoutSessionModePayment)),
		ClientReferenceID: stripe.String(keyID),
		SuccessURL:        stripe.String(base + "/billing/?topup=success"),
		CancelURL:         stripe.String(base + "/billing/?topup=cancel"),
		LineItems: []*stripe.CheckoutSessionLineItemParams{{
			Quantity: stripe.Int64(1),
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				Currency:   stripe.String("usd"),
				UnitAmount: stripe.Int64(cents),
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name: stripe.String("Ayni inference credits"),
				},
			},
		}},
	}
	p.AddMetadata("key_id", keyID)
	p.AddMetadata("credit_micros", strconv.FormatInt(cents*10_000, 10)) // cents -> micro-USD
	sess, err := session.New(p)
	if err != nil {
		s.log.Warn("stripe checkout create failed", "err", err)
		writeError(w, http.StatusBadGateway, "stripe_error", "could not create checkout session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": sess.URL, "session_id": sess.ID})
}

// handleStripeWebhook credits a balance when a Checkout payment completes. Public,
// verified by the endpoint signing secret.
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
		sec := s.cfg.StripeWebhookSecret
		if len(sec) > 12 {
			sec = sec[:12] + "…"
		}
		s.log.Warn("stripe webhook rejected", "err", err, "secret_prefix", sec,
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
		if err := s.store.AddCredit(context.WithoutCancel(r.Context()), keyID, micros, "topup", cs.ID, ""); err != nil {
			s.log.Warn("credit topup failed", "err", err)
			writeError(w, http.StatusInternalServerError, "credit_failed", "could not apply credit")
			return
		}
		s.log.Info("credits topped up", "key_id", keyID, "usd", float64(micros)/1e6)
	}
	writeJSON(w, http.StatusOK, map[string]any{"received": true})
}

// --- admin: provider payout wiring ---

type connectBody struct {
	StaticPK string `json:"static_pk"`
}

func (s *Server) adminPayoutConnect(w http.ResponseWriter, r *http.Request) {
	if !s.requireStripe(w) {
		return
	}
	var b connectBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b); err != nil || strings.TrimSpace(b.StaticPK) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "static_pk required")
		return
	}
	existing, ok, _ := s.store.GetPayoutAccount(r.Context(), b.StaticPK)
	var acctID string
	if ok && existing.StripeAccount != "" {
		acctID = existing.StripeAccount
	} else {
		acct, err := account.New(&stripe.AccountParams{
			Type: stripe.String(string(stripe.AccountTypeExpress)),
			Capabilities: &stripe.AccountCapabilitiesParams{
				Transfers: &stripe.AccountCapabilitiesTransfersParams{Requested: stripe.Bool(true)},
			},
		})
		if err != nil {
			s.log.Warn("stripe account create failed", "err", err)
			writeError(w, http.StatusBadGateway, "stripe_error", "could not create connect account")
			return
		}
		acctID = acct.ID
		_ = s.store.UpsertPayoutAccount(r.Context(), store.PayoutAccount{
			StaticPK: b.StaticPK, StripeAccount: acctID, Status: "pending",
		})
	}
	base := s.cfg.PublicBaseURL
	link, err := accountlink.New(&stripe.AccountLinkParams{
		Account:    stripe.String(acctID),
		RefreshURL: stripe.String(base + "/billing/?connect=refresh"),
		ReturnURL:  stripe.String(base + "/billing/?connect=done"),
		Type:       stripe.String("account_onboarding"),
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "stripe_error", "could not create onboarding link")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account_id": acctID, "onboarding_url": link.URL})
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
	acct, err := account.GetByID(pa.StripeAccount, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, "stripe_error", "could not fetch account")
		return
	}
	status := "pending"
	if acct.PayoutsEnabled {
		status = "enabled"
	}
	pa.Status = status
	_ = s.store.UpsertPayoutAccount(r.Context(), pa)
	writeJSON(w, http.StatusOK, map[string]any{
		"account_id": pa.StripeAccount, "status": status,
		"payouts_enabled": acct.PayoutsEnabled, "charges_enabled": acct.ChargesEnabled,
		"details_submitted": acct.DetailsSubmitted,
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
		tr, err := transfer.New(&stripe.TransferParams{
			Amount:      stripe.Int64(cents),
			Currency:    stripe.String("usd"),
			Destination: stripe.String(pa.StripeAccount),
		})
		if err != nil {
			s.log.Warn("stripe transfer failed", "err", err, "acct", pa.StripeAccount)
			_, _ = s.store.RecordPayoutAndSettle(context.WithoutCancel(r.Context()), store.PayoutRecord{
				StaticPK: a.StaticPK, StripeAccount: pa.StripeAccount,
				AmountMicros: a.OwedMicros, State: "failed",
			})
			res.Skipped = "transfer failed: " + err.Error()
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
