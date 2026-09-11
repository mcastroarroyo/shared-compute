-- Referral links and the opt-in founders leaderboard (docs/GTM-COMMUNITY-PLAN.md).
-- A referral code is minted lazily per user; referred_by is set once, on the first
-- claim after sign-in, and never to the user's own code. Display names appear on the
-- public leaderboard only when leaderboard_opt_in is true.

ALTER TABLE users ADD COLUMN IF NOT EXISTS referral_code TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS referred_by TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS leaderboard_opt_in BOOLEAN NOT NULL DEFAULT false;

CREATE UNIQUE INDEX IF NOT EXISTS users_referral_code_idx ON users (referral_code) WHERE referral_code IS NOT NULL;
CREATE INDEX IF NOT EXISTS users_referred_by_idx ON users (referred_by);
