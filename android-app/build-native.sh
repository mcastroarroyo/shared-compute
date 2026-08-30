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
[ -d "$ANDROID_NDK_HOME" ] || { echo "set ANDROID_NDK_HOME"; exit 1; }
# llama-cpp-sys-2's build.rs probes ANDROID_NDK / NDK_ROOT / ANDROID_NDK_ROOT, not *_HOME.
export ANDROID_NDK_HOME ANDROID_NDK="$ANDROID_NDK_HOME" NDK_ROOT="$ANDROID_NDK_HOME" ANDROID_NDK_ROOT="$ANDROID_NDK_HOME"
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
# NOTE: uniffi-bindgen cannot extract proc-macro metadata from the cross-compiled
# aarch64 .so on a macOS host (it silently emits nothing and exits 0). Generate from
# a freshly built *host* cdylib instead — the Kotlin interface + checksums are defined
# by the Rust source and are identical across targets.
FIRST_ABI="${ABIS%% *}"
OUT="app/src/main/java"
KT="$OUT/uniffi/sc_mobile/sc_mobile.kt"
mkdir -p "$OUT"
rm -f "$KT"
( cd "$CORE" && cargo build -q -p sc-mobile ${FEAT_ARG[@]+"${FEAT_ARG[@]}"} )
HOSTLIB="$CORE/target/debug/libsc_mobile.dylib"
[ -f "$HOSTLIB" ] || HOSTLIB="$CORE/target/debug/libsc_mobile.so"
[ -f "$HOSTLIB" ] || { echo "host cdylib not found for bindgen"; exit 1; }
( cd "$CORE" && cargo run -q -p sc-mobile --bin uniffi-bindgen -- \
    generate --library "$OLDPWD/$HOSTLIB" --language kotlin --out-dir "$OLDPWD/$OUT" )
[ -f "$KT" ] || { echo "ERROR: uniffi-bindgen produced no $KT"; exit 1; }
# Guard against the exact bug that shipped: bindings older than the packaged .so.
if [ "$KT" -ot "app/src/main/jniLibs/$FIRST_ABI/libsc_mobile.so" ]; then
  echo "ERROR: $KT is older than libsc_mobile.so — regeneration failed"; exit 1
fi

echo "==> done"
find app/src/main/jniLibs -name '*.so' -exec ls -lh {} \;
ls "$OUT/uniffi/sc_mobile/" 2>/dev/null || true
