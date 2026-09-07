package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/batch"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/council"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/marketplace"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/registry"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/relay"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/store"
)

// Asynchronous workload runs, for API clients that submit work and collect it later
// ("workload runners" — see docs/WORKLOAD-RUNNERS.md).
//
// PRIVACY: only run *metadata* (status, counts, cost, timings) is persisted. Completion
// content is held in coordinator memory for RunResultsTTLSeconds and served over TLS to
// the owning key, then dropped. Nothing about a job's content reaches disk or logs, so a
// runner moving a sensitive pipeline here keeps the same guarantee as the sync path.

const (
	runStatusQueued    = "queued"
	runStatusRunning   = "running"
	runStatusSucceeded = "succeeded"
	runStatusFailed    = "failed"
	// runMaxWall bounds a single async run so a stuck fleet cannot pin a slot forever.
	runMaxWall = 2 * time.Hour
)

// runResults is the in-memory, TTL'd result of a finished run.
type runResults struct {
	summary   *batch.Summary
	review    any
	quoted    int64
	charged   int64
	expiresAt time.Time
}

// runRegistry tracks in-flight runs and holds finished results in memory only.
type runRegistry struct {
	mu       sync.Mutex
	results  map[string]*runResults
	inflight map[string]int // keyID -> queued+running
	secrets  map[string]string
}

func newRunRegistry() *runRegistry {
	return &runRegistry{
		results:  map[string]*runResults{},
		inflight: map[string]int{},
		secrets:  map[string]string{},
	}
}

func (rr *runRegistry) inflightFor(keyID string) int {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	return rr.inflight[keyID]
}

func (rr *runRegistry) addInflight(keyID string, delta int) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	rr.inflight[keyID] += delta
	if rr.inflight[keyID] <= 0 {
		delete(rr.inflight, keyID)
	}
}

func (rr *runRegistry) put(id string, res *runResults) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	rr.gcLocked(time.Now())
	rr.results[id] = res
}

func (rr *runRegistry) get(id string) (*runResults, bool) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	rr.gcLocked(time.Now())
	res, ok := rr.results[id]
	return res, ok
}

func (rr *runRegistry) setSecret(id, secret string) {
	rr.mu.Lock()
	rr.secrets[id] = secret
	rr.mu.Unlock()
}

func (rr *runRegistry) secret(id string) string {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	return rr.secrets[id]
}

func (rr *runRegistry) gcLocked(now time.Time) {
	for id, res := range rr.results {
		if now.After(res.expiresAt) {
			delete(rr.results, id)
			delete(rr.secrets, id)
		}
	}
}

func newRunID() string {
	b := make([]byte, 9)
	_, _ = rand.Read(b)
	return "run_" + hex.EncodeToString(b)
}

func newWebhookSecret() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return "whsec_" + hex.EncodeToString(b)
}

func (s *Server) runResultsTTL() time.Duration {
	if s.cfg.RunResultsTTLSeconds > 0 {
		return time.Duration(s.cfg.RunResultsTTLSeconds) * time.Second
	}
	return time.Hour
}

func (s *Server) maxAsyncRunsPerKey() int {
	if s.cfg.MaxAsyncRunsPerKey > 0 {
		return s.cfg.MaxAsyncRunsPerKey
	}
	return 4
}

// --- shared execution core (used by both the sync and async accept paths) ---

type runOutcome struct {
	summary *batch.Summary
	charged int64
	review  any
}

// executeWorkload runs an accepted quote: fan-out in chunks, per-item metering and
// provider accrual, one settlement at the quoted price, then a content-free Council
// run review. onProgress (optional) is called after each chunk.
func (s *Server) executeWorkload(ctx context.Context, q *marketplace.Quote, keyID string,
	onProgress func(done, ok, failed int)) (*runOutcome, error) {

	acu := s.acuByPK(ctx)
	concurrency := 0
	if q.Spec.Spot {
		concurrency = marketplace.SpotMaxConcurrency
	}
	merged := &batch.Summary{Providers: map[string]int{}}
	var wallMS int64

	for _, chunk := range chunkItems(q.Spec.Items, batch.MaxItems) {
		sum, err := batch.Run(ctx, s.reg, func(ctx context.Context, prov *registry.Provider, rr relay.Request, onDelta func(string) error) (*relay.Result, error) {
			return relay.ExecuteOn(ctx, s.deps(), prov, rr, onDelta)
		}, chunk, batch.Options{
			Model:       q.Model,
			MinTier:     q.Spec.Tier,
			HWClass:     s.hwClassOf(q.Model),
			ACUByPK:     acu,
			Concurrency: concurrency,
		})
		if err != nil {
			return nil, err
		}
		mergeSummary(merged, sum, len(merged.Items))
		if sum.WallMS > wallMS {
			wallMS = sum.WallMS
		}
		if onProgress != nil {
			onProgress(len(merged.Items), merged.OK, merged.Failed)
		}
	}
	merged.WallMS = wallMS
	merged.Fanout = len(merged.Providers)

	priceMult := s.spotPriceMult(q.Spec.Spot)
	for _, it := range merged.Items {
		if it.ErrCode != "" {
			continue
		}
		s.recordJob(ctx, q.Model, &relay.Result{
			Usage: it.Usage, FinishReason: it.FinishReason,
			ProviderID: it.ProviderID, ProviderPK: it.ProviderPK,
			TrustTier: it.TrustTier, JobID: it.JobID,
		}, priceMult)
	}

	charge := q.Cost.TotalMicros
	if merged.OK == 0 {
		charge = 0 // nothing ran successfully — don't bill
	}
	if charge > 0 {
		if e := s.store.AddCredit(context.WithoutCancel(ctx), keyID, -charge, "workload", "", q.ID); e != nil {
			s.log.Warn("workload debit failed", "err", e)
		}
	}

	review := council.ReviewRun(ctx, council.RunStats{
		WorkloadID: q.ID, Model: q.Model,
		Items: len(q.Spec.Items), OK: merged.OK, Failed: merged.Failed,
		Fanout: merged.Fanout, Concurrency: merged.Concurrency, WallMS: merged.WallMS,
		PromptTokens: merged.PromptTokens, CompletionTokens: merged.CompletionTokens,
		QuotedUSD: float64(q.Cost.TotalMicros) / 1e6, ChargedUSD: float64(charge) / 1e6,
	})
	return &runOutcome{summary: merged, charged: charge, review: review}, nil
}

// --- async submission ---

// startAsyncRun records a queued run and executes it in the background. It returns the
// run row plus a one-time webhook secret when a webhook_url was supplied.
func (s *Server) startAsyncRun(ctx context.Context, q *marketplace.Quote, keyID, webhookURL, label string) (store.WorkloadRun, string) {
	now := time.Now()
	run := store.WorkloadRun{
		ID: newRunID(), KeyID: keyID, WorkloadID: q.ID, Model: q.Model,
		Status: runStatusQueued, TotalItems: len(q.Spec.Items),
		QuotedMicros: q.Cost.TotalMicros, WebhookURL: webhookURL, Label: label,
		CreatedAt: now,
	}
	secret := ""
	if webhookURL != "" {
		run.WebhookState = "pending"
		secret = newWebhookSecret()
		s.runs.setSecret(run.ID, secret)
	}
	if err := s.store.UpsertRun(context.WithoutCancel(ctx), run); err != nil {
		s.log.Warn("run row insert failed", "run_id", run.ID, "err", err)
	}
	s.runs.addInflight(keyID, 1)

	// Detach from the request: the client's connection closes as soon as we answer 202.
	bg := context.WithoutCancel(ctx)
	go s.executeAsyncRun(bg, run, q)
	return run, secret
}

func (s *Server) executeAsyncRun(ctx context.Context, run store.WorkloadRun, q *marketplace.Quote) {
	defer s.runs.addInflight(run.KeyID, -1)
	defer func() {
		if rec := recover(); rec != nil {
			s.log.Error("async run panicked", "run_id", run.ID, "panic", fmt.Sprint(rec))
			run.Status = runStatusFailed
			run.Error = "internal error"
			s.finishRun(ctx, &run, nil)
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, runMaxWall)
	defer cancel()

	started := time.Now()
	run.Status = runStatusRunning
	run.StartedAt = &started
	s.saveRun(ctx, run)

	var lastSave time.Time
	out, err := s.executeWorkload(ctx, q, run.KeyID, func(done, ok, failed int) {
		run.DoneItems, run.OKItems, run.FailedItems = done, ok, failed
		// Throttle progress writes; the row is for humans and pollers, not per-item truth.
		if time.Since(lastSave) > 2*time.Second {
			lastSave = time.Now()
			s.saveRun(ctx, run)
		}
	})
	if err != nil {
		run.Status = runStatusFailed
		run.Error = relayErrMessage(err)
		s.log.Warn("async run failed", "run_id", run.ID, "err", err)
		s.finishRun(ctx, &run, nil)
		return
	}

	run.Status = runStatusSucceeded
	run.DoneItems = len(out.summary.Items)
	run.OKItems, run.FailedItems = out.summary.OK, out.summary.Failed
	run.PromptTokens = int64(out.summary.PromptTokens)
	run.CompletionTokens = int64(out.summary.CompletionTokens)
	run.WallMS, run.Fanout = out.summary.WallMS, out.summary.Fanout
	run.ChargedMicros = out.charged
	s.finishRun(ctx, &run, out)
}

// finishRun stamps the end state, parks results in memory (never on disk) and fires the webhook.
func (s *Server) finishRun(ctx context.Context, run *store.WorkloadRun, out *runOutcome) {
	fin := time.Now()
	run.FinishedAt = &fin
	if out != nil {
		s.runs.put(run.ID, &runResults{
			summary: out.summary, review: out.review,
			quoted: run.QuotedMicros, charged: out.charged,
			expiresAt: fin.Add(s.runResultsTTL()),
		})
	}
	if run.WebhookURL != "" {
		if err := s.deliverRunWebhook(ctx, *run); err != nil {
			run.WebhookState = "failed"
			s.log.Warn("run webhook delivery failed", "run_id", run.ID, "err", err)
		} else {
			run.WebhookState = "delivered"
		}
	}
	s.saveRun(ctx, *run)
}

func (s *Server) saveRun(ctx context.Context, run store.WorkloadRun) {
	if err := s.store.UpsertRun(context.WithoutCancel(ctx), run); err != nil {
		s.log.Warn("run row update failed", "run_id", run.ID, "err", err)
	}
}

// relayErrMessage renders an execution failure without leaking job content.
func relayErrMessage(err error) string {
	msg := err.Error()
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return msg
}

// --- webhook ---

// deliverRunWebhook POSTs a content-free completion notice, signed like Stripe's:
//
//	X-Ayni-Signature: t=<unix>,v1=<hex hmac-sha256 of "t.body" with the run secret>
//
// The payload carries status and counts only; results are fetched over TLS with the API key.
func (s *Server) deliverRunWebhook(ctx context.Context, run store.WorkloadRun) error {
	body, err := json.Marshal(map[string]any{
		"type":        "workload_run." + run.Status,
		"run_id":      run.ID,
		"workload_id": run.WorkloadID,
		"label":       run.Label,
		"status":      run.Status,
		"model":       run.Model,
		"items":       map[string]int{"total": run.TotalItems, "ok": run.OKItems, "failed": run.FailedItems},
		"charged_usd": round2usd(run.ChargedMicros),
		"error":       run.Error,
		"results_url": strings.TrimRight(s.cfg.AuthCallbackBase, "/") + "/v1/runs/" + run.ID + "/results",
		"created":     time.Now().Unix(),
	})
	if err != nil {
		return err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(s.runs.secret(run.ID)))
	mac.Write([]byte(ts + "." + string(body)))
	sig := hex.EncodeToString(mac.Sum(nil))

	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, run.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ayni-coordinator/1")
	req.Header.Set("X-Ayni-Signature", "t="+ts+",v1="+sig)
	resp, err := safeWebhookClient(s.cfg.AllowInsecureWebhooks).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %d", resp.StatusCode)
	}
	return nil
}

// --- handlers ---

func (s *Server) renderRun(run store.WorkloadRun) map[string]any {
	out := map[string]any{
		"id":          run.ID,
		"object":      "workload.run",
		"workload_id": run.WorkloadID,
		"model":       run.Model,
		"status":      run.Status,
		"progress": map[string]int{
			"total": run.TotalItems, "done": run.DoneItems,
			"ok": run.OKItems, "failed": run.FailedItems,
		},
		"quoted_usd":  round2usd(run.QuotedMicros),
		"charged_usd": round2usd(run.ChargedMicros),
		"created_at":  run.CreatedAt.UTC().Format(time.RFC3339),
	}
	if run.Label != "" {
		out["label"] = run.Label
	}
	if run.StartedAt != nil {
		out["started_at"] = run.StartedAt.UTC().Format(time.RFC3339)
	}
	if run.FinishedAt != nil {
		out["finished_at"] = run.FinishedAt.UTC().Format(time.RFC3339)
		out["usage"] = map[string]int64{
			"prompt_tokens": run.PromptTokens, "completion_tokens": run.CompletionTokens,
			"total_tokens": run.PromptTokens + run.CompletionTokens,
		}
		out["wall_ms"] = run.WallMS
		out["fanout"] = run.Fanout
	}
	if run.WebhookState != "" {
		out["webhook_state"] = run.WebhookState
	}
	if run.Error != "" {
		out["error"] = run.Error
	}
	if run.Status == runStatusSucceeded {
		if res, ok := s.runs.get(run.ID); ok {
			out["results_available"] = true
			out["results_expire_at"] = res.expiresAt.UTC().Format(time.RFC3339)
		} else {
			out["results_available"] = false
			out["results_note"] = "results are held in memory only and have expired or were lost to a restart; re-run the workload to regenerate them"
		}
	}
	return out
}

// handleListRuns: GET /v1/runs?limit=50
func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := s.store.ListRuns(r.Context(), keyIDFrom(r.Context()), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_failed", "could not list runs")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, run := range rows {
		out = append(out, s.renderRun(run))
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": out, "count": len(out)})
}

// handleGetRun: GET /v1/runs/{id}
func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	run, ok := s.lookupRun(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.renderRun(run))
}

// handleGetRunResults: GET /v1/runs/{id}/results
func (s *Server) handleGetRunResults(w http.ResponseWriter, r *http.Request) {
	run, ok := s.lookupRun(w, r)
	if !ok {
		return
	}
	switch run.Status {
	case runStatusQueued, runStatusRunning:
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusConflict, "not_finished", "this run is still "+run.Status+"; poll /v1/runs/"+run.ID)
		return
	case runStatusFailed:
		writeError(w, http.StatusConflict, "run_failed", "this run failed: "+run.Error)
		return
	}
	res, ok := s.runs.get(run.ID)
	if !ok {
		writeError(w, http.StatusGone, "results_expired",
			"results are held in memory only and are no longer available; re-run the workload to regenerate them")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":          run.ID,
		"object":      "workload.result",
		"workload_id": run.WorkloadID,
		"model":       run.Model,
		"status":      run.Status,
		"quoted_usd":  round2usd(res.quoted),
		"charged_usd": round2usd(res.charged),
		"items":       renderBatchItems(res.summary),
		"usage": map[string]int{
			"prompt_tokens":     res.summary.PromptTokens,
			"completion_tokens": res.summary.CompletionTokens,
			"total_tokens":      res.summary.PromptTokens + res.summary.CompletionTokens,
		},
		"stats":             batchStats(res.summary),
		"run_review":        res.review,
		"results_expire_at": res.expiresAt.UTC().Format(time.RFC3339),
	})
}

// lookupRun resolves {id} and enforces that the run belongs to the calling key.
func (s *Server) lookupRun(w http.ResponseWriter, r *http.Request) (store.WorkloadRun, bool) {
	id := r.PathValue("id")
	run, found, err := s.store.GetRun(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_failed", "could not read run")
		return store.WorkloadRun{}, false
	}
	// Same 404 for "absent" and "someone else's" — no existence oracle across keys.
	if !found || run.KeyID != keyIDFrom(r.Context()) {
		writeError(w, http.StatusNotFound, "not_found", "no such run")
		return store.WorkloadRun{}, false
	}
	return run, true
}

// --- webhook SSRF guard ---

// validateWebhookURL is the cheap, synchronous check at submission time: scheme, host
// present, and no literal internal IP. It deliberately does NOT resolve DNS — that would
// make submission depend on name resolution and still lose to DNS rebinding. The binding
// guarantee is enforced at connect time by safeWebhookClient below.
func validateWebhookURL(raw string, allowInsecure bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("webhook_url is not a valid URL")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !allowInsecure {
			return fmt.Errorf("webhook_url must be an https:// URL")
		}
	default:
		return fmt.Errorf("webhook_url must be an https:// URL")
	}
	if u.Hostname() == "" {
		return fmt.Errorf("webhook_url must include a host")
	}
	if allowInsecure {
		return nil
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !isPublicIP(ip) {
		return fmt.Errorf("webhook_url must be a public address")
	}
	return nil
}

// isPublicIP reports whether an address is routable on the public internet. Loopback,
// private, link-local (which covers the 169.254.169.254 cloud metadata service),
// multicast and unspecified addresses are not.
func isPublicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified())
}

// safeWebhookClient refuses, at the moment the socket is opened, to connect to any
// non-public address. Checking here rather than before the request closes DNS rebinding:
// whatever the name resolved to, the actual destination address is what gets tested.
func safeWebhookClient(allowInsecure bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if allowInsecure {
		return &http.Client{Transport: &http.Transport{DialContext: dialer.DialContext}}
	}
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ip := net.ParseIP(host)
			if ip == nil || !isPublicIP(ip) {
				return nil, fmt.Errorf("refusing to connect to non-public address %s", host)
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}}
}
