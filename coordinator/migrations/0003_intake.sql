-- Public intake from the marketing site: waitlist signups and initiative proposals.
CREATE TABLE IF NOT EXISTS waitlist (
    id         BIGSERIAL PRIMARY KEY,
    email      TEXT NOT NULL,
    interest   TEXT NOT NULL DEFAULT 'both',
    note       TEXT NOT NULL DEFAULT '',
    ip_hash    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS waitlist_email_idx ON waitlist (lower(email));

CREATE TABLE IF NOT EXISTS proposals (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    email      TEXT NOT NULL,
    title      TEXT NOT NULL,
    summary    TEXT NOT NULL,
    link       TEXT NOT NULL DEFAULT '',
    ip_hash    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS proposals_created_idx ON proposals (created_at DESC);
