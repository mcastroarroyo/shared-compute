package api

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/auth"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/capability"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/events"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/protocol"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/store"
)

// The IT-admin console surface: a one-call overview, an enriched device list
// (live provider joined with its benchmark, owner and ledger), account list,
// recent events, and the operator controls (pause intake, disconnect a device,
// adjust credit). Every control writes an AUDIT event. Nothing here returns
// prompt or completion content.

var startedAt = time.Now()

// deviceKind buckets a provider's platform the way an IT team thinks about
// fleet: phones, PCs/laptops, everything else.
func deviceKind(platform string) string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "android", "ios", "ipados":
		return "phone"
	case "macos", "darwin", "linux", "windows":
		return "pc"
	default:
		return "other"
	}
}

// intakeState is the maintenance switch. While paused, consumer inference
// routes answer 503 with the operator's message; providers stay connected.
type intakeState struct {
	mu      sync.RWMutex
	paused  bool
	message string
	since   time.Time
}

func (s *Server) intakePaused() (bool, string) {
	s.intake.mu.RLock()
	defer s.intake.mu.RUnlock()
	return s.intake.paused, s.intake.message
}

// gate short-circuits inference routes while intake is paused.
func (s *Server) gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if paused, msg := s.intakePaused(); paused {
			if msg == "" {
				msg = "Ayni is in maintenance; new work is not being accepted right now"
			}
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusServiceUnavailable, "maintenance", msg)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- shared lookups ---

type nodeFacts struct {
	ACU             float64
	Class           string
	DecodeTPS       float64
	SustainedEndTPS float64
	CPUCores        int
	AvailableRAMMB  uint64
	Model           string
	UpdatedAt       time.Time
}

func (s *Server) nodeFactsByPK(r *http.Request) map[string]nodeFacts {
	out := map[string]nodeFacts{}
	caps, err := s.store.NodeCapabilities(r.Context())
	if err != nil {
		return out
	}
	for _, c := range caps {
		fp := capability.Fingerprint{
			Model: c.Model, Backend: c.Backend, PrefillTPS: c.PrefillTPS, DecodeTPS: c.DecodeTPS,
			SustainedStartTPS: c.SustainedStartTPS, SustainedEndTPS: c.SustainedEndTPS,
			MemBandwidthGBps: c.MemBandwidthGBps, AvailableRAMMB: c.AvailableRAMMB,
			CPUCores: c.CPUCores, ThermalState: c.ThermalState,
		}
		out[c.StaticPK] = nodeFacts{
			ACU: capability.ACU(fp), Class: capability.Class(fp),
			DecodeTPS: c.DecodeTPS, SustainedEndTPS: c.SustainedEndTPS,
			CPUCores: c.CPUCores, AvailableRAMMB: c.AvailableRAMMB,
			Model: c.Model, UpdatedAt: c.UpdatedAt,
		}
	}
	return out
}

func (s *Server) ownersByPK(r *http.Request) map[string]auth.User {
	if s.auth == nil {
		return nil
	}
	m, err := s.auth.OwnersByStaticPK(r.Context())
	if err != nil {
		s.log.Warn("owners lookup failed", "err", err)
		return nil
	}
	return m
}

func (s *Server) accrualsByPK(r *http.Request) map[string]store.ProviderAccrual {
	out := map[string]store.ProviderAccrual{}
	rows, err := s.store.AccruedByProvider(r.Context())
	if err != nil {
		return out
	}
	for _, a := range rows {
		out[a.StaticPK] = a
	}
	return out
}

// --- devices ---

type ownerView struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
}

type deviceView struct {
	ID            string                        `json:"id"`
	StaticPK      string                        `json:"static_pk"`
	Kind          string                        `json:"kind"` // phone | pc | other
	Platform      string                        `json:"platform"`
	Arch          string                        `json:"arch"`
	CPU           string                        `json:"cpu,omitempty"`
	Backend       string                        `json:"backend"`
	HardwareClass string                        `json:"hardware_class"`
	TrustTier     string                        `json:"trust_tier"`
	Models        []string                      `json:"models"`
	MaxContext    int                           `json:"max_context"`
	ActiveJobs    int                           `json:"active_jobs"`
	Draining      bool                          `json:"draining"`
	RAMMB         int                           `json:"ram_mb"`
	ThermalState  string                        `json:"thermal_state"`
	Security      protocol.SecurityCapabilities `json:"security"`
	Telemetry     protocol.Telemetry            `json:"telemetry"`
	ConnectedAt   string                        `json:"connected_at"`
	LastSeen      string                        `json:"last_seen"`
	ConnectedFor  string                        `json:"connected_for"`
	Owner         *ownerView                    `json:"owner,omitempty"`
	// From the latest self-benchmark (0/"" until the node reports one).
	ACU             float64 `json:"acu"`
	Class           string  `json:"class"`
	DecodeTPS       float64 `json:"decode_tps"`
	SustainedEndTPS float64 `json:"sustained_end_tps"`
	CPUCores        int     `json:"cpu_cores"`
	BenchmarkAt     string  `json:"benchmark_at,omitempty"`
	// From the shadow ledger: jobs with an unpaid accrual and what is owed.
	JobsUnpaid int     `json:"jobs_unpaid"`
	OwedUSD    float64 `json:"owed_usd"`
}

func (s *Server) deviceViews(r *http.Request) []deviceView {
	facts := s.nodeFactsByPK(r)
	owners := s.ownersByPK(r)
	accr := s.accrualsByPK(r)
	var out []deviceView
	for _, p := range s.reg.Snapshot() {
		active, draining := p.Load()
		pk := base64.StdEncoding.EncodeToString(p.StaticPK[:])
		v := deviceView{
			ID: p.ID, StaticPK: pk, Kind: deviceKind(p.Capabilities.Platform),
			Platform: p.Capabilities.Platform, Arch: p.Capabilities.Arch, CPU: p.Capabilities.CPU,
			Backend: p.Capabilities.Backend, HardwareClass: p.Capabilities.HardwareClass,
			TrustTier: p.TrustTier, Models: p.Capabilities.Models, MaxContext: p.Capabilities.MaxContext,
			ActiveJobs: active, Draining: draining, RAMMB: p.Capabilities.RAMMB,
			ThermalState: p.Telemetry.ThermalState, Security: p.Capabilities.Security,
			Telemetry:    p.Telemetry,
			ConnectedAt:  p.ConnectedAt.UTC().Format(time.RFC3339),
			LastSeen:     p.LastSeen.UTC().Format(time.RFC3339),
			ConnectedFor: time.Since(p.ConnectedAt).Round(time.Second).String(),
		}
		if v.Models == nil {
			v.Models = []string{}
		}
		if f, ok := facts[pk]; ok {
			v.ACU, v.Class, v.DecodeTPS, v.SustainedEndTPS, v.CPUCores = f.ACU, f.Class, f.DecodeTPS, f.SustainedEndTPS, f.CPUCores
			v.BenchmarkAt = f.UpdatedAt.UTC().Format(time.RFC3339)
		}
		if u, ok := owners[pk]; ok {
			v.Owner = &ownerView{UserID: u.ID, Email: u.Email, Name: u.Name}
		}
		if a, ok := accr[pk]; ok {
			v.JobsUnpaid, v.OwedUSD = a.Jobs, float64(a.OwedMicros)/1e6
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ConnectedAt < out[j].ConnectedAt })
	return out
}

// adminProviders lists every connected device, enriched. The top-level keys
// ("providers", "count") are unchanged from the original endpoint.
func (s *Server) adminProviders(w http.ResponseWriter, r *http.Request) {
	out := s.deviceViews(r)
	if out == nil {
		out = []deviceView{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": out, "count": len(out)})
}

// --- overview ---

type kindCompute struct {
	Devices    int     `json:"devices"`
	ACU        float64 `json:"acu"`
	DecodeTPS  float64 `json:"decode_tps"`
	RAMMB      int     `json:"ram_mb"`
	ActiveJobs int     `json:"active_jobs"`
	Attested   int     `json:"attested"`
}

func (s *Server) adminOverview(w http.ResponseWriter, r *http.Request) {
	devs := s.deviceViews(r)

	byKind := map[string]int{"phone": 0, "pc": 0, "other": 0}
	byPlatform := map[string]int{}
	byTier := map[string]int{}
	byClass := map[string]int{}
	compute := map[string]*kindCompute{
		"phone": {}, "pc": {}, "other": {},
	}
	activeJobs := 0
	var totalACU float64
	onlineOwners := map[string]bool{}
	models := map[string]int{}
	for _, d := range devs {
		byKind[d.Kind]++
		byPlatform[d.Platform]++
		byTier[d.TrustTier]++
		if d.Class != "" {
			byClass[d.Class]++
		} else {
			byClass["unbenchmarked"]++
		}
		activeJobs += d.ActiveJobs
		totalACU += d.ACU
		c := compute[d.Kind]
		c.Devices++
		c.ACU += d.ACU
		c.DecodeTPS += d.SustainedEndTPS
		if c.DecodeTPS == 0 {
			c.DecodeTPS += d.DecodeTPS
		}
		c.RAMMB += d.RAMMB
		c.ActiveJobs += d.ActiveJobs
		if d.TrustTier != "" && d.TrustTier != "community" {
			c.Attested++
		}
		if d.Owner != nil {
			onlineOwners[d.Owner.UserID] = true
		}
		for _, m := range d.Models {
			models[m]++
		}
	}
	for _, c := range compute {
		c.ACU = capability.Round2(c.ACU)
		c.DecodeTPS = math.Round(c.DecodeTPS*10) / 10
	}

	// Known fleet = every node that ever benchmarked, online or not.
	known := len(s.nodeFactsByPK(r))

	// Accounts.
	usersTotal, usersWithDevices := 0, 0
	if s.auth != nil {
		if us, err := s.auth.ListUsers(r.Context()); err == nil {
			usersTotal = len(us)
			for _, u := range us {
				if u.Devices > 0 {
					usersWithDevices++
				}
			}
		}
	}

	// Traffic in the last 24h (metadata only).
	var req, ptok, ctok int64
	if rows, err := s.store.UsageSince(r.Context(), time.Now().Add(-24*time.Hour)); err == nil {
		for _, u := range rows {
			req += int64(u.Requests)
			ptok += u.PromptTokens
			ctok += u.CompletionTokens
		}
	}
	// Ledger in the last 7d.
	var gross, prov int64
	jobs7 := 0
	if rows, err := s.store.EarningsSince(r.Context(), time.Now().Add(-7*24*time.Hour)); err == nil {
		for _, e := range rows {
			gross += e.GrossMicros
			prov += e.ProviderMicros
			jobs7 += e.Jobs
		}
	}
	waitlist := 0
	if rows, err := s.store.WaitlistSince(r.Context(), time.Now().Add(-10*365*24*time.Hour)); err == nil {
		waitlist = len(rows)
	}
	warns, errs, lastErr := events.Default.Counts(time.Hour)
	paused, msg := s.intakePaused()
	s.intake.mu.RLock()
	since := s.intake.since
	s.intake.mu.RUnlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"coordinator": map[string]any{
			"uptime_s":  int(time.Since(startedAt).Seconds()),
			"revision":  buildRevision(),
			"instance":  os.Getenv("K_REVISION"),
			"database":  s.cfg.DatabaseURL != "",
			"accounts":  s.auth != nil,
			"stripe":    s.stripe != nil,
			"signing":   s.signer != nil,
			"heartbeat": s.cfg.HeartbeatSeconds,
		},
		"intake": map[string]any{
			"paused": paused, "message": msg,
			"since": nonZero(since),
		},
		"providers": map[string]any{
			"online":      len(devs),
			"known":       known,
			"active_jobs": activeJobs,
			"by_kind":     byKind,
			"by_platform": byPlatform,
			"by_tier":     byTier,
			"by_class":    byClass,
		},
		"users": map[string]any{
			"total":        usersTotal,
			"with_devices": usersWithDevices,
			"online":       len(onlineOwners),
		},
		"compute": map[string]any{
			"by_kind":          compute,
			"online_acu_total": capability.Round2(totalACU),
			"models":           models,
		},
		"traffic_24h": map[string]any{
			"requests": req, "prompt_tokens": ptok, "completion_tokens": ctok,
		},
		"ledger_7d": map[string]any{
			"jobs": jobs7, "gross_usd": float64(gross) / 1e6, "provider_usd": float64(prov) / 1e6,
		},
		"waitlist": waitlist,
		"events": map[string]any{
			"warnings_1h": warns, "errors_1h": errs, "last_error": lastErr, "stored": events.Default.Len(),
		},
	})
}

func nonZero(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func buildRevision() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 12 {
				return s.Value[:12]
			}
		}
	}
	return ""
}

// --- users ---

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"users": []any{}, "count": 0,
			"note": "accounts are disabled on this coordinator (no database / OAuth configured)",
		})
		return
	}
	us, err := s.auth.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "users_failed", "could not list users")
		return
	}
	online := map[string]int{}
	for _, d := range s.deviceViews(r) {
		if d.Owner != nil {
			online[d.Owner.UserID]++
		}
	}
	type row struct {
		auth.AdminUser
		CreditUSD     float64 `json:"credit_usd"`
		OnlineDevices int     `json:"online_devices"`
	}
	out := make([]row, 0, len(us))
	for _, u := range us {
		var credit int64
		if u.APIKeyID != "" {
			credit, _ = s.store.CreditBalance(r.Context(), u.APIKeyID)
		}
		out = append(out, row{AdminUser: u, CreditUSD: float64(credit) / 1e6, OnlineDevices: online[u.ID]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out, "count": len(out)})
}

// --- events ---

func (s *Server) adminEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	since, _ := strconv.ParseUint(q.Get("since_seq"), 10, 64)
	evs := events.Default.Recent(limit, q.Get("level"), q.Get("q"), since)
	if evs == nil {
		evs = []events.Event{}
	}
	warns, errs, _ := events.Default.Counts(time.Hour)
	writeJSON(w, http.StatusOK, map[string]any{
		"events": evs, "count": len(evs), "stored": events.Default.Len(),
		"warnings_1h": warns, "errors_1h": errs,
	})
}

// --- controls ---

func adminActor(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.TrimSpace(strings.Split(ip, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) adminDisconnectProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	reason := strings.TrimSpace(body.Reason)
	if reason == "" {
		reason = "disconnected by operator"
	}
	if len(reason) > 120 {
		reason = reason[:120]
	}
	p, ok := s.reg.Get(id)
	if !ok || p.Close == nil {
		writeError(w, http.StatusNotFound, "not_found", "no connected provider with that id")
		return
	}
	p.SetDraining(true)
	p.Close(reason)
	events.Audit("provider disconnected by operator", map[string]string{
		"provider_id": id, "platform": p.Capabilities.Platform, "reason": reason, "actor": adminActor(r),
	})
	s.log.Info("admin disconnect", "provider_id", id, "reason", reason)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "disconnected": true, "reason": reason})
}

func (s *Server) adminGetIntake(w http.ResponseWriter, _ *http.Request) {
	s.intake.mu.RLock()
	defer s.intake.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"paused": s.intake.paused, "message": s.intake.message, "since": nonZero(s.intake.since),
	})
}

func (s *Server) adminSetIntake(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Paused  bool   `json:"paused"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "body must be {\"paused\":bool,\"message\":string}")
		return
	}
	msg := strings.TrimSpace(body.Message)
	if len(msg) > 200 {
		msg = msg[:200]
	}
	s.intake.mu.Lock()
	s.intake.paused = body.Paused
	s.intake.message = msg
	if body.Paused {
		s.intake.since = time.Now()
	} else {
		s.intake.since = time.Time{}
	}
	s.intake.mu.Unlock()
	action := "intake resumed"
	if body.Paused {
		action = "intake paused"
	}
	events.Audit(action, map[string]string{"message": msg, "actor": adminActor(r)})
	s.log.Info("admin intake", "paused", body.Paused, "message", msg)
	s.adminGetIntake(w, r)
}

func (s *Server) adminCredit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		KeyID     string  `json:"key_id"`
		AmountUSD float64 `json:"amount_usd"`
		Note      string  `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.KeyID) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "body must be {\"key_id\",\"amount_usd\",\"note\"}")
		return
	}
	if math.IsNaN(body.AmountUSD) || math.IsInf(body.AmountUSD, 0) || body.AmountUSD == 0 ||
		math.Abs(body.AmountUSD) > 1000 {
		writeError(w, http.StatusBadRequest, "invalid_amount", "amount_usd must be non-zero and within ±1000")
		return
	}
	keyID := strings.TrimSpace(body.KeyID)
	micros := int64(math.Round(body.AmountUSD * 1e6))
	if err := s.store.AddCredit(r.Context(), keyID, micros, "adjust", "", ""); err != nil {
		writeError(w, http.StatusInternalServerError, "credit_failed", "could not apply credit")
		return
	}
	bal, _ := s.store.CreditBalance(r.Context(), keyID)
	note := strings.TrimSpace(body.Note)
	if len(note) > 200 {
		note = note[:200]
	}
	events.Audit("credit adjusted", map[string]string{
		"key_id": keyID, "amount_usd": strconv.FormatFloat(body.AmountUSD, 'f', 2, 64),
		"note": note, "actor": adminActor(r),
	})
	s.log.Info("admin credit", "key_id", keyID, "amount_usd", body.AmountUSD)
	writeJSON(w, http.StatusOK, map[string]any{
		"key_id": keyID, "applied_usd": body.AmountUSD, "balance_usd": float64(bal) / 1e6,
	})
}

// adminConfig exposes the runtime configuration without any secret: booleans
// say whether a credential is present, never what it is.
func (s *Server) adminConfig(w http.ResponseWriter, _ *http.Request) {
	providers := []string{}
	if s.auth != nil {
		providers = s.auth.Providers()
	}
	signer := ""
	if s.signer != nil {
		signer = s.signer.ID()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"http_addr":            s.cfg.HTTPAddr,
		"heartbeat_seconds":    s.cfg.HeartbeatSeconds,
		"job_deadline_ms":      s.cfg.JobDeadlineMS,
		"rate_per_min":         s.cfg.RatePerMin,
		"price_multiplier":     s.cfg.PriceMultiplier,
		"marketplace_margin":   s.cfg.MarketplaceMargin,
		"quote_ttl_seconds":    s.cfg.QuoteTTLSeconds,
		"billing_enforce":      s.cfg.BillingEnforce,
		"manifest_url":         s.cfg.ManifestURL,
		"manifest_signer_id":   signer,
		"manifest_kms":         s.cfg.ManifestKMSKey != "",
		"app_url":              s.cfg.AppURL,
		"public_base_url":      s.cfg.PublicBaseURL,
		"database":             s.cfg.DatabaseURL != "",
		"stripe":               s.stripe != nil,
		"stripe_webhooks":      s.cfg.StripeWebhookSecret != "" && s.cfg.StripeConnectWebhookSecret != "",
		"oauth_providers":      providers,
		"env_provider_tokens":  len(s.cfg.ProviderRegistrationTokens),
		"env_consumer_keys":    len(s.cfg.ConsumerAPIKeys),
		"council_model_review": s.cfg.CouncilModelID != "",
		"revision":             buildRevision(),
		"instance":             os.Getenv("K_REVISION"),
		"uptime_s":             int(time.Since(startedAt).Seconds()),
	})
}
