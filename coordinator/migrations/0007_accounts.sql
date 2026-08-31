-- User accounts and sessions (OAuth sign-in). Postgres-only; the memory store
-- has no auth. No password is ever stored — identity is an OAuth provider.

CREATE TABLE IF NOT EXISTS users (
    id         TEXT PRIMARY KEY,
    email      TEXT NOT NULL,
    name       TEXT NOT NULL DEFAULT '',
    avatar_url TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS users_email_idx ON users (lower(email));

CREATE TABLE IF NOT EXISTS oauth_identities (
    provider         TEXT NOT NULL,          -- github | google
    provider_user_id TEXT NOT NULL,
    user_id          TEXT NOT NULL REFERENCES users(id),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, provider_user_id)
);

CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,             -- sha-256 of the cookie value
    user_id    TEXT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_user_idx ON sessions (user_id);

-- One consumer API key and one provider registration token minted per user on
-- first sign-in. The app authenticates with the session cookie; the coordinator
-- resolves it to this key id.
CREATE TABLE IF NOT EXISTS user_api_keys (
    user_id    TEXT PRIMARY KEY REFERENCES users(id),
    key_id     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- One provider token per user, stored raw (low-value credential: it only lets a
-- machine join the network as that account, and the app must display it).
CREATE TABLE IF NOT EXISTS user_provider_tokens (
    user_id    TEXT PRIMARY KEY REFERENCES users(id),
    token      TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Which account owns a connected provider device, for earnings roll-up.
CREATE TABLE IF NOT EXISTS provider_owners (
    static_pk  TEXT PRIMARY KEY,             -- base64 X25519 static key
    user_id    TEXT NOT NULL REFERENCES users(id),
    first_seen TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS provider_owners_user_idx ON provider_owners (user_id);
