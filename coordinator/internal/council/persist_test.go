package council

import (
	"context"
	"testing"
)

// fakePersister is an in-memory stand-in so these tests need no database.
type fakePersister struct {
	decisions []PublicDecision
	reviews   []RunReview
}

func (f *fakePersister) SaveDecision(_ context.Context, rec PublicDecision) error {
	f.decisions = append(f.decisions, rec)
	return nil
}
func (f *fakePersister) LoadDecisions(_ context.Context, limit int) ([]PublicDecision, error) {
	return lastN(f.decisions, limit), nil
}
func (f *fakePersister) SaveRunReview(_ context.Context, rr RunReview) error {
	f.reviews = append(f.reviews, rr)
	return nil
}
func (f *fakePersister) LoadRunReviews(_ context.Context, limit int) ([]RunReview, error) {
	return lastN(f.reviews, limit), nil
}

func lastN[T any](s []T, n int) []T {
	if len(s) <= n {
		return append([]T(nil), s...)
	}
	return append([]T(nil), s[len(s)-n:]...)
}

// resetCouncilGlobals isolates a test from the package-level ledger state that
// SetPersister/Hydrate/RecordWorkloadDecision/ReviewRun mutate, and restores it
// afterward so other tests in this package see the pre-existing (unpersisted)
// behavior.
func resetCouncilGlobals(t *testing.T) {
	t.Helper()
	prevPersister := persister
	prevRecent := recent
	prevReviews := runReviews
	persister = nil
	recent = nil
	runReviews = nil
	t.Cleanup(func() {
		persister = prevPersister
		recent = prevRecent
		runReviews = prevReviews
	})
}

func TestRecordWorkloadDecisionPersists(t *testing.T) {
	resetCouncilGlobals(t)
	fp := &fakePersister{}
	SetPersister(fp)

	o := Evaluate(context.Background(), normalProposal(), []Reviewer{approver("a"), approver("b"), approver("c")}, DefaultPolicy())
	RecordWorkloadDecision(context.Background(), o, "test workload")

	if len(fp.decisions) != 1 {
		t.Fatalf("expected 1 persisted decision, got %d", len(fp.decisions))
	}
	if len(recent) != 1 || recent[0].DecisionID != fp.decisions[0].DecisionID {
		t.Fatalf("in-process ledger and persisted record disagree: %+v vs %+v", recent, fp.decisions)
	}
}

func TestReviewRunPersists(t *testing.T) {
	resetCouncilGlobals(t)
	fp := &fakePersister{}
	SetPersister(fp)

	rr := ReviewRun(context.Background(), RunStats{WorkloadID: "w1", Model: "m", OK: 1})
	if len(fp.reviews) != 1 || fp.reviews[0].AuditHash != rr.AuditHash {
		t.Fatalf("run review not persisted: %+v", fp.reviews)
	}
}

func TestHydrateRestoresLedgerAndChainsOnward(t *testing.T) {
	resetCouncilGlobals(t)
	fp := &fakePersister{}

	// Simulate a decision recorded by a previous process, then a fresh boot.
	SetPersister(fp)
	o := Evaluate(context.Background(), normalProposal(), []Reviewer{approver("a"), approver("b"), approver("c")}, DefaultPolicy())
	RecordWorkloadDecision(context.Background(), o, "before restart")
	priorTail := recent[len(recent)-1]

	recent = nil // the in-process ledger a fresh process would start with
	Hydrate(context.Background())
	if len(recent) != 1 || recent[0].DecisionID != priorTail.DecisionID {
		t.Fatalf("Hydrate did not restore the persisted decision: %+v", recent)
	}

	// The next decision must chain onto the hydrated (real) tail, not the demo seed.
	RecordWorkloadDecision(context.Background(), o, "after restart")
	if recent[len(recent)-1].PreviousRecordHash != priorTail.RecordHash {
		t.Fatalf("post-hydrate decision did not chain onto the persisted tail: got prev=%s want=%s",
			recent[len(recent)-1].PreviousRecordHash, priorTail.RecordHash)
	}
}

func TestHydrateNoopWithoutPersister(t *testing.T) {
	resetCouncilGlobals(t)
	Hydrate(context.Background()) // persister is nil; must not panic or touch state
	if recent != nil || runReviews != nil {
		t.Fatalf("Hydrate without a persister should leave the ledger untouched")
	}
}
