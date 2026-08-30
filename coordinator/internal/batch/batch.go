// Package batch runs many independent inference items for one model as a single
// unit: fan the items out across every eligible provider, run them in parallel
// with a bounded worker pool, and reassemble the results in submission order.
//
// It is the execution primitive the marketplace quote engine (M10.3) sits on:
// "here are N prompts for model X" -> "here are N answers, here is what each
// provider did, here is the wall time and token total".
//
// No prompt or completion text is logged here; the plaintext of an item lives
// only inside the exec callback, exactly as in package relay.
package batch

import (
	"context"
	"encoding/base64"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/protocol"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/registry"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/relay"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/scheduler"
)

// MaxItems caps a single batch. Beyond this the caller should page.
const MaxItems = 256

// DefaultConcurrency is used when the caller does not pin one.
const DefaultConcurrency = 8

// MaxConcurrency bounds the worker pool regardless of what the caller asks for.
const MaxConcurrency = 16

// Item is one unit of work: a chat turn plus its sampling params.
type Item struct {
	Messages []protocol.ChatMessage
	Params   protocol.SamplingParams
}

// ItemResult is the outcome of one Item, tagged with its submission Index.
type ItemResult struct {
	Index        int
	Content      string
	Usage        protocol.Usage
	FinishReason string
	ProviderID   string
	ProviderPK   string // base64 X25519 static key — the payout identity
	TrustTier    string
	JobID        string
	ErrCode      string // "" on success
	ErrMsg       string
	LatencyMS    int64
}

// Summary aggregates a whole batch.
type Summary struct {
	Items            []ItemResult
	OK               int
	Failed           int
	Fanout           int            // distinct providers that handled at least one item
	Providers        map[string]int // provider ID -> items handled (successful or not)
	PromptTokens     int
	CompletionTokens int
	Concurrency      int
	WallMS           int64
}

// Options controls a run.
type Options struct {
	Model       string
	MinTier     string
	HWClass     string
	MinContext  int
	Concurrency int                // 0 -> DefaultConcurrency; clamped to MaxConcurrency
	ACUByPK     map[string]float64 // optional: base64 static_pk -> ACU, to prefer faster nodes
}

// ExecFunc runs one item against a specific provider. Production passes a thin
// wrapper over relay.ExecuteOn; tests pass a stub.
type ExecFunc func(ctx context.Context, prov *registry.Provider, req relay.Request, onDelta func(string) error) (*relay.Result, error)

// Run fans items out across the providers eligible for opt.Model and blocks
// until every item has finished, failed, or ctx is done. A per-item failure is
// recorded in its ItemResult, not returned as an error; Run only returns an
// error when *nothing* could be attempted (no eligible provider).
func Run(ctx context.Context, reg *registry.Registry, exec ExecFunc, items []Item, opt Options) (*Summary, error) {
	cands, err := scheduler.Eligible(reg, scheduler.Requirements{
		Model:      opt.Model,
		MinTier:    opt.MinTier,
		MinContext: opt.MinContext,
		HWClass:    opt.HWClass,
	})
	if err != nil {
		return nil, err
	}

	// Prefer higher-ACU nodes when we have a score for them; scheduler.Eligible
	// order (least loaded, cooler, ...) is the stable tiebreak.
	if len(opt.ACUByPK) > 0 {
		base := make(map[string]int, len(cands))
		for i, p := range cands {
			base[p.ID] = i
		}
		sort.SliceStable(cands, func(i, j int) bool {
			ai := opt.ACUByPK[pk(cands[i])]
			aj := opt.ACUByPK[pk(cands[j])]
			if ai != aj {
				return ai > aj
			}
			return base[cands[i].ID] < base[cands[j].ID]
		})
	}

	workers := opt.Concurrency
	if workers <= 0 {
		workers = DefaultConcurrency
	}
	if workers > MaxConcurrency {
		workers = MaxConcurrency
	}
	if workers > len(items) {
		workers = len(items)
	}

	results := make([]ItemResult, len(items))

	// choose() spreads work evenly regardless of the global registry counter:
	// fewest in-flight items from this batch wins, then fewest total assigned so
	// far (so ties don't systematically favour the first candidate), then the
	// scheduler's own ranking.
	var mu sync.Mutex
	inflight := make(map[string]int, len(cands))
	assigned := make(map[string]int, len(cands))
	choose := func() *registry.Provider {
		mu.Lock()
		defer mu.Unlock()
		best := cands[0]
		for _, p := range cands[1:] {
			bi, pi := inflight[best.ID], inflight[p.ID]
			if pi < bi || (pi == bi && assigned[p.ID] < assigned[best.ID]) {
				best = p
			}
		}
		inflight[best.ID]++
		assigned[best.ID]++
		return best
	}
	release := func(p *registry.Provider) {
		mu.Lock()
		inflight[p.ID]--
		mu.Unlock()
	}

	start := time.Now()
	work := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				results[idx] = runOne(ctx, exec, choose, release, opt.Model, items[idx], idx)
			}
		}()
	}
	for i := range items {
		work <- i
	}
	close(work)
	wg.Wait()

	return summarize(results, workers, time.Since(start).Milliseconds()), nil
}

func runOne(ctx context.Context, exec ExecFunc, choose func() *registry.Provider, release func(*registry.Provider), model string, it Item, idx int) ItemResult {
	res := ItemResult{Index: idx}
	if err := ctx.Err(); err != nil {
		res.ErrCode, res.ErrMsg = relay.ErrCode(err)
		return res
	}
	prov := choose()
	defer release(prov)

	t0 := time.Now()
	var sb strings.Builder
	out, err := exec(ctx, prov, relay.Request{
		Model:    model,
		Messages: it.Messages,
		Params:   it.Params,
	}, func(delta string) error {
		sb.WriteString(delta)
		return nil
	})
	res.LatencyMS = time.Since(t0).Milliseconds()
	res.ProviderID = prov.ID
	if err != nil {
		res.ErrCode, res.ErrMsg = relay.ErrCode(err)
		return res
	}
	res.Content = sb.String()
	res.Usage = out.Usage
	res.FinishReason = out.FinishReason
	res.ProviderPK = out.ProviderPK
	res.TrustTier = out.TrustTier
	res.JobID = out.JobID
	if out.ProviderID != "" {
		res.ProviderID = out.ProviderID
	}
	return res
}

func summarize(results []ItemResult, workers int, wallMS int64) *Summary {
	s := &Summary{
		Items:       results,
		Providers:   map[string]int{},
		Concurrency: workers,
		WallMS:      wallMS,
	}
	for _, r := range results {
		if r.ProviderID != "" {
			s.Providers[r.ProviderID]++
		}
		if r.ErrCode != "" {
			s.Failed++
			continue
		}
		s.OK++
		s.PromptTokens += r.Usage.PromptTokens
		s.CompletionTokens += r.Usage.CompletionTokens
	}
	s.Fanout = len(s.Providers)
	return s
}

func pk(p *registry.Provider) string {
	return base64.StdEncoding.EncodeToString(p.StaticPK[:])
}
