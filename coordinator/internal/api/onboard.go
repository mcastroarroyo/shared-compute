package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/auth"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/events"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/store"
)

// Onboarding surface for new devices and testers:
//   POST /v1/pair/{code}   phone app redeems a pairing code -> provider token (public, rate-limited)
//   GET  /v1/me/devices    the signed-in user's devices, online and offline, for the Share page
//   POST /v1/feedback      tester / user feedback (public, rate-limited, honeypot)
//   GET  /admin/feedback   operator view of feedback

func (s *Server) handlePairRedeem(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotFound, "accounts_disabled", "accounts are not enabled")
		return
	}
	if !s.intakeAllowed(r) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts; wait a minute")
		return
	}
	code := r.PathValue("code")
	token, email, err := s.auth.RedeemPairCode(r.Context(), code)
	if err != nil {
		if err == auth.ErrPairCode {
			writeError(w, http.StatusNotFound, "bad_code", "that code is not valid any more; get a fresh one from the Share page")
			return
		}
		s.log.Warn("pair redeem failed", "err", err)
		writeError(w, http.StatusInternalServerError, "pair_failed", "could not complete pairing")
		return
	}
	events.Audit("device paired", map[string]string{"account": maskEmail(email), "ip_hash": ipHash(clientIP(r))})
	writeJSON(w, http.StatusOK, map[string]any{
		"registration_token": token,
		"account":            maskEmail(email),
		"coordinator_url":    strings.Replace(s.cfg.AuthCallbackBase, "https://", "wss://", 1) + "/ws/provider",
	})
}

func maskEmail(e string) string {
	at := strings.Index(e, "@")
	if at <= 1 {
		return e
	}
	return e[:1] + "…" + e[at-1:]
}

// myDevice is one of the signed-in user's provider identities.
type myDevice struct {
	StaticPK     string  `json:"static_pk"`
	Online       bool    `json:"online"`
	Kind         string  `json:"kind"` // phone | pc | other
	Platform     string  `json:"platform,omitempty"`
	Arch         string  `json:"arch,omitempty"`
	CPU          string  `json:"cpu,omitempty"`
	TrustTier    string  `json:"trust_tier,omitempty"`
	Class        string  `json:"class,omitempty"`
	ACU          float64 `json:"acu"`
	TPS          float64 `json:"tps"`
	RAMMB        int     `json:"ram_mb,omitempty"`
	ActiveJobs   int     `json:"active_jobs"`
	ConnectedFor string  `json:"connected_for,omitempty"`
	LastSeen     string  `json:"last_seen,omitempty"`
	JobsUnpaid   int     `json:"jobs_unpaid"`
	OwedUSD      float64 `json:"owed_usd"`
}

func (s *Server) handleMyDevices(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotFound, "accounts_disabled", "accounts are not enabled")
		return
	}
	u, ok := s.auth.SessionUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not signed in")
		return
	}
	pks, err := s.auth.DevicePKsForUser(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_failed", "could not list devices")
		return
	}
	mine := map[string]bool{}
	for _, pk := range pks {
		mine[pk] = true
	}
	online := map[string]deviceView{}
	for _, v := range s.deviceViews(r) {
		if mine[v.StaticPK] {
			online[v.StaticPK] = v
		}
	}
	facts := s.nodeFactsByPK(r)
	accr := s.accrualsByPK(r)
	out := []myDevice{}
	for _, pk := range pks {
		d := myDevice{StaticPK: pk, Kind: "other"}
		if v, ok := online[pk]; ok {
			d.Online = true
			d.Kind, d.Platform, d.Arch, d.CPU = v.Kind, v.Platform, v.Arch, v.CPU
			d.TrustTier, d.RAMMB, d.ActiveJobs, d.ConnectedFor = v.TrustTier, v.RAMMB, v.ActiveJobs, v.ConnectedFor
			d.LastSeen = v.LastSeen
		}
		if f, ok := facts[pk]; ok {
			d.ACU, d.Class = f.ACU, f.Class
			d.TPS = f.SustainedEndTPS
			if d.TPS == 0 {
				d.TPS = f.DecodeTPS
			}
			if d.LastSeen == "" {
				d.LastSeen = f.UpdatedAt.UTC().Format(time.RFC3339)
			}
		}
		if a, ok := accr[pk]; ok {
			d.JobsUnpaid, d.OwedUSD = a.Jobs, float64(a.OwedMicros)/1e6
		}
		out = append(out, d)
	}
	n := 0
	for _, d := range out {
		if d.Online {
			n++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": out, "online": n, "total": len(out)})
}

// --- feedback ---

type feedbackBody struct {
	Kind    string `json:"kind"`
	Email   string `json:"email"`
	Device  string `json:"device"`
	Message string `json:"message"`
	App     string `json:"app"`
	Version string `json:"version"`
	HP      string `json:"hp"`
}

func (s *Server) handleFeedback(w http.ResponseWriter, r *http.Request) {
	if !s.intakeAllowed(r) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "slow down")
		return
	}
	var b feedbackBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32*1024)).Decode(&b); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", "could not parse body")
		return
	}
	if b.HP != "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	msg := strings.TrimSpace(clip(b.Message, 4000))
	if len(msg) < 3 {
		writeError(w, http.StatusBadRequest, "empty", "write a message")
		return
	}
	kind := clip(strings.ToLower(b.Kind), 24)
	switch kind {
	case "feedback", "bug", "tester_request", "idea":
	default:
		kind = "feedback"
	}
	email := clip(strings.TrimSpace(b.Email), maxEmail)
	if email != "" && !looksLikeEmail(email) {
		writeError(w, http.StatusBadRequest, "bad_email", "enter a valid email or leave it empty")
		return
	}
	userID := ""
	if s.auth != nil {
		if u, ok := s.auth.SessionUser(r); ok {
			userID = u.ID
			if email == "" {
				email = u.Email
			}
		}
	}
	e := store.FeedbackEntry{
		Kind: kind, Email: email, Device: clip(b.Device, 120), Message: msg,
		App: clip(b.App, 24), Version: clip(b.Version, 40), UserID: userID, IPHash: ipHash(clientIP(r)),
	}
	if err := s.store.AddFeedback(context.WithoutCancel(r.Context()), e); err != nil {
		s.log.Warn("feedback insert failed", "err", err)
		writeError(w, http.StatusInternalServerError, "store_failed", "could not save")
		return
	}
	s.log.Info("feedback received", "kind", kind, "app", e.App, "has_email", email != "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleMyFeedback: GET /v1/me/feedback — the signed-in tester's own thread(s) with replies.
func (s *Server) handleMyFeedback(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotFound, "accounts_disabled", "accounts are not enabled")
		return
	}
	u, ok := s.auth.SessionUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not signed in")
		return
	}
	rows, err := s.store.FeedbackForUser(r.Context(), u.ID, u.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_failed", "could not read messages")
		return
	}
	if rows == nil {
		rows = []store.FeedbackEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"feedback": rows})
}

// handleMyFeedbackReply: POST /v1/me/feedback/{id}/reply — tester continues a thread they own.
func (s *Server) handleMyFeedbackReply(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotFound, "accounts_disabled", "accounts are not enabled")
		return
	}
	u, ok := s.auth.SessionUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not signed in")
		return
	}
	if !s.intakeAllowed(r) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "slow down")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_id", "bad feedback id")
		return
	}
	mine, err := s.store.FeedbackForUser(r.Context(), u.ID, u.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_failed", "could not read messages")
		return
	}
	owned := false
	for _, e := range mine {
		if e.ID == id {
			owned = true
			break
		}
	}
	if !owned {
		writeError(w, http.StatusNotFound, "not_found", "no such thread")
		return
	}
	var b struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&b); err != nil || len(strings.TrimSpace(b.Body)) < 2 {
		writeError(w, http.StatusBadRequest, "empty", "write a message")
		return
	}
	rep, err := s.store.AddFeedbackReply(context.WithoutCancel(r.Context()), id, "tester", clip(strings.TrimSpace(b.Body), 4000))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", "could not save")
		return
	}
	s.log.Info("tester replied", "feedback_id", id)
	writeJSON(w, http.StatusOK, map[string]any{"reply": rep})
}

// adminFeedbackReply: POST /admin/feedback/{id}/reply — the team answers a tester.
func (s *Server) adminFeedbackReply(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_id", "bad feedback id")
		return
	}
	var b struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&b); err != nil || len(strings.TrimSpace(b.Body)) < 2 {
		writeError(w, http.StatusBadRequest, "empty", "write a reply")
		return
	}
	rep, err := s.store.AddFeedbackReply(context.WithoutCancel(r.Context()), id, "ayni", clip(strings.TrimSpace(b.Body), 4000))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_failed", "could not save (does the feedback id exist?)")
		return
	}
	events.Audit("feedback answered", map[string]string{"feedback_id": strconv.FormatInt(id, 10), "actor": adminActor(r)})
	writeJSON(w, http.StatusOK, map[string]any{"reply": rep})
}

func (s *Server) adminFeedback(w http.ResponseWriter, r *http.Request) {
	since := time.Now().Add(-90 * 24 * time.Hour)
	rows, err := s.store.FeedbackSince(r.Context(), since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_failed", "could not read feedback")
		return
	}
	if rows == nil {
		rows = []store.FeedbackEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"feedback": rows, "count": len(rows)})
}
