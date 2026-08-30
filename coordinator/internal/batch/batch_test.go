package batch

import (
	"context"
	"encoding/base64"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/protocol"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/registry"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/relay"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/scheduler"
)

func mkProvider(id string, keyByte byte, model string) *registry.Provider {
	var pk [32]byte
	pk[0] = keyByte
	return &registry.Provider{
		ID:       id,
		StaticPK: pk,
		Capabilities: protocol.Capabilities{
			Models: []string{model}, HardwareClass: "SMALL", MaxContext: 8192,
		},
		Telemetry: protocol.Telemetry{MemAvailableMB: 4000, ThermalState: "nominal"},
		Send:      func(any) error { return nil },
	}
}

func mkItems(n int) []Item {
	items := make([]Item, n)
	for i := range items {
		items[i] = Item{Messages: []protocol.ChatMessage{{Role: "user", Content: strconv.Itoa(i)}}}
	}
	return items
}

// echoExec returns the item's own text as the completion. Optional delay and a
// predicate that turns an item into a provider_gone failure.
func echoExec(delay time.Duration, fail func(text string) bool) ExecFunc {
	return func(ctx context.Context, prov *registry.Provider, req relay.Request, onDelta func(string) error) (*relay.Result, error) {
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		text := req.Messages[0].Content
		if fail != nil && fail(text) {
			return nil, relay.ErrProviderGone
		}
		_ = onDelta(text)
		return &relay.Result{
			Usage:        protocol.Usage{PromptTokens: 1, CompletionTokens: 2},
			FinishReason: "stop",
			ProviderID:   prov.ID,
			ProviderPK:   base64.StdEncoding.EncodeToString(prov.StaticPK[:]),
			TrustTier:    prov.TrustTier,
			JobID:        "job-" + text,
		}, nil
	}
}

func TestRunPreservesOrderAndTotals(t *testing.T) {
	reg := registry.New()
	reg.Add(mkProvider("a", 1, "m"))

	sum, err := Run(context.Background(), reg, echoExec(0, nil), mkItems(6), Options{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if sum.OK != 6 || sum.Failed != 0 {
		t.Fatalf("ok=%d failed=%d", sum.OK, sum.Failed)
	}
	for i, it := range sum.Items {
		if it.Index != i || it.Content != strconv.Itoa(i) {
			t.Fatalf("item %d out of order: index=%d content=%q", i, it.Index, it.Content)
		}
	}
	if sum.PromptTokens != 6 || sum.CompletionTokens != 12 {
		t.Fatalf("tokens prompt=%d completion=%d", sum.PromptTokens, sum.CompletionTokens)
	}
}

func TestRunSpreadsAcrossProviders(t *testing.T) {
	reg := registry.New()
	for i := 0; i < 3; i++ {
		reg.Add(mkProvider("p"+strconv.Itoa(i), byte(i+1), "m"))
	}
	var mu sync.Mutex
	seen := map[string]int{}
	exec := func(ctx context.Context, prov *registry.Provider, req relay.Request, onDelta func(string) error) (*relay.Result, error) {
		mu.Lock()
		seen[prov.ID]++
		mu.Unlock()
		time.Sleep(3 * time.Millisecond)
		return &relay.Result{Usage: protocol.Usage{PromptTokens: 1, CompletionTokens: 1}, ProviderID: prov.ID}, nil
	}
	sum, err := Run(context.Background(), reg, exec, mkItems(30), Options{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Fanout != 3 {
		t.Fatalf("want all 3 providers used, got fanout=%d (%v)", sum.Fanout, seen)
	}
	lo, hi := 1<<30, 0
	for _, c := range seen {
		if c < lo {
			lo = c
		}
		if c > hi {
			hi = c
		}
	}
	if hi-lo > 3 {
		t.Fatalf("uneven spread: %v (spread %d)", seen, hi-lo)
	}
}

func TestRunConcurrencyClampAndPeak(t *testing.T) {
	reg := registry.New()
	reg.Add(mkProvider("a", 1, "m"))

	var cur, peak int64
	exec := func(ctx context.Context, prov *registry.Provider, req relay.Request, onDelta func(string) error) (*relay.Result, error) {
		n := atomic.AddInt64(&cur, 1)
		for {
			p := atomic.LoadInt64(&peak)
			if n <= p || atomic.CompareAndSwapInt64(&peak, p, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		atomic.AddInt64(&cur, -1)
		return &relay.Result{Usage: protocol.Usage{}, ProviderID: prov.ID}, nil
	}

	sum, err := Run(context.Background(), reg, exec, mkItems(4), Options{Model: "m", Concurrency: 100})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Concurrency != 4 {
		t.Fatalf("concurrency should clamp to item count 4, got %d", sum.Concurrency)
	}
	if peak > 4 {
		t.Fatalf("observed %d concurrent execs, want <= 4", peak)
	}
}

func TestRunConcurrencyDefault(t *testing.T) {
	reg := registry.New()
	reg.Add(mkProvider("a", 1, "m"))
	sum, err := Run(context.Background(), reg, echoExec(time.Millisecond, nil), mkItems(20), Options{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Concurrency != DefaultConcurrency {
		t.Fatalf("want default concurrency %d, got %d", DefaultConcurrency, sum.Concurrency)
	}
}

func TestRunPartialFailure(t *testing.T) {
	reg := registry.New()
	reg.Add(mkProvider("a", 1, "m"))
	odd := func(text string) bool { n, _ := strconv.Atoi(text); return n%2 == 1 }

	sum, err := Run(context.Background(), reg, echoExec(0, odd), mkItems(10), Options{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if sum.OK != 5 || sum.Failed != 5 {
		t.Fatalf("ok=%d failed=%d", sum.OK, sum.Failed)
	}
	for i, it := range sum.Items {
		if i%2 == 1 {
			if it.ErrCode != "provider_gone" || it.Content != "" {
				t.Fatalf("item %d: want provider_gone/empty, got code=%q content=%q", i, it.ErrCode, it.Content)
			}
		} else if it.ErrCode != "" {
			t.Fatalf("item %d: unexpected error %q", i, it.ErrCode)
		}
	}
	if sum.PromptTokens != 5 { // only OK items count
		t.Fatalf("prompt tokens should be 5, got %d", sum.PromptTokens)
	}
}

func TestRunNoEligibleProvider(t *testing.T) {
	reg := registry.New()
	_, err := Run(context.Background(), reg, echoExec(0, nil), mkItems(3), Options{Model: "m"})
	if err != scheduler.ErrNoProvider {
		t.Fatalf("want ErrNoProvider, got %v", err)
	}
}

func TestRunPrefersHigherACU(t *testing.T) {
	reg := registry.New()
	lo := mkProvider("lo", 1, "m")
	hi := mkProvider("hi", 2, "m")
	reg.Add(lo)
	reg.Add(hi)
	acu := map[string]float64{
		base64.StdEncoding.EncodeToString(lo.StaticPK[:]): 0.5,
		base64.StdEncoding.EncodeToString(hi.StaticPK[:]): 5.0,
	}
	sum, err := Run(context.Background(), reg, echoExec(0, nil), mkItems(1), Options{Model: "m", ACUByPK: acu})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Items[0].ProviderID != "hi" {
		t.Fatalf("want the high-ACU provider, got %s", sum.Items[0].ProviderID)
	}
}

func TestRunContextAlreadyCancelled(t *testing.T) {
	reg := registry.New()
	reg.Add(mkProvider("a", 1, "m"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sum, err := Run(ctx, reg, echoExec(0, nil), mkItems(4), Options{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if sum.OK != 0 || sum.Failed != 4 {
		t.Fatalf("ok=%d failed=%d", sum.OK, sum.Failed)
	}
	for _, it := range sum.Items {
		if it.ErrCode != "client_closed" {
			t.Fatalf("want client_closed, got %q", it.ErrCode)
		}
	}
}
