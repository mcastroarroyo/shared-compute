#!/usr/bin/env bash
# Run this machine as a provider against a coordinator.
#
#   SC_COORDINATOR_URL=wss://api.ayni-ai.com/ws/provider \
#   SC_REGISTRATION_TOKEN=sc_prov_xxxx \
#   ./infra/run-provider.sh
#
# Defaults to the local llama.cpp build + the Qwen 0.5B GGUF in ./models.
set -euo pipefail
cd "$(dirname "$0")/.."
export PATH="/opt/homebrew/bin:$PATH"

MODEL="${SC_MODEL:-qwen2.5-0.5b-instruct-q4_k_m}"
MODEL_PATH="${SC_MODEL_PATH:-$PWD/models/$MODEL.gguf}"
BACKEND="${SC_BACKEND:-llama}"
: "${SC_COORDINATOR_URL:?set SC_COORDINATOR_URL, e.g. wss://api.ayni-ai.com/ws/provider}"
: "${SC_REGISTRATION_TOKEN:?set SC_REGISTRATION_TOKEN (the sc_prov_... value from deploy)}"

FEATURES=()
if [[ "$BACKEND" == "llama" ]]; then
  FEATURES=(--features llama)
  [[ -f "$MODEL_PATH" ]] || { echo "model not found: $MODEL_PATH"; exit 1; }
fi

echo "==> building provider-daemon (backend=$BACKEND)"
( cd provider-core && cargo build --release --bin provider-daemon "${FEATURES[@]+${FEATURES[@]}}" )

echo "==> connecting to $SC_COORDINATOR_URL as model '$MODEL'"
exec env \
  SC_COORDINATOR_URL="$SC_COORDINATOR_URL" \
  SC_REGISTRATION_TOKEN="$SC_REGISTRATION_TOKEN" \
  SC_MODEL="$MODEL" SC_MODEL_PATH="$MODEL_PATH" SC_BACKEND="$BACKEND" \
  SC_LOG="${SC_LOG:-info}" \
  provider-core/target/release/provider-daemon
