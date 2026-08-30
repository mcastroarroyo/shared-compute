-- 0001_init: consumer keys, provider tokens, providers, usage events.
-- No prompt/completion content is ever stored here.

CREATE TABLE IF NOT EXISTS consumer_api_keys (
    id           TEXT PRIMARY KEY,            -- stable public id, e.g. key_ab12cd34
    key_sha256   BYTEA NOT NULL UNIQUE,       -- sha256 of the raw key
    label        TEXT NOT NULL DEFAULT '',
    disabled     BOOLEAN NOT NULL DEFAULT FALSE,
    rate_per_min INTEGER NOT NULL DEFAULT 60,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS provider_tokens (
    token_sha256 BYTEA PRIMARY KEY,           -- sha256 of the raw registration token
    label        TEXT NOT NULL DEFAULT '',
    disabled     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS providers (
    provider_id  UUID PRIMARY KEY,
    static_pk    TEXT NOT NULL,               -- base64 X25519 public key
    platform     TEXT NOT NULL DEFAULT '',
    arch         TEXT NOT NULL DEFAULT '',
    backend      TEXT NOT NULL DEFAULT '',
    first_seen   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS providers_static_pk_idx ON providers (static_pk);

CREATE TABLE IF NOT EXISTS usage_events (
    id                BIGSERIAL PRIMARY KEY,
    key_id            TEXT NOT NULL,
    provider_id       UUID,
    model             TEXT NOT NULL,
    tier              TEXT NOT NULL DEFAULT 'community',
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    duration_ms       BIGINT NOT NULL DEFAULT 0,
    finish_reason     TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS usage_events_key_created_idx ON usage_events (key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS usage_events_created_idx ON usage_events (created_at DESC);
