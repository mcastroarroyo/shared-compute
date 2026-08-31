# Roadmap

Plan of record, approved 2026-08-29. Each milestone ends with something running.

## Trust tiers

Surfaced to consumers via the `X-Provider-Trust-Level` request header.

| Tier | Name | Guarantee |
|---|---|---|
| 0 | `community` | Signed provider binary + mandatory network encryption. No claim the device owner cannot inspect plaintext. |
| 1 | `device_attested` | Hardware-backed identity key + Secure Boot / measured boot + approved binary. GPU may be outside the boundary. |
| 2 | `confidential` | CPU TEE + confidential GPU + remote attestation before key release. |
| 3 | `cryptographic` | Provider computes on ciphertext; no hardware-trust assumption. FHE / MPC. **Long-term research endpoint, not a committed milestone** (WHITEPAPER §4.2). |

Tier 0 is built fully now. Every trust seam is a trait/interface so 1 and 2 slot in later.
Tier 3 is a research direction. Separately, **proof-of-inference (zkML)** — a
verifiable proof that the provider ran the exact model on the exact input — is
the near-term way to make result integrity (AYNI-004) cryptographic instead of
redundancy-based (WHITEPAPER §4.3); it is not blockchain technology and is a
candidate for a scoped spike.

## Hardware classes

`MICRO` (1–3B, phones) · `SMALL` (3–8B) · `MEDIUM` (12–30B quantized) · `LARGE` (30–70B) ·
`XL` (70B / MoE) · `CONFIDENTIAL_GPU`. Carried in the model manifest and the scheduler.

## Status (2026-08-30)

| M | State | Live at |
|---|---|---|
| M0 | done | — |
| M1 | done (mock + real llama.cpp/Metal) | localhost |
| M2 | done | `https://api.ayni-ai.com` (Fly.io + Postgres, Let's Encrypt) |
| M3 | done | `https://models.ayni-ai.com` (R2, signed manifest, verified download) |
| M4 | done — catalog `/v1/models`, capability scheduler, rate limits, admin API, console on CF Pages | `api.ayni-ai.com` · `ayni-console.pages.dev` |
| M5 | done — Android provider app, encrypted policy-gated streamed inference | Play internal testing |
| M6 | done — Android StrongBox key attestation → `device_attested` | live |
| M9 | partial — Stripe billing + V2 Connect payouts (money loop verified end to end) | live |
| M10.1 | done — node self-benchmark + ACU capability registry (`GET /admin/nodes`) | live |
| M10.2 | done — `POST /v1/batch` fan-out / aggregate execution primitive | live |
| M10.3 | done — `POST /v1/workloads` quote engine (estimate → price + ETA → accept → run) | live |
| M10.4 | done — site reframe + public `POST /v1/quote` live-quote box on the landing page | `ayni-ai.com` |
| M10.5 | done — spot tier (`"spot": true` on quote/workload: ~60% price, wider ETA band, capped concurrency) | live |
| M7–M8 | not started | — |

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

### M10 — Marketplace v0.1 *(no new accounts)*
Reframe from "rent a phone for X hours" to an intelligent hybrid compute marketplace:
"tell Ayni what to run → one aggregated quote → it executes, verifies, pays". Substeps:

- **M10.1 — Node capability registry + benchmarking (done).** Each node self-benchmarks
  (`provider-lib` `benchmark.rs`): memory bandwidth, a short prefill run, and a ~400-token
  sustained run split first-half/second-half to expose thermal decay. Coordinator turns the
  `benchmark_report` into an **Ayni Compute Unit (ACU)** — normalized from *measured*
  sustained throughput, ~1.0 for a mid-range phone (`internal/capability`). Stored in
  `node_capabilities`; surfaced at `GET /admin/nodes` and the console **Nodes** page.
- **M10.2 — Batch fan-out primitive (done).** `POST /v1/batch`: N independent chat items
  for one model, fanned across every eligible provider (`scheduler.Eligible`), run in
  parallel with a bounded worker pool, reassembled in submission order, metered per
  sub-job. Prefers higher-ACU nodes; degrades to fewer providers; per-item errors are
  reported, not fatal (`internal/batch`).
- **M10.3 — Workload estimator + marketplace quote (done).** `POST /v1/workloads` returns a
  time-boxed quote: token volume estimated from explicit items or `{count, avg_*_tokens}`;
  eligible supply discovered via `scheduler.Eligible`; wall-time predicted from the sum of
  eligible nodes' measured sustained tok/s; price built bottom-up in `pricing.QuoteWorkload`
  — `compute_acquisition + coordination(15%) + expected_failure(8%/redundancy) + margin(30%)
  + payment(3%)`, NOT bid + margin. `GET /v1/workloads/{id}` shows status; `POST
  /v1/workloads/{id}/accept` runs an explicit-items quote through the M10.2 primitive,
  meters each sub-job, and debits the consumer once at the quoted price. Quotes are
  in-memory with a 10-min TTL (`internal/marketplace`).
- **M10.4 — Website reframe (done).** Hero "the world's unused compute, on demand". A
  `WorkloadBox` on the landing page makes a debounced live call to the public, unauth,
  rate-limited `POST /v1/quote` (estimate-only, nothing stored/run) and shows the real
  price, humanized ETA, cost breakdown, and live supply status. `/rent` reframed to "Run a
  workload" with the `/v1/workloads` + `/v1/batch` API.
- **M10.5 — Spot tier (done).** `"spot": true` on `/v1/quote` and `/v1/workloads` — a
  service-class axis orthogonal to the hardware trust tier. Priced through a single
  rate-card multiplier (`SC_SPOT_PRICE_FACTOR`, default 0.6) that flows to provider
  accruals too: cheaper for the buyer, less for the seller, both interruptible. ETA is a
  band — the estimate assumes half the aggregate throughput (`SpotSupplyFraction`) and the
  response adds `eta_seconds_max = eta_seconds x SC_SPOT_ETA_SLACK` (default 3). On accept,
  a spot batch runs at a capped worker pool (`SpotMaxConcurrency = 4`) so it can't crowd
  out on-demand + chat. `WorkloadBox` has a Spot toggle.

**Done when:** submit one Llama workload → Ayni quotes it → accept → it fans out across
real phones → results returned in order → providers accrue earnings.

## Account checklist

See the table in the approved plan
(`~/.claude/plans/stateful-drifting-phoenix.md`) and `docs/OPERATOR-RUNBOOK.md` (added at
M2). All account creation, payment, ID verification, and ToS acceptance is done by the
human operator.
