package api

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/council"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/store"
)

// testCouncilStore needs a throwaway Postgres:
//
//	SC_TEST_DATABASE_URL=postgres://postgres@127.0.0.1:55432/ayni_test go test ./internal/api/...
func testCouncilStore(t *testing.T) *councilStore {
	t.Helper()
	dsn := os.Getenv("SC_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set SC_TEST_DATABASE_URL to run council persistence DB tests")
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
	t.Cleanup(pg.Close)

	cs := newCouncilStore(ctx, dsn, log)
	if cs == nil {
		t.Fatal("newCouncilStore returned nil with a valid DSN")
	}
	if _, err := cs.pool.Exec(ctx, "DELETE FROM council_decisions"); err != nil {
		t.Fatalf("clean council_decisions: %v", err)
	}
	if _, err := cs.pool.Exec(ctx, "DELETE FROM council_run_reviews"); err != nil {
		t.Fatalf("clean council_run_reviews: %v", err)
	}
	t.Cleanup(cs.Close)
	return cs
}

func TestCouncilStoreDecisionRoundTrip(t *testing.T) {
	cs := testCouncilStore(t)
	ctx := context.Background()

	rec := council.PublicDecision{
		Protocol: council.PublicRecordProtocol, DecisionID: "wl-abc123",
		ProposalID: "prop-1", Title: "test", RiskClass: "normal", Decision: "APPROVE",
		Severity: "LOW", Votes: map[string]int{"approve": 2}, EvidenceDigests: []string{"deadbeef"},
		PublishedAt: time.Now().Unix(), RecordHash: "hash-1",
	}
	if err := cs.SaveDecision(ctx, rec); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	// saving the same decision_id again must not duplicate or error (idempotent)
	if err := cs.SaveDecision(ctx, rec); err != nil {
		t.Fatalf("SaveDecision (dup): %v", err)
	}

	got, err := cs.LoadDecisions(ctx, 10)
	if err != nil {
		t.Fatalf("LoadDecisions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 decision (dedup by decision_id), got %d", len(got))
	}
	if got[0].DecisionID != rec.DecisionID || got[0].Decision != "APPROVE" || got[0].Votes["approve"] != 2 {
		t.Fatalf("round-tripped decision mismatch: %+v", got[0])
	}
}

func TestCouncilStoreDecisionsOrderedOldestFirst(t *testing.T) {
	cs := testCouncilStore(t)
	ctx := context.Background()

	base := time.Now().Add(-time.Hour)
	for i, id := range []string{"wl-1", "wl-2", "wl-3"} {
		rec := council.PublicDecision{DecisionID: id, PublishedAt: base.Add(time.Duration(i) * time.Minute).Unix()}
		if err := cs.SaveDecision(ctx, rec); err != nil {
			t.Fatalf("SaveDecision %s: %v", id, err)
		}
	}
	got, err := cs.LoadDecisions(ctx, 10)
	if err != nil {
		t.Fatalf("LoadDecisions: %v", err)
	}
	if len(got) != 3 || got[0].DecisionID != "wl-1" || got[2].DecisionID != "wl-3" {
		t.Fatalf("expected oldest-first wl-1..wl-3, got %v", ids(got))
	}

	// limit trims to the most recent N, still oldest-first
	limited, err := cs.LoadDecisions(ctx, 2)
	if err != nil {
		t.Fatalf("LoadDecisions limited: %v", err)
	}
	if len(limited) != 2 || limited[0].DecisionID != "wl-2" || limited[1].DecisionID != "wl-3" {
		t.Fatalf("expected the 2 most recent (wl-2, wl-3), got %v", ids(limited))
	}
}

func ids(ds []council.PublicDecision) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.DecisionID
	}
	return out
}

func TestCouncilStoreRunReviewRoundTrip(t *testing.T) {
	cs := testCouncilStore(t)
	ctx := context.Background()

	rr := council.RunReview{
		WorkloadID: "w1", Model: "m", Verdict: "CLEAN",
		Findings: []council.Finding{}, AuditHash: "audit-1", RecordedAt: time.Now().UTC(),
	}
	if err := cs.SaveRunReview(ctx, rr); err != nil {
		t.Fatalf("SaveRunReview: %v", err)
	}
	if err := cs.SaveRunReview(ctx, rr); err != nil { // idempotent on audit_hash
		t.Fatalf("SaveRunReview (dup): %v", err)
	}

	got, err := cs.LoadRunReviews(ctx, 10)
	if err != nil {
		t.Fatalf("LoadRunReviews: %v", err)
	}
	if len(got) != 1 || got[0].AuditHash != "audit-1" || got[0].Verdict != "CLEAN" {
		t.Fatalf("round-tripped run review mismatch: %+v", got)
	}
}
