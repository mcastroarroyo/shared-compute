#!/usr/bin/env bash
# Install provider-daemon as a launchd LaunchAgent.
#   ./install.sh /path/to/provider-daemon
#   ./install.sh --uninstall
set -euo pipefail

LABEL="dev.ayni.provider"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
PREFIX="$HOME/.shared-compute"
BIN="$PREFIX/bin/provider-daemon"
WRAPPER="$PREFIX/bin/provider-run.sh"
LOGDIR="$PREFIX/logs"
ENVFILE="$PREFIX/provider.env"
HERE="$(cd "$(dirname "$0")" && pwd)"

if [[ "${1:-}" == "--uninstall" ]]; then
  launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || launchctl unload "$PLIST" 2>/dev/null || true
  rm -f "$PLIST"
  echo "uninstalled $LABEL (binary + env left in $PREFIX)"
  exit 0
fi

SRC="${1:?usage: install.sh /path/to/provider-daemon}"
[[ -x "$SRC" ]] || { echo "not executable: $SRC"; exit 1; }

mkdir -p "$PREFIX/bin" "$LOGDIR" "$HOME/Library/LaunchAgents"
install -m 0755 "$SRC" "$BIN"

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
  echo "created $ENVFILE — set SC_REGISTRATION_TOKEN then re-run, or: launchctl kickstart -k gui/$(id -u)/$LABEL"
fi

cat > "$WRAPPER" <<EOF
#!/usr/bin/env bash
set -a; [ -f "$ENVFILE" ] && . "$ENVFILE"; set +a
exec "$BIN"
EOF
chmod 0755 "$WRAPPER"

sed -e "s#__BIN__#$WRAPPER#" -e "s#__LOGDIR__#$LOGDIR#" \
  "$HERE/dev.ayni.provider.plist" > "$PLIST"

launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$PLIST"
launchctl enable "gui/$(id -u)/$LABEL"
echo "installed. logs: $LOGDIR/provider.err.log"
