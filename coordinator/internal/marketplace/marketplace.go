// Package marketplace turns a workload request into a customer quote: estimate
// the token volume, look at the supply that could run it, predict a wall-clock
// completion time from measured ACU throughput, and price it bottom-up
// (package pricing). Accepted quotes are executed via the batch fan-out
// primitive (package batch).
//
// This is Ayni Marketplace v0.1: "tell Ayni what to run -> one price + ETA ->
// accept -> it runs and pays the devices."
package marketplace

import (
	"crypto/rand"
	"encoding/hex"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/batch"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/pricing"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/protocol"
)

// Limits.
const (
	MaxItems                = 5000
	MaxRedundancy           = 3
	DefaultCompletionTokens = 512
)

// Spec is a validated workload request. Exactly one of Items or EstimateCount is
// the source of the volume estimate; Items is also what Accept runs.
type Spec struct {
	Model      string
	ModelClass string
	Tier       string
	Redundancy int

	Items []batch.Item // explicit work; empty => estimate-only quote

	EstimateCount       int // used when Items is empty
	AvgPromptTokens     int
	AvgCompletionTokens int
}

// Runnable reports whether Accept can execute this workload (it has real items).
func (s Spec) Runnable() bool { return len(s.Items) > 0 }

// Supply is the slice of the network that could take this workload right now.
type Supply struct {
	Nodes        int
	AggregateTPS float64 // sum of eligible nodes' measured sustained tok/s
	TopACU       float64
}

// Estimate is the volume + timing prediction shown with a quote.
type Estimate struct {
	Items            int     `json:"items"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	EligibleNodes    int     `json:"eligible_nodes"`
	AggregateTPS     float64 `json:"aggregate_tps"`
	ETASeconds       int64   `json:"eta_seconds"`
}

// EstimateWorkload predicts token volume and wall-clock time for spec given sup.
func EstimateWorkload(spec Spec, sup Supply) Estimate {
	red := spec.Redundancy
	if red < 1 {
		red = 1
	}

	var items int
	var prompt, completion int64
	if len(spec.Items) > 0 {
		items = len(spec.Items)
		for _, it := range spec.Items {
			prompt += estPromptTokens(it.Messages)
			c := it.Params.MaxTokens
			if c <= 0 {
				c = DefaultCompletionTokens
			}
			completion += int64(c)
		}
	} else {
		items = spec.EstimateCount
		p := spec.AvgPromptTokens
		if p < 0 {
			p = 0
		}
		c := spec.AvgCompletionTokens
		if c <= 0 {
			c = DefaultCompletionTokens
		}
		prompt = int64(items) * int64(p)
		completion = int64(items) * int64(c)
	}

	tps := sup.AggregateTPS
	if tps < 1 {
		tps = 1
	}
	// generation dominates; prefill runs ~8x faster than decode; add a little
	// per-round coordination overhead for fan-out.
	gen := float64(completion*int64(red)) / tps
	prefill := float64(prompt*int64(red)) / (tps * 8)
	coord := 0.0
	if sup.Nodes > 0 {
		coord = math.Ceil(float64(items)/float64(sup.Nodes)) * 0.03
	}
	eta := int64(gen+prefill+coord) + 1

	return Estimate{
		Items:            items,
		PromptTokens:     prompt,
		CompletionTokens: completion,
		EligibleNodes:    sup.Nodes,
		AggregateTPS:     round1(tps),
		ETASeconds:       eta,
	}
}

// estPromptTokens approximates tokens from character count (~4 chars/token). The
// coordinator has no tokeniser; actual billing uses provider-reported usage.
func estPromptTokens(msgs []protocol.ChatMessage) int64 {
	chars := 0
	for _, m := range msgs {
		chars += len(m.Content) + len(m.Role) + 4
	}
	t := int64(math.Ceil(float64(chars) / 4.0))
	if t < 1 {
		t = 1
	}
	return t
}

// Quote is a priced, time-boxed offer. It holds the Spec so Accept can run it.
type Quote struct {
	ID         string
	KeyID      string
	Model      string
	Spec       Spec
	Estimate   Estimate
	Cost       pricing.WorkloadCost
	CreatedAt  time.Time
	ExpiresAt  time.Time
	Accepted   bool
	AcceptedAt time.Time
}

// Expired reports whether the quote can no longer be accepted.
func (q *Quote) Expired(now time.Time) bool { return now.After(q.ExpiresAt) }

// Store is an in-memory, TTL'd set of outstanding quotes. Single-coordinator
// only (Fly runs one machine); a restart drops quotes, which is fine at a
// ten-minute TTL.
type Store struct {
	mu  sync.Mutex
	m   map[string]*Quote
	ttl time.Duration
	max int
}

// NewStore builds a quote store with the given TTL (<=0 -> 10 minutes).
func NewStore(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &Store{m: make(map[string]*Quote), ttl: ttl, max: 2048}
}

// TTL is the lifetime a Put quote gets.
func (s *Store) TTL() time.Duration { return s.ttl }

// Create stores a new quote for keyID and returns it with ID/timestamps set.
func (s *Store) Create(keyID, model string, spec Spec, est Estimate, cost pricing.WorkloadCost) *Quote {
	now := time.Now()
	q := &Quote{
		ID:        "wl_" + randID(),
		KeyID:     keyID,
		Model:     model,
		Spec:      spec,
		Estimate:  est,
		Cost:      cost,
		CreatedAt: now,
		ExpiresAt: now.Add(s.ttl),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked(now)
	if len(s.m) >= s.max {
		// drop the oldest to bound memory
		var oldest string
		var oldestAt time.Time
		for id, qq := range s.m {
			if oldest == "" || qq.CreatedAt.Before(oldestAt) {
				oldest, oldestAt = id, qq.CreatedAt
			}
		}
		delete(s.m, oldest)
	}
	s.m[q.ID] = q
	return q
}

// Get returns the quote if it exists and has not expired.
func (s *Store) Get(id string) (*Quote, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.m[id]
	if !ok || q.Expired(time.Now()) {
		return nil, false
	}
	return q, true
}

// MarkAccepted flips the quote to accepted under lock. Returns false if it was
// missing, expired, or already accepted.
func (s *Store) MarkAccepted(id string) (*Quote, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.m[id]
	if !ok || q.Expired(time.Now()) || q.Accepted {
		return nil, false
	}
	q.Accepted = true
	q.AcceptedAt = time.Now()
	return q, true
}

func (s *Store) gcLocked(now time.Time) {
	for id, q := range s.m {
		if q.Expired(now) {
			delete(s.m, id)
		}
	}
}

func randID() string {
	var b [9]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// NormalizeTier lowercases and defaults an empty tier to "community".
func NormalizeTier(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	if t == "" {
		return "community"
	}
	return t
}
