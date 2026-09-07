-- Short-lived pairing codes: a signed-in user mints a 6-character code on the
-- Share page; the phone app redeems it once for the account's provider token.
CREATE TABLE IF NOT EXISTS pairing_codes (
    code_hash  TEXT PRIMARY KEY,               -- sha256 of the upper-cased code
    user_id    TEXT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS pairing_codes_user_idx ON pairing_codes (user_id, created_at DESC);

-- Tester and user feedback from the web app, the phone app and the site.
CREATE TABLE IF NOT EXISTS feedback (
    id         BIGSERIAL PRIMARY KEY,
    kind       TEXT NOT NULL DEFAULT 'feedback', -- feedback | bug | tester_request
    email      TEXT NOT NULL DEFAULT '',
    device     TEXT NOT NULL DEFAULT '',         -- free text: "Pixel 9, Android 15", "MacBook M1"
    message    TEXT NOT NULL,
    app        TEXT NOT NULL DEFAULT '',         -- webapp | android | site | console
    version    TEXT NOT NULL DEFAULT '',
    user_id    TEXT NOT NULL DEFAULT '',
    ip_hash    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS feedback_created_idx ON feedback (created_at DESC);
