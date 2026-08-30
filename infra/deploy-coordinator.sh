#!/usr/bin/env bash
# Deploy the coordinator to Fly.io. Run from the repo root AFTER:
#   1. `fly auth login`         (interactive — you do this)
#   2. account has a payment card on file
#
# First run creates the app and a Postgres cluster and sets secrets. Later runs just
# `fly deploy`. Idempotent.
set -euo pipefail
cd "$(dirname "$0")/.."

APP="${SC_FLY_APP:-shared-compute-coordinator}"
REGION="${SC_FLY_REGION:-iad}"
FLY="${FLY:-flyctl}"

command -v "$FLY" >/dev/null || { echo "flyctl not found (brew install flyctl)"; exit 1; }
"$FLY" auth whoami >/dev/null 2>&1 || { echo "run 'fly auth login' first"; exit 1; }

if ! "$FLY" status --app "$APP" >/dev/null 2>&1; then
  echo "==> creating app $APP in $REGION"
  "$FLY" launch --no-deploy --copy-config --name "$APP" --region "$REGION" \
    --dockerfile infra/coordinator.Dockerfile --yes

  echo "==> generating credentials"
  CONSUMER_KEY="sc_live_$(openssl rand -hex 24)"
  PROVIDER_TOKEN="sc_prov_$(openssl rand -hex 24)"
  "$FLY" secrets set --app "$APP" --stage \
    SC_CONSUMER_API_KEYS="$CONSUMER_KEY" SC_PROVIDER_TOKENS="$PROVIDER_TOKEN"

  echo
  echo "  ┌──────────────────────────────────────────────────────────────────────┐"
  echo "  │ SAVE THESE NOW (shown once):                                          │"
  echo "  │   consumer key : $CONSUMER_KEY"
  echo "  │   provider tok : $PROVIDER_TOKEN"
  echo "  └──────────────────────────────────────────────────────────────────────┘"
  echo

  if ! "$FLY" postgres list 2>/dev/null | grep -q "${APP}-db"; then
    echo "==> creating Postgres cluster ${APP}-db"
    "$FLY" postgres create --name "${APP}-db" --region "$REGION" \
      --initial-cluster-size 1 --vm-size shared-cpu-1x --volume-size 3 --yes
  fi
  "$FLY" postgres attach "${APP}-db" --app "$APP" --yes || true
  # Fly sets DATABASE_URL; the coordinator reads SC_DATABASE_URL.
  DBURL="$("$FLY" ssh console --app "$APP" -C 'printenv DATABASE_URL' 2>/dev/null || true)"
  [ -n "$DBURL" ] && "$FLY" secrets set --app "$APP" --stage SC_DATABASE_URL="$DBURL"
fi

echo "==> deploying"
"$FLY" deploy --app "$APP" --dockerfile infra/coordinator.Dockerfile

echo "==> health"
HOST="$("$FLY" info --app "$APP" 2>/dev/null | sed -n 's/.*Hostname *= *//p' | head -1)"
HOST="${HOST:-$APP.fly.dev}"
curl -fsS "https://$HOST/healthz" && echo && echo "coordinator live at https://$HOST"
