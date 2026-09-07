package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
)

// Pairing codes let a phone join an account without typing a 72-character
// token: the Share page mints a 6-character code, the app redeems it once.
// Codes are hashed at rest, expire after PairCodeTTL and are single-use.

const (
	PairCodeTTL    = 10 * time.Minute
	pairCodeLen    = 6
	pairCodeAlpha  = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O/1/I
	pairCodeMaxAge = 24 * time.Hour                     // GC horizon for used/expired rows
)

// ErrPairCode is returned when a code is unknown, used or expired.
var ErrPairCode = errors.New("pairing code invalid or expired")

func hashPairCode(code string) string {
	sum := sha256.Sum256([]byte(NormalizePairCode(code)))
	return hex.EncodeToString(sum[:])
}

// NormalizePairCode upper-cases and strips spaces/dashes so "7kq4-m2" == "7KQ4M2".
func NormalizePairCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '_' {
			return -1
		}
		return r
	}, code)
}

func newPairCode() (string, error) {
	b := make([]byte, pairCodeLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, pairCodeLen)
	for i, v := range b {
		out[i] = pairCodeAlpha[int(v)%len(pairCodeAlpha)]
	}
	return string(out), nil
}

// CreatePairCode mints a fresh code for the user and returns it with its expiry.
func (a *Auth) CreatePairCode(ctx context.Context, userID string) (code string, expires time.Time, err error) {
	code, err = newPairCode()
	if err != nil {
		return "", time.Time{}, err
	}
	expires = time.Now().Add(PairCodeTTL)
	_, err = a.pool.Exec(ctx, `
		INSERT INTO pairing_codes (code_hash, user_id, expires_at) VALUES ($1,$2,$3)`,
		hashPairCode(code), userID, expires)
	if err != nil {
		return "", time.Time{}, err
	}
	// Opportunistic GC so the table never grows unbounded.
	_, _ = a.pool.Exec(ctx, `DELETE FROM pairing_codes WHERE created_at < now() - $1::interval`,
		pairCodeMaxAge.String())
	return code, expires, nil
}

// RedeemPairCode consumes a code and returns the owning account's provider
// token plus a display hint (the account email) so the app can confirm.
func (a *Auth) RedeemPairCode(ctx context.Context, code string) (token, email string, err error) {
	if len(NormalizePairCode(code)) != pairCodeLen {
		return "", "", ErrPairCode
	}
	var uid string
	err = a.pool.QueryRow(ctx, `
		UPDATE pairing_codes SET used_at = now()
		WHERE code_hash = $1 AND used_at IS NULL AND expires_at > now()
		RETURNING user_id`, hashPairCode(code)).Scan(&uid)
	if err != nil {
		return "", "", ErrPairCode
	}
	token, ok := a.providerTokenForUser(ctx, uid)
	if !ok || token == "" {
		return "", "", errors.New("could not mint provider token")
	}
	_ = a.pool.QueryRow(ctx, `SELECT email FROM users WHERE id=$1`, uid).Scan(&email)
	return token, email, nil
}

// DevicePKsForUser lists every provider identity ever linked to the account,
// most recently seen first.
func (a *Auth) DevicePKsForUser(ctx context.Context, userID string) ([]string, error) {
	rows, err := a.pool.Query(ctx, `
		SELECT static_pk FROM provider_owners WHERE user_id=$1 ORDER BY last_seen DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var pk string
		if err := rows.Scan(&pk); err != nil {
			return nil, err
		}
		out = append(out, pk)
	}
	return out, rows.Err()
}

// handlePairCreate: POST /v1/me/pair (session) -> {code, expires_at}
func (a *Auth) handlePairCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := a.SessionUser(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"error": "not signed in"})
		return
	}
	code, exp, err := a.CreatePairCode(r.Context(), u.ID)
	if err != nil {
		a.log.Warn("create pair code", "err", err)
		writeJSON(w, 500, map[string]any{"error": "could not create code"})
		return
	}
	writeJSON(w, 200, map[string]any{
		"code": code, "expires_at": exp.UTC().Format(time.RFC3339),
		"ttl_seconds": int(PairCodeTTL.Seconds()),
	})
}
