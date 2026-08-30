# Legal notes

**Not legal advice.** Engage counsel before any public or commercial launch.

## Clean-room posture

The reference project that inspired this work — Eigen Labs' Darkbloom
(`Layr-Labs/d-inference`) — is under a **proprietary license** that:

- restricts deployment/operation to the Darkbloom platform as operated by Eigen Labs;
- forbids ports and derivative works outside that platform without written permission;
- forbids uses that compete with Darkbloom in inference coordination or decentralized
  compute.

Permitted there without permission: academic/security research, privacy/security audits,
and contributions under their CLA.

### Rules for this repo

1. No contributor incorporates, copies, or paraphrases source from `d-inference` or any
   other proprietary private-inference platform.
2. We build only from generic, publicly documented primitives: WebSocket transport, NaCl /
   libsodium `crypto_box` (X25519 + XSalsa20-Poly1305), `llama.cpp` C API, GGUF, SHA-256
   file manifests, Ed25519 signatures, TPM 2.0 / Android Key Attestation + Play Integrity /
   Windows VBS enclaves / Android AVF remote attestation. These are industry standards, not
   proprietary to any one product.
3. Architectural *ideas* that are common to the field (an OpenAI-compatible API, a
   coordinator that routes and meters, providers that hold attested keys, per-request
   re-encryption, capability-based scheduling) are not owned by any vendor and may be
   implemented independently.

## Before launch — checklist for counsel

- [ ] Review the "compete with Darkbloom" clause against our intended go-to-market.
- [ ] Confirm no contributor with `d-inference` code exposure worked on the matching
      component (attestation, coordinator protocol).
- [ ] Product name / branding clearance (no association with Darkbloom or Eigen Labs).
- [ ] Terms of Service, Privacy Policy, Acceptable Use / abuse policy.
- [ ] Data Processing Addendum; GDPR/CCPA data-subject request flow (we hold account +
      usage metadata only — no prompt/completion content).
- [ ] Export-control review (cryptography distribution).
- [ ] Provider agreement (the contract with people running provider devices) + payout tax
      handling (1099 / W-8BEN etc.).
- [ ] Model license review for every model shipped in the registry.
