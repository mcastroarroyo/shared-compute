package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/batch"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/capability"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/council"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/marketplace"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/pricing"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/protocol"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/scheduler"
)

// Marketplace v0.1 (M10.3):
//   POST /v1/workloads             -> a priced, time-boxed quote (no execution)
//   GET  /v1/workloads/{id}        -> the quote + its status
//   POST /v1/workloads/{id}/accept -> run it via the batch fan-out primitive
//
// A quote can be built from an estimate (count + average token sizes) OR from
// explicit items. Only an explicit-items quote can be accepted and run.

type workloadEstimateReq struct {
	Count               int `json:"count"`
	AvgPromptTokens     int `json:"avg_prompt_tokens"`
	AvgCompletionTokens int `json:"avg_completion_tokens"`
}

type workloadRequest struct {
	Model       string               `json:"model"`
	Items       []batchItemReq       `json:"items"`
	Estimate    *workloadEstimateReq `json:"estimate"`
	Redundancy  int                  `json:"redundancy"`
	MaxPriceUSD float64              `json:"max_price_usd"`
	Spot        bool                 `json:"spot"` // interruptible, best-effort, discounted
}

// spotPriceMult is the rate-card multiplier for a service class: on-demand uses
// the configured multiplier as-is; spot also applies SpotPriceFactor, which
// flows through to provider accruals too (both sides opt into the spot market).
// acceptRequest is the optional body of POST /v1/workloads/{id}/accept. An empty body
// keeps the original synchronous behaviour; "async": true queues the run instead
// (docs/WORKLOAD-RUNNERS.md).
type acceptRequest struct {
	Async      bool   `json:"async"`
	WebhookURL string `json:"webhook_url"`
	Label      string `json:"label"`
}

func (s *Server) spotPriceMult(spot bool) float64 {
	if spot {
		return s.cfg.PriceMultiplier * s.cfg.SpotPriceFactor
	}
	return s.cfg.PriceMultiplier
}

// spotSupply discounts aggregate throughput for a spot estimate — spot work runs
// on whatever capacity on-demand traffic leaves free.
func spotSupply(sup marketplace.Supply, spot bool) marketplace.Supply {
	if spot {
		sup.AggregateTPS *= marketplace.SpotSupplyFraction
	}
	return sup
}

func (s *Server) handleCreateWorkload(w http.ResponseWriter, r *http.Request) {
	var req workloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "model is required")
		return
	}
	hasItems := len(req.Items) > 0
	hasEstimate := req.Estimate != nil && req.Estimate.Count > 0
	if hasItems == hasEstimate {
		writeError(w, http.StatusBadRequest, "invalid_request",
			"provide exactly one of items[] or estimate{count,...}")
		return
	}
	red := req.Redundancy
	if red == 0 {
		red = 1
	}
	if red < 1 || red > marketplace.MaxRedundancy {
		writeError(w, http.StatusBadRequest, "invalid_request", "redundancy must be 1..3")
		return
	}

	hwClass, ok := s.resolveModel(req.Model)
	if !ok {
		writeError(w, http.StatusNotFound, "model_not_found", "unknown model; see GET /v1/models")
		return
	}
	tier := marketplace.NormalizeTier(r.Header.Get("X-Provider-Trust-Level"))

	spec := marketplace.Spec{
		Model:      req.Model,
		ModelClass: s.classOf(req.Model),
		Tier:       tier,
		Redundancy: red,
		Spot:       req.Spot,
	}
	switch {
	case hasItems:
		if len(req.Items) > marketplace.MaxItems {
			writeError(w, http.StatusRequestEntityTooLarge, "too_many_items",
				"a workload is limited to "+strconv.Itoa(marketplace.MaxItems)+" items")
			return
		}
		items, msg := toBatchItems(req.Items)
		if msg != "" {
			writeError(w, http.StatusBadRequest, "invalid_request", msg)
			return
		}
		spec.Items = items
	default:
		if req.Estimate.Count > marketplace.MaxItems {
			writeError(w, http.StatusRequestEntityTooLarge, "too_many_items",
				"a workload is limited to "+strconv.Itoa(marketplace.MaxItems)+" items")
			return
		}
		spec.EstimateCount = req.Estimate.Count
		spec.AvgPromptTokens = req.Estimate.AvgPromptTokens
		spec.AvgCompletionTokens = req.Estimate.AvgCompletionTokens
	}

	sup, serr := s.supplyFor(req.Model, tier, hwClass)
	if serr != nil {
		mapRelayErr(w, serr)
		return
	}

	est := marketplace.EstimateWorkload(spec, spotSupply(sup, req.Spot))
	cost := pricing.QuoteWorkload(spec.ModelClass, tier, est.PromptTokens, est.CompletionTokens,
		red, s.cfg.MarketplaceMargin, s.spotPriceMult(req.Spot))

	if req.MaxPriceUSD > 0 && float64(cost.TotalMicros)/1e6 > req.MaxPriceUSD {
		writeError(w, http.StatusConflict, "over_budget",
			"quote exceeds max_price_usd; raise the cap or reduce the workload")
		return
	}

	// Ayni Council review — structured facts only, no prompt content.
	prop := council.ForWorkload(req.Model, spec.ModelClass, tier, est.Items,
		est.PromptTokens, est.CompletionTokens, red, req.Spot,
		float64(cost.TotalMicros)/1e6, est.ETASeconds)
	outcome := council.Evaluate(r.Context(), prop, s.council, council.DefaultPolicy())
	council.RecordWorkloadDecision(r.Context(), outcome, prop.Title)
	if outcome.Decision == council.Block {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   "council_blocked",
			"message": "the Ayni Council did not approve this workload",
			"council": outcome,
		})
		return
	}

	q := s.quotes.Create(keyIDFrom(r.Context()), req.Model, spec, est, cost)
	q.Council = &outcome
	writeJSON(w, http.StatusCreated, s.renderQuote(q))
}

// handleQuotePreview is the PUBLIC, unauthenticated version of a workload quote:
// estimate-only (no items, no execution, nothing stored), so the marketing site
// can show a real price + ETA without an account. Rate-limited per client IP.
func (s *Server) handleQuotePreview(w http.ResponseWriter, r *http.Request) {
	if !s.intakeAllowed(r) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "slow down")
		return
	}
	var req struct {
		Model      string               `json:"model"`
		Estimate   *workloadEstimateReq `json:"estimate"`
		Redundancy int                  `json:"redundancy"`
		Tier       string               `json:"tier"`
		Spot       bool                 `json:"spot"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	if req.Model == "" || req.Estimate == nil || req.Estimate.Count <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_request",
			"model and estimate{count, avg_prompt_tokens, avg_completion_tokens} are required")
		return
	}
	if req.Estimate.Count > marketplace.MaxItems {
		writeError(w, http.StatusRequestEntityTooLarge, "too_many_items",
			"preview is limited to "+strconv.Itoa(marketplace.MaxItems)+" items")
		return
	}
	red := req.Redundancy
	if red == 0 {
		red = 1
	}
	if red < 1 || red > marketplace.MaxRedundancy {
		writeError(w, http.StatusBadRequest, "invalid_request", "redundancy must be 1..3")
		return
	}
	hwClass, ok := s.resolveModel(req.Model)
	if !ok {
		writeError(w, http.StatusNotFound, "model_not_found", "unknown model; see GET /v1/models")
		return
	}
	tier := marketplace.NormalizeTier(req.Tier)
	if h := marketplace.NormalizeTier(r.Header.Get("X-Provider-Trust-Level")); h != "community" {
		tier = h
	}

	// Supply is best-effort: if nothing is online for this model, quote against
	// one reference device so the visitor still sees a plausible ETA.
	sup, serr := s.supplyFor(req.Model, tier, hwClass)
	if serr != nil {
		sup = marketplace.Supply{Nodes: 0, AggregateTPS: marketplace.ReferenceNodeTPS}
	}

	spec := marketplace.Spec{
		Model:               req.Model,
		ModelClass:          s.classOf(req.Model),
		Tier:                tier,
		Redundancy:          red,
		Spot:                req.Spot,
		EstimateCount:       req.Estimate.Count,
		AvgPromptTokens:     req.Estimate.AvgPromptTokens,
		AvgCompletionTokens: req.Estimate.AvgCompletionTokens,
	}
	est := marketplace.EstimateWorkload(spec, spotSupply(sup, req.Spot))
	cost := pricing.QuoteWorkload(spec.ModelClass, tier, est.PromptTokens, est.CompletionTokens,
		red, s.cfg.MarketplaceMargin, s.spotPriceMult(req.Spot))

	estOut := map[string]any{
		"items":             est.Items,
		"prompt_tokens":     est.PromptTokens,
		"completion_tokens": est.CompletionTokens,
		"eligible_nodes":    est.EligibleNodes,
		"aggregate_tps":     est.AggregateTPS,
		"eta_seconds":       est.ETASeconds,
	}
	if req.Spot {
		estOut["eta_seconds_max"] = int64(float64(est.ETASeconds) * s.spotEtaSlack())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object":        "workload.preview",
		"model":         req.Model,
		"class":         spec.Class(),
		"spot":          req.Spot,
		"tier":          tier,
		"redundancy":    red,
		"supply_online": sup.Nodes > 0,
		"estimate":      estOut,
		"price": map[string]any{
			"currency":  "usd",
			"total_usd": round2usd(cost.TotalMicros),
			"breakdown_usd": map[string]float64{
				"compute_acquisition": round4usd(cost.ComputeMicros),
				"coordination":        round4usd(cost.CoordinationMicros),
				"expected_failure":    round4usd(cost.FailureMicros),
				"payment_processing":  round4usd(cost.PaymentMicros),
				"ayni_margin":         round4usd(cost.MarginMicros),
			},
		},
	})
}

func (s *Server) handleGetWorkload(w http.ResponseWriter, r *http.Request) {
	q, ok := s.quotes.Get(r.PathValue("id"))
	if !ok || q.KeyID != keyIDFrom(r.Context()) {
		writeError(w, http.StatusNotFound, "not_found", "no such workload quote (it may have expired)")
		return
	}
	writeJSON(w, http.StatusOK, s.renderQuote(q))
}

func (s *Server) handleAcceptWorkload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	q, ok := s.quotes.Get(id)
	if !ok || q.KeyID != keyIDFrom(r.Context()) {
		writeError(w, http.StatusNotFound, "not_found", "no such workload quote (it may have expired)")
		return
	}
	if q.Accepted {
		writeError(w, http.StatusConflict, "already_accepted", "this workload has already been accepted")
		return
	}
	if !q.Spec.Runnable() {
		writeError(w, http.StatusBadRequest, "estimate_only",
			"this quote was built from an estimate; resubmit with explicit items[] to run it")
		return
	}
	if o, ok := q.Council.(*council.Outcome); ok && !o.Acceptable() {
		writeError(w, http.StatusConflict, "council_blocked",
			"the Ayni Council blocked this workload; it cannot be run")
		return
	}
	if s.cfg.BillingEnforce {
		if bal, _ := s.store.CreditBalance(r.Context(), keyIDFrom(r.Context())); bal < q.Cost.TotalMicros {
			writeError(w, http.StatusPaymentRequired, "insufficient_credit",
				"quoted price exceeds your credit balance; add credits at /billing/checkout")
			return
		}
	}
	// Decide sync vs async before locking the quote, so a rejected async submission
	// leaves the quote acceptable.
	var opts acceptRequest
	if r.Body != nil {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024)).Decode(&opts) // empty body = sync
	}
	if opts.WebhookURL != "" && !strings.HasPrefix(opts.WebhookURL, "https://") {
		writeError(w, http.StatusBadRequest, "bad_webhook", "webhook_url must be an https:// URL")
		return
	}
	keyID := keyIDFrom(r.Context())
	if opts.Async && s.runs.inflightFor(keyID) >= s.maxAsyncRunsPerKey() {
		w.Header().Set("Retry-After", "30")
		writeError(w, http.StatusTooManyRequests, "too_many_runs",
			fmt.Sprintf("you already have %d workloads queued or running; wait for one to finish",
				s.maxAsyncRunsPerKey()))
		return
	}

	if _, locked := s.quotes.MarkAccepted(id); !locked {
		writeError(w, http.StatusConflict, "already_accepted", "this workload has already been accepted")
		return
	}

	if opts.Async {
		run, secret := s.startAsyncRun(r.Context(), q, keyID, opts.WebhookURL, clip(opts.Label, 120))
		out := s.renderRun(run)
		out["poll_url"] = "/v1/runs/" + run.ID
		out["results_url"] = "/v1/runs/" + run.ID + "/results"
		if secret != "" {
			// Shown once: the runner needs it to verify the webhook signature.
			out["webhook_secret"] = secret
		}
		w.Header().Set("Location", "/v1/runs/"+run.ID)
		writeJSON(w, http.StatusAccepted, out)
		return
	}

	created := time.Now().Unix()
	out, err := s.executeWorkload(r.Context(), q, keyID, nil)
	if err != nil {
		mapRelayErr(w, err)
		return
	}
	merged := out.summary

	writeJSON(w, http.StatusOK, map[string]any{
		"id":          q.ID,
		"object":      "workload.result",
		"model":       q.Model,
		"class":       q.Spec.Class(),
		"created":     created,
		"quoted_usd":  round2usd(q.Cost.TotalMicros),
		"charged_usd": round2usd(out.charged),
		"items":       renderBatchItems(merged),
		"usage": map[string]int{
			"prompt_tokens":     merged.PromptTokens,
			"completion_tokens": merged.CompletionTokens,
			"total_tokens":      merged.PromptTokens + merged.CompletionTokens,
		},
		"stats":      batchStats(merged),
		"run_review": out.review,
	})
}

// --- helpers ---

func (s *Server) renderQuote(q *marketplace.Quote) map[string]any {
	status := "quoted"
	if q.Accepted {
		status = "accepted"
	}
	c := q.Cost
	est := map[string]any{
		"items":             q.Estimate.Items,
		"prompt_tokens":     q.Estimate.PromptTokens,
		"completion_tokens": q.Estimate.CompletionTokens,
		"eligible_nodes":    q.Estimate.EligibleNodes,
		"aggregate_tps":     q.Estimate.AggregateTPS,
		"eta_seconds":       q.Estimate.ETASeconds,
	}
	if q.Spec.Spot {
		est["eta_seconds_max"] = int64(float64(q.Estimate.ETASeconds) * s.spotEtaSlack())
	}
	body := map[string]any{
		"id":         q.ID,
		"object":     "workload.quote",
		"model":      q.Model,
		"status":     status,
		"class":      q.Spec.Class(),
		"spot":       q.Spec.Spot,
		"tier":       q.Spec.Tier,
		"redundancy": q.Spec.Redundancy,
		"runnable":   q.Spec.Runnable(),
		"estimate":   est,
		"price": map[string]any{
			"currency":  "usd",
			"total_usd": round2usd(c.TotalMicros),
			"breakdown_usd": map[string]float64{
				"compute_acquisition": round4usd(c.ComputeMicros),
				"coordination":        round4usd(c.CoordinationMicros),
				"expected_failure":    round4usd(c.FailureMicros),
				"payment_processing":  round4usd(c.PaymentMicros),
				"ayni_margin":         round4usd(c.MarginMicros),
			},
		},
		"council":    q.Council,
		"created_at": q.CreatedAt.UTC().Format(time.RFC3339),
		"expires_at": q.ExpiresAt.UTC().Format(time.RFC3339),
	}
	if q.Spec.Spot {
		body["note"] = "spot: best-effort completion time, may be interrupted and requeued; priced at " +
			strconv.Itoa(int(s.cfg.SpotPriceFactor*100)) + "% of on-demand"
	}
	if q.Spec.Runnable() {
		body["accept_url"] = "/v1/workloads/" + q.ID + "/accept"
	}
	return body
}

func (s *Server) spotEtaSlack() float64 {
	if s.cfg.SpotEtaSlack > 1 {
		return s.cfg.SpotEtaSlack
	}
	return 3
}

// supplyFor is the slice of connected supply that could take this workload:
// eligible providers (scheduler filter) joined to their measured throughput.
func (s *Server) supplyFor(model, tier, hwClass string) (marketplace.Supply, error) {
	cands, err := scheduler.Eligible(s.reg, scheduler.Requirements{
		Model: model, MinTier: tier, HWClass: hwClass,
	})
	if err != nil {
		return marketplace.Supply{}, err
	}
	caps, _ := s.store.NodeCapabilities(context.Background())
	tpsByPK := make(map[string]float64, len(caps))
	acuByPK := make(map[string]float64, len(caps))
	for _, c := range caps {
		fp := capability.Fingerprint{
			Model: c.Model, Backend: c.Backend, PrefillTPS: c.PrefillTPS, DecodeTPS: c.DecodeTPS,
			SustainedStartTPS: c.SustainedStartTPS, SustainedEndTPS: c.SustainedEndTPS,
			MemBandwidthGBps: c.MemBandwidthGBps, AvailableRAMMB: c.AvailableRAMMB,
			CPUCores: c.CPUCores, ThermalState: c.ThermalState,
		}
		t := c.SustainedEndTPS
		if t <= 0 {
			t = c.DecodeTPS
		}
		tpsByPK[c.StaticPK] = t
		acuByPK[c.StaticPK] = capability.ACU(fp)
	}

	sup := marketplace.Supply{Nodes: len(cands)}
	for _, p := range cands {
		pk := base64.StdEncoding.EncodeToString(p.StaticPK[:])
		if t, ok := tpsByPK[pk]; ok && t > 0 {
			sup.AggregateTPS += t
		} else {
			sup.AggregateTPS += 10 // unbenchmarked node: conservative floor
		}
		if a := acuByPK[pk]; a > sup.TopACU {
			sup.TopACU = a
		}
	}
	return sup, nil
}

func (s *Server) classOf(model string) string {
	if s.cat != nil {
		return s.cat.ClassOf(model)
	}
	return ""
}

func (s *Server) hwClassOf(model string) string {
	hw, _ := s.resolveModel(model)
	return hw
}

func chunkItems(items []batch.Item, size int) [][]batch.Item {
	if len(items) <= size {
		return [][]batch.Item{items}
	}
	var out [][]batch.Item
	for i := 0; i < len(items); i += size {
		end := i + size
		if end > len(items) {
			end = len(items)
		}
		out = append(out, items[i:end])
	}
	return out
}

// mergeSummary appends src's items (re-indexed by offset) and provider counts
// into dst, and folds token / OK / failed totals.
func mergeSummary(dst, src *batch.Summary, offset int) {
	for _, it := range src.Items {
		it.Index += offset
		dst.Items = append(dst.Items, it)
	}
	for id, n := range src.Providers {
		dst.Providers[id] += n
	}
	dst.OK += src.OK
	dst.Failed += src.Failed
	dst.PromptTokens += src.PromptTokens
	dst.CompletionTokens += src.CompletionTokens
	if src.Concurrency > dst.Concurrency {
		dst.Concurrency = src.Concurrency
	}
}

func toBatchItems(in []batchItemReq) ([]batch.Item, string) {
	out := make([]batch.Item, len(in))
	for i, it := range in {
		if len(it.Messages) == 0 {
			return nil, "items[" + strconv.Itoa(i) + "] has no messages"
		}
		if messagesTooBig(it.Messages) {
			return nil, "items[" + strconv.Itoa(i) + "] message content exceeds the limit"
		}
		maxTok := defaultMaxTokens
		if it.MaxTokens != nil && *it.MaxTokens > 0 {
			maxTok = *it.MaxTokens
		}
		out[i] = batch.Item{
			Messages: it.Messages,
			Params: protocol.SamplingParams{
				MaxTokens: maxTok, Temperature: it.Temperature, TopP: it.TopP, TopK: it.TopK,
				Stop: it.Stop, Seed: it.Seed,
			},
		}
	}
	return out, ""
}

func round2usd(micros int64) float64 { return capability.Round2(float64(micros) / 1e6) }
func round4usd(micros int64) float64 {
	return float64(int64(float64(micros)/1e6*1e4+0.5)) / 1e4
}
