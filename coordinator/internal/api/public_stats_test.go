package api

import "testing"

func TestPublicStatsNeedsNoToken(t *testing.T) {
	f := newConsoleFixture(t)
	code, out, hdr := f.do(t, "GET", "/v1/stats", "", nil)
	if code != 200 {
		t.Fatalf("GET /v1/stats = %d, want 200", code)
	}
	for _, k := range []string{"devices_online", "devices_known", "by_kind", "jobs_24h", "generated_at"} {
		if _, ok := out[k]; !ok {
			t.Errorf("stats missing %q", k)
		}
	}
	for _, forbidden := range []string{"users", "email", "static_pk", "key_id"} {
		if _, ok := out[forbidden]; ok {
			t.Errorf("stats must not expose %q", forbidden)
		}
	}
	if cc := hdr.Get("Cache-Control"); cc == "" {
		t.Errorf("stats should be cacheable, got no Cache-Control")
	}
}
