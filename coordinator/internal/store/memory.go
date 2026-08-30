package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Mem is the env-configured, non-durable store. Keys and tokens come from config; usage
// events are counted but not retained. Keys created at runtime live only until restart.
type Mem struct {
	mu          sync.Mutex
	keys        map[string]string // raw key -> stable id
	keyInfo     map[string]KeyInfo
	tokens      map[string]struct{}
	rawByID     map[string]string // id -> raw key (Mem only, so disable works)
	providers   map[string]ProviderRecord
	usageRows   []UsageRow
	earningRows []EarningRow
	waitlist    []WaitlistEntry
	proposals   []Proposal
	credits     map[string]int64         // key_id -> micros
	topupRefs   map[string]struct{}      // idempotency for topups
	payoutAccts map[string]PayoutAccount // static_pk -> account
	accrued     map[string]*ProviderAccrual
	nextPayout  int64
	usage       atomic.Int64
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
	return &Mem{
		keys:        keys,
		keyInfo:     map[string]KeyInfo{},
		tokens:      toks,
		rawByID:     map[string]string{},
		providers:   map[string]ProviderRecord{},
		credits:     map[string]int64{},
		topupRefs:   map[string]struct{}{},
		payoutAccts: map[string]PayoutAccount{},
		accrued:     map[string]*ProviderAccrual{},
	}
}

func keyID(k string) string {
	sum := sha256.Sum256([]byte(k))
	return "key_" + hex.EncodeToString(sum[:6])
}

func (m *Mem) Migrate(context.Context) error { return nil }

func (m *Mem) ValidateConsumerKey(_ context.Context, key string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.keys[key]
	if !ok {
		return "", false
	}
	if info, has := m.keyInfo[id]; has && info.Disabled {
		return "", false
	}
	return id, true
}

func randToken(prefix string) string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func (m *Mem) CreateConsumerKey(_ context.Context, label string) (string, string, error) {
	raw := randToken("sc_live_")
	id := keyID(raw)
	m.mu.Lock()
	m.keys[raw] = id
	m.rawByID[id] = raw
	m.keyInfo[id] = KeyInfo{ID: id, Label: label, CreatedAt: time.Now()}
	m.mu.Unlock()
	return id, raw, nil
}

func (m *Mem) ListConsumerKeys(_ context.Context) ([]KeyInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]KeyInfo, 0, len(m.keyInfo))
	for _, v := range m.keyInfo {
		out = append(out, v)
	}
	return out, nil
}

func (m *Mem) SetConsumerKeyDisabled(_ context.Context, id string, disabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.keyInfo[id]
	if !ok {
		return fmt.Errorf("no such key: %s", id)
	}
	info.Disabled = disabled
	m.keyInfo[id] = info
	return nil
}

func (m *Mem) UsageSince(_ context.Context, _ time.Time) ([]UsageRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]UsageRow, len(m.usageRows))
	copy(out, m.usageRows)
	return out, nil
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

func (m *Mem) RecordUsage(_ context.Context, ev UsageEvent) error {
	m.usage.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.usageRows {
		if m.usageRows[i].KeyID == ev.KeyID && m.usageRows[i].Model == ev.Model {
			m.usageRows[i].Requests++
			m.usageRows[i].PromptTokens += int64(ev.PromptTokens)
			m.usageRows[i].CompletionTokens += int64(ev.CompletionTokens)
			return nil
		}
	}
	m.usageRows = append(m.usageRows, UsageRow{
		KeyID: ev.KeyID, Model: ev.Model, Requests: 1,
		PromptTokens: int64(ev.PromptTokens), CompletionTokens: int64(ev.CompletionTokens),
	})
	return nil
}

// UsageCount is exposed for tests / debugging.
func (m *Mem) UsageCount() int64 { return m.usage.Load() }

func (m *Mem) RecordEarning(_ context.Context, ev EarningEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.earningRows {
		if m.earningRows[i].ProviderID == ev.ProviderID {
			m.earningRows[i].Jobs++
			m.earningRows[i].GrossMicros += ev.GrossMicros
			m.earningRows[i].ProviderMicros += ev.ProviderMicros
			return nil
		}
	}
	m.earningRows = append(m.earningRows, EarningRow{
		ProviderID: ev.ProviderID, Jobs: 1,
		GrossMicros: ev.GrossMicros, ProviderMicros: ev.ProviderMicros,
	})
	if ev.StaticPK != "" {
		a := m.accrued[ev.StaticPK]
		if a == nil {
			a = &ProviderAccrual{StaticPK: ev.StaticPK}
			m.accrued[ev.StaticPK] = a
		}
		a.Jobs++
		a.OwedMicros += ev.ProviderMicros
	}
	return nil
}

func (m *Mem) CreditBalance(_ context.Context, keyID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.credits[keyID], nil
}

func (m *Mem) AddCredit(_ context.Context, keyID string, deltaMicros int64, reason, stripeRef, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if reason == "topup" && stripeRef != "" {
		if _, seen := m.topupRefs[stripeRef]; seen {
			return nil
		}
		m.topupRefs[stripeRef] = struct{}{}
	}
	m.credits[keyID] += deltaMicros
	return nil
}

func (m *Mem) UpsertPayoutAccount(_ context.Context, a PayoutAccount) error {
	m.mu.Lock()
	m.payoutAccts[a.StaticPK] = a
	m.mu.Unlock()
	return nil
}

func (m *Mem) GetPayoutAccount(_ context.Context, staticPK string) (PayoutAccount, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.payoutAccts[staticPK]
	return a, ok, nil
}

func (m *Mem) PayoutAccountByStripe(_ context.Context, stripeAccount string) (PayoutAccount, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.payoutAccts {
		if a.StripeAccount == stripeAccount {
			return a, true, nil
		}
	}
	return PayoutAccount{}, false, nil
}

func (m *Mem) AccruedByProvider(_ context.Context) ([]ProviderAccrual, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []ProviderAccrual
	for _, a := range m.accrued {
		if a.OwedMicros > 0 {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (m *Mem) RecordPayoutAndSettle(_ context.Context, po PayoutRecord) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextPayout++
	if po.State == "created" {
		if a := m.accrued[po.StaticPK]; a != nil {
			a.Jobs = 0
			a.OwedMicros = po.RemainderMicros // carry the dust forward
		}
	}
	return m.nextPayout, nil
}

func (m *Mem) EarningsSince(_ context.Context, _ time.Time) ([]EarningRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]EarningRow, len(m.earningRows))
	copy(out, m.earningRows)
	return out, nil
}

func (m *Mem) AddWaitlist(_ context.Context, e WaitlistEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.waitlist {
		if strings.EqualFold(m.waitlist[i].Email, e.Email) {
			m.waitlist[i] = e
			return nil
		}
	}
	m.waitlist = append(m.waitlist, e)
	return nil
}

func (m *Mem) AddProposal(_ context.Context, p Proposal) error {
	m.mu.Lock()
	m.proposals = append(m.proposals, p)
	m.mu.Unlock()
	return nil
}

func (m *Mem) WaitlistSince(_ context.Context, _ time.Time) ([]WaitlistEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]WaitlistEntry, len(m.waitlist))
	copy(out, m.waitlist)
	return out, nil
}

func (m *Mem) ProposalsSince(_ context.Context, _ time.Time) ([]Proposal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Proposal, len(m.proposals))
	copy(out, m.proposals)
	return out, nil
}

func (m *Mem) Close() {}
