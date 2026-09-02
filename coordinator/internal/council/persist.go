package council

import "context"

// Persister durably stores the public decision ledger and post-run reviews so
// they survive a coordinator redeploy. Without one (the default), everything
// stays in-process only — fine for local dev, a demo, or a DB-less deployment.
// The coordinator wires a Postgres-backed implementation in internal/api when
// SC_DATABASE_URL is set (see internal/api/council_persist.go), following the
// same "small dedicated pool" pattern as internal/auth rather than routing
// through store.Store, since this is a narrow, optional concern.
type Persister interface {
	SaveDecision(ctx context.Context, rec PublicDecision) error
	// LoadDecisions returns up to limit of the most recent decisions, oldest first.
	LoadDecisions(ctx context.Context, limit int) ([]PublicDecision, error)
	SaveRunReview(ctx context.Context, rr RunReview) error
	// LoadRunReviews returns up to limit of the most recent reviews, oldest first.
	LoadRunReviews(ctx context.Context, limit int) ([]RunReview, error)
}

var persister Persister

// SetPersister wires durable storage. Call once at startup, before Hydrate.
func SetPersister(p Persister) { persister = p }

// Hydrate loads the most recent decisions and run reviews from the persister
// into the in-process ledger, so a redeploy doesn't reset the observatory to
// empty and so the next recorded decision's hash chain links onto the real
// tail instead of the seeded demo data. Best-effort: call before serving
// traffic; a load error just means starting from an empty ledger, same as today.
func Hydrate(ctx context.Context) {
	if persister == nil {
		return
	}
	if ds, err := persister.LoadDecisions(ctx, maxRecent); err == nil {
		recentMu.Lock()
		recent = ds
		recentMu.Unlock()
	}
	if rs, err := persister.LoadRunReviews(ctx, maxRecent); err == nil {
		runsMu.Lock()
		runReviews = rs
		runsMu.Unlock()
	}
}
