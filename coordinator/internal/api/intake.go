package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/store"
)

// Public, unauthenticated intake from the marketing site. Rate-limited per client
// IP, honeypot-guarded, length-capped. Never trusts anything it stores.

const (
	maxEmail   = 254
	maxNote    = 2000
	maxTitle   = 200
	maxSummary = 4000
	maxLink    = 500
	maxName    = 120
)

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("Fly-Client-IP"); xff != "" {
		return xff
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func ipHash(ip string) string {
	sum := sha256.Sum256([]byte("ayni-ip:" + ip))
	return hex.EncodeToString(sum[:8])
}

func looksLikeEmail(s string) bool {
	at := strings.IndexByte(s, '@')
	return at > 0 && at < len(s)-3 && strings.IndexByte(s[at+1:], '.') > 0 && !strings.ContainsAny(s, " \t\r\n")
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (s *Server) intakeAllowed(r *http.Request) bool {
	allowed, _ := s.intakeRL.Allow("intake:" + clientIP(r))
	return allowed
}

type waitlistBody struct {
	Email    string `json:"email"`
	Interest string `json:"interest"`
	Note     string `json:"note"`
	HP       string `json:"hp"`
}

func (s *Server) handleWaitlist(w http.ResponseWriter, r *http.Request) {
	if !s.intakeAllowed(r) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "slow down")
		return
	}
	var b waitlistBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&b); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", "could not parse body")
		return
	}
	if b.HP != "" { // honeypot filled → silently accept, store nothing
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	email := clip(b.Email, maxEmail)
	if !looksLikeEmail(email) {
		writeError(w, http.StatusBadRequest, "bad_email", "enter a valid email")
		return
	}
	interest := clip(b.Interest, 20)
	switch interest {
	case "share", "rent", "both", "initiatives":
	default:
		interest = "both"
	}
	err := s.store.AddWaitlist(context.WithoutCancel(r.Context()), store.WaitlistEntry{
		Email: email, Interest: interest, Note: clip(b.Note, maxNote), IPHash: ipHash(clientIP(r)),
	})
	if err != nil {
		s.log.Warn("waitlist insert failed", "err", err)
		writeError(w, http.StatusInternalServerError, "store_failed", "could not save")
		return
	}
	s.log.Info("waitlist signup", "interest", interest)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type initiativeBody struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Link    string `json:"link"`
	HP      string `json:"hp"`
}

func (s *Server) handleInitiative(w http.ResponseWriter, r *http.Request) {
	if !s.intakeAllowed(r) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "slow down")
		return
	}
	var b initiativeBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32*1024)).Decode(&b); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", "could not parse body")
		return
	}
	if b.HP != "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	email := clip(b.Email, maxEmail)
	title := clip(b.Title, maxTitle)
	summary := clip(b.Summary, maxSummary)
	if !looksLikeEmail(email) || title == "" || len(summary) < 20 {
		writeError(w, http.StatusBadRequest, "incomplete", "email, title and a real summary are required")
		return
	}
	err := s.store.AddProposal(context.WithoutCancel(r.Context()), store.Proposal{
		Name: clip(b.Name, maxName), Email: email, Title: title, Summary: summary,
		Link: clip(b.Link, maxLink), IPHash: ipHash(clientIP(r)),
	})
	if err != nil {
		s.log.Warn("proposal insert failed", "err", err)
		writeError(w, http.StatusInternalServerError, "store_failed", "could not save")
		return
	}
	s.log.Info("initiative proposed", "title", title)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) adminWaitlist(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.WaitlistSince(r.Context(), time.Now().AddDate(-10, 0, 0))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_failed", "could not read waitlist")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(rows), "rows": rows})
}

func (s *Server) adminProposals(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ProposalsSince(r.Context(), time.Now().AddDate(-10, 0, 0))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_failed", "could not read proposals")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(rows), "rows": rows})
}
