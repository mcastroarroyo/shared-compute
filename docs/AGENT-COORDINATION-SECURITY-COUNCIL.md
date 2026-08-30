# Agent Coordination: Security Harness and Ayni Council

This is the shared handoff contract between the marketplace/platform workstream and the
security/Council workstream. Read it before changing files touched by PR
[#1](https://github.com/mcastroarroyo/shared-compute/pull/1).

## Active workstream

| Item | Value |
|---|---|
| Security branch | `codex/security-harness-council-v01` |
| Pull request | [#1 — Ayni Security v0.1](https://github.com/mcastroarroyo/shared-compute/pull/1) |
| Security owner | This Codex session |
| Marketplace owner | Parallel cloud cowork agent |
| Merge authority | Human project owner |

The security branch is intentionally kept separate and the PR remains a draft. The
security session will not merge it without explicit authorization.

## Non-overlapping ownership

### Security/Council session owns

- `security/**`
- `.github/workflows/security.yml`
- Ayni Constitution and Council policy
- Council model qualification, independence, structured adapters, quorum, dissent, and
  audit records
- Offline adversarial simulations for manifests, malicious nodes, replay, and ledger
  invariants
- Security contract documentation

### Marketplace/platform agent owns

- marketplace, pricing, quotes, and capability discovery
- scheduler and batch orchestration
- renter and provider product flows
- console and website
- non-security product APIs and product documentation

The security session will not edit marketplace pricing, scheduler, batch, quote,
console, or site code. The marketplace agent should not edit `security/**` or weaken the
security workflow. If a security interface must change, coordinate through this document
or the PR rather than making parallel edits to the same file.

## Existing coordinator changes in PR #1

Before this ownership boundary was established, PR #1 added narrow production
hardening. The marketplace agent should coordinate before editing these exact files on
the security branch:

- `coordinator/internal/api/chat.go`
- `coordinator/internal/jobs/jobs.go`
- `coordinator/internal/jobs/jobs_test.go`
- `coordinator/internal/relay/relay.go`
- `coordinator/internal/store/billing_pg.go`
- `coordinator/internal/store/memory.go`
- `coordinator/internal/store/postgres.go`
- `coordinator/internal/store/postgres_test.go`
- `coordinator/internal/store/security_invariants_test.go`
- `coordinator/internal/store/store.go`
- `coordinator/internal/wshub/wshub.go`
- `coordinator/migrations/0006_security_invariants.sql`

Those changes bind a job to the selected provider, validate result stream identity and
sequence, serialize close/deliver paths, and enforce idempotent billing and earnings.
Going forward, this session will stay under `security/**` and documentation unless the
project owner explicitly authorizes another production integration change.

## Security contracts the platform must preserve

These are interface requirements, not optional implementation suggestions:

1. A renter requests an approved operation; a renter never supplies arbitrary code or
   node commands.
2. Production workloads use allowlisted runtime IDs, runtime hashes, model IDs, and
   model hashes. Arbitrary GGUF upload is outside v1.
3. Workload egress is `NONE` by default. Product flags cannot grant arbitrary Internet,
   localhost, link-local, metadata-service, or private-network access.
4. Every assignment is bound to `job_id`, `lease_id`, `nonce`, `expires_at`, and
   `device_pk`.
5. The node's immutable CPU, temperature, memory, storage, data, duration, battery, and
   charging ceilings cannot be raised by the coordinator, quote engine, renter, or
   Council.
6. One verified `(job_id, device_pk)` payable event may create provider earnings exactly
   once.
7. No unbilled or unverified computation creates earnings. Payouts cannot exceed accrued
   provider balance.
8. Raw customer prompts, completions, files, secrets, and model weights do not enter
   Council contexts or Council audit records.
9. Two independent `CRITICAL` Council objections block a security-critical change.
   Missing required votes fail closed.
10. The Council cannot deploy code, transfer money, alter ledger balances, sign arbitrary
    node commands, or override deterministic policy.

## Integration boundary

The marketplace may construct an unsigned workload proposal containing only structured
facts. It must not mint a trusted manifest itself.

```text
Marketplace proposal
  -> deterministic schema and policy validation
  -> isolated manifest signer
  -> signed Workload Manifest v1
  -> node signature, allowlist, and local-ceiling verification
  -> execution
  -> signed result receipt
  -> assignment and replay verification
  -> settlement
```

Expected proposal facts include runtime/model identifiers and hashes, operation,
resource request, input digest, and requested trust tier. Customer plaintext is passed
through the encrypted workload path, not the Council path.

The Council reviews architecture, policies, risky code changes, anomaly summaries, and
new workload classes. It should not synchronously approve every ordinary customer job.
Deterministic policy remains available and enforceable if model providers are unavailable.

## What exists on the security branch

- strict Ed25519 Workload Manifest v1 reference implementation
- immutable local node safety ceilings and runtime/model allowlists
- replay and settlement reference guards
- financial conservation reference ledger
- ten specialist Council seats with a two-seat-per-provider maximum
- risk-based 3/5/10 reviewer selection, protected dissent, and fail-closed policy
- hash-chained audit records without raw customer content
- strict provider-neutral structured-response adapter
- deterministic qualification suite with security, calibration, reliability,
  role-fitness, provider-diversity, and open-weight requirements
- signed result receipt verification bound to job, lease, device, workload, nonce,
  expiry, and output ceiling
- offline hostile-manifest and malicious-node harness
- security CI

## Current test commands

```bash
cd security
python -m pip install -r requirements.txt
python -m unittest discover -s tests -v
python -m ayni_security.harness
```

The harness is offline and sends no traffic to live Ayni services.

## Open integration work

These items are intentionally not implemented by this isolated workstream yet:

- integrating manifest signing into the Go coordinator
- enforcing manifest verification and immutable ceilings in Android/Rust node code
- hardware-backed Android Keystore identity and attestation challenge verification
- KMS/HSM custody, key rotation, and signer separation
- OS-level deny-by-default workload egress sandboxing
- production model-provider adapters, timeouts, budgets, and secret-manager wiring
- continuous Council qualification against current external and Ayni internal benchmarks
- production persistence for audit chains, replay state, and Council decisions

The marketplace agent can continue independently while treating the contracts above as
fixed. Any product feature that requires arbitrary code, arbitrary egress, custom model
weights, customer plaintext in the Council, or client-reported earnings needs an explicit
security design review before implementation.
