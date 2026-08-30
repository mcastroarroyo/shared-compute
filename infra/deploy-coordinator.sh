#!/usr/bin/env bash
# Deploy the coordinator to Fly.io. Run from the repo root AFTER:
#   1. `fly auth login`         (interactive — you do this)
#   2. account has a payment card on file
#
# First run: creates the app + a Postgres cluster, sets secrets, deploys.
# Later runs: just `fly deploy`. Idempotent.
set -euo pipefail
cd "$(dirname "$0")/.."

APP="${SC_FLY_APP:-shared-compute-coordinator}"
REGION="${SC_FLY_REGION:-iad}"
FLY="${FLY:-flyctl}"
CONFIG="infra/fly.toml"

command -v "$FLY" >/dev/null || { echo "flyctl not found (brew install flyctl)"; exit 1; }
"$FLY" auth whoami >/dev/null 2>&1 || { echo "run 'fly auth login' first"; exit 1; }
echo "==> fly account: $("$FLY" auth whoami 2>/dev/null)"

if ! "$FLY" status --app "$APP" >/dev/null 2>&1; then
  echo "==> creating app: $APP  (region $REGION)"
  "$FLY" apps create "$APP" --org personal

  echo "==> generating live credentials"
  CONSUMER_KEY="sc_live_$(openssl rand -hex 24)"
  PROVIDER_TOKEN="sc_prov_$(openssl rand -hex 24)"
  "$FLY" secrets set --app "$APP" --stage \
    SC_CONSUMER_API_KEYS="$CONSUMER_KEY" SC_PROVIDER_TOKENS="$PROVIDER_TOKEN"
  echo
  echo "  ================  SAVE THESE (shown once)  ================"
  echo "   consumer key : $CONSUMER_KEY"
  echo "   provider tok : $PROVIDER_TOKEN"
  echo "  ========================================================="
  echo

  if ! "$FLY" postgres list 2>/dev/null | grep -q "${APP}-db"; then
    echo "==> creating Postgres: ${APP}-db"
    "$FLY" postgres create --name "${APP}-db" --region "$REGION" \
      --initial-cluster-size 1 --vm-size shared-cpu-1x --volume-size 3
  fi
  echo "==> attaching Postgres as SC_DATABASE_URL"
  "$FLY" postgres attach "${APP}-db" --app "$APP" --variable-name SC_DATABASE_URL --yes
fi

echo "==> deploying"
"$FLY" deploy --app "$APP" --config "$CONFIG" \
  --dockerfile infra/coordinator.Dockerfile --ha=false

HOST="$APP.fly.dev"
echo "==> health check (up to 60s)"
for _ in $(seq 1 20); do
  if curl -fsS "https://$HOST/healthz" 2>/dev/null; then
    echo; echo "coordinator live: https://$HOST"; exit 0
  fi
  sleep 3
done
echo "WARN: health check did not pass yet — check 'fly logs --app $APP'"
