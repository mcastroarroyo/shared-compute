#!/usr/bin/env bash
# Keep a provider connected: (re)build once, then run the daemon in a restart
# loop so a coordinator redeploy / transient network drop doesn't end the shift.
#
#   SC_COORDINATOR_URL=wss://api.ayni-ai.com/ws/provider \
#   SC_REGISTRATION_TOKEN=sc_prov_xxxx \
#   [SC_MANIFEST_VERIFY_KEY=signer-id:<b64>] [SC_REQUIRE_MANIFEST=1] \
#   ./infra/supervise-provider.sh
set -uo pipefail
cd "$(dirname "$0")/.."
export PATH="/opt/homebrew/bin:$PATH"

MODEL="${SC_MODEL:-qwen2.5-0.5b-instruct-q4_k_m}"
MODEL_PATH="${SC_MODEL_PATH:-$PWD/models/$MODEL.gguf}"
BACKEND="${SC_BACKEND:-llama}"
: "${SC_COORDINATOR_URL:?set SC_COORDINATOR_URL}"
: "${SC_REGISTRATION_TOKEN:?set SC_REGISTRATION_TOKEN}"

FEATURES=()
[[ "$BACKEND" == "llama" ]] && FEATURES=(--features llama)
echo "==> building provider-daemon (backend=$BACKEND)"
( cd provider-core && cargo build --release --bin provider-daemon "${FEATURES[@]+${FEATURES[@]}}" )

BIN=provider-core/target/release/provider-daemon
backoff=2
while true; do
  echo "==> $(date -u +%H:%M:%S) connecting to $SC_COORDINATOR_URL"
  env SC_COORDINATOR_URL="$SC_COORDINATOR_URL" \
      SC_REGISTRATION_TOKEN="$SC_REGISTRATION_TOKEN" \
      SC_MODEL="$MODEL" SC_MODEL_PATH="$MODEL_PATH" SC_BACKEND="$BACKEND" \
      SC_MANIFEST_URL="${SC_MANIFEST_URL:-}" SC_REGISTRY_PUBKEY="${SC_REGISTRY_PUBKEY:-}" \
      SC_MANIFEST_VERIFY_KEY="${SC_MANIFEST_VERIFY_KEY:-}" \
      SC_REQUIRE_MANIFEST="${SC_REQUIRE_MANIFEST:-}" \
      SC_LOG="${SC_LOG:-info}" \
      "$BIN" || true
  echo "==> $(date -u +%H:%M:%S) daemon exited; reconnecting in ${backoff}s"
  sleep "$backoff"
  backoff=$(( backoff < 30 ? backoff * 2 : 30 ))
done
