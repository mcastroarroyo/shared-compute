package api

import (
	"fmt"
	"net/http"
	"strings"
)

// GET /install/provider.sh — a self-contained installer the "Share your computer"
// page hands out. It downloads a prebuilt provider-daemon for the host (falling
// back to a source build), fetches a small model, and connects to this
// coordinator. The account's registration token is passed in the environment,
// never baked into the script.
func (s *Server) handleInstallScript(w http.ResponseWriter, _ *http.Request) {
	apiBase := s.cfg.AuthCallbackBase // the API origin, e.g. https://api.ayni-ai.com
	if apiBase == "" {
		apiBase = "https://api.ayni-ai.com"
	}
	ws := strings.Replace(apiBase, "http", "ws", 1) + "/ws/provider"
	model := "qwen2.5-0.5b-instruct-q4_k_m"
	modelURL := strings.TrimRight(s.cfg.ManifestURL, "/")
	if modelURL == "" {
		modelURL = "https://models.ayni-ai.com"
	}
	repo := "https://github.com/mcastroarroyo/shared-compute"

	script := fmt.Sprintf(`#!/usr/bin/env bash
# Ayni provider installer. Runs your machine as a compute provider.
set -euo pipefail

: "${SC_REGISTRATION_TOKEN:?set SC_REGISTRATION_TOKEN (from the Share your computer page)}"
COORD_WS="${SC_COORDINATOR_URL:-%s}"
MODEL="%s"
# signed registry layout: {base}/{model_id}/{model_id}.gguf  (see sc-models)
MODEL_URL="%s/${MODEL}/${MODEL}.gguf"
DIR="${AYNI_DIR:-$HOME/.ayni}"
mkdir -p "$DIR/models"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"; [ "$arch" = "x86_64" ] && arch="x64"; [ "$arch" = "aarch64" ] && arch="arm64"
asset="provider-daemon-${os}-${arch}"
BIN="$DIR/provider-daemon"

if [ ! -x "$BIN" ]; then
  url="%s/releases/download/provider-latest/${asset}"
  echo "==> fetching $asset"
  if curl -fsSL "$url" -o "$BIN" 2>/dev/null; then
    chmod +x "$BIN"
  else
    echo "==> no prebuilt binary for $os/$arch; building from source (needs Rust + CMake)"
    [ -d "$DIR/src" ] || git clone --depth 1 %s "$DIR/src"
    ( cd "$DIR/src/provider-core" && cargo build --release --bin provider-daemon --features llama )
    cp "$DIR/src/provider-core/target/release/provider-daemon" "$BIN"
  fi
fi

if [ ! -f "$DIR/models/$MODEL.gguf" ]; then
  echo "==> downloading model ($MODEL)"
  curl -fSL "$MODEL_URL" -o "$DIR/models/$MODEL.gguf"
fi

echo "==> connecting to $COORD_WS"
exec env \
  SC_COORDINATOR_URL="$COORD_WS" \
  SC_REGISTRATION_TOKEN="$SC_REGISTRATION_TOKEN" \
  SC_MODEL="$MODEL" \
  SC_MODEL_PATH="$DIR/models/$MODEL.gguf" \
  SC_BACKEND="${SC_BACKEND:-llama}" \
  SC_IDENTITY_PATH="$DIR/identity.key" \
  "$BIN"
`, ws, model, modelURL, repo, repo)

	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(script))
}
