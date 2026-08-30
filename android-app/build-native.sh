#!/usr/bin/env bash
# Cross-compile provider-core/sc-mobile for Android and generate the uniffi Kotlin
# bindings. Outputs:
#   app/src/main/jniLibs/<abi>/libsc_mobile.so
#   app/src/main/java/uniffi/sc_mobile/sc_mobile.kt
#
# Requires: rustup with android targets, Android NDK. See docs/DEVELOPMENT.md.
set -euo pipefail
cd "$(dirname "$0")"

ABIS="${ABIS:-arm64-v8a}"                 # space-separated: arm64-v8a armeabi-v7a x86_64
API="${ANDROID_API:-24}"
PROFILE="${PROFILE:-release}"
CORE="../provider-core"

# Use rustup's toolchain (has the android std); brew's rustc does not.
export RUSTUP_HOME="${RUSTUP_HOME:-$HOME/.rustup}"
export PATH="/opt/homebrew/opt/rustup/bin:$HOME/.cargo/bin:$PATH"
export CARGO="${CARGO:-/opt/homebrew/opt/rustup/bin/cargo}"
export RUSTC="${RUSTC:-/opt/homebrew/opt/rustup/bin/rustc}"

: "${ANDROID_NDK_HOME:=$(ls -d /opt/homebrew/share/android-commandlinetools/ndk/* 2>/dev/null | sort -V | tail -1)}"
export ANDROID_NDK_HOME
[ -d "$ANDROID_NDK_HOME" ] || { echo "set ANDROID_NDK_HOME"; exit 1; }
echo "NDK: $ANDROID_NDK_HOME"

FEATURES="${FEATURES:-}"
FEAT_ARG=()
[ -n "$FEATURES" ] && FEAT_ARG=(--features "$FEATURES")
PROFILE_ARG=(--release)
[ "$PROFILE" = "debug" ] && PROFILE_ARG=(--profile dev)

TARGETS_ARG=()
for abi in $ABIS; do TARGETS_ARG+=(-t "$abi"); done

JNILIBS="$(mkdir -p app/src/main/jniLibs && cd app/src/main/jniLibs && pwd)"
echo "==> cross-compiling sc-mobile ($ABIS, api $API, $PROFILE) ${FEATURES:+[$FEATURES]}"
( cd "$CORE" && cargo ndk "${TARGETS_ARG[@]}" --platform "$API" -o "$JNILIBS" \
    build "${PROFILE_ARG[@]}" -p sc-mobile ${FEAT_ARG[@]+"${FEAT_ARG[@]}"} )

echo "==> generating uniffi Kotlin bindings"
FIRST_ABI="${ABIS%% *}"
SO="app/src/main/jniLibs/$FIRST_ABI/libsc_mobile.so"
OUT="app/src/main/java"
mkdir -p "$OUT"
( cd "$CORE" && cargo run -q -p sc-mobile --bin uniffi-bindgen -- \
    generate --library "$OLDPWD/$SO" --language kotlin --out-dir "$OLDPWD/$OUT" )

echo "==> done"
find app/src/main/jniLibs -name '*.so' -exec ls -lh {} \;
ls "$OUT/uniffi/sc_mobile/" 2>/dev/null || true
