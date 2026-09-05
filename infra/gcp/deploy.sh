#!/usr/bin/env bash
# Deploy the coordinator to Cloud Run (project ayni1-507216, us-east1).
#   infra/gcp/deploy.sh [image-tag]        # default: current git short sha (must exist in Artifact Registry)
# Secrets come from Secret Manager (never env files); the manifest signer is Cloud KMS.
set -euo pipefail
cd "$(dirname "$0")/../.."
P="${GCP_PROJECT:-ayni1-507216}"; R="${GCP_REGION:-us-east1}"; SVC="${GCP_SERVICE:-ayni-coordinator}"
TAG="${1:-$(git rev-parse --short HEAD)}"
IMG="$R-docker.pkg.dev/$P/ayni/coordinator:$TAG"
SA="ayni-coordinator@$P.iam.gserviceaccount.com"
SQL_CONN="$(gcloud sql instances describe ayni-pg --project="$P" --format='value(connectionName)')"
KMS_KEY="projects/$P/locations/$R/keyRings/ayni/cryptoKeys/manifest-signer/cryptoKeyVersions/1"

# Secrets that must have a version before deploy (Cloud Run fails to start otherwise).
# newest ENABLED version number of a secret ("" if none). We pin the number rather
# than the "latest" alias: Cloud Run resolves "latest" to the highest version even
# when that one is disabled, and the revision then fails to start.
newest_enabled() {
  gcloud secrets versions list "$1" --project="$P" --filter="state=enabled" \
    --sort-by=~createTime --limit=1 --format="value(name.basename())" 2>/dev/null
}
SECRETS=""
for s in SC_CONSUMER_API_KEYS SC_PROVIDER_TOKENS SC_ADMIN_TOKEN SC_DATABASE_URL; do
  v="$(newest_enabled "$s")"; [ -n "$v" ] || { echo "secret $s has no enabled version"; exit 1; }
  SECRETS="$SECRETS,$s=$s:$v"
done
# Optional secrets: only mounted if they have an enabled version (so a missing Stripe key doesn't block deploy).
for s in SC_GITHUB_CLIENT_ID SC_GITHUB_CLIENT_SECRET SC_STRIPE_SECRET_KEY SC_STRIPE_WEBHOOK_SECRET SC_STRIPE_CONNECT_WEBHOOK_SECRET SC_COUNCIL_MODEL_KEY; do
  v="$(newest_enabled "$s")"; [ -n "$v" ] && SECRETS="$SECRETS,$s=$s:$v"
done
SECRETS="${SECRETS#,}"

echo "==> deploying $IMG to Cloud Run $SVC ($R)"
gcloud run deploy "$SVC" --project="$P" --region="$R" \
  --image="$IMG" \
  --service-account="$SA" \
  --platform=managed --ingress=all --allow-unauthenticated \
  --port=8080 --cpu=1 --memory=512Mi \
  --min-instances=1 --max-instances=3 --concurrency=250 \
  --timeout=3600 --session-affinity --no-cpu-throttling \
  --network=default --subnet=default --vpc-egress=private-ranges-only \
  --add-cloudsql-instances="$SQL_CONN" \
  --set-env-vars="SC_HTTP_ADDR=:8080,SC_HEARTBEAT_SECONDS=15,SC_RATE_PER_MIN=120,SC_MANIFEST_URL=https://models.ayni-ai.com,SC_REGISTRY_PUBKEY=p7UUs6aCFebGUfSvFV5Wczh7kYBCEW2tDRO+dV0IpDM=,SC_BILLING_ENFORCE=1,SC_DEMO_ENABLED=1,SC_PUBLIC_BASE_URL=https://app.ayni-ai.com,SC_MANIFEST_SIGNER_ID=ayni-coordinator-kms-v1,SC_MANIFEST_KMS_KEY=$KMS_KEY,SC_COUNCIL_MODEL_API=${SC_COUNCIL_MODEL_API:-},SC_COUNCIL_MODEL_ID=${SC_COUNCIL_MODEL_ID:-}" \
  --set-secrets="$SECRETS" \
  --labels=app=ayni,tier=coordinator

URL="$(gcloud run services describe "$SVC" --project="$P" --region="$R" --format='value(status.url)')"
echo "==> $URL"; curl -fsS "$URL/health"; echo
