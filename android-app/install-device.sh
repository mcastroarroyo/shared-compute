#!/usr/bin/env bash
# Build + install the debug APK on a connected Android device (USB debugging on) or a
# running emulator, launch it, and stream its logs.
set -euo pipefail
cd "$(dirname "$0")"

export JAVA_HOME="${JAVA_HOME:-/opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home}"
export ANDROID_HOME="${ANDROID_HOME:-/opt/homebrew/share/android-commandlinetools}"
ADB="$ANDROID_HOME/platform-tools/adb"

[ -f app/src/main/jniLibs/arm64-v8a/libsc_mobile.so ] || {
  echo "native lib missing — running build-native.sh"; ./build-native.sh; }

echo "==> assembling debug APK"
./gradlew :app:assembleDebug -q

APK="app/build/outputs/apk/debug/app-debug.apk"
echo "==> devices"; "$ADB" devices
echo "==> installing $APK"
"$ADB" install -r "$APK"
echo "==> launching"
"$ADB" shell am start -n dev.ayni.provider/.MainActivity
echo "==> logs (Ctrl-C to stop)"
"$ADB" logcat -c
"$ADB" logcat --pid="$("$ADB" shell pidof dev.ayni.provider | tr -d '\r')" \
  '*:I' 2>/dev/null || "$ADB" logcat | grep -E "ayni|sc_mobile|provider_lib|AndroidRuntime"
