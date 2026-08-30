package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

func (p *PG) CreditBalance(ctx context.Context, keyID string) (int64, error) {
	var micros int64
	err := p.pool.QueryRow(ctx,
		`SELECT credit_micros FROM billing_accounts WHERE key_id = $1`, keyID).Scan(&micros)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return micros, err
}

func (p *PG) AddCredit(ctx context.Context, keyID string, deltaMicros int64, reason, stripeRef, jobID string) error {
	ct, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := p.pool.Begin(ct)
	if err != nil {
		return err
	}
	defer tx.Rollback(ct) //nolint:errcheck

	// Idempotent topups: skip if this stripe_ref already produced one.
	if reason == "topup" && stripeRef != "" {
		var n int
		if err := tx.QueryRow(ct,
			`SELECT count(*) FROM credit_ledger WHERE reason='topup' AND stripe_ref=$1`, stripeRef,
		).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return tx.Commit(ct)
		}
	}
	if reason == "debit" && jobID != "" {
		var n int
		if err := tx.QueryRow(ct,
			`SELECT count(*) FROM credit_ledger WHERE reason='debit' AND job_id=$1`, jobID,
		).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return tx.Commit(ct)
		}
	}

	if _, err := tx.Exec(ct, `
		INSERT INTO billing_accounts (key_id, credit_micros)
		VALUES ($1, $2)
		ON CONFLICT (key_id) DO UPDATE
		  SET credit_micros = billing_accounts.credit_micros + EXCLUDED.credit_micros,
		      updated_at = now()`, keyID, deltaMicros); err != nil {
		return err
	}
	if _, err := tx.Exec(ct, `
		INSERT INTO credit_ledger (key_id, delta_micros, reason, stripe_ref, job_id)
		VALUES ($1,$2,$3,$4,$5)`, keyID, deltaMicros, reason, stripeRef, jobID); err != nil {
		return err
	}
	return tx.Commit(ct)
}

func (p *PG) UpsertPayoutAccount(ctx context.Context, a PayoutAccount) error {
	ct, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := p.pool.Exec(ct, `
		INSERT INTO provider_payout_accounts (static_pk, stripe_account, status)
		VALUES ($1,$2,$3)
		ON CONFLICT (static_pk) DO UPDATE
		  SET stripe_account = EXCLUDED.stripe_account,
		      status = EXCLUDED.status,
		      updated_at = now()`, a.StaticPK, a.StripeAccount, a.Status)
	return err
}

func (p *PG) GetPayoutAccount(ctx context.Context, staticPK string) (PayoutAccount, bool, error) {
	var a PayoutAccount
	err := p.pool.QueryRow(ctx,
		`SELECT static_pk, stripe_account, status FROM provider_payout_accounts WHERE static_pk=$1`,
		staticPK).Scan(&a.StaticPK, &a.StripeAccount, &a.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, false, nil
	}
	return a, err == nil, err
}

func (p *PG) PayoutAccountByStripe(ctx context.Context, stripeAccount string) (PayoutAccount, bool, error) {
	var a PayoutAccount
	err := p.pool.QueryRow(ctx,
		`SELECT static_pk, stripe_account, status FROM provider_payout_accounts WHERE stripe_account=$1`,
		stripeAccount).Scan(&a.StaticPK, &a.StripeAccount, &a.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, false, nil
	}
	return a, err == nil, err
}

func (p *PG) UpsertNodeCapability(ctx context.Context, c NodeCapabilityRow) error {
	ct, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := p.pool.Exec(ct, `
		INSERT INTO node_capabilities
		  (static_pk, model, backend, prefill_tps, decode_tps, sustained_start_tps,
		   sustained_end_tps, mem_bandwidth_gbps, available_ram_mb, cpu_cores, thermal_state, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11, now())
		ON CONFLICT (static_pk) DO UPDATE SET
		  model=EXCLUDED.model, backend=EXCLUDED.backend, prefill_tps=EXCLUDED.prefill_tps,
		  decode_tps=EXCLUDED.decode_tps, sustained_start_tps=EXCLUDED.sustained_start_tps,
		  sustained_end_tps=EXCLUDED.sustained_end_tps, mem_bandwidth_gbps=EXCLUDED.mem_bandwidth_gbps,
		  available_ram_mb=EXCLUDED.available_ram_mb, cpu_cores=EXCLUDED.cpu_cores,
		  thermal_state=EXCLUDED.thermal_state, updated_at=now()`,
		c.StaticPK, c.Model, c.Backend, c.PrefillTPS, c.DecodeTPS, c.SustainedStartTPS,
		c.SustainedEndTPS, c.MemBandwidthGBps, int64(c.AvailableRAMMB), c.CPUCores, c.ThermalState)
	return err
}

func (p *PG) NodeCapabilities(ctx context.Context) ([]NodeCapabilityRow, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT static_pk, model, backend, prefill_tps, decode_tps, sustained_start_tps,
		       sustained_end_tps, mem_bandwidth_gbps, available_ram_mb, cpu_cores,
		       thermal_state, updated_at
		FROM node_capabilities ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NodeCapabilityRow
	for rows.Next() {
		var c NodeCapabilityRow
		var ram int64
		if err := rows.Scan(&c.StaticPK, &c.Model, &c.Backend, &c.PrefillTPS, &c.DecodeTPS,
			&c.SustainedStartTPS, &c.SustainedEndTPS, &c.MemBandwidthGBps, &ram, &c.CPUCores,
			&c.ThermalState, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.AvailableRAMMB = uint64(ram)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (p *PG) AccruedByProvider(ctx context.Context) ([]ProviderAccrual, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT static_pk, count(*), coalesce(sum(provider_micros),0)
		FROM provider_earnings
		WHERE state = 'accrued' AND static_pk <> ''
		GROUP BY static_pk
		ORDER BY sum(provider_micros) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProviderAccrual
	for rows.Next() {
		var a ProviderAccrual
		if err := rows.Scan(&a.StaticPK, &a.Jobs, &a.OwedMicros); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (p *PG) RecordPayoutAndSettle(ctx context.Context, po PayoutRecord) (int64, error) {
	ct, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := p.pool.Begin(ct)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ct) //nolint:errcheck

	var id int64
	if err := tx.QueryRow(ct, `
		INSERT INTO payouts (static_pk, stripe_account, amount_micros, stripe_transfer, state)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		po.StaticPK, po.StripeAccount, po.AmountMicros, po.StripeTransfer, po.State,
	).Scan(&id); err != nil {
		return 0, err
	}
	if po.State == "created" {
		if _, err := tx.Exec(ct, `
			UPDATE provider_earnings SET state='paid', payout_id=$1
			WHERE static_pk=$2 AND state='accrued'`, id, po.StaticPK); err != nil {
			return 0, err
		}
		if po.RemainderMicros > 0 {
			// Re-accrue the sub-cent dust so it carries to the next payout.
			if _, err := tx.Exec(ct, `
				INSERT INTO provider_earnings
				  (static_pk, model_class, tier, gross_micros, provider_micros, state)
				VALUES ($1,'','carryforward',0,$2,'accrued')`, po.StaticPK, po.RemainderMicros); err != nil {
				return 0, err
			}
		}
	}
	return id, tx.Commit(ct)
}
