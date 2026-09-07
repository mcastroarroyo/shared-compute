package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mcastroarroyo/shared-compute/coordinator/migrations"
)

// PG is the Postgres-backed store. Consumer keys and provider tokens are matched by
// SHA-256; env-provided bootstrap credentials are also accepted (see NewPG).
type PG struct {
	pool         *pgxpool.Pool
	log          *slog.Logger
	bootstrapKey map[string]string   // raw key -> synthetic id, from env
	bootstrapTok map[string]struct{} // raw token, from env
}

// NewPG connects to databaseURL and returns a store. Any keys/tokens in the env sets are
// accepted directly (bootstrap) in addition to whatever is in the database.
func NewPG(ctx context.Context, databaseURL string, log *slog.Logger,
	bootstrapConsumerKeys, bootstrapProviderTokens map[string]struct{}) (*PG, error) {

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("pg connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pg ping: %w", err)
	}

	bk := make(map[string]string, len(bootstrapConsumerKeys))
	for k := range bootstrapConsumerKeys {
		bk[k] = keyID(k)
	}
	bt := make(map[string]struct{}, len(bootstrapProviderTokens))
	for t := range bootstrapProviderTokens {
		bt[t] = struct{}{}
	}
	return &PG{pool: pool, log: log, bootstrapKey: bk, bootstrapTok: bt}, nil
}

func sha(b string) []byte {
	s := sha256.Sum256([]byte(b))
	return s[:]
}

// migrationLockKey is an arbitrary fixed key for a Postgres advisory lock that
// serializes Migrate() across concurrent callers (multiple coordinator
// instances starting up together, or multiple test binaries sharing one CI
// database). Without it, two callers can both see a migration as "not yet
// applied" (the check-then-create below isn't atomic on its own) and race to
// CREATE the same table/type, which Postgres reports as a duplicate-key error
// on its own catalog rather than anything migration-specific.
const migrationLockKey = 84719205

func (p *PG) Migrate(ctx context.Context) error {
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, int64(migrationLockKey)); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, int64(migrationLockKey))
	}()

	if _, err := conn.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
	); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		var exists bool
		if err := conn.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, version,
		).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, version); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		p.log.Info("migration applied", "version", version)
	}
	return nil
}

func (p *PG) ValidateConsumerKey(ctx context.Context, key string) (string, bool) {
	if id, ok := p.bootstrapKey[key]; ok {
		return id, true
	}
	var id string
	err := p.pool.QueryRow(ctx,
		`SELECT id FROM consumer_api_keys WHERE key_sha256=$1 AND NOT disabled`, sha(key),
	).Scan(&id)
	if err != nil {
		if err != pgx.ErrNoRows {
			p.log.Warn("consumer key lookup failed", "err", err)
		}
		return "", false
	}
	return id, true
}

func (p *PG) ValidateProviderToken(ctx context.Context, token string) bool {
	if _, ok := p.bootstrapTok[token]; ok {
		return true
	}
	var one int
	err := p.pool.QueryRow(ctx,
		`SELECT 1 FROM provider_tokens WHERE token_sha256=$1 AND NOT disabled`, sha(token),
	).Scan(&one)
	if err != nil && err != pgx.ErrNoRows {
		p.log.Warn("provider token lookup failed", "err", err)
	}
	return err == nil
}

func (p *PG) UpsertProvider(ctx context.Context, r ProviderRecord) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO providers (provider_id, static_pk, platform, arch, backend, last_seen)
		VALUES ($1,$2,$3,$4,$5, now())
		ON CONFLICT (provider_id) DO UPDATE
		  SET static_pk=EXCLUDED.static_pk, platform=EXCLUDED.platform,
		      arch=EXCLUDED.arch, backend=EXCLUDED.backend, last_seen=now()`,
		r.ProviderID, r.StaticPK, r.Platform, r.Arch, r.Backend)
	return err
}

func (p *PG) RecordUsage(ctx context.Context, ev UsageEvent) error {
	var providerID any
	if ev.ProviderID != "" {
		providerID = ev.ProviderID
	}
	ct, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := p.pool.Exec(ct, `
		INSERT INTO usage_events
		  (key_id, provider_id, model, tier, prompt_tokens, completion_tokens, duration_ms, finish_reason)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		ev.KeyID, providerID, ev.Model, ev.Tier,
		ev.PromptTokens, ev.CompletionTokens, ev.DurationMS, ev.FinishReason)
	return err
}

func (p *PG) RecordEarning(ctx context.Context, ev EarningEvent) error {
	if ev.JobID == "" || ev.StaticPK == "" {
		return fmt.Errorf("earning event requires job_id and static_pk")
	}
	var providerID any
	if ev.ProviderID != "" {
		providerID = ev.ProviderID
	}
	ct, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	// UNIQUE (job_id, static_pk) — migration 0006 — enforces one payable event
	// per verified job/device even under concurrent retries.
	_, err := p.pool.Exec(ct, `
		INSERT INTO provider_earnings
		  (job_id, provider_id, static_pk, key_id, model, model_class, tier, gross_micros, provider_micros)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT DO NOTHING`,
		ev.JobID, providerID, ev.StaticPK, ev.KeyID, ev.Model, ev.ModelClass, ev.Tier,
		ev.GrossMicros, ev.ProviderMicros)
	return err
}

func (p *PG) EarningsSince(ctx context.Context, t time.Time) ([]EarningRow, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT coalesce(provider_id::text,''), count(*),
		       coalesce(sum(gross_micros),0), coalesce(sum(provider_micros),0)
		FROM provider_earnings WHERE created_at >= $1
		GROUP BY provider_id ORDER BY sum(provider_micros) DESC`, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EarningRow
	for rows.Next() {
		var e EarningRow
		if err := rows.Scan(&e.ProviderID, &e.Jobs, &e.GrossMicros, &e.ProviderMicros); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (p *PG) AddWaitlist(ctx context.Context, e WaitlistEntry) error {
	ct, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := p.pool.Exec(ct, `
		INSERT INTO waitlist (email, interest, note, ip_hash)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (lower(email)) DO UPDATE SET interest = EXCLUDED.interest, note = EXCLUDED.note`,
		e.Email, e.Interest, e.Note, e.IPHash)
	return err
}

func (p *PG) AddProposal(ctx context.Context, pr Proposal) error {
	ct, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := p.pool.Exec(ct, `
		INSERT INTO proposals (name, email, title, summary, link, ip_hash)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		pr.Name, pr.Email, pr.Title, pr.Summary, pr.Link, pr.IPHash)
	return err
}

func (p *PG) AddFeedback(ctx context.Context, e FeedbackEntry) error {
	ct, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := p.pool.Exec(ct, `
		INSERT INTO feedback (kind, email, device, message, app, version, user_id, ip_hash)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		e.Kind, e.Email, e.Device, e.Message, e.App, e.Version, e.UserID, e.IPHash)
	return err
}

func (p *PG) FeedbackSince(ctx context.Context, t time.Time) ([]FeedbackEntry, error) {
	return p.feedbackQuery(ctx, `
		SELECT id, kind, email, device, message, app, version, user_id, created_at FROM feedback
		WHERE created_at >= $1 ORDER BY created_at DESC LIMIT 500`, t)
}

func (p *PG) FeedbackForUser(ctx context.Context, userID, email string) ([]FeedbackEntry, error) {
	return p.feedbackQuery(ctx, `
		SELECT id, kind, email, device, message, app, version, user_id, created_at FROM feedback
		WHERE (user_id = $1 AND $1 <> '') OR (lower(email) = lower($2) AND $2 <> '')
		ORDER BY created_at DESC LIMIT 100`, userID, email)
}

func (p *PG) feedbackQuery(ctx context.Context, sql string, args ...any) ([]FeedbackEntry, error) {
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	var out []FeedbackEntry
	var ids []int64
	for rows.Next() {
		var e FeedbackEntry
		if err := rows.Scan(&e.ID, &e.Kind, &e.Email, &e.Device, &e.Message, &e.App, &e.Version, &e.UserID, &e.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		e.Replies = []FeedbackReply{}
		out = append(out, e)
		ids = append(ids, e.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(ids) == 0 {
		return out, err
	}
	rrows, err := p.pool.Query(ctx, `
		SELECT id, feedback_id, author, body, created_at FROM feedback_replies
		WHERE feedback_id = ANY($1) ORDER BY created_at ASC`, ids)
	if err != nil {
		return nil, err
	}
	defer rrows.Close()
	byID := map[int64]int{}
	for i, e := range out {
		byID[e.ID] = i
	}
	for rrows.Next() {
		var r FeedbackReply
		if err := rrows.Scan(&r.ID, &r.FeedbackID, &r.Author, &r.Body, &r.CreatedAt); err != nil {
			return nil, err
		}
		if i, ok := byID[r.FeedbackID]; ok {
			out[i].Replies = append(out[i].Replies, r)
		}
	}
	return out, rrows.Err()
}

func (p *PG) AddFeedbackReply(ctx context.Context, feedbackID int64, author, body string) (FeedbackReply, error) {
	ct, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	r := FeedbackReply{FeedbackID: feedbackID, Author: author, Body: body}
	err := p.pool.QueryRow(ct, `
		INSERT INTO feedback_replies (feedback_id, author, body) VALUES ($1,$2,$3)
		RETURNING id, created_at`, feedbackID, author, body).Scan(&r.ID, &r.CreatedAt)
	return r, err
}

func (p *PG) WaitlistSince(ctx context.Context, t time.Time) ([]WaitlistEntry, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT email, interest, note, created_at FROM waitlist
		WHERE created_at >= $1 ORDER BY created_at DESC`, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WaitlistEntry
	for rows.Next() {
		var e WaitlistEntry
		if err := rows.Scan(&e.Email, &e.Interest, &e.Note, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (p *PG) ProposalsSince(ctx context.Context, t time.Time) ([]Proposal, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT name, email, title, summary, link, created_at FROM proposals
		WHERE created_at >= $1 ORDER BY created_at DESC`, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Proposal
	for rows.Next() {
		var pr Proposal
		if err := rows.Scan(&pr.Name, &pr.Email, &pr.Title, &pr.Summary, &pr.Link, &pr.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, pr)
	}
	return out, rows.Err()
}

func (p *PG) CreateConsumerKey(ctx context.Context, label string) (string, string, error) {
	raw := randToken("sc_live_")
	id := keyID(raw)
	_, err := p.pool.Exec(ctx,
		`INSERT INTO consumer_api_keys (id, key_sha256, label) VALUES ($1,$2,$3)`,
		id, sha(raw), label)
	if err != nil {
		return "", "", err
	}
	return id, raw, nil
}

func (p *PG) ListConsumerKeys(ctx context.Context) ([]KeyInfo, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, label, disabled, created_at FROM consumer_api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KeyInfo
	for rows.Next() {
		var k KeyInfo
		if err := rows.Scan(&k.ID, &k.Label, &k.Disabled, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (p *PG) SetConsumerKeyDisabled(ctx context.Context, id string, disabled bool) error {
	ct, err := p.pool.Exec(ctx,
		`UPDATE consumer_api_keys SET disabled=$2 WHERE id=$1`, id, disabled)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("no such key: %s", id)
	}
	return nil
}

func (p *PG) UsageSince(ctx context.Context, t time.Time) ([]UsageRow, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT key_id, model, count(*), coalesce(sum(prompt_tokens),0), coalesce(sum(completion_tokens),0)
		FROM usage_events WHERE created_at >= $1
		GROUP BY key_id, model ORDER BY count(*) DESC`, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageRow
	for rows.Next() {
		var u UsageRow
		if err := rows.Scan(&u.KeyID, &u.Model, &u.Requests, &u.PromptTokens, &u.CompletionTokens); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (p *PG) Close() { p.pool.Close() }

// --- asynchronous workload runs (metadata only; never job content) ---

func (p *PG) UpsertRun(ctx context.Context, r WorkloadRun) error {
	ct, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := p.pool.Exec(ct, `
		INSERT INTO workload_runs (id, key_id, workload_id, model, status, total_items, done_items,
			ok_items, failed_items, quoted_micros, charged_micros, prompt_tokens, completion_tokens,
			wall_ms, fanout, webhook_url, webhook_state, label, error, created_at, started_at, finished_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,
			COALESCE($20, now()),$21,$22)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status, total_items = EXCLUDED.total_items,
			done_items = EXCLUDED.done_items, ok_items = EXCLUDED.ok_items,
			failed_items = EXCLUDED.failed_items, charged_micros = EXCLUDED.charged_micros,
			prompt_tokens = EXCLUDED.prompt_tokens, completion_tokens = EXCLUDED.completion_tokens,
			wall_ms = EXCLUDED.wall_ms, fanout = EXCLUDED.fanout,
			webhook_state = EXCLUDED.webhook_state, error = EXCLUDED.error,
			started_at = EXCLUDED.started_at, finished_at = EXCLUDED.finished_at`,
		r.ID, r.KeyID, r.WorkloadID, r.Model, r.Status, r.TotalItems, r.DoneItems,
		r.OKItems, r.FailedItems, r.QuotedMicros, r.ChargedMicros, r.PromptTokens, r.CompletionTokens,
		r.WallMS, r.Fanout, r.WebhookURL, r.WebhookState, r.Label, r.Error,
		nullTime(&r.CreatedAt), r.StartedAt, r.FinishedAt)
	return err
}

func nullTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return *t
}

const runCols = `id, key_id, workload_id, model, status, total_items, done_items, ok_items,
	failed_items, quoted_micros, charged_micros, prompt_tokens, completion_tokens, wall_ms,
	fanout, webhook_url, webhook_state, label, error, created_at, started_at, finished_at`

func scanRun(row interface{ Scan(...any) error }) (WorkloadRun, error) {
	var r WorkloadRun
	err := row.Scan(&r.ID, &r.KeyID, &r.WorkloadID, &r.Model, &r.Status, &r.TotalItems, &r.DoneItems,
		&r.OKItems, &r.FailedItems, &r.QuotedMicros, &r.ChargedMicros, &r.PromptTokens,
		&r.CompletionTokens, &r.WallMS, &r.Fanout, &r.WebhookURL, &r.WebhookState, &r.Label,
		&r.Error, &r.CreatedAt, &r.StartedAt, &r.FinishedAt)
	return r, err
}

func (p *PG) GetRun(ctx context.Context, id string) (WorkloadRun, bool, error) {
	r, err := scanRun(p.pool.QueryRow(ctx, `SELECT `+runCols+` FROM workload_runs WHERE id=$1`, id))
	if err != nil {
		return WorkloadRun{}, false, nil //nolint:nilerr // absent or unreadable → "not found"
	}
	return r, true, nil
}

func (p *PG) ListRuns(ctx context.Context, keyID string, limit int) ([]WorkloadRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := p.pool.Query(ctx, `SELECT `+runCols+` FROM workload_runs
		WHERE key_id=$1 ORDER BY created_at DESC LIMIT $2`, keyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkloadRun
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
