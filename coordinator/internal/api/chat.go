package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/metrics"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/pricing"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/protocol"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/relay"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/scheduler"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/store"
)

type chatRequest struct {
	Model       string                 `json:"model"`
	Messages    []protocol.ChatMessage `json:"messages"`
	Stream      bool                   `json:"stream"`
	MaxTokens   *int                   `json:"max_tokens"`
	Temperature *float64               `json:"temperature"`
	TopP        *float64               `json:"top_p"`
	TopK        *int                   `json:"top_k"`
	Stop        []string               `json:"stop"`
	Seed        *int64                 `json:"seed"`
}

const defaultMaxTokens = 512

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	if req.Model == "" || len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "model and messages are required")
		return
	}

	// If a signed catalog is loaded, reject unknown models up front and carry the model's
	// hardware class into scheduling.
	var hwClass string
	if s.cat != nil && s.cat.Enabled() && len(s.cat.Models()) > 0 {
		found := false
		for _, m := range s.cat.Models() {
			if m.ModelID == req.Model {
				found, hwClass = true, m.HardwareClass
				break
			}
		}
		if !found {
			writeError(w, http.StatusNotFound, "model_not_found", "unknown model; see GET /v1/models")
			return
		}
	}

	maxTok := defaultMaxTokens
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		maxTok = *req.MaxTokens
	}
	rr := relay.Request{
		Model:    req.Model,
		Messages: req.Messages,
		MinTier:  strings.TrimSpace(r.Header.Get("X-Provider-Trust-Level")),
		HWClass:  hwClass,
		Params: protocol.SamplingParams{
			MaxTokens:   maxTok,
			Temperature: req.Temperature,
			TopP:        req.TopP,
			TopK:        req.TopK,
			Stop:        req.Stop,
			Seed:        req.Seed,
		},
	}

	// Prepaid-credit gate (opt-in via SC_BILLING_ENFORCE=1).
	if s.cfg.BillingEnforce {
		if bal, _ := s.store.CreditBalance(r.Context(), keyIDFrom(r.Context())); bal <= 0 {
			writeError(w, http.StatusPaymentRequired, "insufficient_credit",
				"add credits at /billing/checkout")
			return
		}
	}

	id := "chatcmpl-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	created := time.Now().Unix()

	if req.Stream {
		s.streamChat(w, r, rr, id, created)
		return
	}
	s.blockingChat(w, r, rr, id, created)
}

// relayErrInfo classifies a relay error into a metric label, HTTP status, and error code.
func relayErrInfo(err error) (label string, status int, code, msg string) {
	switch {
	case errors.Is(err, scheduler.ErrNoProvider):
		return "no_provider", http.StatusServiceUnavailable, "no_provider", "no provider is currently serving this model"
	case errors.Is(err, scheduler.ErrTierUnmet):
		return "tier_unmet", http.StatusConflict, "trust_tier_unmet", "no connected provider meets the requested trust level"
	case errors.Is(err, scheduler.ErrCapsUnmet):
		return "caps_unmet", http.StatusServiceUnavailable, "capabilities_unmet", "no connected provider meets the model's resource requirements"
	case errors.Is(err, relay.ErrProviderGone):
		return "provider_gone", http.StatusBadGateway, "provider_gone", "the assigned provider disconnected"
	case errors.Is(err, context.Canceled):
		return "client_cancel", 499, "client_closed", "client closed the request"
	default:
		return "error", http.StatusInternalServerError, "internal", "the request could not be completed"
	}
}

// mapRelayErr counts the failure and, if w != nil, writes the error response.
func mapRelayErr(w http.ResponseWriter, err error) {
	label, status, code, msg := relayErrInfo(err)
	metrics.JobsTotal.WithLabelValues(label).Inc()
	if w != nil && status != 499 {
		writeError(w, status, code, msg)
	}
}

// meter records token usage and a successful job, and appends a usage event.
func (s *Server) meter(ctx context.Context, model string, res *relay.Result) {
	metrics.JobsTotal.WithLabelValues("ok").Inc()
	metrics.TokensTotal.WithLabelValues("prompt").Add(float64(res.Usage.PromptTokens))
	metrics.TokensTotal.WithLabelValues("completion").Add(float64(res.Usage.CompletionTokens))

	// Best-effort; never fail the response on a metering error.
	err := s.store.RecordUsage(context.WithoutCancel(ctx), store.UsageEvent{
		KeyID:            keyIDFrom(ctx),
		ProviderID:       res.ProviderID,
		Model:            model,
		Tier:             res.TrustTier,
		PromptTokens:     res.Usage.PromptTokens,
		CompletionTokens: res.Usage.CompletionTokens,
		DurationMS:       0,
		FinishReason:     res.FinishReason,
	})
	if err != nil {
		s.log.Warn("record usage failed", "err", err)
	}

	// Shadow-accrue provider earnings (docs/PAYMENTS.md Phase 1 — no money moves).
	modelClass := ""
	if s.cat != nil {
		modelClass = s.cat.ClassOf(model)
	}
	q := pricing.QuoteJob(modelClass, res.TrustTier,
		res.Usage.PromptTokens, res.Usage.CompletionTokens, 1.0)
	if e := s.store.RecordEarning(context.WithoutCancel(ctx), store.EarningEvent{
		ProviderID:     res.ProviderID,
		StaticPK:       res.ProviderPK,
		KeyID:          keyIDFrom(ctx),
		Model:          model,
		ModelClass:     q.ModelClass,
		Tier:           res.TrustTier,
		GrossMicros:    q.GrossMicros,
		ProviderMicros: q.ProviderMicros,
	}); e != nil {
		s.log.Warn("record earning failed", "err", e)
	}
	// Debit the consumer's prepaid balance (allowed to go slightly negative;
	// the pre-flight gate blocks the next request).
	if e := s.store.AddCredit(context.WithoutCancel(ctx), keyIDFrom(ctx),
		-q.GrossMicros, "debit", "", res.JobID); e != nil {
		s.log.Warn("credit debit failed", "err", e)
	}
}

// --- non-streaming ---

func (s *Server) blockingChat(w http.ResponseWriter, r *http.Request, rr relay.Request, id string, created int64) {
	var sb strings.Builder
	res, err := relay.Execute(r.Context(), s.deps(), rr, func(delta string) error {
		sb.WriteString(delta)
		return nil
	})
	if err != nil {
		mapRelayErr(w, err)
		return
	}
	s.meter(r.Context(), rr.Model, res)
	resp := map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": created,
		"model":   rr.Model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       map[string]string{"role": "assistant", "content": sb.String()},
			"finish_reason": res.FinishReason,
		}},
		"usage": res.Usage,
	}
	w.Header().Set("X-Provider-Trust-Level", res.TrustTier)
	writeJSON(w, http.StatusOK, resp)
}

// --- streaming (SSE) ---

func (s *Server) streamChat(w http.ResponseWriter, r *http.Request, rr relay.Request, id string, created int64) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "no_stream", "streaming not supported by the server")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	sendChunk := func(delta string, finish *string) error {
		choice := map[string]any{"index": 0, "delta": map[string]string{}, "finish_reason": nil}
		if delta != "" {
			choice["delta"] = map[string]string{"content": delta}
		}
		if finish != nil {
			choice["finish_reason"] = *finish
		}
		payload := map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": created,
			"model": rr.Model, "choices": []any{choice},
		}
		b, _ := json.Marshal(payload)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	// role primer chunk, as OpenAI does
	_ = sendChunk("", nil)

	dispatch := time.Now()
	first := true
	res, err := relay.Execute(r.Context(), s.deps(), rr, func(delta string) error {
		if first {
			metrics.JobTTFT.Observe(time.Since(dispatch).Seconds())
			first = false
		}
		return sendChunk(delta, nil)
	})
	if err != nil {
		mapRelayErr(nil, err) // count only; response already started
		b, _ := json.Marshal(map[string]any{"error": map[string]string{"message": "upstream error", "type": "server_error"}})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
		return
	}
	s.meter(r.Context(), rr.Model, res)

	fin := res.FinishReason
	if fin == "" {
		fin = "stop"
	}
	_ = sendChunk("", &fin)
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}
