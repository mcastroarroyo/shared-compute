# Security Workstream Ownership

`security/**` belongs to the Security Harness and Ayni Council workstream.

Parallel agents may read and depend on its public contracts, but should coordinate before
editing them. This workstream does not own marketplace, pricing, scheduler, batch, quote,
console, or site implementation.

Shared coordination details and integration requirements are in
`docs/AGENT-COORDINATION-SECURITY-COUNCIL.md`.

## Public contracts

- `ayni_security.manifest`: signed, device-bound workload authorization
- `ayni_security.receipts`: signed, assignment-bound result verification
- `ayni_security.replay`: single-use nonce and settlement guards
- `ayni_security.ledger`: financial invariant reference model
- `ayni_security.council`: deterministic Council policy and audit decision
- `ayni_security.adapters`: strict provider-neutral model response boundary
- `ayni_security.qualification`: capability and independence-based roster selection

The Council is an intelligence and governance layer. It is never a substitute for
cryptographic verification, sandboxing, allowlists, attestation, resource ceilings,
database constraints, or human legal authority.
