package auth

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/store"
)

// These tests need a throwaway Postgres:
//
//	SC_TEST_DATABASE_URL=postgres://postgres@127.0.0.1:55432/ayni_test go test ./internal/auth/...
func testAuth(t *testing.T) (*Auth, *store.PG) {
	t.Helper()
	dsn := os.Getenv("SC_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set SC_TEST_DATABASE_URL to run auth DB tests")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	pg, err := store.NewPG(ctx, dsn, log, nil, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pg.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	a, err := New(ctx, dsn, Config{
		GitHubClientID: "gh-id", GitHubSecret: "gh-secret",
		CallbackBase: "http://api.test", AppURL: "http://app.test",
	}, pg, log)
	if err != nil || a == nil {
		t.Fatalf("New: %v", err)
	}
	// clean slate (shared DB across tests)
	for _, tbl := range []string{
		"provider_owners", "user_provider_tokens", "user_api_keys", "sessions",
		"oauth_identities", "provider_earnings", "users",
	} {
		if _, err := a.pool.Exec(ctx, "DELETE FROM "+tbl); err != nil {
			t.Fatalf("clean %s: %v", tbl, err)
		}
	}
	t.Cleanup(func() { a.Close(); pg.Close() })
	return a, pg
}

func TestUpsertUser_NewMatchAndRefresh(t *testing.T) {
	a, _ := testAuth(t)
	ctx := context.Background()

	u1, err := a.upsertUser(ctx, "github", "gh-1", "sam@example.com", "Sam", "http://a/1.png")
	if err != nil {
		t.Fatal(err)
	}
	if u1.Email != "sam@example.com" || u1.Name != "Sam" {
		t.Fatalf("u1 = %+v", u1)
	}

	// same identity again -> same user, profile refreshed
	u1b, err := a.upsertUser(ctx, "github", "gh-1", "sam@example.com", "Samuel", "http://a/2.png")
	if err != nil {
		t.Fatal(err)
	}
	if u1b.ID != u1.ID || u1b.Name != "Samuel" {
		t.Fatalf("expected same id with refreshed name, got %+v", u1b)
	}

	// different provider, same email (case-insensitive) -> links to the existing user
	u1c, err := a.upsertUser(ctx, "google", "goog-9", "SAM@example.com", "Sam G", "")
	if err != nil {
		t.Fatal(err)
	}
	if u1c.ID != u1.ID {
		t.Fatalf("same email on a new provider should reuse the account: %s vs %s", u1c.ID, u1.ID)
	}

	// brand-new person
	u2, err := a.upsertUser(ctx, "github", "gh-2", "alex@example.com", "Alex", "")
	if err != nil {
		t.Fatal(err)
	}
	if u2.ID == u1.ID {
		t.Fatal("distinct email must create a distinct user")
	}
}

func TestEnsureUserCredentialsIsIdempotent(t *testing.T) {
	a, _ := testAuth(t)
	ctx := context.Background()
	u, err := a.upsertUser(ctx, "github", "gh-1", "sam@example.com", "Sam", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := a.ensureUserCredentials(ctx, u.ID, u.Email); err != nil {
		t.Fatal(err)
	}
	if err := a.ensureUserCredentials(ctx, u.ID, u.Email); err != nil {
		t.Fatal(err)
	}
	key1, ok := a.keyIDForUser(ctx, u.ID)
	if !ok || key1 == "" {
		t.Fatal("no consumer key minted")
	}
	tok1, ok := a.providerTokenForUser(ctx, u.ID)
	if !ok || tok1 == "" {
		t.Fatal("no provider token minted")
	}

	// second call must not create new rows
	var nk, nt int
	if err := a.pool.QueryRow(ctx, `SELECT count(*) FROM user_api_keys WHERE user_id=$1`, u.ID).Scan(&nk); err != nil {
		t.Fatal(err)
	}
	if err := a.pool.QueryRow(ctx, `SELECT count(*) FROM user_provider_tokens WHERE user_id=$1`, u.ID).Scan(&nt); err != nil {
		t.Fatal(err)
	}
	if nk != 1 || nt != 1 {
		t.Fatalf("idempotency broken: keys=%d tokens=%d", nk, nt)
	}

	// the provider token round-trips
	if uid, ok := a.userForProviderToken(ctx, tok1); !ok || uid != u.ID {
		t.Fatalf("provider token lookup failed: %s %v", uid, ok)
	}
	if !a.ValidProviderToken(ctx, tok1) || a.ValidProviderToken(ctx, "sc_prov_nope") {
		t.Fatal("ValidProviderToken wrong")
	}
}

func TestSessionCreateLookupExpire(t *testing.T) {
	a, _ := testAuth(t)
	ctx := context.Background()
	u, err := a.upsertUser(ctx, "github", "gh-1", "sam@example.com", "Sam", "")
	if err != nil {
		t.Fatal(err)
	}

	raw := randToken()
	if err := a.createSession(ctx, raw, u.ID); err != nil {
		t.Fatal(err)
	}
	r := httpGetWithCookie(sessionCookie, raw).WithContext(ctx)
	got, ok := a.SessionUser(r)
	if !ok || got.ID != u.ID {
		t.Fatalf("session lookup failed: %+v %v", got, ok)
	}

	// expire it in the DB
	if _, err := a.pool.Exec(ctx,
		`UPDATE sessions SET expires_at = now() - interval '1 hour' WHERE token_hash=$1`,
		hashToken(raw)); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.SessionUser(r); ok {
		t.Fatal("expired session still resolved")
	}
}

func TestLinkProviderDeviceAndEarnings(t *testing.T) {
	a, pg := testAuth(t)
	ctx := context.Background()
	u, err := a.upsertUser(ctx, "github", "gh-1", "sam@example.com", "Sam", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ensureUserCredentials(ctx, u.ID, u.Email); err != nil {
		t.Fatal(err)
	}
	tok, _ := a.providerTokenForUser(ctx, u.ID)

	const dev = "ZGV2LXBrLTAwMDAwMDAwMDAwMDAwMDAwMDAwMDA="
	a.LinkProviderDevice(ctx, tok, dev)
	a.LinkProviderDevice(ctx, tok, dev) // idempotent

	owed, jobs, devices, err := a.EarningsForUser(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if devices != 1 || owed != 0 || jobs != 0 {
		t.Fatalf("fresh device: owed=%d jobs=%d devices=%d", owed, jobs, devices)
	}

	// two accruals for that device
	for _, jid := range []string{"job-a", "job-b"} {
		if err := pg.RecordEarning(ctx, store.EarningEvent{
			JobID: jid, StaticPK: dev, KeyID: "k",
			Model: "m", ModelClass: "MICRO", Tier: "community",
			GrossMicros: 1000, ProviderMicros: 700,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// a duplicate accrual must not double-count (AYNI-002)
	if err := pg.RecordEarning(ctx, store.EarningEvent{
		JobID: "job-a", StaticPK: dev, KeyID: "k",
		Model: "m", ModelClass: "MICRO", Tier: "community",
		GrossMicros: 1000, ProviderMicros: 700,
	}); err != nil {
		t.Fatal(err)
	}

	owed, jobs, devices, err = a.EarningsForUser(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if owed != 1400 || jobs != 2 || devices != 1 {
		t.Fatalf("after 2 accruals: owed=%d jobs=%d devices=%d (want 1400/2/1)", owed, jobs, devices)
	}

	// earnings for an unrelated user are zero
	other, err := a.upsertUser(ctx, "github", "gh-2", "alex@example.com", "Alex", "")
	if err != nil {
		t.Fatal(err)
	}
	o2, _, d2, _ := a.EarningsForUser(ctx, other.ID)
	if o2 != 0 || d2 != 0 {
		t.Fatalf("cross-user leak: owed=%d devices=%d", o2, d2)
	}
}

func httpGetWithCookie(name, value string) *http.Request {
	r, _ := http.NewRequest(http.MethodGet, "http://app.test/", nil)
	r.AddCookie(&http.Cookie{Name: name, Value: value})
	return r
}
