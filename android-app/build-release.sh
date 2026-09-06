#!/usr/bin/env bash
# Build the shippable Android release (Play AAB + sideload APK).
#
# build-native.sh defaults to the MOCK inference backend (what CI uses, no
# cmake/llama.cpp cross-compile). A release must carry the real llama backend,
# so this script forces FEATURES=llama and refuses to package a mock library.
#
#   ./build-release.sh            # -> app/build/outputs/bundle/release/app-release.aab
set -euo pipefail
cd "$(dirname "$0")"
export JAVA_HOME="${JAVA_HOME:-/opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home}"
export ANDROID_HOME="${ANDROID_HOME:-/opt/homebrew/share/android-commandlinetools}"

[ -f keystore/keystore.properties ] || { echo "keystore/keystore.properties missing — release must be signed with the upload key"; exit 1; }

echo "==> native (FEATURES=llama)"
FEATURES=llama ./build-native.sh

SO=app/src/main/jniLibs/arm64-v8a/libsc_mobile.so
if ! strings "$SO" | grep -q 'llama_model_load'; then
  echo "ERROR: $SO has no llama backend (mock build) — refusing to package a release"; exit 1
fi

echo "==> lint + bundle + apk"
./gradlew :app:lintRelease :app:bundleRelease :app:assembleRelease -q
ls -la app/build/outputs/bundle/release/app-release.aab app/build/outputs/apk/release/app-release.apk
echo "==> signed by:"
"$JAVA_HOME/bin/keytool" -printcert -jarfile app/build/outputs/bundle/release/app-release.aab | grep -E 'Owner|SHA256' | head -2
