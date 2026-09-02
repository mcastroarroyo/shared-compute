-- Durable storage for the Ayni Council's public decision ledger and post-run
-- reviews (internal/council keeps them in-process only, which resets to empty
-- on every coordinator redeploy). Each row is the exact JSON already served at
-- GET /v1/council/decisions and GET /v1/council/run-reviews — content-free by
-- construction (see council.PublicDecision / council.RunReview) — so JSONB is
-- the right shape here rather than mirroring every field as a column.

CREATE TABLE IF NOT EXISTS council_decisions (
    decision_id  TEXT PRIMARY KEY,
    record       JSONB NOT NULL,
    recorded_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS council_decisions_recorded_at_idx ON council_decisions (recorded_at);

CREATE TABLE IF NOT EXISTS council_run_reviews (
    audit_hash   TEXT PRIMARY KEY,
    record       JSONB NOT NULL,
    recorded_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS council_run_reviews_recorded_at_idx ON council_run_reviews (recorded_at);
