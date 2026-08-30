# android-app

Kotlin + Jetpack Compose provider app — **scaffolded in M5**.

Planned:
- foreground `ProviderService` with persistent notification
- `uniffi`-generated JNI bindings to `provider-core` built as an `arm64-v8a` cdylib
- llama.cpp Android backend: CPU first (M5), then Vulkan / OpenCL with auto-benchmark (M6)
- policy engine: run only while charging / on Wi-Fi / screen-off / above battery threshold /
  below a temperature cap; nightly schedule; max RAM
- hardware-backed identity key + Key Attestation + Play Integrity (M6) → Tier 1

Build prerequisites (installed at M5): Android Studio, Android SDK + NDK, a device or
emulator running Android 13+.

Distribution: Android App Bundle to the Google Play internal-testing track (operator
creates the Play Console account — $25 one-time + ID verification).
