package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/batch"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/council"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/marketplace"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/pricing"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/protocol"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/registry"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/relay"
)

// The public, no-sign-up investor demo (demo.ayni-ai.com). One endpoint runs the
// whole loop — quote, Council review, encrypted execution on a live provider —
// for a single fixed workload (summarize a pasted document with the 0.5B model).
// Gated by SC_DEMO_ENABLED=1. Strictly rate-limited per IP, length-capped, and
// billed to nobody: the point is to show the mechanism, not to sell it here.

const (
	demoModel     = "qwen2.5-0.5b-instruct-q4_k_m"
	demoMaxTokens = 256
	demoMaxChars  = 6000
	demoMinChars  = 40
	demoKeyID     = "demo" // usage attribution only; never debited
	demoTemplate  = "Summarize the following document in 4–6 sentences. Be faithful and concise.\n\n"
)

// demoLastJob is the content-free record of the most recent successful demo run,
// served at GET /v1/demo/last-job and shown in the "what the network kept" panel.
type demoLastJob struct {
	Model            string         `json:"model"`
	Class            string         `json:"class"`
	Tier             string         `json:"tier"`
	PromptTokens     int            `json:"prompt_tokens"`
	CompletionTokens int            `json:"completion_tokens"`
	WallMS           int64          `json:"wall_ms"`
	WouldCostUSD     float64        `json:"would_cost_usd"`
	Device           map[string]any `json:"device"`
	CouncilDecision  string         `json:"council_decision"`
	AuditHash        string         `json:"audit_hash"`
	RecordedAt       string         `json:"recorded_at"`
}

type demoState struct {
	mu   sync.Mutex
	last *demoLastJob
}

func (d *demoState) set(j demoLastJob) {
	d.mu.Lock()
	d.last = &j
	d.mu.Unlock()
}

func (d *demoState) get() *demoLastJob {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.last
}

type demoSummarizeReq struct {
	Text string `json:"text"`
}

func (s *Server) handleDemoSummarize(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.DemoEnabled {
		writeError(w, http.StatusNotFound, "not_found", "demo is not enabled")
		return
	}
	if allowed, _ := s.demoRL.Allow("demo:" + clientIP(r)); !allowed {
		writeError(w, http.StatusTooManyRequests, "rate_limited",
			"the demo is limited to a few runs per minute per visitor")
		return
	}

	var req demoSummarizeReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	text := clipRunes(req.Text, demoMaxChars)
	if len(text) < demoMinChars {
		writeError(w, http.StatusBadRequest, "too_short",
			"paste at least a paragraph to summarize")
		return
	}

	hwClass, ok := s.resolveModel(demoModel)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "demo_model_missing",
			"the demo model is not in the catalog")
		return
	}

	item := batch.Item{
		Messages: []protocol.ChatMessage{{Role: "user", Content: demoTemplate + text}},
		Params:   protocol.SamplingParams{MaxTokens: demoMaxTokens},
	}
	spec := marketplace.Spec{
		Model:      demoModel,
		ModelClass: s.classOf(demoModel),
		Tier:       "community",
		Redundancy: 1,
		Items:      []batch.Item{item},
	}
	sup, serr := s.supplyFor(demoModel, "community", hwClass)
	if serr != nil {
		sup = marketplace.Supply{Nodes: 0, AggregateTPS: marketplace.ReferenceNodeTPS}
	}
	est := marketplace.EstimateWorkload(spec, sup)
	cost := pricing.QuoteWorkload(spec.ModelClass, "community", est.PromptTokens, est.CompletionTokens,
		1, s.cfg.MarketplaceMargin, s.cfg.PriceMultiplier)

	prop := council.ForWorkload(demoModel, spec.ModelClass, "community", est.Items,
		est.PromptTokens, est.CompletionTokens, 1, false,
		float64(cost.TotalMicros)/1e6, est.ETASeconds)
	outcome := council.Evaluate(r.Context(), prop, s.council, council.DefaultPolicy())
	council.RecordWorkloadDecision(outcome, prop.Title)

	resp := map[string]any{
		"object": "demo.summarize",
		"quote": map[string]any{
			"currency":       "usd",
			"total_usd":      round2usd(cost.TotalMicros),
			"eta_seconds":    est.ETASeconds,
			"eligible_nodes": est.EligibleNodes,
			"supply_online":  sup.Nodes > 0,
			"breakdown_usd": map[string]float64{
				"compute_acquisition": round4usd(cost.ComputeMicros),
				"coordination":        round4usd(cost.CoordinationMicros),
				"expected_failure":    round4usd(cost.FailureMicros),
				"payment_processing":  round4usd(cost.PaymentMicros),
				"ayni_margin":         round4usd(cost.MarginMicros),
			},
		},
		"council": map[string]any{
			"decision":          outcome.Decision,
			"risk_class":        prop.RiskClass,
			"reviews":           outcome.Reviews,
			"required_controls": outcome.RequiredControls,
			"dissent":           outcome.Dissent,
			"missing":           outcome.Missing,
			"audit_hash":        outcome.AuditHash,
			// the exact facts the Council saw — note the absence of any prompt text
			"facts": prop.Facts,
		},
	}

	if !outcome.Acceptable() {
		resp["blocked"] = true
		resp["result"] = nil
		writeJSON(w, http.StatusOK, resp)
		return
	}

	ctx := context.WithValue(r.Context(), ctxKeyID, demoKeyID)
	exec := func(c context.Context, prov *registry.Provider, rr relay.Request, onDelta func(string) error) (*relay.Result, error) {
		return relay.ExecuteOn(c, s.deps(), prov, rr, onDelta)
	}
	sum, err := batch.Run(ctx, s.reg, exec, []batch.Item{item}, batch.Options{
		Model:   demoModel,
		HWClass: hwClass,
		ACUByPK: s.acuByPK(ctx),
	})
	if err != nil || sum == nil || len(sum.Items) == 0 || sum.Items[0].ErrCode != "" {
		// no capacity online (or the one node errored) — degrade to the last real run
		resp["result"] = nil
		resp["fallback"] = s.demo.get()
		if err == nil && len(sum.Items) > 0 {
			resp["error"] = sum.Items[0].ErrCode
		} else {
			resp["error"] = "no_provider_online"
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	it := sum.Items[0]
	// meter it (best effort) so the provider actually accrues for the work
	s.recordJob(ctx, demoModel, &relay.Result{
		Usage: it.Usage, FinishReason: it.FinishReason,
		ProviderID: it.ProviderID, ProviderPK: it.ProviderPK,
		TrustTier: it.TrustTier, JobID: it.JobID,
	}, s.cfg.PriceMultiplier)

	device := map[string]any{"trust_tier": it.TrustTier}
	if p, ok := s.reg.Get(it.ProviderID); ok {
		device["platform"] = p.Capabilities.Platform
		device["arch"] = p.Capabilities.Arch
		device["backend"] = p.Capabilities.Backend
		device["hardware_class"] = p.Capabilities.HardwareClass
		device["label"] = deviceLabel(p)
	}

	last := demoLastJob{
		Model:            demoModel,
		Class:            spec.Class(),
		Tier:             it.TrustTier,
		PromptTokens:     it.Usage.PromptTokens,
		CompletionTokens: it.Usage.CompletionTokens,
		WallMS:           sum.WallMS,
		WouldCostUSD:     round2usd(cost.TotalMicros),
		Device:           device,
		CouncilDecision:  string(outcome.Decision),
		AuditHash:        outcome.AuditHash,
		RecordedAt:       time.Now().UTC().Format(time.RFC3339),
	}
	s.demo.set(last)

	resp["result"] = map[string]any{
		"summary":           it.Content,
		"wall_ms":           sum.WallMS,
		"prompt_tokens":     it.Usage.PromptTokens,
		"completion_tokens": it.Usage.CompletionTokens,
		"device":            device,
		"charged_usd":       0,
		"would_cost_usd":    round2usd(cost.TotalMicros),
	}
	resp["record"] = last
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDemoLastJob(w http.ResponseWriter, _ *http.Request) {
	if !s.cfg.DemoEnabled {
		writeError(w, http.StatusNotFound, "not_found", "demo is not enabled")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": s.demo.get()})
}

// deviceLabel is a friendly one-liner for "who served this", from the advertised
// capabilities. Deliberately coarse — we never fingerprint a specific unit.
func deviceLabel(p *registry.Provider) string {
	c := p.Capabilities
	base := c.Platform + " " + c.Arch
	switch {
	case c.Platform == "android":
		base = "Android phone"
	case c.Platform == "darwin" && c.Arch == "arm64":
		base = "Apple-silicon Mac"
	case c.Platform == "linux" && (c.Arch == "amd64" || c.Arch == "x86_64"):
		base = "Linux desktop (x86-64)"
	case c.Platform == "linux" && (c.Arch == "arm64" || c.Arch == "aarch64"):
		base = "Linux (ARM64)"
	case c.Platform == "windows":
		base = "Windows PC"
	}
	if c.Backend != "" {
		base += " · " + c.Backend
	}
	return base
}

// clipRunes trims to at most n runes (not bytes) and strips surrounding space.
func clipRunes(s string, n int) string {
	r := []rune(s)
	// trim leading/trailing whitespace cheaply via strings would re-alloc; do it on runes
	start, end := 0, len(r)
	for start < end && isSpace(r[start]) {
		start++
	}
	for end > start && isSpace(r[end-1]) {
		end--
	}
	r = r[start:end]
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f'
}
