package marketplace

import (
	"strings"
	"testing"
	"time"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/batch"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/pricing"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/protocol"
)

func itemWith(content string, maxTok int) batch.Item {
	return batch.Item{
		Messages: []protocol.ChatMessage{{Role: "user", Content: content}},
		Params:   protocol.SamplingParams{MaxTokens: maxTok},
	}
}

func TestEstimateFromItems(t *testing.T) {
	spec := Spec{
		Model: "m", Tier: "community", Redundancy: 1,
		Items: []batch.Item{
			itemWith(strings.Repeat("a", 400), 100), // ~101 prompt tokens, 100 completion
			itemWith(strings.Repeat("b", 800), 200),
		},
	}
	est := EstimateWorkload(spec, Supply{Nodes: 2, AggregateTPS: 100})
	if est.Items != 2 {
		t.Fatalf("items = %d", est.Items)
	}
	if est.CompletionTokens != 300 {
		t.Fatalf("completion tokens = %d, want 300", est.CompletionTokens)
	}
	// prompt ~ (400+800 + small overhead) / 4
	if est.PromptTokens < 300 || est.PromptTokens > 320 {
		t.Fatalf("prompt tokens = %d, want ~300-320", est.PromptTokens)
	}
	if est.ETASeconds < 1 {
		t.Fatalf("eta = %d", est.ETASeconds)
	}
}

func TestEstimateFromCount(t *testing.T) {
	spec := Spec{
		Model: "m", Tier: "community", Redundancy: 1,
		EstimateCount: 1000, AvgPromptTokens: 400, AvgCompletionTokens: 150,
	}
	est := EstimateWorkload(spec, Supply{Nodes: 5, AggregateTPS: 250})
	if est.Items != 1000 || est.PromptTokens != 400_000 || est.CompletionTokens != 150_000 {
		t.Fatalf("got items=%d p=%d c=%d", est.Items, est.PromptTokens, est.CompletionTokens)
	}
	// gen time ~ 150000 / 250 = 600s, plus a little
	if est.ETASeconds < 600 || est.ETASeconds > 900 {
		t.Fatalf("eta = %d, want ~600-900", est.ETASeconds)
	}
}

func TestEstimateFasterSupplyLowersETA(t *testing.T) {
	spec := Spec{Model: "m", Redundancy: 1, EstimateCount: 500, AvgPromptTokens: 200, AvgCompletionTokens: 200}
	slow := EstimateWorkload(spec, Supply{Nodes: 2, AggregateTPS: 40})
	fast := EstimateWorkload(spec, Supply{Nodes: 2, AggregateTPS: 400})
	if fast.ETASeconds >= slow.ETASeconds {
		t.Fatalf("faster supply eta %d should be < slower %d", fast.ETASeconds, slow.ETASeconds)
	}
}

func TestEstimateRedundancyRaisesETA(t *testing.T) {
	base := Spec{Model: "m", EstimateCount: 500, AvgPromptTokens: 100, AvgCompletionTokens: 200}
	one := base
	one.Redundancy = 1
	two := base
	two.Redundancy = 2
	sup := Supply{Nodes: 3, AggregateTPS: 100}
	if EstimateWorkload(two, sup).ETASeconds <= EstimateWorkload(one, sup).ETASeconds {
		t.Fatal("redundancy 2 should raise the ETA")
	}
}

func TestStoreCreateGetExpire(t *testing.T) {
	st := NewStore(40 * time.Millisecond)
	spec := Spec{Model: "m", Redundancy: 1, EstimateCount: 10}
	q := st.Create("key_1", "m", spec, Estimate{Items: 10}, pricing.WorkloadCost{TotalMicros: 5000})
	if q.ID == "" || !strings.HasPrefix(q.ID, "wl_") {
		t.Fatalf("bad id %q", q.ID)
	}
	if _, ok := st.Get(q.ID); !ok {
		t.Fatal("quote should be retrievable immediately")
	}
	time.Sleep(60 * time.Millisecond)
	if _, ok := st.Get(q.ID); ok {
		t.Fatal("quote should have expired")
	}
}

func TestStoreMarkAcceptedOnce(t *testing.T) {
	st := NewStore(time.Minute)
	q := st.Create("key_1", "m", Spec{Model: "m", Redundancy: 1}, Estimate{}, pricing.WorkloadCost{})
	if _, ok := st.MarkAccepted(q.ID); !ok {
		t.Fatal("first accept should succeed")
	}
	if _, ok := st.MarkAccepted(q.ID); ok {
		t.Fatal("second accept must fail")
	}
	got, _ := st.Get(q.ID)
	if !got.Accepted {
		t.Fatal("quote should be marked accepted")
	}
}

func TestSpecRunnable(t *testing.T) {
	if (Spec{EstimateCount: 5}).Runnable() {
		t.Fatal("estimate-only spec is not runnable")
	}
	if !(Spec{Items: []batch.Item{itemWith("hi", 8)}}).Runnable() {
		t.Fatal("spec with items is runnable")
	}
}
