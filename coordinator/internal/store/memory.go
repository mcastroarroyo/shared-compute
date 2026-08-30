package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"sync/atomic"
)

// Mem is the env-configured, non-durable store. Keys and tokens come from config; usage
// events are counted but not retained.
type Mem struct {
	keys   map[string]string // key -> stable id (sha256 prefix)
	tokens map[string]struct{}

	mu        sync.Mutex
	providers map[string]ProviderRecord
	usage     atomic.Int64
}

// NewMem builds an in-memory store from the accepted consumer keys and provider tokens.
func NewMem(consumerKeys, providerTokens map[string]struct{}) *Mem {
	keys := make(map[string]string, len(consumerKeys))
	for k := range consumerKeys {
		keys[k] = keyID(k)
	}
	toks := make(map[string]struct{}, len(providerTokens))
	for t := range providerTokens {
		toks[t] = struct{}{}
	}
	return &Mem{keys: keys, tokens: toks, providers: map[string]ProviderRecord{}}
}

func keyID(k string) string {
	sum := sha256.Sum256([]byte(k))
	return "key_" + hex.EncodeToString(sum[:6])
}

func (m *Mem) Migrate(context.Context) error { return nil }

func (m *Mem) ValidateConsumerKey(_ context.Context, key string) (string, bool) {
	id, ok := m.keys[key]
	return id, ok
}

func (m *Mem) ValidateProviderToken(_ context.Context, token string) bool {
	_, ok := m.tokens[token]
	return ok
}

func (m *Mem) UpsertProvider(_ context.Context, p ProviderRecord) error {
	m.mu.Lock()
	m.providers[p.ProviderID] = p
	m.mu.Unlock()
	return nil
}

func (m *Mem) RecordUsage(_ context.Context, _ UsageEvent) error {
	m.usage.Add(1)
	return nil
}

// UsageCount is exposed for tests / debugging.
func (m *Mem) UsageCount() int64 { return m.usage.Load() }

func (m *Mem) Close() {}
