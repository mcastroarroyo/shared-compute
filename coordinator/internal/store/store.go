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

// EarningEvent is one job's accrual for a provider (see docs/PAYMENTS.md).
type EarningEvent struct {
	JobID          string // exactly one accrual per (job_id, static_pk)
	ProviderID     string
	StaticPK       string // base64 X25519 — the durable payout identity
	KeyID          string
	Model          string
	ModelClass     string
	Tier           string
	GrossMicros    int64
	ProviderMicros int64
	CreatedAt      time.Time
}

// ProviderAccrual is unpaid provider_earnings summed per payout identity.
type ProviderAccrual struct {
	StaticPK   string `json:"static_pk"`
	Jobs       int    `json:"jobs"`
	OwedMicros int64  `json:"owed_micros"`
}

// NodeCapabilityRow is a provider's latest self-benchmark fingerprint.
type NodeCapabilityRow struct {
	StaticPK          string    `json:"static_pk"`
	Model             string    `json:"model"`
	Backend           string    `json:"backend"`
	PrefillTPS        float64   `json:"prefill_tps"`
	DecodeTPS         float64   `json:"decode_tps"`
	SustainedStartTPS float64   `json:"sustained_start_tps"`
	SustainedEndTPS   float64   `json:"sustained_end_tps"`
	MemBandwidthGBps  float64   `json:"mem_bandwidth_gbps"`
	AvailableRAMMB    uint64    `json:"available_ram_mb"`
	CPUCores          int       `json:"cpu_cores"`
	ThermalState      string    `json:"thermal_state"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// PayoutAccount links a provider identity to a Stripe Connect account.
type PayoutAccount struct {
	StaticPK      string `json:"static_pk"`
	StripeAccount string `json:"stripe_account"`
	Status        string `json:"status"` // pending | enabled | disabled
}

// PayoutRecord is one completed transfer to a provider.
type PayoutRecord struct {
	StaticPK       string
	StripeAccount  string
	AmountMicros   int64
	StripeTransfer string
	State          string
	// RemainderMicros is the sub-cent dust that couldn't be transferred; it is
	// re-accrued so it carries forward to the next payout.
	RemainderMicros int64
}

// EarningRow aggregates provider_earnings per provider for the admin view.
type EarningRow struct {
	ProviderID     string `json:"provider_id"`
	Jobs           int    `json:"jobs"`
	GrossMicros    int64  `json:"gross_micros"`
	ProviderMicros int64  `json:"provider_micros"`
}

// WaitlistEntry is one public waitlist signup from the marketing site.
type WaitlistEntry struct {
	Email     string    `json:"email"`
	Interest  string    `json:"interest"`
	Note      string    `json:"note"`
	IPHash    string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

// FeedbackEntry is one message from a tester or user (web app, phone app, site).
type FeedbackEntry struct {
	ID        int64           `json:"id"`
	Kind      string          `json:"kind"`
	Email     string          `json:"email"`
	Device    string          `json:"device"`
	Message   string          `json:"message"`
	App       string          `json:"app"`
	Version   string          `json:"version"`
	UserID    string          `json:"user_id,omitempty"`
	IPHash    string          `json:"-"`
	CreatedAt time.Time       `json:"created_at"`
	Replies   []FeedbackReply `json:"replies"`
}

// FeedbackReply is one message in the thread under a feedback entry.
type FeedbackReply struct {
	ID         int64     `json:"id"`
	FeedbackID int64     `json:"feedback_id"`
	Author     string    `json:"author"` // ayni | tester
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
}

// Proposal is one submitted community-initiative proposal.
type Proposal struct {
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Title     string    `json:"title"`
	Summary   string    `json:"summary"`
	Link      string    `json:"link"`
	IPHash    string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
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

	// AddWaitlist stores a public waitlist signup. Idempotent on email.
	AddWaitlist(ctx context.Context, e WaitlistEntry) error
	// AddProposal stores a submitted initiative proposal.
	AddProposal(ctx context.Context, p Proposal) error
	// AddFeedback stores tester / user feedback; FeedbackSince is the admin read.
	AddFeedback(ctx context.Context, e FeedbackEntry) error
	FeedbackSince(ctx context.Context, t time.Time) ([]FeedbackEntry, error)
	// FeedbackForUser is the tester's own thread view: entries by user id or email, with replies.
	FeedbackForUser(ctx context.Context, userID, email string) ([]FeedbackEntry, error)
	// AddFeedbackReply appends a message under a feedback entry (author "ayni" or "tester").
	AddFeedbackReply(ctx context.Context, feedbackID int64, author, body string) (FeedbackReply, error)
	// WaitlistSince / ProposalsSince are admin reads.
	WaitlistSince(ctx context.Context, t time.Time) ([]WaitlistEntry, error)
	ProposalsSince(ctx context.Context, t time.Time) ([]Proposal, error)

	// --- billing / payouts ---

	// CreditBalance returns a consumer key's credit balance in micro-USD (0 if none).
	CreditBalance(ctx context.Context, keyID string) (int64, error)
	// AddCredit applies a signed delta and appends a ledger row. For reason "topup"
	// a non-empty stripeRef makes it idempotent; for reason "debit" a non-empty
	// jobID does (a duplicate returns nil, no change).
	AddCredit(ctx context.Context, keyID string, deltaMicros int64, reason, stripeRef, jobID string) error

	// UpsertPayoutAccount links (or updates) a Stripe Connect account for a provider identity.
	UpsertPayoutAccount(ctx context.Context, a PayoutAccount) error
	// GetPayoutAccount returns the linked account for a provider identity, if any.
	GetPayoutAccount(ctx context.Context, staticPK string) (PayoutAccount, bool, error)
	// PayoutAccountByStripe reverse-looks-up by Stripe account id (webhook path).
	PayoutAccountByStripe(ctx context.Context, stripeAccount string) (PayoutAccount, bool, error)

	// UpsertNodeCapability stores a provider's latest benchmark fingerprint.
	UpsertNodeCapability(ctx context.Context, c NodeCapabilityRow) error
	// NodeCapabilities returns all stored fingerprints.
	NodeCapabilities(ctx context.Context) ([]NodeCapabilityRow, error)
	// AccruedByProvider sums unpaid provider_earnings per payout identity.
	AccruedByProvider(ctx context.Context) ([]ProviderAccrual, error)
	// RecordPayoutAndSettle inserts a payout row and marks that identity's accrued
	// earnings paid, in one transaction. Returns the new payout id.
	RecordPayoutAndSettle(ctx context.Context, p PayoutRecord) (int64, error)

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
