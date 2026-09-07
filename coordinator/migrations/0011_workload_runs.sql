-- Asynchronous workload runs for API "workload runners" (see docs/WORKLOAD-RUNNERS.md).
--
-- METADATA ONLY. Prompt and completion content is never written here, keeping the
-- architecture's rule that no component persists job content to disk. Results live
-- in coordinator memory for a bounded window and are fetched over TLS by the runner.
CREATE TABLE IF NOT EXISTS workload_runs (
    id             TEXT PRIMARY KEY,          -- run_<hex>
    key_id         TEXT NOT NULL,             -- owning consumer API key
    workload_id    TEXT NOT NULL,             -- wl_<hex> quote this run executes
    model          TEXT NOT NULL,
    status         TEXT NOT NULL,             -- queued | running | succeeded | failed
    total_items    INT NOT NULL DEFAULT 0,
    done_items     INT NOT NULL DEFAULT 0,
    ok_items       INT NOT NULL DEFAULT 0,
    failed_items   INT NOT NULL DEFAULT 0,
    quoted_micros  BIGINT NOT NULL DEFAULT 0,
    charged_micros BIGINT NOT NULL DEFAULT 0,
    prompt_tokens  BIGINT NOT NULL DEFAULT 0,
    completion_tokens BIGINT NOT NULL DEFAULT 0,
    wall_ms        BIGINT NOT NULL DEFAULT 0,
    fanout         INT NOT NULL DEFAULT 0,
    webhook_url    TEXT NOT NULL DEFAULT '',
    webhook_state  TEXT NOT NULL DEFAULT '',  -- '' | pending | delivered | failed
    label          TEXT NOT NULL DEFAULT '',  -- runner-supplied tag, e.g. "logs-2026-09-07T08"
    error          TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at     TIMESTAMPTZ,
    finished_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS workload_runs_key_idx ON workload_runs (key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS workload_runs_status_idx ON workload_runs (status, created_at DESC);
