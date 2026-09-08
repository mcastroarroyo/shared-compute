# Security policy

## Reporting a vulnerability

Pre-alpha: email the maintainer. A `security@` address and a published disclosure policy
land before any public release (M9). Do not open public issues for security reports.

## Design commitments

- **End-to-end encryption of job content.** The coordinator relays prompt/completion
  bytes only transiently in memory (in a Confidential VM from M8); providers hold the
  plaintext only inside the inference process. Neither writes it to logs, metrics, traces,
  or disk. CI enforces the logging rule (`scripts/check-no-prompt-logging.sh`).
- **Fresh key material per job.** One X25519 ephemeral keypair per inference request;
  the coordinator zeroizes the secret at job end.
- **Signed model artifacts.** Providers verify an Ed25519 signature over the manifest and
  every per-file SHA-256 before serving a model (M3).
- **Least privilege.** Providers dial out only; no inbound ports. Consumer API keys are
  hashed at rest (M2). Provider registration tokens are single-purpose.
- **Honest trust tiers.** A provider is only labelled `device_attested` / `confidential`
  when the coordinator has verified the corresponding hardware evidence. Tier 0 explicitly
  makes no owner-resistance claim.

## Supply chain

- Pinned `go.sum` / `Cargo.lock`.
- CI: `govulncheck`, `cargo audit`, Dependabot, secret scanning (added incrementally).
- Signed releases with cosign + SBOMs (M9).

## Android device attestation (M6, Tier 1)

The Android app creates an EC P-256 key in the Keystore (StrongBox when present, TEE
otherwise) with a Key Attestation challenge, and signs the provider's X25519 static
key with it (`"ayni-bind-v1" || static_pk || nonce || be64(issued_at)`). At `register`
the coordinator (`internal/attest`):

1. verifies the X.509 chain, terminating at a **pinned Google hardware-attestation
   root** (`coordinator/internal/attest/roots/`);
2. parses the key-attestation extension: requires `attestationSecurityLevel` ≥
   TrustedEnvironment, `verifiedBootState == Verified`, `deviceLocked == true`;
3. verifies the binding signature against the leaf key and checks the timestamp is
   within ±5 min.

Only then is the node labelled `device_attested`. Freshness rests on the binding
signature (per-session nonce + timestamp) over the already-TLS-protected channel, not
on a coordinator-issued challenge — a full challenge/response handshake is a planned
hardening step.

> **TODO before GA:** the embedded EC root (`Key Attestation CA1`) was captured from a
> known device; reconcile the pinned set against Google's official published roots and
> add automated freshness checks.

## Edge WAF and inference content (open, 2026-09-07)

Cloud Armor in front of `api.ayni-ai.com` runs the preconfigured `sqli`, `xss`, `lfi` and `rce`
rule sets against request bodies. Prompts are arbitrary text, and a security-log workload
contains path-traversal and SQL strings by nature, so those rules currently deny (403, HTML
body) exactly the content that use case sends. Reproduced on `POST /v1/workloads`: a prompt
with `../../etc/passwd` or `UNION SELECT` is refused; benign prompts pass. The coordinator
parses these bodies as typed JSON, builds no SQL or file paths from them, and relays the text
sealed to a device.

Proposed change, for the operator to apply: keep every rule as is except the four
content-inspecting sets, which stop evaluating on the three endpoints that carry inference
content. Scanner detection, protocol attack and the per-IP throttle still apply there, and the
public demo endpoint keeps the full WAF.

```bash
for pair in "2000:sqli-v33-stable" "2001:xss-v33-stable" "2002:lfi-v33-stable" "2003:rce-v33-stable"; do
  gcloud compute security-policies rules update "${pair%%:*}" --security-policy ayni-api-policy \
    --expression "evaluatePreconfiguredWaf('${pair##*:}', {'sensitivity': 1}) && !request.path.matches('^/v1/(workloads|batch|chat/completions)$')" \
    --description "WAF ${pair##*:}; inference-content endpoints excluded (prompts carry arbitrary text)"
done
```

Verify afterwards: a `POST /v1/workloads` whose prompt contains `../../etc/passwd` returns 201,
and `GET /healthz?q=../../etc/passwd` still returns 403. The Python client reports a non-JSON
403 as `edge_blocked` so runners can tell an edge refusal from a coordinator error.

## Hardening roadmap

TPM / measured boot (Linux, M7) · VBS enclave (Windows, M7) · Play Integrity + a
coordinator-issued attestation challenge (Android) · SEV-SNP / TDX + NVIDIA CC GPU
attestation (M8) · AVF/pKVM (Android, M8) · external pen test before GA (M9).
