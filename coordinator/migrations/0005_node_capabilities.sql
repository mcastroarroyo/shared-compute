-- Latest self-benchmark fingerprint per provider identity (M10 marketplace).
CREATE TABLE IF NOT EXISTS node_capabilities (
    static_pk             TEXT PRIMARY KEY,
    model                 TEXT NOT NULL DEFAULT '',
    backend               TEXT NOT NULL DEFAULT '',
    prefill_tps           DOUBLE PRECISION NOT NULL DEFAULT 0,
    decode_tps            DOUBLE PRECISION NOT NULL DEFAULT 0,
    sustained_start_tps   DOUBLE PRECISION NOT NULL DEFAULT 0,
    sustained_end_tps     DOUBLE PRECISION NOT NULL DEFAULT 0,
    mem_bandwidth_gbps    DOUBLE PRECISION NOT NULL DEFAULT 0,
    available_ram_mb      BIGINT NOT NULL DEFAULT 0,
    cpu_cores             INTEGER NOT NULL DEFAULT 0,
    thermal_state         TEXT NOT NULL DEFAULT '',
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
