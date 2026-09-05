#!/usr/bin/env bash
# One-shot data move: Fly Postgres (ayni-coordinator-db) -> Cloud SQL (ayni-pg).
# Requires: flyctl (logged in), cloud-sql-proxy, psql/pg_dump (brew install libpq).
#   FLY_PG_PASSWORD=... SQL_PASSWORD=... infra/gcp/migrate-db.sh
set -euo pipefail
P="${GCP_PROJECT:-ayni1-507216}"
CONN="$(gcloud sql instances describe ayni-pg --project="$P" --format='value(connectionName)')"
DUMP="${DUMP:-/tmp/ayni_coordinator.dump}"
: "${FLY_PG_PASSWORD:?operator password for Fly Postgres (fly ssh console -a ayni-coordinator-db -C 'printenv OPERATOR_PASSWORD')}"
: "${SQL_PASSWORD:?password for the 'ayni' Cloud SQL user}"

echo "==> dumping from Fly"; fly proxy 15432:5432 -a ayni-coordinator-db >/dev/null 2>&1 & FP=$!; sleep 3
PGPASSWORD="$FLY_PG_PASSWORD" pg_dump -h 127.0.0.1 -p 15432 -U postgres -d ayni_coordinator -Fc --no-owner --no-acl -f "$DUMP"
kill $FP; ls -la "$DUMP"

echo "==> restoring into Cloud SQL"; cloud-sql-proxy --private-ip --port 15433 "$CONN" >/dev/null 2>&1 & CP=$!; sleep 4
PGPASSWORD="$SQL_PASSWORD" pg_restore -h 127.0.0.1 -p 15433 -U ayni -d ayni_coordinator --no-owner --no-acl --clean --if-exists "$DUMP" || true
PGPASSWORD="$SQL_PASSWORD" psql -h 127.0.0.1 -p 15433 -U ayni -d ayni_coordinator -Atc "select relname||' rows='||n_live_tup from pg_stat_user_tables order by 1"
kill $CP
