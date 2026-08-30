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

## Hardening roadmap

TPM / measured boot (Linux, M7) · VBS enclave (Windows, M7) · StrongBox + Play Integrity
(Android, M6) · SEV-SNP / TDX + NVIDIA CC GPU attestation (M8) · AVF/pKVM (Android, M8) ·
external pen test before GA (M9).
