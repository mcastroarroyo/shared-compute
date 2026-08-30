#!/usr/bin/env bash
# End-to-end: coordinator + one provider, over the real encrypted job envelope. Asserts a
# streamed and a non-streamed OpenAI-compatible completion, auth enforcement, and that no
# prompt/completion plaintext lands in either process's logs.
#
# Default backend: mock (no model, runs in CI). Set SC_E2E_MODEL_PATH=/path/to.gguf to
# exercise the real llama.cpp backend instead.
set -euo pipefail
cd "$(dirname "$0")/.."

export PATH="/opt/homebrew/bin:$PATH"
TMP="$(mktemp -d "${TMPDIR:-/tmp}/sc-e2e.XXXXXX")"
COORD_LOG="$TMP/coordinator.log"
PROV_LOG="$TMP/provider.log"
PORT="${SC_E2E_PORT:-8873}"
CONSUMER_KEY="e2e-consumer-key"
PROVIDER_TOKEN="e2e-provider-token"
PROMPT="quantum entanglement in one sentence"

BACKEND="mock"; MODEL="mock-echo"; CARGO_FEATURES=(); MODEL_PATH_ENV=()
if [[ -n "${SC_E2E_MODEL_PATH:-}" ]]; then
  BACKEND="llama"
  MODEL="$(basename "${SC_E2E_MODEL_PATH%.gguf}")"
  CARGO_FEATURES=(--features llama)
  MODEL_PATH_ENV=(env "SC_MODEL_PATH=$SC_E2E_MODEL_PATH")
fi

pids=()
cleanup() {
  for pid in "${pids[@]:-}"; do kill "$pid" 2>/dev/null || true; done
  wait 2>/dev/null || true
  [[ "${SC_E2E_KEEP:-}" == "1" ]] || rm -rf "$TMP"
}
trap cleanup EXIT

echo "==> building (backend=$BACKEND)"
( cd coordinator && go build -o "$TMP/coordinator" ./cmd/coordinator )
( cd provider-core && cargo build --quiet --bin provider-daemon ${CARGO_FEATURES[@]+"${CARGO_FEATURES[@]}"} )
PROVIDER_BIN="provider-core/target/debug/provider-daemon"

echo "==> starting coordinator on :$PORT"
SC_HTTP_ADDR=":$PORT" SC_CONSUMER_API_KEYS="$CONSUMER_KEY" SC_PROVIDER_TOKENS="$PROVIDER_TOKEN" \
SC_HEARTBEAT_SECONDS="5" SC_DEBUG="1" \
  "$TMP/coordinator" >"$COORD_LOG" 2>&1 &
pids+=($!)
for _ in $(seq 1 50); do curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1 && break; sleep 0.1; done

echo "==> starting provider (backend=$BACKEND, model=$MODEL)"
${MODEL_PATH_ENV[@]+"${MODEL_PATH_ENV[@]}"} env \
  SC_COORDINATOR_URL="ws://127.0.0.1:$PORT/ws/provider" \
  SC_REGISTRATION_TOKEN="$PROVIDER_TOKEN" \
  SC_MODEL="$MODEL" SC_BACKEND="$BACKEND" \
  SC_IDENTITY_PATH="$TMP/identity.key" SC_LOG="warn" \
  "$PROVIDER_BIN" >"$PROV_LOG" 2>&1 &
pids+=($!)

echo "==> waiting for provider to register"
registered=0
for _ in $(seq 1 200); do
  n="$(curl -fsS "http://127.0.0.1:$PORT/healthz" | sed -n 's/.*"providers":\([0-9]*\).*/\1/p')"
  if [[ "${n:-0}" -ge 1 ]]; then registered=1; break; fi
  sleep 0.1
done
[[ "$registered" == "1" ]] || { echo "FAIL: provider never registered"; cat "$PROV_LOG"; exit 1; }

echo "==> streaming completion"
STREAM_OUT="$(curl -fsS -N "http://127.0.0.1:$PORT/v1/chat/completions" \
  -H "Authorization: Bearer $CONSUMER_KEY" -H "Content-Type: application/json" \
  -d "{\"model\":\"$MODEL\",\"stream\":true,\"max_tokens\":40,\"messages\":[{\"role\":\"user\",\"content\":\"$PROMPT\"}]}")"
echo "$STREAM_OUT" | grep -q "data: \[DONE\]" || { echo "FAIL: no [DONE] sentinel"; echo "$STREAM_OUT"; exit 1; }
ASSEMBLED="$(echo "$STREAM_OUT" | sed -n 's/^data: //p' | grep -v '^\[DONE\]$' \
  | python3 -c "import sys,json;print(''.join(json.loads(l)['choices'][0]['delta'].get('content','') for l in sys.stdin if l.strip()))")"
echo "    assembled: $ASSEMBLED"
[[ -n "$ASSEMBLED" ]] || { echo "FAIL: empty streamed content"; exit 1; }
if [[ "$BACKEND" == "mock" ]]; then
  echo "$ASSEMBLED" | grep -q "mock reply to: $PROMPT" || { echo "FAIL: streamed content mismatch"; exit 1; }
fi

echo "==> non-streaming completion"
BLOCK_OUT="$(curl -fsS "http://127.0.0.1:$PORT/v1/chat/completions" \
  -H "Authorization: Bearer $CONSUMER_KEY" -H "Content-Type: application/json" \
  -d "{\"model\":\"$MODEL\",\"max_tokens\":40,\"messages\":[{\"role\":\"user\",\"content\":\"$PROMPT\"}]}")"
echo "$BLOCK_OUT" | BACKEND="$BACKEND" python3 -c "
import sys,json,os
r=json.load(sys.stdin)
c=r['choices'][0]['message']['content']
assert c.strip(), 'empty content'
assert r['usage']['completion_tokens'] > 0, r['usage']
if os.environ['BACKEND']=='mock':
    assert 'mock reply to: $PROMPT' in c, c
print('    content:', c[:120])
print('    usage:', r['usage'])
"

echo "==> auth is enforced"
code="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/v1/chat/completions" \
  -H "Content-Type: application/json" -d "{\"model\":\"$MODEL\",\"messages\":[]}")"
[[ "$code" == "401" ]] || { echo "FAIL: expected 401 without key, got $code"; exit 1; }

echo "==> no prompt plaintext in logs"
if grep -aF "$PROMPT" "$COORD_LOG" "$PROV_LOG"; then echo "FAIL: prompt text in a process log"; exit 1; fi
if [[ "$BACKEND" == "mock" ]] && grep -aF "mock reply to:" "$COORD_LOG" "$PROV_LOG"; then
  echo "FAIL: completion text in a process log"; exit 1
fi

echo "==> metrics exposed"
curl -fsS "http://127.0.0.1:$PORT/metrics" | grep -q "sc_jobs_total" || { echo "FAIL: no metrics"; exit 1; }

echo
echo "PASS: encrypted end-to-end round-trip verified (backend=$BACKEND; stream + block + auth + log hygiene + metrics)"
