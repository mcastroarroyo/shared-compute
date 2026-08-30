# Threat model (living document)

## Assets

| Asset | Where | Protection goal |
|---|---|---|
| Prompt + completion content | in transit; provider inference process | Confidentiality from the coordinator operator, the provider device owner, and network observers |
| Consumer API keys | coordinator DB | Confidentiality, integrity; hashed at rest |
| Provider identity keys | provider device | Non-exfiltration; hardware-bound at Tier 1+ |
| Model weights + manifest | registry (R2) | Integrity (signed manifest, SHA-256); availability |
| Usage / billing records | coordinator DB | Integrity; minimal PII |

## Actors

- **Consumer** — calls the API. May be malicious (abuse, jailbreak, resource exhaustion).
- **Coordinator operator** — us. Trusted for availability and billing; **not** trusted to
  see plaintext beyond the transient relay boundary. At M8 the coordinator runs in a CVM so
  even we cannot inspect relayed plaintext.
- **Provider device owner** — runs a provider. Assumed potentially adversarial: wants to
  read other people's prompts. Tier 0 does not defend against them; Tier 1 raises the bar
  (attested binary, hardware key); Tier 2 defends via TEE + confidential GPU.
- **Network attacker** — on-path. Defeated by TLS + `crypto_box`.
- **Malicious provider** — returns garbage, stalls, or forks responses. Mitigated by
  benchmarking, reputation, redundant sampling (M4+), and cancellation.

## Trust boundaries

```
consumer ──TLS──▶ [coordinator process] ──crypto_box per job──▶ [provider process] ──▶ llama.cpp
                        │                                              │
                 sees plaintext only                          plaintext exists only
                 transiently for relay                        in-process, never on disk
                 (CVM-isolated at M8)                         or in logs
```

## Key rules enforced in code / CI

1. **Fresh ephemeral keypair per job.** Coordinator generates it, seals the job to the
   provider's registered X25519 key, discards the private key after the job ends.
2. **No prompt/completion content** in logs, metrics, traces, error messages, or disk
   outside the provider inference call. CI: `scripts/check-no-prompt-logging.sh`.
3. **Manifest verification before serving.** Provider verifies the Ed25519 signature on
   `manifest.json` and every per-file SHA-256 before advertising a model.
4. **Auth on every request.** Consumer requests require a valid API key; provider
   connections require a registration token; both rate-limited.
5. **Tamper rejection.** Any `crypto_box` open failure aborts the job with a generic error.

## Out of scope (for now, tracked)

- Side-channel attacks on the provider GPU at Tier 0/1 (documented limitation).
- Supply-chain attacks on `llama.cpp` / crates / Go modules — mitigated by pinning, lockfiles,
  `cargo audit`, `govulncheck`, and (M9) SBOM + cosign-signed releases.
- Sybil providers gaming reputation — needs stake/identity design (M9).
