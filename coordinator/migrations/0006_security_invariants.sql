-- Security invariants: one accrual per (job_id, device) and one debit per job.
-- Empty job_id values are legacy rows and are intentionally excluded.
ALTER TABLE provider_earnings
    ADD COLUMN IF NOT EXISTS job_id TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS provider_earnings_job_device_idx
    ON provider_earnings (job_id, static_pk)
    WHERE job_id <> '' AND static_pk <> '';

CREATE UNIQUE INDEX IF NOT EXISTS credit_ledger_debit_job_idx
    ON credit_ledger (job_id)
    WHERE reason = 'debit' AND job_id <> '';
