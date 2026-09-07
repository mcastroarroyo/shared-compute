-- Two-way tester channel: the Ayni team answers feedback in place; testers see
-- the reply on app.ayni-ai.com/testers (and from the phone app's feedback link).
CREATE TABLE IF NOT EXISTS feedback_replies (
    id          BIGSERIAL PRIMARY KEY,
    feedback_id BIGINT NOT NULL REFERENCES feedback(id) ON DELETE CASCADE,
    author      TEXT NOT NULL DEFAULT 'ayni',   -- ayni | tester
    body        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS feedback_replies_fk_idx ON feedback_replies (feedback_id, created_at);
CREATE INDEX IF NOT EXISTS feedback_user_idx ON feedback (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS feedback_email_idx ON feedback (lower(email), created_at DESC);
