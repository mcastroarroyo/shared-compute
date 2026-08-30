-- Shadow-accrual ledger for provider compute payments (docs/PAYMENTS.md, Phase 1).
-- No money moves yet; payout runs (Stripe Connect) read this once wired.
CREATE TABLE IF NOT EXISTS provider_earnings (
    id              BIGSERIAL PRIMARY KEY,
    provider_id     UUID,
    key_id          TEXT NOT NULL DEFAULT '',
    model           TEXT NOT NULL DEFAULT '',
    model_class     TEXT NOT NULL DEFAULT '',
    tier            TEXT NOT NULL DEFAULT 'community',
    gross_micros    BIGINT NOT NULL DEFAULT 0,   -- consumer charge, micro-USD
    provider_micros BIGINT NOT NULL DEFAULT 0,   -- provider accrual, micro-USD
    state           TEXT NOT NULL DEFAULT 'accrued', -- accrued|vested|paid|reversed
    payout_id       BIGINT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS provider_earnings_provider_idx
    ON provider_earnings (provider_id, created_at DESC);
CREATE INDEX IF NOT EXISTS provider_earnings_state_idx
    ON provider_earnings (state);
