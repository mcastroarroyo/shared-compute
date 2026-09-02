package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/council"
)

// councilStore is a Postgres-backed council.Persister. It gets its own small
// dedicated pool (mirroring internal/auth) rather than routing through
// store.Store, since this is a narrow, optional concern: without
// SC_DATABASE_URL the Council observatory simply stays in-process only, same
// as before this existed.
type councilStore struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// newCouncilStore connects and pings; on any failure it logs and returns nil
// (council persistence disabled, not a startup-fatal error).
func newCouncilStore(ctx context.Context, dbURL string, log *slog.Logger) *councilStore {
	if dbURL == "" {
		return nil
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Warn("council persistence disabled", "err", err)
		return nil
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		log.Warn("council persistence disabled", "err", err)
		return nil
	}
	return &councilStore{pool: pool, log: log}
}

func (c *councilStore) Close() {
	if c != nil && c.pool != nil {
		c.pool.Close()
	}
}

func (c *councilStore) SaveDecision(ctx context.Context, rec council.PublicDecision) error {
	b, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal decision: %w", err)
	}
	at := time.Now().UTC()
	if rec.PublishedAt > 0 {
		at = time.Unix(rec.PublishedAt, 0).UTC()
	}
	_, err = c.pool.Exec(ctx, `
		INSERT INTO council_decisions (decision_id, record, recorded_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (decision_id) DO NOTHING`,
		rec.DecisionID, b, at)
	return err
}

func (c *councilStore) LoadDecisions(ctx context.Context, limit int) ([]council.PublicDecision, error) {
	rows, err := c.pool.Query(ctx,
		`SELECT record FROM council_decisions ORDER BY recorded_at DESC, decision_id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []council.PublicDecision
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		var rec council.PublicDecision
		if err := json.Unmarshal(b, &rec); err != nil {
			c.log.Warn("skip unreadable council decision row", "err", err)
			continue
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	reverse(out)
	return out, nil
}

func (c *councilStore) SaveRunReview(ctx context.Context, rr council.RunReview) error {
	b, err := json.Marshal(rr)
	if err != nil {
		return fmt.Errorf("marshal run review: %w", err)
	}
	_, err = c.pool.Exec(ctx, `
		INSERT INTO council_run_reviews (audit_hash, record, recorded_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (audit_hash) DO NOTHING`,
		rr.AuditHash, b, rr.RecordedAt)
	return err
}

func (c *councilStore) LoadRunReviews(ctx context.Context, limit int) ([]council.RunReview, error) {
	rows, err := c.pool.Query(ctx,
		`SELECT record FROM council_run_reviews ORDER BY recorded_at DESC, audit_hash DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []council.RunReview
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		var rr council.RunReview
		if err := json.Unmarshal(b, &rr); err != nil {
			c.log.Warn("skip unreadable council run review row", "err", err)
			continue
		}
		out = append(out, rr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	reverse(out)
	return out, nil
}

func reverse[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
