#!/usr/bin/env bash
# Install provider-daemon as a systemd --user service.
#   ./install.sh /path/to/provider-daemon
#   ./install.sh --uninstall
set -euo pipefail

UNIT="shared-compute-provider.service"
PREFIX="$HOME/.shared-compute"
UNITDIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
ENVFILE="$PREFIX/provider.env"
HERE="$(cd "$(dirname "$0")" && pwd)"

if [[ "${1:-}" == "--uninstall" ]]; then
  systemctl --user disable --now "$UNIT" 2>/dev/null || true
  rm -f "$UNITDIR/$UNIT"
  systemctl --user daemon-reload || true
  echo "uninstalled $UNIT"
  exit 0
fi

SRC="${1:?usage: install.sh /path/to/provider-daemon}"
[[ -x "$SRC" ]] || { echo "not executable: $SRC"; exit 1; }

mkdir -p "$PREFIX/bin" "$UNITDIR"
install -m 0755 "$SRC" "$PREFIX/bin/provider-daemon"

if [[ ! -f "$ENVFILE" ]]; then
  cat > "$ENVFILE" <<'EOF'
SC_COORDINATOR_URL=wss://api.ayni-ai.com/ws/provider
SC_REGISTRATION_TOKEN=CHANGE_ME
SC_MODEL=qwen2.5-0.5b-instruct-q4_k_m
SC_BACKEND=llama
SC_MANIFEST_URL=https://models.ayni-ai.com
SC_REGISTRY_PUBKEY=p7UUs6aCFebGUfSvFV5Wczh7kYBCEW2tDRO+dV0IpDM=
EOF
  chmod 600 "$ENVFILE"
  echo "created $ENVFILE — set SC_REGISTRATION_TOKEN"
fi

install -m 0644 "$HERE/$UNIT" "$UNITDIR/$UNIT"
systemctl --user daemon-reload
systemctl --user enable --now "$UNIT"
loginctl enable-linger "$USER" 2>/dev/null || true
echo "installed. logs: journalctl --user -u $UNIT -f"
