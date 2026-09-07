package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/catalog"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/config"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/jobs"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/protocol"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/registry"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/store"
)

const testAdminToken = "unit-test-admin-token-9f3a"

type consoleFixture struct {
	h      http.Handler
	st     *store.Mem
	reg    *registry.Registry
	closed map[string]string
}

func newConsoleFixture(t *testing.T) *consoleFixture {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{
		HTTPAddr: ":0", HeartbeatSeconds: 15, RatePerMin: 0, PriceMultiplier: 1,
		MarketplaceMargin: 0.3, QuoteTTLSeconds: 600, JobDeadlineMS: 30000,
		AdminToken:                 testAdminToken,
		ConsumerAPIKeys:            map[string]struct{}{"consumer-key-1": {}},
		ProviderRegistrationTokens: map[string]struct{}{"dev-provider-token": {}},
	}
	st := store.NewMem(cfg.ConsumerAPIKeys, cfg.ProviderRegistrationTokens)
	cat, err := catalog.New("", "", log)
	if err != nil {
		t.Fatal(err)
	}
	reg := registry.New()
	srv := NewServer(cfg, reg, jobs.New(), st, cat, log)
	f := &consoleFixture{h: srv.Handler(), st: st, reg: reg, closed: map[string]string{}}

	add := func(id, platform, tier string, ram int, bench bool) {
		var pk [32]byte
		copy(pk[:], id+strings.Repeat("-", 32))
		p := &registry.Provider{
			ID: id, StaticPK: pk, TrustTier: tier,
			Capabilities: protocol.Capabilities{
				Platform: platform, Arch: "arm64", RAMMB: ram, Backend: "llama",
				HardwareClass: "SMALL", Models: []string{"qwen2.5-0.5b-instruct-q4_k_m"},
				Security: protocol.SecurityCapabilities{HardwareKey: tier != "community"},
			},
			ConnectedAt: time.Now().Add(-time.Minute), LastSeen: time.Now(),
			Close: func(reason string) { f.closed[id] = reason },
			// A fixture cannot run inference; the relay must fail cleanly, not panic.
			Send: func(any) error { return errors.New("fixture provider cannot receive jobs") },
		}
		reg.Add(p)
		if bench {
			_ = st.UpsertNodeCapability(t.Context(), store.NodeCapabilityRow{
				StaticPK: base64.StdEncoding.EncodeToString(pk[:]), Model: "qwen2.5-0.5b-instruct-q4_k_m",
				Backend: "llama", PrefillTPS: 120, DecodeTPS: 20, SustainedStartTPS: 20, SustainedEndTPS: 18,
				MemBandwidthGBps: 18, AvailableRAMMB: 4096, CPUCores: 8, ThermalState: "nominal",
				UpdatedAt: time.Now(),
			})
		}
	}
	add("phone-1", "android", "device_attested", 8192, true)
	add("phone-2", "android", "community", 6144, false)
	add("mac-1", "macos", "community", 32768, true)
	add("tv-1", "tvos", "community", 2048, false)
	return f
}

func (f *consoleFixture) do(t *testing.T, method, path, token string, body any) (int, map[string]any, http.Header) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	if token != "" {
		req.Header.Set("X-Admin-Token", token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out, rec.Header()
}

func num(m map[string]any, path ...string) float64 {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return -1
		}
		cur = mm[p]
	}
	if v, ok := cur.(float64); ok {
		return v
	}
	return -1
}

func TestAdminConsoleRequiresToken(t *testing.T) {
	f := newConsoleFixture(t)
	for _, p := range []string{"/admin/overview", "/admin/providers", "/admin/users", "/admin/events", "/admin/config", "/admin/intake"} {
		if code, _, _ := f.do(t, "GET", p, "", nil); code != 401 {
			t.Errorf("%s without token = %d, want 401", p, code)
		}
		if code, _, _ := f.do(t, "GET", p, "wrong-token", nil); code != 401 {
			t.Errorf("%s with wrong token = %d, want 401", p, code)
		}
	}
	if code, _, _ := f.do(t, "POST", "/admin/intake", "", map[string]any{"paused": true}); code != 401 {
		t.Errorf("intake without token = %d, want 401", code)
	}
}

func TestAdminOverviewClassifiesFleet(t *testing.T) {
	f := newConsoleFixture(t)
	code, ov, _ := f.do(t, "GET", "/admin/overview", testAdminToken, nil)
	if code != 200 {
		t.Fatalf("overview = %d %v", code, ov)
	}
	if got := num(ov, "providers", "online"); got != 4 {
		t.Fatalf("online = %v, want 4", got)
	}
	if p, pc, o := num(ov, "providers", "by_kind", "phone"), num(ov, "providers", "by_kind", "pc"), num(ov, "providers", "by_kind", "other"); p != 2 || pc != 1 || o != 1 {
		t.Fatalf("by_kind phone=%v pc=%v other=%v, want 2/1/1", p, pc, o)
	}
	if got := num(ov, "compute", "by_kind", "phone", "devices"); got != 2 {
		t.Fatalf("compute.phone.devices = %v", got)
	}
	if got := num(ov, "compute", "by_kind", "phone", "attested"); got != 1 {
		t.Fatalf("compute.phone.attested = %v, want 1", got)
	}
	if got := num(ov, "compute", "by_kind", "phone", "acu"); got <= 0 {
		t.Fatalf("phone ACU should come from the benchmark join, got %v", got)
	}
	if got := num(ov, "compute", "by_kind", "other", "acu"); got != 0 {
		t.Fatalf("unbenchmarked device must contribute 0 ACU, got %v", got)
	}
	if got := num(ov, "providers", "by_class", "unbenchmarked"); got != 2 {
		t.Fatalf("by_class.unbenchmarked = %v, want 2", got)
	}
	if got := num(ov, "providers", "known"); got != 2 {
		t.Fatalf("known (catalog) = %v, want 2", got)
	}
	if ov["coordinator"].(map[string]any)["database"] != false {
		t.Fatal("database flag should be false on the memory store")
	}
	if got := num(ov, "users", "total"); got != 0 {
		t.Fatalf("users.total = %v with accounts disabled", got)
	}
}

func TestAdminProvidersEnriched(t *testing.T) {
	f := newConsoleFixture(t)
	code, out, _ := f.do(t, "GET", "/admin/providers", testAdminToken, nil)
	if code != 200 || num(out, "count") != 4 {
		t.Fatalf("providers = %d %v", code, out)
	}
	byID := map[string]map[string]any{}
	for _, p := range out["providers"].([]any) {
		m := p.(map[string]any)
		byID[m["id"].(string)] = m
	}
	if byID["phone-1"]["kind"] != "phone" || byID["mac-1"]["kind"] != "pc" || byID["tv-1"]["kind"] != "other" {
		t.Fatalf("kind classification wrong: %v %v %v", byID["phone-1"]["kind"], byID["mac-1"]["kind"], byID["tv-1"]["kind"])
	}
	if byID["phone-1"]["acu"].(float64) <= 0 || byID["phone-1"]["class"] == "" || byID["phone-1"]["benchmark_at"] == nil {
		t.Fatalf("benchmark join missing on phone-1: %v", byID["phone-1"])
	}
	if byID["phone-2"]["acu"].(float64) != 0 {
		t.Fatal("phone-2 has no benchmark and must report acu 0")
	}
	if _, has := byID["phone-1"]["connected_at"]; !has {
		t.Fatal("connected_at must be present")
	}
	if sec := byID["phone-1"]["security"].(map[string]any); sec["hardware_key"] != true {
		t.Fatal("security capabilities must be surfaced")
	}
}

func TestAdminDisconnectAndAudit(t *testing.T) {
	f := newConsoleFixture(t)
	code, out, _ := f.do(t, "POST", "/admin/providers/phone-2/disconnect", testAdminToken, map[string]any{"reason": "fleet test"})
	if code != 200 || out["disconnected"] != true {
		t.Fatalf("disconnect = %d %v", code, out)
	}
	if f.closed["phone-2"] != "fleet test" {
		t.Fatalf("Close not invoked with reason, got %q", f.closed["phone-2"])
	}
	if code, _, _ := f.do(t, "POST", "/admin/providers/nope/disconnect", testAdminToken, nil); code != 404 {
		t.Fatalf("unknown provider = %d, want 404", code)
	}
	_, evs, _ := f.do(t, "GET", "/admin/events?level=AUDIT", testAdminToken, nil)
	found := false
	for _, e := range evs["events"].([]any) {
		m := e.(map[string]any)
		if m["level"] == "AUDIT" && strings.Contains(m["msg"].(string), "disconnected by operator") {
			if m["attrs"].(map[string]any)["provider_id"] == "phone-2" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("audit event for disconnect missing: %v", evs)
	}
}

func TestAdminIntakePauseGatesInference(t *testing.T) {
	f := newConsoleFixture(t)
	chat := func() (int, map[string]any, http.Header) {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"qwen2.5-0.5b-instruct-q4_k_m","messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Authorization", "Bearer consumer-key-1")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		f.h.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out, rec.Header()
	}
	errCode := func(m map[string]any) string {
		if e, ok := m["error"].(map[string]any); ok {
			s, _ := e["code"].(string)
			return s
		}
		return ""
	}

	code, out, _ := f.do(t, "POST", "/admin/intake", testAdminToken, map[string]any{"paused": true, "message": "patching"})
	if code != 200 || out["paused"] != true || out["message"] != "patching" {
		t.Fatalf("pause = %d %v", code, out)
	}
	c, body, hdr := chat()
	if c != 503 || errCode(body) != "maintenance" || hdr.Get("Retry-After") == "" {
		t.Fatalf("paused chat = %d %v retry=%q, want 503 maintenance", c, body, hdr.Get("Retry-After"))
	}
	if !strings.Contains(body["error"].(map[string]any)["message"].(string), "patching") {
		t.Fatal("operator message must reach the caller")
	}
	// Read-only routes are not gated.
	if c, b, _ := f.do(t, "GET", "/v1/manifest-key", "", nil); c == 503 {
		t.Fatalf("manifest-key must not be gated: %d %v", c, b)
	}
	_, ov, _ := f.do(t, "GET", "/admin/overview", testAdminToken, nil)
	if ov["intake"].(map[string]any)["paused"] != true {
		t.Fatal("overview must report the paused state")
	}

	code, out, _ = f.do(t, "POST", "/admin/intake", testAdminToken, map[string]any{"paused": false})
	if code != 200 || out["paused"] != false {
		t.Fatalf("resume = %d %v", code, out)
	}
	if c, body, _ := chat(); errCode(body) == "maintenance" {
		t.Fatalf("after resume chat still gated: %d %v", c, body)
	}
	_, evs, _ := f.do(t, "GET", "/admin/events?level=AUDIT&q=intake", testAdminToken, nil)
	if n := num(evs, "count"); n < 2 {
		t.Fatalf("expected pause+resume audit events, got %v", n)
	}
}

func TestAdminCreditAdjust(t *testing.T) {
	f := newConsoleFixture(t)
	// Resolve the key id the store assigned to the env key.
	req := httptest.NewRequest("GET", "/billing/balance", nil)
	req.Header.Set("Authorization", "Bearer consumer-key-1")
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	var bal map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &bal)
	keyID, _ := bal["key_id"].(string)
	if keyID == "" {
		t.Fatalf("could not resolve key id: %d %s", rec.Code, rec.Body.String())
	}

	code, out, _ := f.do(t, "POST", "/admin/credit", testAdminToken, map[string]any{"key_id": keyID, "amount_usd": 2.5, "note": "qa"})
	if code != 200 || num(out, "balance_usd") != 2.5 {
		t.Fatalf("credit = %d %v", code, out)
	}
	if got, _ := f.st.CreditBalance(t.Context(), keyID); got != 2_500_000 {
		t.Fatalf("store balance = %d micros, want 2500000", got)
	}
	code, out, _ = f.do(t, "POST", "/admin/credit", testAdminToken, map[string]any{"key_id": keyID, "amount_usd": -1})
	if code != 200 || num(out, "balance_usd") != 1.5 {
		t.Fatalf("negative adjust = %d %v", code, out)
	}
	for _, bad := range []any{0, 1001, -5000} {
		if code, _, _ := f.do(t, "POST", "/admin/credit", testAdminToken, map[string]any{"key_id": keyID, "amount_usd": bad}); code != 400 {
			t.Errorf("amount %v accepted with %d, want 400", bad, code)
		}
	}
	if code, _, _ := f.do(t, "POST", "/admin/credit", testAdminToken, map[string]any{"amount_usd": 1}); code != 400 {
		t.Error("missing key_id must be rejected")
	}
	_, evs, _ := f.do(t, "GET", "/admin/events?level=AUDIT&q=credit", testAdminToken, nil)
	if num(evs, "count") < 2 {
		t.Fatalf("credit adjustments must be audited: %v", evs)
	}
}

func TestAdminConfigHasNoSecrets(t *testing.T) {
	f := newConsoleFixture(t)
	req := httptest.NewRequest("GET", "/admin/config", nil)
	req.Header.Set("X-Admin-Token", testAdminToken)
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("config = %d %s", rec.Code, body)
	}
	if strings.Contains(body, testAdminToken) || strings.Contains(body, "consumer-key-1") || strings.Contains(body, "dev-provider-token") {
		t.Fatal("config leaked a credential")
	}
	for _, k := range []string{"price_multiplier", "billing_enforce", "database", "stripe", "env_provider_tokens", "uptime_s"} {
		if !strings.Contains(body, `"`+k+`"`) {
			t.Errorf("config missing %s", k)
		}
	}
}

func TestAdminUsersWithoutAccounts(t *testing.T) {
	f := newConsoleFixture(t)
	code, out, _ := f.do(t, "GET", "/admin/users", testAdminToken, nil)
	if code != 200 || num(out, "count") != 0 || out["note"] == nil {
		t.Fatalf("users = %d %v", code, out)
	}
}
