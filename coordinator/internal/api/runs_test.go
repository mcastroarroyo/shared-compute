package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/batch"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/config"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/store"
)

func runTestServer(t *testing.T) *Server {
	t.Helper()
	return &Server{
		cfg:   config.Config{RunResultsTTLSeconds: 3600, MaxAsyncRunsPerKey: 2},
		store: store.NewMem(map[string]struct{}{}, map[string]struct{}{}),
		runs:  newRunRegistry(),
		log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func withKey(r *http.Request, keyID string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxKeyID, keyID))
}

func seedRun(t *testing.T, s *Server, id, keyID, status string) store.WorkloadRun {
	t.Helper()
	run := store.WorkloadRun{
		ID: id, KeyID: keyID, WorkloadID: "wl_test", Model: "m", Status: status,
		TotalItems: 2, DoneItems: 2, OKItems: 2, CreatedAt: time.Now(),
	}
	if err := s.store.UpsertRun(context.Background(), run); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	return run
}

// A run belongs to the key that created it. Another key must get 404 — not 403 —
// so the API never confirms that someone else's run id exists.
func TestRunIsolatedByAPIKey(t *testing.T) {
	s := runTestServer(t)
	seedRun(t, s, "run_abc", "key_owner", runStatusSucceeded)

	for _, tc := range []struct {
		key  string
		want int
	}{
		{"key_owner", http.StatusOK},
		{"key_other", http.StatusNotFound},
	} {
		r := withKey(httptest.NewRequest("GET", "/v1/runs/run_abc", nil), tc.key)
		r.SetPathValue("id", "run_abc")
		rec := httptest.NewRecorder()
		s.handleGetRun(rec, r)
		if rec.Code != tc.want {
			t.Fatalf("key %s: want %d, got %d (%s)", tc.key, tc.want, rec.Code, rec.Body.String())
		}
	}
}

// Results must not be readable before the run finishes, and the caller is told to poll.
func TestRunResultsConflictWhileRunning(t *testing.T) {
	s := runTestServer(t)
	seedRun(t, s, "run_run", "key_owner", runStatusRunning)

	r := withKey(httptest.NewRequest("GET", "/v1/runs/run_run/results", nil), "key_owner")
	r.SetPathValue("id", "run_run")
	rec := httptest.NewRecorder()
	s.handleGetRunResults(rec, r)

	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409 while running, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("expected a Retry-After hint for pollers")
	}
}

// Results live in memory only: once the TTL passes the run still reports succeeded,
// but the content is gone and the API says so plainly rather than 500ing.
func TestRunResultsExpireFromMemory(t *testing.T) {
	s := runTestServer(t)
	seedRun(t, s, "run_exp", "key_owner", runStatusSucceeded)
	s.runs.put("run_exp", &runResults{
		summary:   &batch.Summary{Providers: map[string]int{}},
		expiresAt: time.Now().Add(-time.Minute), // already expired
	})

	r := withKey(httptest.NewRequest("GET", "/v1/runs/run_exp/results", nil), "key_owner")
	r.SetPathValue("id", "run_exp")
	rec := httptest.NewRecorder()
	s.handleGetRunResults(rec, r)

	if rec.Code != http.StatusGone {
		t.Fatalf("want 410 for expired results, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "results_expired") {
		t.Errorf("want a results_expired code, got %s", rec.Body.String())
	}
}

// Listing is scoped to the caller's key.
func TestListRunsScopedToKey(t *testing.T) {
	s := runTestServer(t)
	seedRun(t, s, "run_mine", "key_owner", runStatusSucceeded)
	seedRun(t, s, "run_theirs", "key_other", runStatusSucceeded)

	r := withKey(httptest.NewRequest("GET", "/v1/runs", nil), "key_owner")
	rec := httptest.NewRecorder()
	s.handleListRuns(rec, r)

	var got struct {
		Data  []map[string]any `json:"data"`
		Count int              `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Count != 1 || got.Data[0]["id"] != "run_mine" {
		t.Fatalf("want only the caller's run, got %+v", got.Data)
	}
}

// The per-key ceiling keeps one runner from parking the whole fleet.
func TestInflightCapPerKey(t *testing.T) {
	s := runTestServer(t)
	if s.maxAsyncRunsPerKey() != 2 {
		t.Fatalf("want cap 2, got %d", s.maxAsyncRunsPerKey())
	}
	s.runs.addInflight("key_owner", 2)
	if s.runs.inflightFor("key_owner") < s.maxAsyncRunsPerKey() {
		t.Fatal("expected the key to be at its ceiling")
	}
	if s.runs.inflightFor("key_other") != 0 {
		t.Error("one key's load must not count against another")
	}
	s.runs.addInflight("key_owner", -2)
	if s.runs.inflightFor("key_owner") != 0 {
		t.Error("inflight should drain back to zero")
	}
}

// The webhook body is content-free and its signature verifies with the run secret.
func TestWebhookIsSignedAndCarriesNoContent(t *testing.T) {
	s := runTestServer(t)
	s.cfg.AuthCallbackBase = "https://api.example.com"

	var gotBody []byte
	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotSig = r.Header.Get("X-Ayni-Signature")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	secret := "whsec_test"
	run := store.WorkloadRun{
		ID: "run_hook", KeyID: "key_owner", WorkloadID: "wl_1", Model: "m",
		Status: runStatusSucceeded, TotalItems: 3, OKItems: 3, WebhookURL: srv.URL,
	}
	s.runs.setSecret(run.ID, secret)
	if err := s.deliverRunWebhook(context.Background(), run); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	ts, v1, ok := strings.Cut(gotSig, ",")
	if !ok || !strings.HasPrefix(ts, "t=") || !strings.HasPrefix(v1, "v1=") {
		t.Fatalf("malformed signature header: %q", gotSig)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strings.TrimPrefix(ts, "t=") + "." + string(gotBody)))
	if want := hex.EncodeToString(mac.Sum(nil)); want != strings.TrimPrefix(v1, "v1=") {
		t.Error("signature does not verify with the run secret")
	}

	// The notice must carry status and a fetch URL, never completion text.
	var payload map[string]any
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload["results_url"] != "https://api.example.com/v1/runs/run_hook/results" {
		t.Errorf("want a results_url to fetch over TLS, got %v", payload["results_url"])
	}
	for _, banned := range []string{"items\":[", "content", "message"} {
		if strings.Contains(string(gotBody), banned) {
			t.Errorf("webhook payload must not carry job content; found %q in %s", banned, gotBody)
		}
	}
}
