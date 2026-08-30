# Development setup

## Toolchains (macOS, Homebrew)

```bash
brew install go rust cmake ninja pkg-config libsodium openjdk gradle
```

- **Go** 1.23+ — coordinator
- **Rust** stable — provider core, model-registry CLI
- **CMake + Ninja** — building the bundled `llama.cpp`
- **libsodium** — NaCl `crypto_box` for Go (`pkg-config` finds it) and Rust
- **OpenJDK + Gradle** — Android app (M5+); also add Android Studio + NDK then
- **Docker** (Desktop or `colima`) — `make dev` local stack

After installing `openjdk`:

```bash
sudo ln -sfn /opt/homebrew/opt/openjdk/libexec/openjdk.jdk \
  /Library/Java/JavaVirtualMachines/openjdk.jdk
```

## Android (M5+)

```bash
brew install --cask android-commandlinetools
brew install rustup && rustup default stable
rustup target add aarch64-linux-android armv7-linux-androideabi x86_64-linux-android
cargo install cargo-ndk
export ANDROID_HOME=/opt/homebrew/share/android-commandlinetools
yes | sdkmanager --licenses
sdkmanager "platform-tools" "platforms;android-35" "build-tools;35.0.0" \
           "ndk;27.2.12479018" "cmake;3.22.1"
```

`android-app/build-native.sh` cross-compiles `provider-core/sc-mobile` and regenerates the
uniffi Kotlin bindings. It uses **rustup's** toolchain (Homebrew's `rustc` has no Android
`std`) — the script sets `CARGO=/opt/homebrew/opt/rustup/bin/cargo` for you.

Then `cd android-app && ./gradlew :app:assembleDebug`.

## llama.cpp

Vendored as a git submodule at `provider-core/sc-inference/vendor/llama.cpp`.

```bash
git submodule update --init --recursive
```

The `sc-inference` build script compiles it with:
- **macOS:** Metal
- **Linux:** CPU by default; `--features cuda` or `--features vulkan`
- **Android (M5+):** CPU; `--features vulkan` / `--features opencl`

## Layout

```
protocol/       contract: JSON Schemas + envelope.md + VERSION
coordinator/    Go: cmd/coordinator + internal/{api,wshub,crypto,registry,scheduler,relay,protocol,config}
provider-core/  Rust workspace: sc-protocol, sc-crypto, sc-net, sc-models, sc-inference,
                sc-telemetry, sc-attest, provider-daemon
model-registry/ Rust: sc-modelctl
infra/          docker-compose.yml, coordinator.Dockerfile, fly.toml (M2)
e2e-tests/      run.sh
scripts/        check-no-prompt-logging.sh, validate-schemas.sh
```

## Common commands

```bash
make build      # build coordinator + provider workspace
make test       # go test ./... ; cargo test
make dev        # docker compose up (coordinator + postgres + minio)
make e2e        # end-to-end encrypted round-trip
make lint       # go vet + golangci-lint + cargo clippy + no-prompt-logging check
make fmt        # gofmt + cargo fmt
```

## Test models (M1)

Download a small GGUF manually for now (M3 automates this from the registry):

```bash
mkdir -p models
# e.g. a 1B–2B instruct model quantized to Q4_K_M, placed at:
#   models/llama-3.2-1b-instruct-q4_k_m.gguf
```

Point the provider at it with `--model-path models/<file>.gguf`.
