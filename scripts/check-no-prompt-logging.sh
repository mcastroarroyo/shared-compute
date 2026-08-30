#!/usr/bin/env bash
# Fail if source appears to log prompt/completion content.
# Heuristic: flag logging calls whose arguments mention message/prompt/delta/content/
# completion identifiers. Allowed: token *counts*, model names, job ids.
#
# Comment lines are ignored (doc comments legitimately discuss "do not log the prompt").
# Suppress a real false positive by appending `nolog-ok` to the line.
set -euo pipefail
cd "$(dirname "$0")/.."

call='(slog\.|[^a-zA-Z_]log\.|println!|eprintln!|fmt\.Print|tracing::(info|debug|warn|error)!?|console\.(log|info|debug|warn|error))'
fields='(promptText|prompt_text|\bmessages\b|\.content|token_text|completion_text|response_text|chat_text|delta\b)'

hits=$(grep -RInE "$call.*$fields" \
  --include='*.go' --include='*.rs' --include='*.ts' --include='*.tsx' --include='*.kt' \
  coordinator provider-core console android-app model-registry 2>/dev/null \
  | grep -vE '(_test\.go|_test\.rs|\.test\.ts|/tests?/|testdata|nolog-ok)' \
  | grep -vE ':[0-9]+:\s*(//|///|#|\*|/\*)' \
  || true)

if [[ -n "$hits" ]]; then
  echo "FAIL: possible prompt/completion content in a log/print call:" >&2
  echo "$hits" >&2
  echo >&2
  echo "If this is a false positive, append 'nolog-ok' as a trailing comment on the line." >&2
  exit 1
fi
echo "ok: no prompt/completion content found in logging calls"
