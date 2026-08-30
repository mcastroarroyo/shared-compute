// Package store persists consumer API keys, provider registration tokens, known provider
// identities, and usage events. The in-memory implementation (env-configured) is used
// when SC_DATABASE_URL is unset; the Postgres implementation is used otherwise. Prompt and
// completion content is never stored — only counts and metadata.
package store

import (
	"context"
	"time"
)

// UsageEvent is one metered inference job.
type UsageEvent struct {
	KeyID            string
	ProviderID       string
	Model            string
	Tier             string
	PromptTokens     int
	CompletionTokens int
	DurationMS       int64
	FinishReason     string
	CreatedAt        time.Time
}

// ProviderRecord is a durable note that a given static key has been seen.
type ProviderRecord struct {
	ProviderID string
	StaticPK   string // base64
	Platform   string
	Arch       string
	Backend    string
	LastSeen   time.Time
}

// KeyInfo describes a consumer API key without revealing its secret.
type KeyInfo struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"created_at"`
}

// UsageRow is an aggregate of usage_events for the console dashboard.
type UsageRow struct {
	KeyID            string `json:"key_id"`
	Model            string `json:"model"`
	Requests         int    `json:"requests"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
}

// EarningEvent is one job's shadow-accrual for a provider (see docs/PAYMENTS.md).
// No money moves; this is the ledger the payout runs will read once Stripe is wired.
type EarningEvent struct {
	ProviderID     string
	KeyID          string
	Model          string
	ModelClass     string
	Tier           string
	GrossMicros    int64
	ProviderMicros int64
	CreatedAt      time.Time
}

// EarningRow aggregates provider_earnings per provider for the admin view.
type EarningRow struct {
	ProviderID     string `json:"provider_id"`
	Jobs           int    `json:"jobs"`
	GrossMicros    int64  `json:"gross_micros"`
	ProviderMicros int64  `json:"provider_micros"`
}

// Store is the coordinator's persistence boundary.
type Store interface {
	// Migrate applies any pending schema migrations. No-op for the memory store.
	Migrate(ctx context.Context) error

	// ValidateConsumerKey returns a stable key id if the key is accepted.
	ValidateConsumerKey(ctx context.Context, key string) (keyID string, ok bool)

	// ValidateProviderToken reports whether a provider registration token is accepted.
	ValidateProviderToken(ctx context.Context, token string) bool

	// UpsertProvider records that a provider connected. Best-effort.
	UpsertProvider(ctx context.Context, p ProviderRecord) error

	// RecordUsage appends a usage event. Best-effort; a failure must not fail the request.
	RecordUsage(ctx context.Context, ev UsageEvent) error

	// RecordEarning appends a provider earnings accrual. Best-effort, shadow-only.
	RecordEarning(ctx context.Context, ev EarningEvent) error

	// EarningsSince aggregates provider_earnings per provider since t.
	EarningsSince(ctx context.Context, t time.Time) ([]EarningRow, error)

	// --- admin / console ---

	// CreateConsumerKey generates a new key, stores its hash, and returns (id, rawKey).
	// The raw key is shown once.
	CreateConsumerKey(ctx context.Context, label string) (id, rawKey string, err error)
	// ListConsumerKeys returns all keys (env bootstrap keys are not listed).
	ListConsumerKeys(ctx context.Context) ([]KeyInfo, error)
	// SetConsumerKeyDisabled enables/disables a key by id.
	SetConsumerKeyDisabled(ctx context.Context, id string, disabled bool) error
	// UsageSince aggregates usage_events grouped by key + model since t.
	UsageSince(ctx context.Context, t time.Time) ([]UsageRow, error)

	Close()
}
