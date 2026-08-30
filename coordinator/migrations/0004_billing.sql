-- Consumer prepaid credits and provider payout wiring (docs/PAYMENTS.md).
-- All money is integer micro-USD.

CREATE TABLE IF NOT EXISTS billing_accounts (
    key_id            TEXT PRIMARY KEY,
    credit_micros     BIGINT NOT NULL DEFAULT 0,
    stripe_customer   TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS credit_ledger (
    id                BIGSERIAL PRIMARY KEY,
    key_id            TEXT NOT NULL,
    delta_micros      BIGINT NOT NULL,
    reason            TEXT NOT NULL,               -- topup | debit | adjust
    stripe_ref        TEXT NOT NULL DEFAULT '',    -- checkout session / payment intent
    job_id            TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Idempotency for webhook credits: one topup per stripe_ref.
CREATE UNIQUE INDEX IF NOT EXISTS credit_ledger_topup_ref_idx
    ON credit_ledger (stripe_ref) WHERE reason = 'topup' AND stripe_ref <> '';
CREATE INDEX IF NOT EXISTS credit_ledger_key_idx ON credit_ledger (key_id, created_at DESC);

CREATE TABLE IF NOT EXISTS provider_payout_accounts (
    static_pk         TEXT PRIMARY KEY,           -- base64 X25519 provider identity
    stripe_account    TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'pending', -- pending | enabled | disabled
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS payouts (
    id                BIGSERIAL PRIMARY KEY,
    static_pk         TEXT NOT NULL,
    stripe_account    TEXT NOT NULL,
    amount_micros     BIGINT NOT NULL,
    stripe_transfer   TEXT NOT NULL DEFAULT '',
    state             TEXT NOT NULL DEFAULT 'created', -- created | failed
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Link earnings rows to the provider identity (static_pk) so payouts can aggregate
-- them. Older rows only have provider_id (the ephemeral registration uuid).
ALTER TABLE provider_earnings ADD COLUMN IF NOT EXISTS static_pk TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS provider_earnings_staticpk_state_idx
    ON provider_earnings (static_pk, state);
