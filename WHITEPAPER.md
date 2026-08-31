# Ayni: a community network for private, shared AI inference

**Draft v0.2 — 2026-08**
Zalesgen LLC · Apache 2.0 · https://github.com/mcastroarroyo/shared-compute

---

## Abstract

Ayni is a decentralized network that turns idle consumer and prosumer hardware
into a private inference service. A consumer calls an OpenAI-compatible API; a
coordinator authenticates, meters, and relays the request — end-to-end encrypted —
to a provider device, which runs the model locally and streams encrypted tokens
back. Providers are paid a transparent share of usage. Every trust boundary is a
verifiable seam: transport is TLS, the coordinator→provider hop is sealed with a
fresh NaCl key per job, and providers can attest their hardware to reach higher
trust tiers. No component writes prompt or response content to disk or logs.

Compute sharing is the first initiative of a broader community whose aim is to
spread the benefits and value of AI — and a say in how it is used — beyond the
handful of organizations that currently capture them. A fixed share of net
revenue is allocated to AI-facing public goods, and the network's parameters are
governed by the community.

---

## 1. Motivation

Two problems, one design.

**Concentration.** Modern models are trained on a collective inheritance — the
open web, publicly funded research, the work of millions — yet the value they
produce accrues to a small number of firms with the capital to train and serve
them. There is no standard mechanism for that value to flow back.

**Idle capacity.** Billions of capable devices — phones with NPUs, laptops with
GPUs, gaming PCs, workstations — sit unused most of the day. In aggregate this is
a large, geographically distributed, low-marginal-cost inference fleet.

Ayni connects the two: let anyone contribute spare device time as private
inference capacity and be paid for it, let anyone rent that capacity cheaply and
privately, and route a defined portion of the proceeds to keeping AI open and
safe.

The hard requirement is **privacy without trust in the operator or the provider**.
A consumer's prompt must not be readable by the coordinator beyond the moment of
relay, must not be persisted anywhere, and — at higher tiers — must run only on
hardware whose integrity has been cryptographically demonstrated.

---

## 2. Architecture

```
                       ┌─────────────────────────────┐
  consumer ──TLS───────▶      Coordinator (Go)        │
  (OpenAI-compatible)  │  auth · scheduler · metering │
                       │  per-job re-seal · relay     │
                       └──────────────┬──────────────┘
                                      │  outbound WSS, NaCl box per job
             ┌────────────────────────┼────────────────────────┐
             ▼                        ▼                        ▼
   Provider core (Rust)      Provider core (Rust)      Android app (Kotlin
   macOS / Linux / Windows   Linux / Windows           + JNI → same core)
   llama.cpp: Metal/CUDA/    CUDA/Vulkan/CPU           llama.cpp: CPU →
   Vulkan/CPU                                          Vulkan/NPU
```

### 2.1 Coordinator (Go)

Stateless request handling backed by Postgres. Responsibilities:

- **Authentication** of consumer API keys (hashed at rest) and provider
  registration tokens.
- **Scheduling**: given a request's model, minimum context, hardware class, and
  required trust tier, pick a connected provider; rank by load, then tier, then
  thermal and battery headroom, then free RAM.
- **Per-job re-seal and relay**: generate an ephemeral X25519 keypair for the
  job, seal the plaintext request to the provider's registered static key,
  forward sealed token chunks to the consumer as Server-Sent Events, decrypting
  each chunk only in memory at the relay boundary.
- **Metering**: record token counts, duration, model, tier, and provider per
  request — never content — and shadow-accrue provider earnings.
- **Attestation verification**: validate provider hardware evidence and assign a
  trust tier (§4).

### 2.2 Provider core (Rust)

A portable library (`provider-lib`) with thin platform shells:

- **`provider-daemon`** — a CLI binary for macOS, Linux, and Windows, run as a
  user agent / systemd unit / service.
- **Android app** — a Kotlin/Compose foreground service with a policy engine,
  binding to the same core through UniFFI-generated JNI.

The core dials the coordinator over WSS, registers its **capabilities** (platform,
arch, CPU/GPU, RAM, backend, hardware class, models, static public key, and a
security-capabilities descriptor), maintains a heartbeat, and handles jobs. It
embeds `llama.cpp` via the `llama-cpp-2` crate (Metal on macOS; CUDA/Vulkan/CPU
elsewhere). Models are pulled from a signed registry, verified per-file by
SHA-256 against an Ed25519-signed manifest, and only then advertised.

Prompt and completion text live only in the provider process's memory. They are
never written to disk, never logged, and — outside the relay boundary — never
seen by the coordinator.

---

## 3. Protocol and crypto envelope

### 3.1 Messages (version 0, immutable contract)

`register`, `register_ack`, `heartbeat` / `heartbeat_ack`, `model_pull`,
`model_ready`, `job_request` (sealed), `job_chunk` (sealed), `job_done`,
`job_error`, `cancel`, `benchmark_report`, `attestation`. Each is specified by a
JSON Schema in `protocol/`.

### 3.2 Envelope

- **Consumer → coordinator**: TLS. Optionally the request body is additionally
  NaCl-sealed to a published coordinator X25519 key.
- **Coordinator → provider**: mandatory `crypto_box` (X25519 key agreement,
  XSalsa20-Poly1305 AEAD) using a **fresh ephemeral keypair generated per job**.
  The sealed payload carries the ephemeral public key so the provider can reply.
- **Provider → coordinator**: each `job_chunk` is sealed back to that job's
  ephemeral public key.

A compromised or malicious coordinator that logs everything it can still only
retain ciphertext for the provider hop plus the plaintext it momentarily relays;
it is structurally prevented from persisting that plaintext by a codebase-wide CI
check, and a future hardened deployment runs the relay in a TEE so even a
compromised host cannot capture it.

### 3.3 Metering integrity

The coordinator counts tokens from the stream it decrypts and relays, not from
the provider's self-reported `job_done.usage`. A provider therefore cannot inflate
a bill or a payout by lying about token counts.

---

## 4. Trust tiers and attestation

Consumers set `X-Provider-Trust-Level`; the coordinator routes only to providers
that meet or exceed it, or returns `409`.

| Tier | Meaning |
|---|---|
| `community` | Signed binary + network crypto only. The device owner is not prevented from inspecting plaintext. Priced lowest. |
| `device_attested` | A hardware-backed key (Android StrongBox / a TEE / TPM 2.0) plus verified boot, with the attestation chain verified to the platform vendor's root. |
| `confidential` | CPU TEE (SEV-SNP / TDX) + confidential GPU with remote attestation. The per-job key is released only after both verify. (Roadmap.) |
| `cryptographic` | The provider computes on ciphertext and never holds plaintext — no hardware-trust assumption at all. Candidate primitives: fully homomorphic encryption (FHE) and secret-shared multi-party computation (MPC). **Long-term research endpoint, not on the near-term roadmap.** See §4.2. |

The tiers form a ladder of *decreasing* trust in the provider: `community` trusts
the binary, `device_attested` trusts the hardware vendor, `confidential` trusts
the CPU/GPU TEE, `cryptographic` trusts only the math (§4.2). None of this is
blockchain or token technology — "cryptographic" means encryption schemes you can
compute on, and proofs, verified off-chain by the coordinator.

### 4.1 Android device attestation (implemented)

The app generates an EC P-256 key in StrongBox (or the TEE) with a Key
Attestation challenge and signs the provider's X25519 identity with it:

```
sig = ECDSA-P256/SHA-256( "ayni-bind-v1" || static_pk[32] || nonce || be64(issued_at) )
```

At `register` the coordinator:

1. verifies the X.509 chain, terminating at a pinned Google
   hardware-attestation root;
2. parses the key-attestation extension (`1.3.6.1.4.1.11129.2.1.17`): requires
   `attestationSecurityLevel ≥ TrustedEnvironment`, `verifiedBootState == Verified`,
   `deviceLocked == true`;
3. verifies the binding signature against the leaf key and checks the timestamp
   is within ±5 minutes.

Freshness currently rests on the binding signature over a per-session nonce and
timestamp, on top of the TLS channel; a coordinator-issued challenge handshake is
a planned hardening step. Play Integrity, Linux/TPM measured boot, and Windows VBS
enclaves follow the same `AttestationProvider` trait.

### 4.2 The `cryptographic` tier (long-term research)

FHE and MPC would let a `community`-class device serve genuinely private
inference: the device owner, a rooted OS, and a malicious provider build all see
only ciphertext, and only the consumer — who holds the secret key — decrypts the
result. This maps cleanly onto Ayni's design; the coordinator already relays
sealed payloads and never needs plaintext.

It is not viable for LLM inference today. FHE transformer inference is roughly
10⁴–10⁶× slower than plaintext and is bottlenecked on large-polynomial arithmetic
and memory bandwidth — the opposite of what phone CPUs are good at — so it
collapses the economics of an idle-device supply pool. MPC (secret-shared
inference across several providers) is closer to practical, perhaps 10–100×
plaintext, and it reuses the existing fan-out primitive, but adds network-round
latency and a threshold-collusion assumption: it *distributes* trust rather than
eliminating it. Both are tracked as research. Neither is a committed milestone.

### 4.3 Proof-of-inference (near-term integrity hardening)

Distinct from confidentiality: a **zero-knowledge proof of correct inference
(zkML)** lets a provider prove it ran the exact allow-listed model on the exact
input and produced the exact output, revealing nothing else. It does not hide the
prompt, but it addresses result integrity (threat-model AYNI-004)
cryptographically — instead of the coordinator re-deriving token counts and
paying a redundancy tax to cross-check untrusted output, the provider's result
carries a verifiable proof. Proving is currently on the order of minutes for small
models, but it is asynchronous, which suits batch execution. This is the
highest-leverage cryptographic investment on a months — not years — horizon and
is a candidate for a scoped spike. Proofs are generated on the provider, verified
by the coordinator, and never touch a chain.

---

## 5. Scheduling and reliability

The scheduler filters providers by model availability, minimum context length,
and hardware class, then ranks by: current load → trust tier → thermal state →
battery headroom → free RAM. Sessions can be made sticky. On provider
disconnection mid-job the coordinator surfaces a clean, retriable error;
job cancellation propagates end to end.

Hardware classes (in the signed manifest and the scheduler): `MICRO` (1–3B,
phones), `SMALL` (3–8B), `MEDIUM` (12–30B quantized), `LARGE` (30–70B),
`XL` (70B / MoE), `CONFIDENTIAL_GPU`.

---

## 6. Economics and payments

All amounts are integer micro-USD.

**Consumers** hold a prepaid credit balance (Stripe Checkout top-ups). Each
request is priced per token by model class, times a multiplier for the trust tier
served. Postpaid metered billing is available for vetted accounts.

**Providers** onboard through Stripe Connect (Stripe handles KYC and bank
details; Ayni stores only an account id). Each completed job writes a
`provider_earnings` row:

```
gross    = price_card(model_class, io_split) · tokens · tier_multiplier
provider = gross · rev_share · quality_multiplier
```

`rev_share` is 0.70 (`community` / `device_attested`) or 0.60 (`confidential`).
`quality_multiplier ∈ [0.5, 1.1]` reflects a rolling window of uptime, p95 TTFT,
error rate, and cancellations; it defaults to 1.0. Payout runs are weekly, above
a minimum threshold, with a 7-day reserve against chargebacks and abuse.

The remainder of gross, after Stripe fees and coordinator infrastructure, splits
between a community treasury and the **AI-stakeholder allocation** — a fixed,
published percentage of net revenue directed to hosting open-weights models for
free public use, funding alignment and interpretability research, and supporting
AI-welfare work as that science matures. The percentage changes only by community
process.

At launch rates, a single phone running a small model earns very little; the
network's value is in aggregate scale, in larger models on real GPUs, and in
premium private and confidential workloads. Ayni's design goal is that anyone can
take part and share in the upside, not that any one device becomes a business.

Full detail: `docs/PAYMENTS.md`.

---

## 7. Threat model (summary)

- **Curious or malicious coordinator operator.** Cannot read the provider hop
  (sealed per job). Can see relayed plaintext transiently; cannot persist it
  (CI-enforced, TEE-relay on the roadmap). Cannot forge token counts in a way
  that benefits a provider, since it is the counter.
- **Malicious provider (`community`).** Sees plaintext of jobs routed to it —
  this is the explicit limitation of Tier 0, disclosed to consumers, and the
  reason `device_attested` exists. Cannot inflate payouts. Cannot serve tiers it
  cannot attest.
- **Sybil provider farm.** `device_attested` requires a unique hardware key
  attestation per node; `community` earns near zero; per-identity rate and
  earning caps apply.
- **Network attacker.** TLS plus per-job sealing; tampered ciphertext is rejected
  (authenticated encryption).
- **Consumer fraud / chargebacks.** Prepaid credits bound exposure; Stripe Radar
  on top-ups; payout-side reserve.

Full detail: `docs/THREAT-MODEL.md` and the marketplace delta in
`security/threat-model/SECURITY_V0.1.md`. An external penetration test and
security audit are planned before general availability.

### 7.1 Cyber harness and hardened settlement

`security/` holds an **offline adversarial harness** — ~30 hostile cases for
signed manifests and result receipts (tampered runtime/model hashes,
`network_policy` widening, shell operations, expired leases, unbounded resource
limits, wrong-device binding, replay, post-signature tamper) — that must all be
rejected, run on every push by a dedicated CI job. It ships with executable
reference models for the financial invariants: credits conserved
(`funded = balances + charged`), provider earnings never exceed customer charges,
payouts never exceed accrued earnings, one payable event per verified
`(job_id, device)`.

The coordinator enforces the money-critical subset today: each job is bound to
its assigned provider and result frames from any other connection are dropped;
`provider_earnings` is unique on `(job_id, static_pk)` and debits are idempotent
on `job_id`; the coordinator counts completion tokens itself from the
authenticated, strictly-ordered chunk stream and overwrites the provider's
self-report before billing.

### 7.2 Workload Manifest v1

Every dispatched job can carry an **Ed25519-signed Workload Manifest** (isolated
signer key; public key at `GET /v1/manifest-key`) that binds the job to one
device key, runtime, model, `operation: "inference"`, `network_policy: "NONE"`, a
resource envelope, and an expiry. The provider node verifies the signature and
checks every resource limit against **immutable local ceilings the coordinator
cannot raise**, refusing to run on any mismatch. A Go↔Rust known-answer test
guards the canonical signing form. `docs/WORKLOAD-MANIFEST.md`.

---

## 8. Governance — the Ayni Council

Ayni's parameters — the rate card, revenue split, the AI-stakeholder percentage,
quality-multiplier formula, and which community initiatives get access to the
payout and attestation rails — are set by community process, in the open.

Specialist and adversarial review runs through the **Ayni Council**: ten
role-specific seats (security critic, red team, privacy, financial integrity,
node safety, renter abuse, reliability, human impact, AI stewardship, an
independent dissenter), filled by a deterministic qualification suite with hard
diversity rules — at least five providers, at most two seats each, at least two
open-weight seats. Reviews are risk-tiered (3 / 5 / 10 reviewers); model output
is untrusted and passes a strict structured schema; policy is **fail-closed**
(any missing vote or two CRITICAL objections blocks a security-critical change);
dissent is preserved and cannot be deleted. Decisions are published under
`ayni-council-public-record-v1`, a hash-linked schema with no field capable of
carrying prompts, chain-of-thought, exploits, credentials, or customer content.

The Council is an **intelligence and governance layer, never an enforcement
path** — it cannot move money, deploy code, alter balances, or command nodes.
AI advisors reason, challenge, and recommend; deterministic policy, cryptography,
sandboxes, and transactional ledgers enforce. Reference implementation and
Constitution: `security/`. Public observatory: `/council` and
`GET /v1/council/*` (labelled demonstration data until a signed roster and live
providers exist).

---

## 9. Status and roadmap

**Live:** coordinator with TLS and Postgres; signed model registry; macOS/Linux
provider daemon; Android provider app with real `llama.cpp` (CPU) and StrongBox
`device_attested` attestation; end-to-end encrypted streaming inference across all
of the above; an operator console; a shadow earnings ledger.

**Next:** Android GPU backends (Vulkan/OpenCL); Play Integrity; Linux/Windows TPM
and VBS attestation (Tier 1 on desktop); Stripe consumer credits and provider
payouts; confidential compute (Tier 2); multi-region coordinators; the governance
mechanism; external security audit; and legal review preceding general
availability.

**Research:** proof-of-inference (zkML) to make result integrity cryptographic
rather than redundancy-based (§4.3); the `cryptographic` trust tier (FHE / MPC,
§4.2) as the long-term endpoint of the trust ladder.

---

## Appendix: reproduce it

```bash
git clone https://github.com/mcastroarroyo/shared-compute
cd shared-compute
make dev     # coordinator + one local provider
make e2e     # encrypted round-trip: streaming + blocking, cancellation,
             # tamper rejection, and a log scan for zero prompt plaintext
```

This document is a draft and will change as the system and the community evolve.
Corrections and challenges welcome as issues or pull requests.
