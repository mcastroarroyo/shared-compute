# Ayni Security v0.1

This directory contains two independent systems:

1. **Cyber Harness** — executable adversarial tests for workload policy, signatures,
   replay resistance, and financial conservation.
2. **Ayni Council** — a provider-neutral ten-seat governance engine that gathers
   structured model reviews and applies deterministic, fail-closed policy.

The Council is an intelligence layer, not a security boundary. It cannot execute
workloads, deploy code, change balances, initiate payouts, or command provider nodes.
Cryptography, local node ceilings, allowlists, attestation, database constraints, and
network sandboxing remain the enforceable controls.

## Run locally

```bash
cd security
python -m pip install -r requirements.txt
python -m unittest discover -s tests -v
python -m ayni_security.harness
```

The harness is offline by default and sends no traffic to live Ayni infrastructure.
GitHub dependency review should be enabled as an additional gate once Dependency Graph /
Advanced Security is available for the private repository.

## Security invariants encoded here

- No valid signed manifest means no compute.
- A manifest is bound to one job, lease, nonce, and device identity.
- Only allowlisted runtime and model hashes may execute.
- Workloads have no arbitrary network egress.
- A coordinator cannot raise immutable node resource ceilings.
- One `(job_id, device_pk)` can create provider earnings exactly once.
- Credits cannot appear from nowhere; earnings cannot exceed job gross; payouts cannot
  exceed accrued earnings.
- Two independent CRITICAL Council objections block a security-critical change.
- Missing Council votes fail closed.
- Raw customer prompts/completions never enter Council context or audit records.
- Council membership passes minimum security, calibration, and reliability scores, uses
  at least five providers, reserves open-weight representation, and limits each provider
  to two seats.
- A signed result receipt is bound to its job, lease, device, workload, nonce, expiry,
  result hash, and output ceiling before it can become a payable event.
- Public Council records use a narrower, hash-linked disclosure schema that excludes
  prompts, chain-of-thought, credentials, customer content, and exploit reproductions.

## Live model adapters

`StructuredModelReviewer` is intentionally provider-neutral. It enforces timeouts,
strict output validation, bounded responses, and structured facts before producing a
`MemberReview`.
Credentials belong in a secret manager. No API keys or permanent model names are stored
here. Council membership is a deployment configuration selected by capability,
independence, and Ayni's qualification suite—not rank alone.

See `OWNERSHIP.md`, `constitution/AYNI_CONSTITUTION.md`,
`threat-model/SECURITY_V0.1.md`, and
`../docs/AGENT-COORDINATION-SECURITY-COUNCIL.md`. The Council UX and public
disclosure contract is in `../docs/AYNI-COUNCIL-EXPERIENCE.md`.
