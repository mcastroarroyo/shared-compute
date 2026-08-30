package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// admin routes are mounted only when SC_ADMIN_TOKEN is set. They back the web console:
// API-key CRUD, connected-provider list, usage rollup. Never returns prompt content.
func (s *Server) mountAdmin(mux *http.ServeMux) {
	if s.cfg.AdminToken == "" {
		return
	}
	mux.Handle("POST /admin/keys", s.withAdmin(s.adminCreateKey))
	mux.Handle("GET /admin/keys", s.withAdmin(s.adminListKeys))
	mux.Handle("POST /admin/keys/{id}/disable", s.withAdmin(s.adminSetKey(true)))
	mux.Handle("POST /admin/keys/{id}/enable", s.withAdmin(s.adminSetKey(false)))
	mux.Handle("GET /admin/providers", s.withAdmin(s.adminProviders))
	mux.Handle("GET /admin/usage", s.withAdmin(s.adminUsage))
}

func (s *Server) withAdmin(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := bearer(r)
		if tok == "" {
			tok = r.Header.Get("X-Admin-Token")
		}
		if subtle.ConstantTimeCompare([]byte(tok), []byte(s.cfg.AdminToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "admin token required")
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", "*") // console is a separate origin
		next(w, r)
	})
}

func (s *Server) adminCreateKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label string `json:"label"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	id, raw, err := s.store.CreateConsumerKey(r.Context(), strings.TrimSpace(body.Label))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create_failed", "could not create key")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "key": raw, "label": body.Label})
}

func (s *Server) adminListKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.store.ListConsumerKeys(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", "could not list keys")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

func (s *Server) adminSetKey(disabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := s.store.SetConsumerKeyDisabled(r.Context(), id, disabled); err != nil {
			writeError(w, http.StatusNotFound, "not_found", "no such key")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "disabled": disabled})
	}
}

type providerView struct {
	ID            string   `json:"id"`
	Platform      string   `json:"platform"`
	Arch          string   `json:"arch"`
	Backend       string   `json:"backend"`
	HardwareClass string   `json:"hardware_class"`
	TrustTier     string   `json:"trust_tier"`
	Models        []string `json:"models"`
	ActiveJobs    int      `json:"active_jobs"`
	RAMMB         int      `json:"ram_mb"`
	ThermalState  string   `json:"thermal_state"`
	ConnectedFor  string   `json:"connected_for"`
}

func (s *Server) adminProviders(w http.ResponseWriter, _ *http.Request) {
	var out []providerView
	for _, p := range s.reg.Snapshot() {
		active, _ := p.Load()
		out = append(out, providerView{
			ID: p.ID, Platform: p.Capabilities.Platform, Arch: p.Capabilities.Arch,
			Backend: p.Capabilities.Backend, HardwareClass: p.Capabilities.HardwareClass,
			TrustTier: p.TrustTier, Models: p.Capabilities.Models, ActiveJobs: active,
			RAMMB: p.Capabilities.RAMMB, ThermalState: p.Telemetry.ThermalState,
			ConnectedFor: time.Since(p.ConnectedAt).Round(time.Second).String(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": out, "count": len(out)})
}

func (s *Server) adminUsage(w http.ResponseWriter, r *http.Request) {
	hours := 24
	if h := r.URL.Query().Get("since_hours"); h != "" {
		if n, err := strconv.Atoi(h); err == nil && n > 0 && n <= 24*90 {
			hours = n
		}
	}
	rows, err := s.store.UsageSince(r.Context(), time.Now().Add(-time.Duration(hours)*time.Hour))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "usage_failed", "could not read usage")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"since_hours": hours, "rows": rows})
}
