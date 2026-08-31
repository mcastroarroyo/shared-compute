// Package api serves the consumer-facing OpenAI-compatible REST surface and mounts the
// provider WebSocket handler.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/auth"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/catalog"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/config"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/connectdemo"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/council"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/jobs"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/manifest"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/marketplace"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/metrics"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/ratelimit"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/registry"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/relay"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/store"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/wshub"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	stripe "github.com/stripe/stripe-go/v86"
)

type ctxKey int

const ctxKeyID ctxKey = 0

type Server struct {
	cfg      config.Config
	reg      *registry.Registry
	job      *jobs.Manager
	store    store.Store
	cat      *catalog.Catalog
	rl       *ratelimit.Limiter
	intakeRL *ratelimit.Limiter
	demoRL   *ratelimit.Limiter
	demo     *demoState
	log      *slog.Logger
	hub      *wshub.Hub
	stripe   *stripe.Client // nil unless SC_STRIPE_SECRET_KEY is set
	quotes   *marketplace.Store
	signer   *manifest.Signer // nil unless SC_MANIFEST_SIGNING_KEY is set
	auth     *auth.Auth       // nil unless Postgres + an OAuth client are configured
	council  []council.Reviewer
}

func NewServer(cfg config.Config, reg *registry.Registry, job *jobs.Manager, st store.Store, cat *catalog.Catalog, log *slog.Logger) *Server {
	signer, err := manifest.NewSigner(cfg.ManifestSignerID, cfg.ManifestSigningKey)
	if err != nil {
		log.Error("manifest signer disabled", "err", err)
		signer = nil
	} else if signer != nil {
		log.Info("workload manifest signing enabled", "signer_id", signer.ID())
	}
	au, aerr := auth.New(context.Background(), cfg.DatabaseURL, auth.Config{
		GitHubClientID: cfg.GitHubClientID, GitHubSecret: cfg.GitHubClientSecret,
		GoogleClientID: cfg.GoogleClientID, GoogleSecret: cfg.GoogleClientSecret,
		CallbackBase: cfg.AuthCallbackBase, AppURL: cfg.AppURL,
		CookieDomain: cfg.CookieDomain, Secure: true,
	}, st, log)
	if aerr != nil {
		log.Error("auth disabled", "err", aerr)
	} else if au != nil {
		log.Info("account sign-in enabled", "providers", au.Providers())
	}
	hub := &wshub.Hub{Cfg: cfg, Reg: reg, Job: job, Store: st, Log: log, Auth: au}

	reviewers := []council.Reviewer{
		council.NewDeterministicReviewer("security_critic", "Chief Security Critic"),
		council.NewDeterministicReviewer("node_safety", "Node and Provider Safety Guardian"),
	}
	if mr := council.NewModelReviewer("red_team", "Red Team Director",
		cfg.CouncilModelAPI, cfg.CouncilModelKey, cfg.CouncilModelID); mr != nil {
		reviewers = append(reviewers, mr)
		log.Info("council model reviewer enabled", "api", cfg.CouncilModelAPI, "model", cfg.CouncilModelID)
	}

	return &Server{
		cfg: cfg, reg: reg, job: job, store: st, cat: cat,
		rl:       ratelimit.New(cfg.RatePerMin),
		intakeRL: ratelimit.New(6), // public site forms: 6/min per IP
		demoRL:   ratelimit.New(3), // investor demo: 3 runs/min per IP
		demo:     &demoState{},
		log:      log,
		hub:      hub,
		stripe:   newStripeClient(cfg.StripeSecretKey),
		quotes:   marketplace.NewStore(time.Duration(cfg.QuoteTTLSeconds) * time.Second),
		signer:   signer,
		auth:     au,
		council:  reviewers,
	}
}

// Close releases resources the server owns (the auth DB pool).
func (s *Server) Close() {
	if s.auth != nil {
		s.auth.Close()
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", instrument("healthz", http.HandlerFunc(s.handleHealth)))
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.Handle("GET /v1/models", instrument("v1_models", s.withAuth(s.handleModels)))
	mux.Handle("GET /v1/manifest-key", instrument("v1_manifest_key", http.HandlerFunc(s.handleManifestKey)))
	mux.Handle("GET /install/provider.sh", instrument("install_provider", http.HandlerFunc(s.handleInstallScript)))
	mux.Handle("POST /v1/chat/completions", instrument("v1_chat_completions", s.withAuth(s.handleChatCompletions)))
	mux.Handle("POST /v1/batch", instrument("v1_batch", s.withAuth(s.handleBatch)))
	mux.Handle("POST /v1/quote", instrument("v1_quote", http.HandlerFunc(s.handleQuotePreview)))
	mux.Handle("POST /v1/demo/summarize", instrument("v1_demo_summarize", http.HandlerFunc(s.handleDemoSummarize)))
	mux.Handle("GET /v1/demo/last-job", instrument("v1_demo_last_job", http.HandlerFunc(s.handleDemoLastJob)))
	mux.Handle("POST /v1/workloads", instrument("v1_workloads_create", s.withAuth(s.handleCreateWorkload)))
	mux.Handle("GET /v1/workloads/{id}", instrument("v1_workloads_get", s.withAuth(s.handleGetWorkload)))
	mux.Handle("POST /v1/workloads/{id}/accept", instrument("v1_workloads_accept", s.withAuth(s.handleAcceptWorkload)))
	mux.HandleFunc("/ws/provider", s.hub.HandleProvider)
	mux.Handle("POST /waitlist", instrument("waitlist", http.HandlerFunc(s.handleWaitlist)))
	mux.Handle("POST /initiatives", instrument("initiatives", http.HandlerFunc(s.handleInitiative)))
	mux.Handle("GET /billing/balance", instrument("billing_balance", s.withAuth(s.handleBalance)))
	mux.Handle("POST /billing/checkout", instrument("billing_checkout", s.withAuth(s.handleCheckout)))
	mux.Handle("POST /billing/webhook", instrument("billing_webhook", http.HandlerFunc(s.handleStripeWebhook)))
	mux.Handle("POST /payouts/webhook", instrument("payouts_webhook", http.HandlerFunc(s.handlePayoutWebhook)))

	// Sample Stripe Connect integration (onboard, products, storefront, charges).
	if h, err := connectdemo.New(s.cfg.StripeSecretKey, s.cfg.StripeConnectWebhookSecret,
		s.cfg.PublicBaseURL, s.log); err != nil {
		s.log.Info("connect demo not mounted", "reason", err)
	} else {
		h.Mount(mux)
		s.log.Info("connect demo mounted at /connect/")
	}

	s.mountCouncil(mux)
	if s.auth != nil {
		s.auth.Mount(mux)
	}
	s.mountAdmin(mux)
	return withCORS(s.cfg.AppURL, logRequests(s.log, mux))
}

// withCORS makes the API reachable from the browser console and the signed-in
// app (separate origins). Token-authenticated endpoints stay open to any origin;
// the signed-in app origin additionally gets credentialed CORS so the session
// cookie can travel.
func withCORS(appURL string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		credentialed := origin != "" && (origin == appURL ||
			strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:"))
		if credentialed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, X-Admin-Token, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// statusRecorder captures the response status for metrics.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// instrument records request count + duration for a route. The route label is a fixed
// string, never derived from the URL, so no user data enters metrics.
func instrument(route string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		metrics.HTTPDuration.WithLabelValues(route).Observe(time.Since(start).Seconds())
		metrics.HTTPRequests.WithLabelValues(route, strconv.Itoa(rec.status/100)+"xx").Inc()
	})
}

func (s *Server) deps() relay.Deps {
	return relay.Deps{Reg: s.reg, Job: s.job, Cfg: s.cfg, Log: s.log, Manifest: s.signer}
}

// handleManifestKey publishes the Workload Manifest v1 signing public key so a
// provider node can verify what it is asked to run. 404 when signing is off.
func (s *Server) handleManifestKey(w http.ResponseWriter, _ *http.Request) {
	if s.signer == nil {
		writeError(w, http.StatusNotFound, "not_enabled", "manifest signing is not configured")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"signer_id":        s.signer.ID(),
		"public_key":       s.signer.PublicKeyB64(),
		"algorithm":        "ed25519",
		"manifest_version": manifest.Version,
	})
}

// withAuth checks the consumer Bearer API key and stashes its stable id in the context.
func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var keyID string
		var ok bool
		if key := bearer(r); key != "" {
			keyID, ok = s.store.ValidateConsumerKey(r.Context(), key)
			if !ok {
				writeError(w, http.StatusUnauthorized, "invalid_api_key", "the API key is not recognized")
				return
			}
		} else if s.auth != nil {
			// signed-in app: resolve the session cookie to the account's key
			keyID, ok = s.auth.KeyIDForRequest(r)
		}
		if !ok || keyID == "" {
			writeError(w, http.StatusUnauthorized, "missing_api_key", "provide an API key via Authorization: Bearer, or sign in")
			return
		}
		if allowed, retry := s.rl.Allow(keyID); !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
			metrics.JobsTotal.WithLabelValues("rate_limited").Inc()
			writeError(w, http.StatusTooManyRequests, "rate_limited", "request rate exceeded for this API key")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKeyID, keyID)))
	}
}

func keyIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyID).(string); ok {
		return v
	}
	return ""
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		// Method + path + remote only. Never the body.
		log.Debug("http", "method", r.Method, "path", r.URL.Path)
	})
}
