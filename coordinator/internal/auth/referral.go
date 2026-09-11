package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Referral links and the founders leaderboard. See migrations/0012_referrals.sql.

const referralAlphabet = "abcdefghjkmnpqrstuvwxyz23456789" // no 0/o/1/l/i

func newReferralCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, x := range b {
		sb.WriteByte(referralAlphabet[int(x)%len(referralAlphabet)])
	}
	return sb.String(), nil
}

// ReferralCode returns the user's invite code, minting one on first use.
func (a *Auth) ReferralCode(ctx context.Context, userID string) (string, error) {
	var code *string
	if err := a.pool.QueryRow(ctx, `SELECT referral_code FROM users WHERE id = $1`, userID).Scan(&code); err != nil {
		return "", err
	}
	if code != nil && *code != "" {
		return *code, nil
	}
	for attempt := 0; attempt < 5; attempt++ {
		c, err := newReferralCode()
		if err != nil {
			return "", err
		}
		tag, err := a.pool.Exec(ctx,
			`UPDATE users SET referral_code = $2 WHERE id = $1 AND referral_code IS NULL`, userID, c)
		if err != nil {
			continue // unique collision: try another code
		}
		if tag.RowsAffected() == 1 {
			return c, nil
		}
		// Someone else minted it concurrently; read it back.
		if err := a.pool.QueryRow(ctx, `SELECT referral_code FROM users WHERE id = $1`, userID).Scan(&code); err == nil && code != nil {
			return *code, nil
		}
	}
	return "", errors.New("could not mint a referral code")
}

// ClaimReferral records who invited the user. It applies once: a second claim, a
// claim of the user's own code, or an unknown code all return false with no error.
func (a *Auth) ClaimReferral(ctx context.Context, userID, code string) (bool, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return false, nil
	}
	var referrer string
	err := a.pool.QueryRow(ctx, `SELECT id FROM users WHERE referral_code = $1`, code).Scan(&referrer)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if referrer == userID {
		return false, nil
	}
	tag, err := a.pool.Exec(ctx,
		`UPDATE users SET referred_by = $2 WHERE id = $1 AND referred_by = ''`, userID, referrer)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ReferralStats counts the accounts a user invited and the devices those accounts linked.
func (a *Auth) ReferralStats(ctx context.Context, userID string) (users, devices int, err error) {
	err = a.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM users r WHERE r.referred_by = $1),
		       (SELECT count(*) FROM provider_owners o JOIN users r ON r.id = o.user_id WHERE r.referred_by = $1)`,
		userID).Scan(&users, &devices)
	return
}

// Profile is what a user chooses to show on the leaderboard.
type Profile struct {
	DisplayName      string `json:"display_name"`
	LeaderboardOptIn bool   `json:"leaderboard_opt_in"`
}

func (a *Auth) GetProfile(ctx context.Context, userID string) (Profile, error) {
	var p Profile
	err := a.pool.QueryRow(ctx, `SELECT display_name, leaderboard_opt_in FROM users WHERE id = $1`, userID).
		Scan(&p.DisplayName, &p.LeaderboardOptIn)
	return p, err
}

func (a *Auth) SetProfile(ctx context.Context, userID string, p Profile) error {
	name := strings.TrimSpace(p.DisplayName)
	if len(name) > 40 {
		name = name[:40]
	}
	_, err := a.pool.Exec(ctx,
		`UPDATE users SET display_name = $2, leaderboard_opt_in = $3 WHERE id = $1`, userID, name, p.LeaderboardOptIn)
	return err
}

// LeaderRow is one public leaderboard entry. Only opted-in users appear, under the
// display name they chose; no email, id or device identifier is exposed.
type LeaderRow struct {
	Rank            int    `json:"rank"`
	DisplayName     string `json:"display_name"`
	Devices         int    `json:"devices"`
	ReferredUsers   int    `json:"referred_users"`
	ReferredDevices int    `json:"referred_devices"`
}

func (a *Auth) Leaderboard(ctx context.Context, limit int) ([]LeaderRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := a.pool.Query(ctx, `
		SELECT u.display_name,
		       (SELECT count(*) FROM provider_owners o WHERE o.user_id = u.id) AS devices,
		       (SELECT count(*) FROM users r WHERE r.referred_by = u.id) AS ref_users,
		       (SELECT count(*) FROM provider_owners o JOIN users r ON r.id = o.user_id WHERE r.referred_by = u.id) AS ref_devices
		FROM users u
		WHERE u.leaderboard_opt_in AND u.display_name <> ''
		ORDER BY ref_devices DESC, ref_users DESC, devices DESC, u.created_at ASC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LeaderRow
	for rows.Next() {
		var r LeaderRow
		if err := rows.Scan(&r.DisplayName, &r.Devices, &r.ReferredUsers, &r.ReferredDevices); err != nil {
			return nil, err
		}
		r.Rank = len(out) + 1
		out = append(out, r)
	}
	if out == nil {
		out = []LeaderRow{}
	}
	return out, rows.Err()
}

// FoundingDevices is how many devices have ever been linked to an account.
func (a *Auth) FoundingDevices(ctx context.Context) (int, error) {
	var n int
	err := a.pool.QueryRow(ctx, `SELECT count(*) FROM provider_owners`).Scan(&n)
	return n, err
}

// FoundingRank is the 1-based join order of a user's first device, or 0 if none.
func (a *Auth) FoundingRank(ctx context.Context, userID string) (int, error) {
	var n int
	err := a.pool.QueryRow(ctx, `
		SELECT COALESCE((SELECT count(*) FROM provider_owners o
		                 WHERE o.first_seen <= (SELECT min(first_seen) FROM provider_owners WHERE user_id = $1)), 0)
		WHERE EXISTS (SELECT 1 FROM provider_owners WHERE user_id = $1)`, userID).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("founding rank: %w", err)
	}
	return n, nil
}
