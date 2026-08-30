# Roadmap

Plan of record, approved 2026-08-29. Each milestone ends with something running.

## Trust tiers

Surfaced to consumers via the `X-Provider-Trust-Level` request header.

| Tier | Name | Guarantee |
|---|---|---|
| 0 | `community` | Signed provider binary + mandatory network encryption. No claim the device owner cannot inspect plaintext. |
| 1 | `device_attested` | Hardware-backed identity key + Secure Boot / measured boot + approved binary. GPU may be outside the boundary. |
| 2 | `confidential` | CPU TEE + confidential GPU + remote attestation before key release. |

Tier 0 is built fully now. Every trust seam is a trait/interface so 1 and 2 slot in later.

## Hardware classes

`MICRO` (1–3B, phones) · `SMALL` (3–8B) · `MEDIUM` (12–30B quantized) · `LARGE` (30–70B) ·
`XL` (70B / MoE) · `CONFIDENTIAL_GPU`. Carried in the model manifest and the scheduler.

## Milestones

### M0 — Foundations *(no accounts)*
Toolchains, monorepo scaffold, `protocol/` contract + JSON Schemas + crypto envelope spec,
compiling Go + Rust skeletons, `docker-compose` local stack, CI. **Done when:** repo builds
green, `make dev` starts the stack.

### M1 — First end-to-end encrypted inference on localhost *(no accounts)*
Coordinator: in-memory registry, `POST /v1/chat/completions` (SSE), `GET /v1/models`,
`/ws/provider`, `/healthz`, single-provider scheduler, per-job ephemeral keypair + re-seal
+ decrypt + relay. Provider daemon on this Mac: connect, register (Metal), open sealed job,
run `llama.cpp` on a small GGUF, seal chunks. Crypto property tests. **Done when:**
`curl -N localhost:8080/v1/chat/completions` streams a real model response.

### M2 — Coordinator online with TLS *(Fly.io, Cloudflare, domain)*
Production Dockerfile, `fly.toml`, Postgres schema + migrations, Postgres-backed registry +
auth, Prometheus metrics. Deploy to Fly.io. `api.<domain>` via Cloudflare + TLS. Mac
provider dials the public coordinator. **Done when:** `https://api.<domain>/v1/chat/completions`
serves from the Mac provider.

### M3 — Model registry online + macOS/Linux packaging *(Cloudflare R2)*
`sc-modelctl` builds + Ed25519-signs `manifest.json`. Provider does real streaming download
+ SHA-256 verify + cache. `launchd` / `systemd --user` units, `.deb` / `.tar.gz`, GitHub
Releases via CI. **Done when:** a fresh macOS/Linux box joins the live network with one
command.

### M4 — Scheduler, tiers, metering, console *(Vercel or CF Pages, auth provider)*
Capability-based scheduler (model / RAM / VRAM / context / latency / price / thermal),
trust-tier routing, per-request usage metering + rate limits, `/v1/models` from the signed
manifest. Next.js console: chat playground, API keys, provider earnings. **Done when:** a
key made in the console routes across live providers with visible metering.

### M5 — Android provider app *(Google Play Console — $25 + ID)*
Kotlin + Compose, foreground `ProviderService`, `uniffi` JNI → `provider-core` cdylib,
llama.cpp Android CPU backend, 1–3B GGUF, battery/thermal/charging/Wi-Fi policy. Internal
testing track. **Done when:** a phone does encrypted policy-gated streamed inference against
the live coordinator.

### M6 — Android acceleration + hardware identity (Tier 1)
Vulkan + OpenCL backends with first-run auto-benchmark. Keystore/StrongBox identity key +
Key Attestation chain + Play Integrity verdict at registration; coordinator verifies →
`device_attested`. **Done when:** Android nodes appear as Tier 1.

### M7 — Linux/Windows device attestation (Tier 1) *(code-signing cert)*
Linux: TPM 2.0 AK, Secure Boot + PCR + IMA report, optional Keylime. Windows: MSI +
Service, Authenticode, TPM/Secure Boot/VBS checks, VBS Enclave holds key + does request
decryption, CUDA/Vulkan/SYCL. **Done when:** Tier 1 on all desktop OSes.

### M8 — Confidential compute (Tier 2) *(cloud CVM + CC GPU)*
Linux provider inside AMD SEV-SNP / Intel TDX CVM + NVIDIA CC GPU; coordinator releases job
key only after CPU + GPU attestation. Android AVF/pKVM protected-VM provider + remote VM
attestation. **Done when:** `confidential` tier serves traffic.

### M9 — Production scale & enterprise readiness *(Stripe, audit, legal)*
Multi-region coordinators + global LB, Postgres HA, Redis. Stripe Connect provider payouts
(operator moves the money, not Claude). OpenTelemetry + Grafana + Loki + alerting + SLOs +
runbooks. Threat model, dependency scanning, signed releases, external pen test, IR plan,
lawyer-reviewed ToS/privacy, abuse policy, GDPR/CCPA flows, SOC 2 readiness. **Done when:**
audited multi-region production network with paid providers.

## Account checklist

See the table in the approved plan
(`~/.claude/plans/stateful-drifting-phoenix.md`) and `docs/OPERATOR-RUNBOOK.md` (added at
M2). All account creation, payment, ID verification, and ToS acceptance is done by the
human operator.
