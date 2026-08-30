#!/usr/bin/env bash
# Build + sign the model manifest and upload it (plus any new model files) to the R2
# bucket behind models.ayni-ai.com.
#
# Prereqs:
#   - secrets/ayni-registry-signing.key  (from: sc-modelctl keygen --out secrets/ayni-registry-signing)
#   - rclone with an "r2" remote configured for the Cloudflare R2 account, OR set
#     R2_ACCOUNT_ID / R2_ACCESS_KEY_ID / R2_SECRET_ACCESS_KEY and this script will
#     write a temporary rclone config.
#
# Usage:
#   ./infra/publish-registry.sh add  <model_id> <arch> <quant> <hw_class> <ctx> <file.gguf>...
#   ./infra/publish-registry.sh push        # (re)sign + upload manifest and all model dirs
set -euo pipefail
cd "$(dirname "$0")/.."

BUCKET="${SC_R2_BUCKET:-models}"
STAGE="${SC_REGISTRY_STAGE:-.registry}"      # local staging mirror of the bucket
KEY="secrets/ayni-registry-signing.key"
MODELCTL="${MODELCTL:-model-registry/target/release/sc-modelctl}"
[[ -x "$MODELCTL" ]] || MODELCTL="model-registry/target/debug/sc-modelctl"

[[ -f "$KEY" ]] || { echo "missing $KEY"; exit 1; }
command -v rclone >/dev/null || { echo "install rclone (brew install rclone)"; exit 1; }

rclone_remote() {
  if rclone listremotes 2>/dev/null | grep -q '^r2:'; then echo "r2"; return; fi
  : "${R2_ACCOUNT_ID:?}" "${R2_ACCESS_KEY_ID:?}" "${R2_SECRET_ACCESS_KEY:?}"
  export RCLONE_CONFIG="$(mktemp)"
  cat > "$RCLONE_CONFIG" <<EOF
[r2]
type = s3
provider = Cloudflare
access_key_id = $R2_ACCESS_KEY_ID
secret_access_key = $R2_SECRET_ACCESS_KEY
endpoint = https://$R2_ACCOUNT_ID.r2.cloudflarestorage.com
acl = private
EOF
  echo "r2"
}

case "${1:-}" in
  add)
    shift
    model_id="$1"; arch="$2"; quant="$3"; hw="$4"; ctx="$5"; shift 5
    mkdir -p "$STAGE/$model_id"
    for f in "$@"; do cp -n "$f" "$STAGE/$model_id/"; done
    "$MODELCTL" add --manifest "$STAGE/manifest.json" --model-id "$model_id" \
      --arch "$arch" --quant "$quant" --hw-class "$hw" --ctx "$ctx" "$@"
    echo "staged. run: $0 push"
    ;;
  push)
    "$MODELCTL" sign --manifest "$STAGE/manifest.json" --key "$KEY"
    "$MODELCTL" verify --manifest "$STAGE/manifest.json" --pub model-registry/PUBKEY
    R=$(rclone_remote)
    # model files first (immutable), manifest + sig last (atomic-ish switch)
    rclone copy --progress --exclude 'manifest.json*' "$STAGE" "$R:$BUCKET"
    rclone copyto "$STAGE/manifest.json"     "$R:$BUCKET/manifest.json"
    rclone copyto "$STAGE/manifest.json.sig" "$R:$BUCKET/manifest.json.sig"
    echo "published to r2://$BUCKET  (models.ayni-ai.com)"
    ;;
  *)
    echo "usage: $0 add <model_id> <arch> <quant> <hw_class> <ctx> <file.gguf>... | push"
    exit 2
    ;;
esac
