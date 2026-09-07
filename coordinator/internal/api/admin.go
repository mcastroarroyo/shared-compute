package api

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/capability"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/events"
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
	mux.Handle("POST /admin/providers/{id}/disconnect", s.withAdmin(s.adminDisconnectProvider))
	mux.Handle("POST /admin/drain", s.withAdmin(s.adminDrain))
	// IT-admin console (admin_console.go).
	mux.Handle("GET /admin/overview", s.withAdmin(s.adminOverview))
	mux.Handle("GET /admin/users", s.withAdmin(s.adminUsers))
	mux.Handle("GET /admin/events", s.withAdmin(s.adminEvents))
	mux.Handle("GET /admin/intake", s.withAdmin(s.adminGetIntake))
	mux.Handle("POST /admin/intake", s.withAdmin(s.adminSetIntake))
	mux.Handle("POST /admin/credit", s.withAdmin(s.adminCredit))
	mux.Handle("GET /admin/config", s.withAdmin(s.adminConfig))
	mux.Handle("GET /admin/nodes", s.withAdmin(s.adminNodes))
	mux.Handle("GET /admin/usage", s.withAdmin(s.adminUsage))
	mux.Handle("GET /admin/earnings", s.withAdmin(s.adminEarnings))
	mux.Handle("GET /admin/waitlist", s.withAdmin(s.adminWaitlist))
	mux.Handle("GET /admin/proposals", s.withAdmin(s.adminProposals))
	mux.Handle("POST /admin/payouts/connect", s.withAdmin(s.adminPayoutConnect))
	mux.Handle("POST /admin/payouts/connect/refresh", s.withAdmin(s.adminPayoutRefresh))
	mux.Handle("GET /admin/payouts/pending", s.withAdmin(s.adminPayoutsPending))
	mux.Handle("POST /admin/payouts/run", s.withAdmin(s.adminPayoutsRun))
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

// adminProviders (the connected-device list) lives in admin_console.go.

// adminNodes lists provider capability fingerprints with the derived ACU / class
// and whether the node is connected right now — the marketplace's view of supply.
func (s *Server) adminNodes(w http.ResponseWriter, r *http.Request) {
	caps, err := s.store.NodeCapabilities(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_failed", "could not read capabilities")
		return
	}
	online := map[string]bool{}
	for _, p := range s.reg.Snapshot() {
		online[base64.StdEncoding.EncodeToString(p.StaticPK[:])] = true
	}
	type node struct {
		StaticPK          string  `json:"static_pk"`
		Online            bool    `json:"online"`
		Model             string  `json:"model"`
		Backend           string  `json:"backend"`
		DecodeTPS         float64 `json:"decode_tps"`
		SustainedStartTPS float64 `json:"sustained_start_tps"`
		SustainedEndTPS   float64 `json:"sustained_end_tps"`
		ThermalDecayPct   float64 `json:"thermal_decay_pct"`
		MemBandwidthGBps  float64 `json:"mem_bandwidth_gbps"`
		AvailableRAMMB    uint64  `json:"available_ram_mb"`
		CPUCores          int     `json:"cpu_cores"`
		ACU               float64 `json:"acu"`
		Class             string  `json:"class"`
		UpdatedAt         string  `json:"updated_at"`
	}
	var out []node
	var totalACU float64
	for _, c := range caps {
		fp := capability.Fingerprint{
			Model: c.Model, Backend: c.Backend, PrefillTPS: c.PrefillTPS, DecodeTPS: c.DecodeTPS,
			SustainedStartTPS: c.SustainedStartTPS, SustainedEndTPS: c.SustainedEndTPS,
			MemBandwidthGBps: c.MemBandwidthGBps, AvailableRAMMB: c.AvailableRAMMB,
			CPUCores: c.CPUCores, ThermalState: c.ThermalState,
		}
		acu := capability.ACU(fp)
		if online[c.StaticPK] {
			totalACU += acu
		}
		out = append(out, node{
			StaticPK: c.StaticPK, Online: online[c.StaticPK], Model: c.Model, Backend: c.Backend,
			DecodeTPS: c.DecodeTPS, SustainedStartTPS: c.SustainedStartTPS,
			SustainedEndTPS: c.SustainedEndTPS, ThermalDecayPct: capability.ThermalDecayPct(fp),
			MemBandwidthGBps: c.MemBandwidthGBps, AvailableRAMMB: c.AvailableRAMMB,
			CPUCores: c.CPUCores, ACU: acu, Class: capability.Class(fp),
			UpdatedAt: c.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nodes": out, "count": len(out), "online_acu_total": capability.Round2(totalACU),
	})
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

// adminEarnings reports the provider-earnings shadow ledger (docs/PAYMENTS.md).
// Amounts are micro-USD; no money has moved.
func (s *Server) adminEarnings(w http.ResponseWriter, r *http.Request) {
	hours := 24 * 7
	if h := r.URL.Query().Get("since_hours"); h != "" {
		if n, err := strconv.Atoi(h); err == nil && n > 0 && n <= 24*365 {
			hours = n
		}
	}
	rows, err := s.store.EarningsSince(r.Context(), time.Now().Add(-time.Duration(hours)*time.Hour))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "earnings_failed", "could not read earnings")
		return
	}
	var grossTotal, provTotal int64
	for _, e := range rows {
		grossTotal += e.GrossMicros
		provTotal += e.ProviderMicros
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"since_hours":           hours,
		"shadow":                true,
		"rows":                  rows,
		"gross_micros_total":    grossTotal,
		"provider_micros_total": provTotal,
		"provider_usd_total":    float64(provTotal) / 1_000_000,
	})
}

// adminDrain disconnects every provider on THIS instance. Providers reconnect
// within seconds and land on whichever revision currently receives traffic.
// deploy.sh calls it on the previous revision after a rollout, because Cloud
// Run keeps a superseded instance alive while its WebSockets are open, which
// would otherwise strand every provider on an instance the scheduler no longer
// sees for up to the request timeout.
func (s *Server) adminDrain(w http.ResponseWriter, r *http.Request) {
	n := 0
	for _, p := range s.reg.Snapshot() {
		if p.Close != nil {
			p.Close("coordinator draining: reconnect")
			n++
		}
	}
	events.Audit("drain: all providers disconnected", map[string]string{
		"providers": strconv.Itoa(n), "actor": adminActor(r),
	})
	s.log.Info("admin drain", "providers", n)
	writeJSON(w, http.StatusOK, map[string]any{"drained": n})
}
