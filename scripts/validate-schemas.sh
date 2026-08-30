#!/usr/bin/env bash
# Validate the protocol JSON Schemas parse and (if a validator is available) are well-formed
# 2020-12 schemas. Uses `check-jsonschema` if present, else falls back to a JSON parse check.
set -euo pipefail
cd "$(dirname "$0")/.."

schemas=(protocol/schemas/*.json)

if command -v check-jsonschema >/dev/null 2>&1; then
  check-jsonschema --check-metaschema "${schemas[@]}"
  echo "ok: ${#schemas[@]} schemas valid against 2020-12 metaschema"
else
  for f in "${schemas[@]}"; do
    if command -v jq >/dev/null 2>&1; then
      jq -e . "$f" >/dev/null || { echo "FAIL: invalid JSON: $f" >&2; exit 1; }
    else
      python3 -c "import json,sys; json.load(open(sys.argv[1]))" "$f" \
        || { echo "FAIL: invalid JSON: $f" >&2; exit 1; }
    fi
  done
  echo "ok: ${#schemas[@]} schemas parse (install 'check-jsonschema' for full metaschema validation)"
fi
