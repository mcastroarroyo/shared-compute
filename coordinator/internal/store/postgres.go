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

func (p *PG) Migrate(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx,
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
		if err := p.pool.QueryRow(ctx,
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
		tx, err := p.pool.Begin(ctx)
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

func (p *PG) Close() { p.pool.Close() }
