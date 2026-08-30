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

	Close()
}
