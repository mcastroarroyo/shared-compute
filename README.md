# shared-compute

A decentralized **private-inference network**. Idle devices (macOS, Linux, Windows,
Android) act as inference providers. A consumer calls an OpenAI-compatible API; a
coordinator authenticates, schedules, and relays an **end-to-end-encrypted** request to a
provider device, which runs the model locally with `llama.cpp` and streams encrypted
tokens back.

> **Working name.** `shared-compute` is a placeholder. Pick a production name with no
> association to any existing product before public launch.

## Status

Pre-alpha. Building milestone by milestone — see
[`docs/ROADMAP.md`](docs/ROADMAP.md).

| Milestone | Scope | State |
|---|---|---|
| M0 | Repo, protocol contract, toolchains, CI, local stack | in progress |
| M1 | First end-to-end encrypted inference on localhost | — |
| M2 | Coordinator online (Fly.io) with TLS | — |
| M3 | Model registry (Cloudflare R2) + macOS/Linux provider packaging | — |
| M4 | Scheduler, trust tiers, metering, web console | — |
| M5 | Android provider app | — |
| M6 | Android acceleration + hardware attestation (Tier 1) | — |
| M7 | Linux/Windows device attestation (Tier 1) | — |
| M8 | Confidential compute (Tier 2) | — |
| M9 | Production scale + enterprise readiness | — |

## Architecture

```
                       ┌─────────────────────────────┐
  consumer ──TLS───────▶      Coordinator (Go)        │
  (OpenAI-compatible)  │  auth · scheduler · metering │
                       │  per-request re-seal · relay │
                       └──────────────┬──────────────┘
                                      │  outbound WSS, NaCl box per job
             ┌────────────────────────┼────────────────────────┐
             ▼                        ▼                        ▼
   Provider daemon (Rust)     Provider daemon (Rust)    Android app (Kotlin
   macOS / Linux / Windows    Linux / Windows           + JNI → Rust core)
   llama.cpp: Metal/CUDA/     llama.cpp: CUDA/Vulkan/    llama.cpp: CPU →
   Vulkan/CPU                 SYCL/CPU                   Vulkan/OpenCL/NPU
```

## Repository layout

| Path | What |
|---|---|
| `protocol/` | Language-neutral contract: message JSON Schemas, crypto envelope spec, version |
| `coordinator/` | Go control plane: HTTP API, provider WebSocket hub, scheduler, re-seal, metering |
| `provider-core/` | Rust workspace: protocol, crypto, net, models, inference, telemetry, attestation, daemon |
| `android-app/` | Kotlin + Compose provider app (JNI → `provider-core`) |
| `console/` | Next.js dashboard: chat playground, API keys, provider earnings |
| `model-registry/` | `sc-modelctl` — build and sign the model manifest |
| `infra/` | Dockerfiles, `docker-compose.yml`, `fly.toml`, Terraform, CI |
| `e2e-tests/` | Boot coordinator + provider + stub model; assert encrypted round-trip |
| `docs/` | Roadmap, threat model, security notes |

## Quick start (local, no accounts)

```bash
make dev        # bring up coordinator + postgres + object-store stub
make test       # unit tests for coordinator and provider-core
make e2e        # end-to-end encrypted round-trip test
```

See [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md) for toolchain setup.

## Legal

Clean-room implementation. This project does **not** copy or adapt code from any
proprietary private-inference platform. It is built from generic, publicly documented
primitives (WebSockets, NaCl/libsodium `crypto_box`, `llama.cpp`, GGUF, SHA-256 manifests,
TPM/StrongBox/VBS/AVF attestation). Obtain independent legal review before any commercial
launch. See [`docs/LEGAL.md`](docs/LEGAL.md).
