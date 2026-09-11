// Package config loads coordinator configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	// HTTPAddr is the listen address for the consumer API + provider WebSocket.
	HTTPAddr string
	// ConsumerAPIKeys is the set of accepted Bearer keys (M1: static from env; M2: Postgres).
	ConsumerAPIKeys map[string]struct{}
	// ProviderRegistrationTokens is the set of accepted provider registration tokens.
	ProviderRegistrationTokens map[string]struct{}
	// HeartbeatSeconds is advertised to providers in register_ack.
	HeartbeatSeconds int
	// ManifestURL is the signed model registry base URL (e.g. https://models.ayni-ai.com).
	ManifestURL string
	// RegistryPubKey is the base64 Ed25519 key the manifest must be signed with.
	RegistryPubKey string
	// JobDeadlineMS bounds a single inference job.
	JobDeadlineMS int64
	// DatabaseURL, when set, switches persistence from in-memory to Postgres.
	DatabaseURL string
	// RatePerMin is the default per-API-key request budget (0 disables limiting).
	RatePerMin int
	// AdminToken gates /admin/* (key management, provider list, usage). Empty = disabled.
	AdminToken string

	// --- billing / payouts (Stripe). All optional; empty StripeSecretKey disables. ---
	// StripeSecretKey is the Stripe API key (sk_test_… or sk_live_…).
	StripeSecretKey string
	// StripeWebhookSecret verifies /billing/webhook signatures (whsec_…).
	StripeWebhookSecret string
	// StripeConnectWebhookSecret verifies /connect/webhook thin-event signatures.
	StripeConnectWebhookSecret string
	// PublicBaseURL is where Stripe Checkout redirects back to (the console or site).
	PublicBaseURL string
	// AppBaseURL is the signed-in web app, used to build invite links.
	AppBaseURL string
	// BillingEnforce, when true, rejects inference from a consumer key with a
	// non-positive credit balance (402). Off by default so nothing breaks until opt-in.
	BillingEnforce bool
	// PriceMultiplier scales the whole rate card (default 1.0). A lever for tuning
	// and for exercising payouts on tiny models during testing.
	PriceMultiplier float64

	// --- marketplace (M10.3) ---
	// MarketplaceMargin is Ayni's cut on a workload quote, applied to
	// (compute + coordination + failure). Default 0.30.
	MarketplaceMargin float64
	// QuoteTTLSeconds is how long a workload quote stays acceptable. Default 600.
	QuoteTTLSeconds int
	// RunResultsTTLSeconds is how long an async run's results are held in memory for
	// collection. Results are never written to disk (docs/WORKLOAD-RUNNERS.md). Default 3600.
	RunResultsTTLSeconds int
	// MaxAsyncRunsPerKey bounds concurrent queued/running async workloads per API key. Default 4.
	MaxAsyncRunsPerKey int
	// AllowInsecureWebhooks permits http:// and private/loopback webhook targets.
	// Local development only: in production this is an SSRF hole. Default false.
	AllowInsecureWebhooks bool

	// --- accounts / OAuth sign-in (app.ayni-ai.com). Postgres-only. ---
	GitHubClientID, GitHubClientSecret string
	GoogleClientID, GoogleClientSecret string
	// AuthCallbackBase is where OAuth providers redirect back (this API's origin).
	AuthCallbackBase string
	// AppURL is the signed-in web app; OAuth ends with a redirect here.
	AppURL string
	// CookieDomain scopes the session cookie across api./app. subdomains.
	CookieDomain string

	// --- workload manifest signing (security v0.1) ---
	// ManifestSigningKey is a base64 32-byte Ed25519 seed. When set, the
	// coordinator attaches a signed Workload Manifest v1 to every job and serves
	// its public key at GET /v1/manifest-key. Empty disables it entirely.
	ManifestSigningKey string
	// ManifestSignerID is the signer identity carried in each manifest.
	ManifestSignerID string
	// ManifestKMSKey, when set, signs manifests with this Cloud KMS
	// cryptoKeyVersion (EC_SIGN_ED25519) instead of a local seed. Takes
	// precedence over ManifestSigningKey. Production setting.
	ManifestKMSKey string

	// --- spot tier (M10.5) ---
	// SpotPriceFactor scales the whole quote (and provider accruals) for a spot
	// workload: cheaper for the buyer, less for the seller, both interruptible.
	// Default 0.6.
	SpotPriceFactor float64
	// SpotEtaSlack multiplies the optimistic ETA to give the upper bound of the
	// spot completion-time band. Default 3.
	SpotEtaSlack float64

	// DemoEnabled turns on the public, no-sign-up investor demo endpoints
	// (POST /v1/demo/summarize, GET /v1/demo/last-job). Off by default.
	DemoEnabled bool
	// ConnectDemoEnabled mounts the sample Stripe Connect integration at /connect/.
	// It is UNAUTHENTICATED and creates real objects on the configured Stripe
	// account, so it must never default on: mounting used to be implied by having
	// Stripe keys, which is always true in production.
	ConnectDemoEnabled bool

	// --- Ayni Council workload review ---
	// One real model seat on the Council that reviews each workload before it is
	// quoted. Empty -> only the deterministic reviewers run.
	CouncilModelAPI string // "anthropic" | "openai"
	CouncilModelKey string
	CouncilModelID  string
}

func Load() (Config, error) {
	c := Config{
		HTTPAddr:                   getenv("SC_HTTP_ADDR", ":8080"),
		ConsumerAPIKeys:            set(getenv("SC_CONSUMER_API_KEYS", "dev-consumer-key")),
		ProviderRegistrationTokens: set(getenv("SC_PROVIDER_TOKENS", "dev-provider-token")),
		HeartbeatSeconds:           getenvInt("SC_HEARTBEAT_SECONDS", 15),
		ManifestURL:                getenv("SC_MANIFEST_URL", ""),
		RegistryPubKey:             getenv("SC_REGISTRY_PUBKEY", ""),
		JobDeadlineMS:              int64(getenvInt("SC_JOB_DEADLINE_MS", 120000)),
		DatabaseURL:                getenv("SC_DATABASE_URL", ""),
		RatePerMin:                 getenvInt("SC_RATE_PER_MIN", 120),
		AdminToken:                 getenv("SC_ADMIN_TOKEN", ""),
		StripeSecretKey:            getenv("SC_STRIPE_SECRET_KEY", ""),
		StripeWebhookSecret:        getenv("SC_STRIPE_WEBHOOK_SECRET", ""),
		StripeConnectWebhookSecret: getenv("SC_STRIPE_CONNECT_WEBHOOK_SECRET", ""),
		PublicBaseURL:              strings.TrimRight(getenv("SC_PUBLIC_BASE_URL", "https://ayni-ai.com"), "/"),
		AppBaseURL:                 strings.TrimRight(getenv("SC_APP_BASE_URL", "https://app.ayni-ai.com"), "/"),
		BillingEnforce:             getenv("SC_BILLING_ENFORCE", "") == "1",
		PriceMultiplier:            getenvFloat("SC_PRICE_MULTIPLIER", 1.0),
		MarketplaceMargin:          getenvFloat("SC_MARKETPLACE_MARGIN", 0.30),
		QuoteTTLSeconds:            getenvInt("SC_QUOTE_TTL_SECONDS", 600),
		RunResultsTTLSeconds:       getenvInt("SC_RUN_RESULTS_TTL_SECONDS", 3600),
		MaxAsyncRunsPerKey:         getenvInt("SC_MAX_ASYNC_RUNS_PER_KEY", 4),
		AllowInsecureWebhooks:      getenv("SC_ALLOW_INSECURE_WEBHOOKS", "") == "1",
		SpotPriceFactor:            getenvFloat("SC_SPOT_PRICE_FACTOR", 0.6),
		SpotEtaSlack:               getenvFloat("SC_SPOT_ETA_SLACK", 3),
		ManifestSigningKey:         getenv("SC_MANIFEST_SIGNING_KEY", ""),
		ManifestSignerID:           getenv("SC_MANIFEST_SIGNER_ID", "signer-v1"),
		ManifestKMSKey:             getenv("SC_MANIFEST_KMS_KEY", ""),
		GitHubClientID:             getenv("SC_GITHUB_CLIENT_ID", ""),
		GitHubClientSecret:         getenv("SC_GITHUB_CLIENT_SECRET", ""),
		GoogleClientID:             getenv("SC_GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret:         getenv("SC_GOOGLE_CLIENT_SECRET", ""),
		AuthCallbackBase:           strings.TrimRight(getenv("SC_AUTH_CALLBACK_BASE", "https://api.ayni-ai.com"), "/"),
		AppURL:                     strings.TrimRight(getenv("SC_APP_URL", "https://app.ayni-ai.com"), "/"),
		CookieDomain:               getenv("SC_COOKIE_DOMAIN", ".ayni-ai.com"),
		DemoEnabled:                getenv("SC_DEMO_ENABLED", "") == "1",
		ConnectDemoEnabled:         getenv("SC_CONNECT_DEMO_ENABLED", "") == "1",
		CouncilModelAPI:            getenv("SC_COUNCIL_MODEL_API", ""),
		CouncilModelKey:            getenv("SC_COUNCIL_MODEL_KEY", ""),
		CouncilModelID:             getenv("SC_COUNCIL_MODEL_ID", ""),
	}
	if len(c.ConsumerAPIKeys) == 0 {
		return c, fmt.Errorf("SC_CONSUMER_API_KEYS must not be empty")
	}
	if len(c.ProviderRegistrationTokens) == 0 {
		return c, fmt.Errorf("SC_PROVIDER_TOKENS must not be empty")
	}
	if c.HeartbeatSeconds < 5 || c.HeartbeatSeconds > 120 {
		return c, fmt.Errorf("SC_HEARTBEAT_SECONDS out of range [5,120]")
	}
	return c, nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getenvFloat(k string, def float64) float64 {
	if v := os.Getenv(k); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return def
}

// set parses a comma-separated env value into a set, trimming whitespace and empties.
func set(csv string) map[string]struct{} {
	m := make(map[string]struct{})
	for _, p := range strings.Split(csv, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			m[p] = struct{}{}
		}
	}
	return m
}
