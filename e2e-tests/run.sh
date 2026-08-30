#!/usr/bin/env bash
# End-to-end: coordinator + one mock-backend provider, over the real encrypted job
# envelope. Asserts a streamed and a non-streamed OpenAI-compatible completion, and that
# no prompt/completion plaintext lands in either process's logs.
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

pids=()
cleanup() {
  for pid in "${pids[@]:-}"; do kill "$pid" 2>/dev/null || true; done
  wait 2>/dev/null || true
  [[ "${SC_E2E_KEEP:-}" == "1" ]] || rm -rf "$TMP"
}
trap cleanup EXIT

echo "==> building"
( cd coordinator && go build -o "$TMP/coordinator" ./cmd/coordinator )
( cd provider-core && cargo build --quiet --bin provider-daemon )
PROVIDER_BIN="provider-core/target/debug/provider-daemon"

echo "==> starting coordinator on :$PORT"
SC_HTTP_ADDR=":$PORT" \
SC_CONSUMER_API_KEYS="$CONSUMER_KEY" \
SC_PROVIDER_TOKENS="$PROVIDER_TOKEN" \
SC_HEARTBEAT_SECONDS="5" \
SC_DEBUG="1" \
  "$TMP/coordinator" >"$COORD_LOG" 2>&1 &
pids+=($!)

for _ in $(seq 1 50); do
  curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1 && break
  sleep 0.1
done

echo "==> starting provider (mock backend)"
SC_COORDINATOR_URL="ws://127.0.0.1:$PORT/ws/provider" \
SC_REGISTRATION_TOKEN="$PROVIDER_TOKEN" \
SC_MODEL="mock-echo" \
SC_BACKEND="mock" \
SC_IDENTITY_PATH="$TMP/identity.key" \
SC_LOG="info" \
  "$PROVIDER_BIN" >"$PROV_LOG" 2>&1 &
pids+=($!)

echo "==> waiting for provider to register"
registered=0
for _ in $(seq 1 100); do
  n="$(curl -fsS "http://127.0.0.1:$PORT/healthz" | sed -n 's/.*"providers":\([0-9]*\).*/\1/p')"
  if [[ "${n:-0}" -ge 1 ]]; then registered=1; break; fi
  sleep 0.1
done
[[ "$registered" == "1" ]] || { echo "FAIL: provider never registered"; cat "$PROV_LOG"; exit 1; }

echo "==> streaming completion"
STREAM_OUT="$(curl -fsS -N "http://127.0.0.1:$PORT/v1/chat/completions" \
  -H "Authorization: Bearer $CONSUMER_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"model\":\"mock-echo\",\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"$PROMPT\"}]}")"
echo "$STREAM_OUT" | grep -q "data: \[DONE\]" || { echo "FAIL: no [DONE] sentinel"; echo "$STREAM_OUT"; exit 1; }
ASSEMBLED="$(echo "$STREAM_OUT" | sed -n 's/^data: //p' | grep -v '^\[DONE\]$' \
  | python3 -c "import sys,json;print(''.join(json.loads(l)['choices'][0]['delta'].get('content','') for l in sys.stdin if l.strip()))")"
echo "    assembled: $ASSEMBLED"
echo "$ASSEMBLED" | grep -q "mock reply to: $PROMPT" || { echo "FAIL: streamed content mismatch"; exit 1; }

echo "==> non-streaming completion"
BLOCK_OUT="$(curl -fsS "http://127.0.0.1:$PORT/v1/chat/completions" \
  -H "Authorization: Bearer $CONSUMER_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"model\":\"mock-echo\",\"messages\":[{\"role\":\"user\",\"content\":\"$PROMPT\"}]}")"
echo "$BLOCK_OUT" | python3 -c "
import sys,json
r=json.load(sys.stdin)
c=r['choices'][0]['message']['content']
assert 'mock reply to: $PROMPT' in c, c
assert r['usage']['completion_tokens'] > 0, r['usage']
print('    content:', c)
print('    usage:', r['usage'])
"

echo "==> auth is enforced"
code="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/v1/chat/completions" \
  -H "Content-Type: application/json" -d '{"model":"mock-echo","messages":[]}')"
[[ "$code" == "401" ]] || { echo "FAIL: expected 401 without key, got $code"; exit 1; }

echo "==> no prompt/completion plaintext in logs"
if grep -aF "$PROMPT" "$COORD_LOG" "$PROV_LOG"; then
  echo "FAIL: prompt text found in a process log"; exit 1
fi
if grep -aF "mock reply to:" "$COORD_LOG" "$PROV_LOG"; then
  echo "FAIL: completion text found in a process log"; exit 1
fi

echo
echo "PASS: encrypted end-to-end round-trip verified (stream + block + auth + log hygiene)"
