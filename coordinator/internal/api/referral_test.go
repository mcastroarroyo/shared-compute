package api

import "testing"

// Without accounts (memory store, no Postgres) the leaderboard is public and empty,
// and the signed-in routes say so instead of failing.
func TestLeaderboardPublicWithoutAccounts(t *testing.T) {
	f := newConsoleFixture(t)
	code, out, _ := f.do(t, "GET", "/v1/leaderboard", "", nil)
	if code != 200 {
		t.Fatalf("GET /v1/leaderboard = %d, want 200", code)
	}
	if _, ok := out["rows"]; !ok {
		t.Errorf("leaderboard missing rows")
	}
	if num(out, "founding_cap") != 1000 {
		t.Errorf("founding_cap = %v, want 1000", out["founding_cap"])
	}
	for _, p := range []string{"/v1/me/referral"} {
		if code, _, _ := f.do(t, "GET", p, "", nil); code != 404 {
			t.Errorf("%s without accounts = %d, want 404 accounts_disabled", p, code)
		}
	}
}
