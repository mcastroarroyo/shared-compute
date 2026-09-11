package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/auth"
)

// Referral links, founding-device recognition and the opt-in leaderboard
// (docs/GTM-COMMUNITY-PLAN.md, items 2 and 3).

const foundingDeviceCap = 1000

func (s *Server) joinBase() string {
	if b := strings.TrimRight(s.cfg.AppBaseURL, "/"); b != "" {
		return b
	}
	return "https://app.ayni-ai.com"
}

// GET /v1/me/referral
func (s *Server) handleMyReferral(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotFound, "accounts_disabled", "accounts are not enabled")
		return
	}
	u, ok := s.auth.SessionUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not signed in")
		return
	}
	code, err := s.auth.ReferralCode(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "referral_failed", "could not read referral code")
		return
	}
	users, devices, _ := s.auth.ReferralStats(r.Context(), u.ID)
	prof, _ := s.auth.GetProfile(r.Context(), u.ID)
	rank, _ := s.auth.FoundingRank(r.Context(), u.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"code":               code,
		"link":               s.joinBase() + "/join/?c=" + code,
		"referred_users":     users,
		"referred_devices":   devices,
		"display_name":       prof.DisplayName,
		"leaderboard_opt_in": prof.LeaderboardOptIn,
		"founding_rank":      rank,
		"founding":           rank > 0 && rank <= foundingDeviceCap,
		"founding_cap":       foundingDeviceCap,
	})
}

// POST /v1/me/referral/claim {"code": "..."}
func (s *Server) handleClaimReferral(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotFound, "accounts_disabled", "accounts are not enabled")
		return
	}
	u, ok := s.auth.SessionUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not signed in")
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", "could not parse body")
		return
	}
	applied, err := s.auth.ClaimReferral(r.Context(), u.ID, body.Code)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "referral_failed", "could not record referral")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"applied": applied})
}

// POST /v1/me/profile {"display_name": "...", "leaderboard_opt_in": true}
func (s *Server) handleMyProfile(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotFound, "accounts_disabled", "accounts are not enabled")
		return
	}
	u, ok := s.auth.SessionUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not signed in")
		return
	}
	var p auth.Profile
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", "could not parse body")
		return
	}
	if p.LeaderboardOptIn && strings.TrimSpace(p.DisplayName) == "" {
		writeError(w, http.StatusBadRequest, "name_required", "choose a display name to appear on the leaderboard")
		return
	}
	if err := s.auth.SetProfile(r.Context(), u.ID, p); err != nil {
		writeError(w, http.StatusInternalServerError, "profile_failed", "could not save profile")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GET /v1/leaderboard — public, aggregates and opted-in display names only.
var leaderboardCache struct {
	mu   sync.Mutex
	at   time.Time
	body map[string]any
}

func (s *Server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeJSON(w, http.StatusOK, map[string]any{"rows": []any{}, "founding_devices": 0, "founding_cap": foundingDeviceCap})
		return
	}
	leaderboardCache.mu.Lock()
	defer leaderboardCache.mu.Unlock()
	if time.Since(leaderboardCache.at) < 30*time.Second && leaderboardCache.body != nil {
		w.Header().Set("Cache-Control", "public, max-age=30")
		writeJSON(w, http.StatusOK, leaderboardCache.body)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := s.auth.Leaderboard(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "leaderboard_failed", "could not read leaderboard")
		return
	}
	founding, _ := s.auth.FoundingDevices(r.Context())
	body := map[string]any{
		"rows":             rows,
		"founding_devices": founding,
		"founding_cap":     foundingDeviceCap,
		"generated_at":     time.Now().UTC().Format(time.RFC3339),
	}
	leaderboardCache.at, leaderboardCache.body = time.Now(), body
	w.Header().Set("Cache-Control", "public, max-age=30")
	writeJSON(w, http.StatusOK, body)
}
