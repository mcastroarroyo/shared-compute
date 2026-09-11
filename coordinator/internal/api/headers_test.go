package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/config"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/marketplace"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/ratelimit"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/registry"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/store"
)

func TestSecureHeadersPresent(t *testing.T) {
	h := secureHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	for k, want := range map[string]string{
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains; preload",
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "no-referrer",
		"Content-Security-Policy":   "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'",
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

// TestMetricsRequireAdminInProduction: /metrics is operational detail and must
// not be readable without the admin token once one is configured.
func TestMetricsRequireAdminInProduction(t *testing.T) {
	srv := (&Server{cfg: config.Config{AdminToken: "adm-secret", ConsumerAPIKeys: map[string]struct{}{"k": {}}}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}).Handler()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /metrics = %d, want 401", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer adm-secret")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin /metrics = %d, want 200", rec.Code)
	}
}

// The Stripe Connect sample is unauthenticated and creates real objects on the
// configured Stripe account. It used to mount whenever Stripe keys were present,
// which is always true in production, exposing /connect/ to the internet. It must
// stay off unless an operator opts in explicitly.
func TestConnectDemoOffUnlessExplicitlyEnabled(t *testing.T) {
	withStripe := config.Config{
		StripeSecretKey: "sk_live_notreal", StripeConnectWebhookSecret: "whsec_notreal",
		PublicBaseURL: "https://api.example.com",
	}
	srv := &Server{cfg: withStripe, log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		reg: registry.New(), quotes: marketplace.NewStore(time.Minute), runs: newRunRegistry(),
		store: store.NewMem(map[string]struct{}{}, map[string]struct{}{}), rl: ratelimit.New(120),
		intakeRL: ratelimit.New(6), demoRL: ratelimit.New(3), demo: &demoState{}}

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/connect/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/connect/ must be absent when SC_CONNECT_DEMO_ENABLED is unset, got %d", rec.Code)
	}

	// And it still works when an operator deliberately turns it on.
	on := withStripe
	on.ConnectDemoEnabled = true
	srv2 := &Server{cfg: on, log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		reg: registry.New(), quotes: marketplace.NewStore(time.Minute), runs: newRunRegistry(),
		store: store.NewMem(map[string]struct{}{}, map[string]struct{}{}), rl: ratelimit.New(120),
		intakeRL: ratelimit.New(6), demoRL: ratelimit.New(3), demo: &demoState{}}
	rec2 := httptest.NewRecorder()
	srv2.Handler().ServeHTTP(rec2, httptest.NewRequest("GET", "/connect/", nil))
	if rec2.Code == http.StatusNotFound {
		t.Error("opting in should mount the demo")
	}
}
