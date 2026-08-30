package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/batch"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/capability"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/protocol"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/registry"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/relay"
)

// POST /v1/batch — run many independent chat items for one model in parallel
// across every eligible provider, and return the answers in submission order.
// This is the aggregate execution primitive the marketplace quote engine builds
// on. Non-streaming only: a batch is an aggregate, not a stream.

type batchItemReq struct {
	Messages    []protocol.ChatMessage `json:"messages"`
	MaxTokens   *int                   `json:"max_tokens"`
	Temperature *float64               `json:"temperature"`
	TopP        *float64               `json:"top_p"`
	TopK        *int                   `json:"top_k"`
	Stop        []string               `json:"stop"`
	Seed        *int64                 `json:"seed"`
}

type batchRequest struct {
	Model string         `json:"model"`
	Items []batchItemReq `json:"items"`
	// Defaults applied to any item that doesn't set its own.
	MaxTokens   *int     `json:"max_tokens"`
	Temperature *float64 `json:"temperature"`
	Concurrency int      `json:"concurrency"`
}

func (s *Server) handleBatch(w http.ResponseWriter, r *http.Request) {
	var req batchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	if req.Model == "" || len(req.Items) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "model and a non-empty items array are required")
		return
	}
	if len(req.Items) > batch.MaxItems {
		writeError(w, http.StatusRequestEntityTooLarge, "too_many_items",
			"a batch is limited to "+strconv.Itoa(batch.MaxItems)+" items; split into multiple requests")
		return
	}
	for i, it := range req.Items {
		if len(it.Messages) == 0 {
			writeError(w, http.StatusBadRequest, "invalid_request",
				"items["+strconv.Itoa(i)+"] has no messages")
			return
		}
	}

	hwClass, ok := s.resolveModel(req.Model)
	if !ok {
		writeError(w, http.StatusNotFound, "model_not_found", "unknown model; see GET /v1/models")
		return
	}

	if s.cfg.BillingEnforce {
		if bal, _ := s.store.CreditBalance(r.Context(), keyIDFrom(r.Context())); bal <= 0 {
			writeError(w, http.StatusPaymentRequired, "insufficient_credit",
				"add credits at /billing/checkout")
			return
		}
	}

	items := make([]batch.Item, len(req.Items))
	for i, it := range req.Items {
		maxTok := defaultMaxTokens
		if req.MaxTokens != nil && *req.MaxTokens > 0 {
			maxTok = *req.MaxTokens
		}
		if it.MaxTokens != nil && *it.MaxTokens > 0 {
			maxTok = *it.MaxTokens
		}
		temp := req.Temperature
		if it.Temperature != nil {
			temp = it.Temperature
		}
		items[i] = batch.Item{
			Messages: it.Messages,
			Params: protocol.SamplingParams{
				MaxTokens:   maxTok,
				Temperature: temp,
				TopP:        it.TopP,
				TopK:        it.TopK,
				Stop:        it.Stop,
				Seed:        it.Seed,
			},
		}
	}

	exec := func(ctx context.Context, prov *registry.Provider, rr relay.Request, onDelta func(string) error) (*relay.Result, error) {
		return relay.ExecuteOn(ctx, s.deps(), prov, rr, onDelta)
	}

	created := time.Now().Unix()
	sum, err := batch.Run(r.Context(), s.reg, exec, items, batch.Options{
		Model:       req.Model,
		MinTier:     strings.TrimSpace(r.Header.Get("X-Provider-Trust-Level")),
		HWClass:     hwClass,
		Concurrency: req.Concurrency,
		ACUByPK:     s.acuByPK(r.Context()),
	})
	if err != nil {
		mapRelayErr(w, err)
		return
	}

	// Meter every successful sub-job through the same path as /v1/chat/completions
	// (usage event + provider accrual + credit debit).
	for _, it := range sum.Items {
		if it.ErrCode != "" {
			continue
		}
		s.meter(r.Context(), req.Model, &relay.Result{
			Usage:        it.Usage,
			FinishReason: it.FinishReason,
			ProviderID:   it.ProviderID,
			ProviderPK:   it.ProviderPK,
			TrustTier:    it.TrustTier,
			JobID:        it.JobID,
		})
	}

	writeJSON(w, http.StatusOK, renderBatch(req.Model, created, sum))
}

func renderBatch(model string, created int64, sum *batch.Summary) map[string]any {
	out := make([]map[string]any, len(sum.Items))
	for i, it := range sum.Items {
		row := map[string]any{"index": it.Index, "latency_ms": it.LatencyMS}
		if it.ErrCode != "" {
			row["error"] = map[string]string{"code": it.ErrCode, "message": it.ErrMsg}
		} else {
			row["message"] = map[string]string{"role": "assistant", "content": it.Content}
			row["finish_reason"] = it.FinishReason
			row["usage"] = it.Usage
			row["trust_level"] = it.TrustTier
			row["provider"] = shortID(it.ProviderID)
		}
		out[i] = row
	}
	return map[string]any{
		"id":      "batch-" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		"object":  "batch.completion",
		"created": created,
		"model":   model,
		"items":   out,
		"usage": map[string]int{
			"prompt_tokens":     sum.PromptTokens,
			"completion_tokens": sum.CompletionTokens,
			"total_tokens":      sum.PromptTokens + sum.CompletionTokens,
		},
		"stats": map[string]any{
			"ok":          sum.OK,
			"failed":      sum.Failed,
			"fanout":      sum.Fanout,
			"providers":   shortCounts(sum.Providers),
			"concurrency": sum.Concurrency,
			"wall_ms":     sum.WallMS,
		},
	}
}

// acuByPK builds a base64 static_pk -> ACU map from the capability registry, so
// the batch runner can prefer faster nodes. Best-effort; nil on any error.
func (s *Server) acuByPK(ctx context.Context) map[string]float64 {
	rows, err := s.store.NodeCapabilities(ctx)
	if err != nil || len(rows) == 0 {
		return nil
	}
	m := make(map[string]float64, len(rows))
	for _, c := range rows {
		m[c.StaticPK] = capability.ACU(capability.Fingerprint{
			Model: c.Model, Backend: c.Backend, PrefillTPS: c.PrefillTPS, DecodeTPS: c.DecodeTPS,
			SustainedStartTPS: c.SustainedStartTPS, SustainedEndTPS: c.SustainedEndTPS,
			MemBandwidthGBps: c.MemBandwidthGBps, AvailableRAMMB: c.AvailableRAMMB,
			CPUCores: c.CPUCores, ThermalState: c.ThermalState,
		})
	}
	return m
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func shortCounts(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[shortID(k)] = v
	}
	return out
}
