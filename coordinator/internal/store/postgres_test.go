package store

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

// Runs only when SC_TEST_DATABASE_URL points at a disposable Postgres.
func testPG(t *testing.T) *PG {
	t.Helper()
	url := os.Getenv("SC_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set SC_TEST_DATABASE_URL to run the Postgres store tests")
	}
	ctx := context.Background()
	pg, err := NewPG(ctx, url, slog.New(slog.NewTextHandler(os.Stderr, nil)),
		map[string]struct{}{"boot-key": {}}, map[string]struct{}{"boot-tok": {}})
	if err != nil {
		t.Fatal(err)
	}
	if err := pg.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pg.Close)
	return pg
}

func TestPGBootstrapAndDBKeys(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()

	if id, ok := pg.ValidateConsumerKey(ctx, "boot-key"); !ok || id == "" {
		t.Fatalf("bootstrap key not accepted")
	}
	if _, ok := pg.ValidateConsumerKey(ctx, "nope"); ok {
		t.Fatalf("unknown key accepted")
	}
	if !pg.ValidateProviderToken(ctx, "boot-tok") {
		t.Fatalf("bootstrap provider token not accepted")
	}

	// A DB-stored key also works.
	if _, err := pg.pool.Exec(ctx,
		`INSERT INTO consumer_api_keys (id, key_sha256) VALUES ($1,$2)
		 ON CONFLICT (key_sha256) DO NOTHING`, "key_dbtest", sha("db-key")); err != nil {
		t.Fatal(err)
	}
	if id, ok := pg.ValidateConsumerKey(ctx, "db-key"); !ok || id != "key_dbtest" {
		t.Fatalf("db key not resolved, got id=%q ok=%v", id, ok)
	}
}

func TestPGUpsertAndUsage(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()

	pid := "00000000-0000-0000-0000-0000000000aa"
	if err := pg.UpsertProvider(ctx, ProviderRecord{
		ProviderID: pid, StaticPK: "AAA", Platform: "linux", Arch: "x86_64", Backend: "cpu",
	}); err != nil {
		t.Fatal(err)
	}
	// idempotent
	if err := pg.UpsertProvider(ctx, ProviderRecord{
		ProviderID: pid, StaticPK: "BBB", Platform: "linux", Arch: "x86_64", Backend: "cuda",
	}); err != nil {
		t.Fatal(err)
	}

	if err := pg.RecordUsage(ctx, UsageEvent{
		KeyID: "key_dbtest", ProviderID: pid, Model: "m", Tier: "community",
		PromptTokens: 10, CompletionTokens: 20, DurationMS: 1234, FinishReason: "stop",
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := pg.pool.QueryRow(ctx,
		`SELECT count(*) FROM usage_events WHERE provider_id=$1 AND completion_tokens=20`, pid,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 usage row, got %d", n)
	}
}
