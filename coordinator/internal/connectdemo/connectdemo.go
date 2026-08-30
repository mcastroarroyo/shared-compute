// Package connectdemo is a self-contained sample Stripe Connect integration:
// it onboards connected accounts, creates platform-level products mapped to those
// accounts, shows a storefront, and processes purchases as destination charges
// with an application fee.
//
// It is mounted at /connect/* on the coordinator when SC_STRIPE_SECRET_KEY is set.
// Everything here uses the modern Stripe *Client* (stripe.NewClient) and the V2
// Accounts API. The Stripe API version is chosen automatically by the SDK, so it
// is never set explicitly.
//
// Environment:
//
//	SC_STRIPE_SECRET_KEY            required — "sk_test_…" / "sk_live_…"
//	SC_STRIPE_CONNECT_WEBHOOK_SECRET optional — "whsec_…" for the /connect/webhook
//	                                 thin-event endpoint (see setup notes below)
//	SC_PUBLIC_BASE_URL             optional — external origin for redirect URLs
//	                                 (defaults to https://ayni-ai.com)
//
// To receive requirement/capability updates locally, run the Stripe CLI:
//
//	stripe listen \
//	  --thin-events 'v2.core.account[requirements].updated,v2.core.account[.recipient].capability_status_updated' \
//	  --forward-thin-to localhost:8080/connect/webhook
//
// In the Dashboard: Developers → Webhooks → Add destination → Events from
// "Connected accounts" → Payload style "Thin" → subscribe to
// v2.core.account[requirements].updated and
// v2.core.account[configuration.recipient].capability_status_updated.
package connectdemo

import (
	"context"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	stripe "github.com/stripe/stripe-go/v86"
)

// Handler holds the Stripe client and the demo's tiny in-memory state.
type Handler struct {
	sc            *stripe.Client
	webhookSecret string // SC_STRIPE_CONNECT_WEBHOOK_SECRET
	baseURL       string // SC_PUBLIC_BASE_URL
	log           *slog.Logger

	// DEMO ONLY: a user↔account mapping. In a real app this belongs in your
	// database, keyed by your own user id. Account *status* is always read live
	// from the Stripe API (see accountStatus), never from here.
	mu       sync.RWMutex
	accounts map[string]string // email -> connected account id
}

// New builds the handler. Returns (nil, error) with a helpful message if the
// Stripe secret key is missing, so the caller can decide whether to mount it.
func New(secretKey, webhookSecret, baseURL string, log *slog.Logger) (*Handler, error) {
	if secretKey == "" {
		// PLACEHOLDER: set SC_STRIPE_SECRET_KEY to your Stripe secret key.
		return nil, fmt.Errorf("connectdemo: SC_STRIPE_SECRET_KEY is not set — " +
			"get one at https://dashboard.stripe.com/apikeys (use a sk_test_… key first)")
	}
	if !strings.HasPrefix(secretKey, "sk_") && !strings.HasPrefix(secretKey, "rk_") {
		return nil, fmt.Errorf("connectdemo: SC_STRIPE_SECRET_KEY does not look like a Stripe secret key")
	}
	if baseURL == "" {
		baseURL = "https://ayni-ai.com"
	}
	return &Handler{
		// Create ONE Stripe Client and use it for every request.
		sc:            stripe.NewClient(secretKey),
		webhookSecret: webhookSecret,
		baseURL:       strings.TrimRight(baseURL, "/"),
		log:           log,
		accounts:      map[string]string{},
	}, nil
}

// Mount registers the demo routes on mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /connect/", h.home)
	mux.HandleFunc("GET /connect", h.home)
	mux.HandleFunc("POST /connect/accounts", h.createAccount)
	mux.HandleFunc("POST /connect/onboard", h.onboard)
	mux.HandleFunc("GET /connect/onboard/refresh", h.onboardRefresh)
	mux.HandleFunc("GET /connect/onboard/return", h.onboardReturn)
	mux.HandleFunc("GET /connect/status", h.status)
	mux.HandleFunc("GET /connect/products", h.productsPage)
	mux.HandleFunc("POST /connect/products", h.createProduct)
	mux.HandleFunc("GET /connect/store", h.storefront)
	mux.HandleFunc("POST /connect/buy", h.buy)
	mux.HandleFunc("GET /connect/success", h.success)
	mux.HandleFunc("POST /connect/webhook", h.webhook)
}

// ---------------------------------------------------------------------------
// 1. Create a connected account (V2 Accounts API)
// ---------------------------------------------------------------------------

func (h *Handler) createAccount(w http.ResponseWriter, r *http.Request) {
	displayName := strings.TrimSpace(r.FormValue("display_name"))
	email := strings.TrimSpace(r.FormValue("contact_email"))
	if displayName == "" || email == "" {
		h.render(w, http.StatusBadRequest, page("Create account", `<p class="err">display_name and contact_email are required.</p><p><a href="/connect/">back</a></p>`))
		return
	}

	// The platform is responsible for pricing and fee collection, so we create a
	// RECIPIENT-configured account: the platform is the merchant of record and
	// collects fees/losses. Use ONLY the properties below. Never pass a top-level
	// `type` (no "express"/"standard"/"custom").
	acct, err := h.sc.V2CoreAccounts.Create(r.Context(), &stripe.V2CoreAccountCreateParams{
		DisplayName:  stripe.String(displayName),
		ContactEmail: stripe.String(email),
		// The connected user gets an Express dashboard.
		Dashboard: stripe.String("express"),
		Identity: &stripe.V2CoreAccountCreateIdentityParams{
			// PLACEHOLDER: hard-coded to the US for this demo. Collect the real
			// country from the user in production.
			Country: stripe.String("us"),
		},
		Defaults: &stripe.V2CoreAccountCreateDefaultsParams{
			Responsibilities: &stripe.V2CoreAccountCreateDefaultsResponsibilitiesParams{
				// Platform collects fees and owns losses.
				FeesCollector:   stripe.String("application"),
				LossesCollector: stripe.String("application"),
			},
		},
		Configuration: &stripe.V2CoreAccountCreateConfigurationParams{
			Recipient: &stripe.V2CoreAccountCreateConfigurationRecipientParams{
				Capabilities: &stripe.V2CoreAccountCreateConfigurationRecipientCapabilitiesParams{
					StripeBalance: &stripe.V2CoreAccountCreateConfigurationRecipientCapabilitiesStripeBalanceParams{
						// Request the ability to receive transfers into the
						// connected account's Stripe balance.
						StripeTransfers: &stripe.V2CoreAccountCreateConfigurationRecipientCapabilitiesStripeBalanceStripeTransfersParams{
							Requested: stripe.Bool(true),
						},
					},
				},
			},
		},
	})
	if err != nil {
		h.stripeErr(w, "create connected account", err)
		return
	}

	// DEMO storage of the user→account mapping. Put this in your DB in production.
	h.mu.Lock()
	h.accounts[email] = acct.ID
	h.mu.Unlock()
	h.log.Info("connectdemo: account created", "account", acct.ID, "email", email)

	http.Redirect(w, r, "/connect/?email="+urlq(email), http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// 2. Onboard the connected account (V2 Account Links)
// ---------------------------------------------------------------------------

func (h *Handler) onboard(w http.ResponseWriter, r *http.Request) {
	acctID := strings.TrimSpace(r.FormValue("account_id"))
	if acctID == "" {
		http.Error(w, "account_id required", http.StatusBadRequest)
		return
	}
	link, err := h.accountLink(r.Context(), acctID)
	if err != nil {
		h.stripeErr(w, "create account link", err)
		return
	}
	// Send the user to Stripe's hosted onboarding.
	http.Redirect(w, r, link, http.StatusSeeOther)
}

// onboardRefresh is the refresh_url target: the previous link expired or was
// already used, so mint a fresh one and bounce the user straight back into it.
func (h *Handler) onboardRefresh(w http.ResponseWriter, r *http.Request) {
	acctID := r.URL.Query().Get("accountId")
	link, err := h.accountLink(r.Context(), acctID)
	if err != nil {
		h.stripeErr(w, "refresh account link", err)
		return
	}
	http.Redirect(w, r, link, http.StatusSeeOther)
}

// onboardReturn is the return_url target: the user finished (or bailed out of)
// the hosted flow. We show the live status pulled from the API.
func (h *Handler) onboardReturn(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/connect/?accountId="+urlq(r.URL.Query().Get("accountId")), http.StatusSeeOther)
}

func (h *Handler) accountLink(ctx context.Context, acctID string) (string, error) {
	al, err := h.sc.V2CoreAccountLinks.Create(ctx, &stripe.V2CoreAccountLinkCreateParams{
		Account: stripe.String(acctID),
		UseCase: &stripe.V2CoreAccountLinkCreateUseCaseParams{
			Type: stripe.String("account_onboarding"),
			AccountOnboarding: &stripe.V2CoreAccountLinkCreateUseCaseAccountOnboardingParams{
				// Collect information for the "recipient" configuration.
				Configurations: []*string{stripe.String("recipient")},
				RefreshURL:     stripe.String(h.baseURL + "/connect/onboard/refresh?accountId=" + urlq(acctID)),
				ReturnURL:      stripe.String(h.baseURL + "/connect/onboard/return?accountId=" + urlq(acctID)),
			},
		},
	})
	if err != nil {
		return "", err
	}
	return al.URL, nil
}

// ---------------------------------------------------------------------------
// 3. Account status — always read live from the API (never a database)
// ---------------------------------------------------------------------------

type acctStatus struct {
	AccountID              string
	ReadyToReceivePayments bool
	OnboardingComplete     bool
	RequirementsStatus     string
}

func (h *Handler) accountStatus(ctx context.Context, acctID string) (acctStatus, error) {
	acct, err := h.sc.V2CoreAccounts.Retrieve(ctx, acctID, &stripe.V2CoreAccountRetrieveParams{
		// Ask Stripe to include the recipient configuration and the requirements
		// block in the response.
		Include: []*string{
			stripe.String("configuration.recipient"),
			stripe.String("requirements"),
		},
	})
	if err != nil {
		return acctStatus{}, err
	}

	s := acctStatus{AccountID: acctID}

	// The stripe_transfers capability is "active" once the account can receive money.
	if c := acct.Configuration; c != nil && c.Recipient != nil && c.Recipient.Capabilities != nil &&
		c.Recipient.Capabilities.StripeBalance != nil && c.Recipient.Capabilities.StripeBalance.StripeTransfers != nil {
		s.ReadyToReceivePayments = string(c.Recipient.Capabilities.StripeBalance.StripeTransfers.Status) == "active"
	}

	// Onboarding is "complete" when nothing is currently_due or past_due.
	if acct.Requirements != nil && acct.Requirements.Summary != nil && acct.Requirements.Summary.MinimumDeadline != nil {
		s.RequirementsStatus = string(acct.Requirements.Summary.MinimumDeadline.Status)
	}
	s.OnboardingComplete = s.RequirementsStatus != "currently_due" && s.RequirementsStatus != "past_due"
	return s, nil
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	acctID := r.URL.Query().Get("account_id")
	if acctID == "" {
		http.Error(w, "account_id required", http.StatusBadRequest)
		return
	}
	s, err := h.accountStatus(r.Context(), acctID)
	if err != nil {
		h.stripeErr(w, "retrieve account", err)
		return
	}
	h.render(w, http.StatusOK, page("Account status", statusBlock(s)))
}

func statusBlock(s acctStatus) string {
	pill := func(ok bool) string {
		if ok {
			return `<span class="pill good">yes</span>`
		}
		return `<span class="pill bad">no</span>`
	}
	return fmt.Sprintf(`<div class="panel">
  <p>Account <code>%s</code></p>
  <p>Ready to receive payments: %s</p>
  <p>Onboarding complete: %s <span class="muted">(requirements: %s)</span></p>
</div>`, html.EscapeString(s.AccountID), pill(s.ReadyToReceivePayments),
		pill(s.OnboardingComplete), html.EscapeString(orDash(s.RequirementsStatus)))
}

// ---------------------------------------------------------------------------
// 4. Create products — at the PLATFORM level, mapped to a connected account
// ---------------------------------------------------------------------------

func (h *Handler) createProduct(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	desc := strings.TrimSpace(r.FormValue("description"))
	acctID := strings.TrimSpace(r.FormValue("account_id"))
	dollars, _ := strconv.ParseFloat(strings.TrimSpace(r.FormValue("price_usd")), 64)
	cents := int64(dollars*100 + 0.5)
	if name == "" || acctID == "" || cents <= 0 {
		h.render(w, http.StatusBadRequest, page("Create product",
			`<p class="err">name, account_id and a positive price_usd are required.</p><p><a href="/connect/products">back</a></p>`))
		return
	}

	// Created on the PLATFORM account (not on the connected account). The
	// product→account mapping lives in metadata so the storefront knows where
	// to route the money. You could also store this mapping in your DB.
	p, err := h.sc.V1Products.Create(r.Context(), &stripe.ProductCreateParams{
		Name:        stripe.String(name),
		Description: stripe.String(desc),
		DefaultPriceData: &stripe.ProductCreateDefaultPriceDataParams{
			UnitAmount: stripe.Int64(cents),
			Currency:   stripe.String("usd"),
		},
		Metadata: map[string]string{"connected_account_id": acctID},
	})
	if err != nil {
		h.stripeErr(w, "create product", err)
		return
	}
	h.log.Info("connectdemo: product created", "product", p.ID, "account", acctID, "cents", cents)
	http.Redirect(w, r, "/connect/products", http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// 5. Storefront + destination charge with application fee
// ---------------------------------------------------------------------------

type storeItem struct {
	ProductID string
	Name      string
	Desc      string
	Cents     int64
	Currency  string
	AccountID string
}

func (h *Handler) listItems(ctx context.Context) ([]storeItem, error) {
	var items []storeItem
	list := h.sc.V1Products.List(ctx, &stripe.ProductListParams{
		Active: stripe.Bool(true),
	})
	for p, err := range list.All(ctx) {
		if err != nil {
			return items, err
		}
		acct := p.Metadata["connected_account_id"]
		if acct == "" {
			continue // not one of our marketplace products
		}
		it := storeItem{
			ProductID: p.ID, Name: p.Name, Desc: p.Description, AccountID: acct,
			Currency: "usd",
		}
		if p.DefaultPrice != nil {
			it.Cents = p.DefaultPrice.UnitAmount
			if p.DefaultPrice.Currency != "" {
				it.Currency = string(p.DefaultPrice.Currency)
			}
		}
		items = append(items, it)
	}
	return items, nil
}

func (h *Handler) buy(w http.ResponseWriter, r *http.Request) {
	productID := strings.TrimSpace(r.FormValue("product_id"))
	if productID == "" {
		http.Error(w, "product_id required", http.StatusBadRequest)
		return
	}

	// Look the product up so we know the price and the destination account.
	prod, err := h.sc.V1Products.Retrieve(r.Context(), productID, &stripe.ProductRetrieveParams{
		Expand: []*string{stripe.String("default_price")},
	})
	if err != nil {
		h.stripeErr(w, "retrieve product", err)
		return
	}
	acctID := prod.Metadata["connected_account_id"]
	if acctID == "" || prod.DefaultPrice == nil {
		http.Error(w, "product is not purchasable (missing account or price)", http.StatusBadRequest)
		return
	}
	cents := prod.DefaultPrice.UnitAmount
	currency := string(prod.DefaultPrice.Currency)

	// The platform takes a 10% + 30¢ application fee; the rest is transferred to
	// the connected account. This is a "destination charge": the platform is the
	// merchant of record and Stripe moves the remainder to `destination`.
	fee := cents/10 + 30

	sess, err := h.sc.V1CheckoutSessions.Create(r.Context(), &stripe.CheckoutSessionCreateParams{
		Mode: stripe.String("payment"),
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{{
			Quantity: stripe.Int64(1),
			PriceData: &stripe.CheckoutSessionCreateLineItemPriceDataParams{
				Currency:   stripe.String(currency),
				UnitAmount: stripe.Int64(cents),
				ProductData: &stripe.CheckoutSessionCreateLineItemPriceDataProductDataParams{
					Name: stripe.String(prod.Name),
				},
			},
		}},
		PaymentIntentData: &stripe.CheckoutSessionCreatePaymentIntentDataParams{
			ApplicationFeeAmount: stripe.Int64(fee),
			TransferData: &stripe.CheckoutSessionCreatePaymentIntentDataTransferDataParams{
				Destination: stripe.String(acctID),
			},
		},
		SuccessURL: stripe.String(h.baseURL + "/connect/success?session_id={CHECKOUT_SESSION_ID}"),
		CancelURL:  stripe.String(h.baseURL + "/connect/store"),
	})
	if err != nil {
		h.stripeErr(w, "create checkout session", err)
		return
	}
	http.Redirect(w, r, sess.URL, http.StatusSeeOther)
}

func (h *Handler) success(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("session_id")
	body := `<div class="panel"><p class="good">Payment complete.</p>`
	if id != "" {
		if s, err := h.sc.V1CheckoutSessions.Retrieve(r.Context(), id, nil); err == nil {
			body += fmt.Sprintf(`<p class="muted">Session <code>%s</code> · %s · total %s %d</p>`,
				html.EscapeString(s.ID), html.EscapeString(string(s.PaymentStatus)),
				html.EscapeString(string(s.Currency)), s.AmountTotal)
		}
	}
	body += `</div><p><a href="/connect/store">back to storefront</a></p>`
	h.render(w, http.StatusOK, page("Success", body))
}

// ---------------------------------------------------------------------------
// 6. Thin-event webhook: requirement / capability changes on connected accounts
// ---------------------------------------------------------------------------

func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) {
	if h.webhookSecret == "" {
		// PLACEHOLDER: set SC_STRIPE_CONNECT_WEBHOOK_SECRET (whsec_…) from the
		// Dashboard destination or `stripe listen`.
		http.Error(w, "SC_STRIPE_CONNECT_WEBHOOK_SECRET not set", http.StatusNotImplemented)
		return
	}
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256*1024))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	// Thin events carry only an id + type; verify the signature, then fetch the
	// full event (and any related object) from the API.
	container, err := h.sc.ParseEventNotification(payload, r.Header.Get("Stripe-Signature"), h.webhookSecret)
	if err != nil {
		h.log.Warn("connectdemo: thin event rejected", "err", err)
		http.Error(w, "signature verification failed", http.StatusBadRequest)
		return
	}
	notif := container.GetEventNotification() // *stripe.V2CoreEventNotification (id, type, context)
	acctID := relatedAccountID(container)

	switch notif.Type {
	case "v2.core.account[requirements].updated":
		// Requirements changed (often due to regulators / card networks). Re-pull
		// the account and collect whatever is now due.
		h.handleRequirementsUpdated(r.Context(), acctID, notif.ID)

	case "v2.core.account[configuration.recipient].capability_status_updated":
		// A capability (e.g. stripe_transfers) flipped active/pending/restricted.
		h.handleCapabilityUpdated(r.Context(), acctID, notif.ID)

	default:
		h.log.Info("connectdemo: unhandled thin event", "type", notif.Type, "id", notif.ID)
	}

	// Always 2xx quickly so Stripe doesn't retry a handled event.
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"received":true}`))
}

func (h *Handler) handleRequirementsUpdated(ctx context.Context, acctID, eventID string) {
	if acctID == "" {
		h.log.Warn("connectdemo: requirements.updated without account id", "event", eventID)
		return
	}
	s, err := h.accountStatus(ctx, acctID)
	if err != nil {
		h.log.Warn("connectdemo: could not refresh account after requirements.updated", "err", err)
		return
	}
	// In a real app: notify the connected user, and if s.OnboardingComplete is
	// now false, send them a fresh onboarding link (accountLink) to collect the
	// newly-required fields.
	h.log.Info("connectdemo: requirements updated",
		"account", acctID, "requirements", s.RequirementsStatus,
		"onboarding_complete", s.OnboardingComplete, "ready", s.ReadyToReceivePayments)
}

func (h *Handler) handleCapabilityUpdated(ctx context.Context, acctID, eventID string) {
	if acctID == "" {
		h.log.Warn("connectdemo: capability update without account id", "event", eventID)
		return
	}
	s, err := h.accountStatus(ctx, acctID)
	if err != nil {
		h.log.Warn("connectdemo: could not refresh account after capability update", "err", err)
		return
	}
	h.log.Info("connectdemo: capability status updated",
		"account", acctID, "ready_to_receive", s.ReadyToReceivePayments)
}

// relatedAccountID digs the connected-account id out of a thin event. Thin events
// only carry an id/type/context, so we use the related object (populated on an
// UnknownEventNotification) or the authentication context's path segments.
func relatedAccountID(c stripe.EventNotificationContainer) string {
	if u, ok := c.(*stripe.UnknownEventNotification); ok && u.RelatedObject != nil &&
		strings.HasPrefix(u.RelatedObject.ID, "acct_") {
		return u.RelatedObject.ID
	}
	n := c.GetEventNotification()
	if n != nil && n.Context != nil {
		for _, seg := range n.Context.Segments {
			if strings.HasPrefix(seg, "acct_") {
				return seg
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Pages
// ---------------------------------------------------------------------------

func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	acctID := r.URL.Query().Get("accountId")

	h.mu.RLock()
	if acctID == "" && email != "" {
		acctID = h.accounts[email]
	}
	known := make([][2]string, 0, len(h.accounts))
	for e, a := range h.accounts {
		known = append(known, [2]string{e, a})
	}
	h.mu.RUnlock()

	var b strings.Builder
	b.WriteString(`<h2>1 · Onboard a connected account</h2>`)
	b.WriteString(`<div class="panel"><form method="post" action="/connect/accounts">
    <label>Display name<br><input name="display_name" required></label><br><br>
    <label>Contact email<br><input name="contact_email" type="email" required></label><br><br>
    <button type="submit">Create connected account</button>
  </form></div>`)

	if acctID != "" {
		s, err := h.accountStatus(r.Context(), acctID)
		if err != nil {
			b.WriteString(`<p class="err">` + html.EscapeString(err.Error()) + `</p>`)
		} else {
			b.WriteString(statusBlock(s))
			b.WriteString(`<form method="post" action="/connect/onboard">
      <input type="hidden" name="account_id" value="` + html.EscapeString(acctID) + `">
      <button type="submit">Onboard to collect payments</button>
    </form>`)
		}
	}

	if len(known) > 0 {
		b.WriteString(`<h2>Known accounts (demo)</h2><div class="panel"><table><tr><th>Email</th><th>Account</th><th></th></tr>`)
		for _, kv := range known {
			b.WriteString(fmt.Sprintf(`<tr><td>%s</td><td><code>%s</code></td><td><a href="/connect/?accountId=%s">status</a></td></tr>`,
				html.EscapeString(kv[0]), html.EscapeString(kv[1]), urlq(kv[1])))
		}
		b.WriteString(`</table></div>`)
	}

	b.WriteString(`<h2>Next</h2><div class="panel"><a href="/connect/products">Create products →</a> &nbsp; <a href="/connect/store">Open the storefront →</a></div>`)
	h.render(w, http.StatusOK, page("Stripe Connect demo", b.String()))
}

func (h *Handler) productsPage(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	opts := make([]string, 0, len(h.accounts))
	for e, a := range h.accounts {
		opts = append(opts, fmt.Sprintf(`<option value="%s">%s — %s</option>`,
			html.EscapeString(a), html.EscapeString(e), html.EscapeString(a)))
	}
	h.mu.RUnlock()

	sel := `<input name="account_id" placeholder="acct_… (create an account first)" required>`
	if len(opts) > 0 {
		sel = `<select name="account_id" required>` + strings.Join(opts, "") + `</select>`
	}

	items, err := h.listItems(r.Context())
	rows := ""
	if err != nil {
		rows = `<tr><td colspan="4" class="err">` + html.EscapeString(err.Error()) + `</td></tr>`
	}
	for _, it := range items {
		rows += fmt.Sprintf(`<tr><td>%s</td><td>%s</td><td>%s %.2f</td><td><code>%s</code></td></tr>`,
			html.EscapeString(it.Name), html.EscapeString(it.Desc),
			strings.ToUpper(it.Currency), float64(it.Cents)/100, html.EscapeString(it.AccountID))
	}

	body := `<h2>Create a product (platform-level)</h2><div class="panel">
  <form method="post" action="/connect/products">
    <label>Name<br><input name="name" required></label><br><br>
    <label>Description<br><input name="description"></label><br><br>
    <label>Price (USD)<br><input name="price_usd" type="number" step="0.01" min="0.5" required></label><br><br>
    <label>Sells for connected account<br>` + sel + `</label><br><br>
    <button type="submit">Create product</button>
  </form></div>
  <h2>Products</h2><div class="panel"><table>
    <tr><th>Name</th><th>Description</th><th>Price</th><th>Connected account</th></tr>` + rows + `</table></div>
  <p><a href="/connect/store">Open the storefront →</a></p>`
	h.render(w, http.StatusOK, page("Products", body))
}

func (h *Handler) storefront(w http.ResponseWriter, r *http.Request) {
	items, err := h.listItems(r.Context())
	if err != nil {
		h.render(w, http.StatusOK, page("Storefront", `<p class="err">`+html.EscapeString(err.Error())+`</p>`))
		return
	}

	// Group by connected account so the storefront "displays all products and all
	// connected accounts".
	byAcct := map[string][]storeItem{}
	order := []string{}
	for _, it := range items {
		if _, ok := byAcct[it.AccountID]; !ok {
			order = append(order, it.AccountID)
		}
		byAcct[it.AccountID] = append(byAcct[it.AccountID], it)
	}

	var b strings.Builder
	if len(items) == 0 {
		b.WriteString(`<p class="muted">No products yet. <a href="/connect/products">Create one</a>.</p>`)
	}
	for _, acct := range order {
		b.WriteString(`<h2>Seller <code>` + html.EscapeString(acct) + `</code></h2><div class="panel"><table>
      <tr><th>Product</th><th>Price</th><th></th></tr>`)
		for _, it := range byAcct[acct] {
			b.WriteString(fmt.Sprintf(`<tr><td>%s<br><span class="muted">%s</span></td><td>%s %.2f</td>
        <td><form method="post" action="/connect/buy">
          <input type="hidden" name="product_id" value="%s">
          <button type="submit">Buy</button>
        </form></td></tr>`,
				html.EscapeString(it.Name), html.EscapeString(it.Desc),
				strings.ToUpper(it.Currency), float64(it.Cents)/100, html.EscapeString(it.ProductID)))
		}
		b.WriteString(`</table></div>`)
	}
	h.render(w, http.StatusOK, page("Storefront", b.String()))
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func (h *Handler) stripeErr(w http.ResponseWriter, ctx string, err error) {
	h.log.Warn("connectdemo: stripe error", "op", ctx, "err", err)
	h.render(w, http.StatusBadGateway, page("Stripe error",
		`<p class="err">`+html.EscapeString(ctx)+`: `+html.EscapeString(err.Error())+`</p>
     <p><a href="/connect/">back</a></p>`))
}

func (h *Handler) render(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_, _ = io.WriteString(w, body)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func urlq(s string) string {
	return strings.NewReplacer(" ", "%20", "&", "%26", "?", "%3F", "#", "%23", "+", "%2B", "/", "%2F", "=", "%3D").Replace(s)
}

// page wraps content in a minimal dark shell that matches the operator console.
func page(title, body string) string {
	return `<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>` + html.EscapeString(title) + ` — Ayni</title>
<style>
:root{--bg:#0b0d10;--panel:#14171c;--border:#262b33;--text:#e6e8eb;--muted:#8b929e;--accent:#5b8def;--good:#3fb950;--bad:#f85149}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font:15px/1.6 ui-sans-serif,system-ui,-apple-system,Segoe UI,Roboto,sans-serif}
.wrap{max-width:860px;margin:0 auto;padding:24px}
nav{display:flex;gap:18px;padding:14px 24px;border-bottom:1px solid var(--border)}
nav a{color:var(--muted);text-decoration:none}nav a:hover{color:var(--text)}
h1{font-size:20px;margin:18px 0}h2{font-size:13px;text-transform:uppercase;letter-spacing:.5px;color:var(--muted);margin:26px 0 10px}
.panel{background:var(--panel);border:1px solid var(--border);border-radius:10px;padding:16px;margin-bottom:8px}
table{width:100%;border-collapse:collapse}th,td{text-align:left;padding:8px 10px;border-bottom:1px solid var(--border)}
th{color:var(--muted);font-size:12px;text-transform:uppercase}
input,select{background:#0e1116;color:var(--text);border:1px solid var(--border);border-radius:8px;padding:8px 10px;font:inherit;min-width:280px}
button{background:var(--accent);color:#fff;border:0;border-radius:8px;padding:8px 14px;font:inherit;cursor:pointer}
code{background:#0e1116;padding:1px 5px;border-radius:4px}
.pill{padding:2px 8px;border-radius:999px;font-size:12px;border:1px solid var(--border)}
.pill.good{color:var(--good);border-color:#1c3a24}.pill.bad{color:var(--bad);border-color:#4a1f1f}
.err{color:var(--bad)}.good{color:var(--good)}.muted{color:var(--muted)}
a{color:var(--accent)}
</style></head><body>
<nav><a href="/connect/">Onboarding</a><a href="/connect/products">Products</a><a href="/connect/store">Storefront</a></nav>
<div class="wrap"><h1>` + html.EscapeString(title) + `</h1>` + body + `</div></body></html>`
}
