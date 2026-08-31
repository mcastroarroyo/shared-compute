# Ayni Security v0.1 — threat model delta

This document extends `docs/THREAT-MODEL.md`. It records the adversarial marketplace risks
that become material once strangers can buy compute and providers can earn money.

## Catastrophic outcomes to prevent

1. A compromised coordinator turns provider devices into a botnet.
2. A malicious renter executes arbitrary code or obtains arbitrary network egress.
3. A malicious provider creates earnings without verified compute.
4. A replay, race, or webhook retry creates credits or earnings twice.
5. A malicious model or parser input compromises a provider device.
6. A Council prompt injection disables or bypasses deterministic controls.

## Current repository findings at branch creation

| ID | Severity | Finding | Required control |
|---|---|---|---|
| AYNI-001 | Critical → **partly addressed** | Job requests are encrypted but did not carry a signed Workload Manifest v1. **Done:** `internal/manifest` signs a per-job manifest (isolated Ed25519 key, `SC_MANIFEST_SIGNING_KEY`, published at `GET /v1/manifest-key`); the Rust node (`sc_manifest::workload`) verifies signature, device binding, fixed vocabulary, expiry, and immutable resource ceilings before running, and `SC_REQUIRE_MANIFEST` rejects unsigned jobs. A Go↔Rust known-answer test guards the canonical form. **Remaining:** registry-hash allowlists, persisted nonce store, KMS/HSM signer custody. See `docs/WORKLOAD-MANIFEST.md`. |
| AYNI-002 | Critical | `provider_earnings` has no `job_id` uniqueness constraint, so the database does not enforce one payable event per verified job/device. | Persist `job_id`; unique `(job_id, static_pk)`; idempotent insert under concurrency. |
| AYNI-003 | Critical | Provider job frames are routed by `job_id` alone; the in-flight manager does not bind delivery to the provider selected for that job. | Bind `(job_id, provider_id)` and reject frames from every other connection. |
| AYNI-004 | High | Provider-reported `job_done.usage` currently feeds metering and earnings despite documentation calling it advisory. | Coordinator-authored counts or cryptographically/verifiably derived receipts; reject inconsistent sequences and ceilings. |
| AYNI-005 | High | Android binding freshness uses a device-generated nonce rather than a coordinator challenge. | Challenge/response, one-time challenge storage, expiry, and Play Integrity binding. |
| AYNI-006 | High | Ordinary Android execution does not provide confidential-compute protection from a rooted device owner. | Market v1 for non-sensitive data; explicit tiers; future AVF/pKVM or other attested confidential execution. |

## Trust rule

The coordinator schedules work; the node independently decides whether the work is valid.
Node hard ceilings cannot be disabled remotely. Workload network policy is `NONE` in v1.

## Council input boundary

The Council receives structured facts, source hashes, test results, and redacted evidence.
Raw prompts, completions, secrets, and renter instructions are forbidden by the Council
schema. Model output is untrusted data and must pass structured validation before policy
evaluation.

## Release gates

- Critical or High harness failure: block merge and release.
- Two CRITICAL Council objections: block security-critical deployment.
- Missing required Council votes: fail closed.
- Council approval never overrides a failed deterministic control.
