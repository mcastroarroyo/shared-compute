# Workload Manifest v1 (coordinator ↔ node)

Every job the coordinator dispatches can carry a **signed Workload Manifest v1** —
an Ed25519-signed authorization that binds one job to one device, runtime, model,
resource envelope, and expiry. The provider node verifies it against the
coordinator's published key and its own immutable ceilings before running
anything.

It is an **additive, optional** extension to protocol v0: absent unless the
coordinator is configured to sign, ignored unless the node is configured to
verify. Nothing breaks for an un-upgraded node.

## Flow

```
marketplace / chat / batch
  → relay builds a WorkloadManifest for the job
  → internal/manifest.Signer signs it (isolated Ed25519 key)
  → JobRequest.manifest = {manifest, signer_id, signature}
  → node: sc_manifest::workload::verify(...)
      · signature vs the coordinator's published key
      · device_pk == this node's static key
      · operation == "inference", network_policy == "NONE"
      · resource_limits within the node's immutable NodeSafetyPolicy
      · not expired, lease within the node ceiling
      · manifest job_id / model_id match the sealed request
  → run, or job_error("manifest-invalid")
```

Canonical bytes are compact, struct-field-order JSON with HTML escaping off, so
Go's `encoding/json` and Rust's `serde_json` produce identical input to
sign/verify. A cross-language known-answer test
(`manifest.TestWriteCrossLanguageFixture` → `workload::tests::cross_language_go_fixture`)
guards that contract.

## Config

Coordinator:

| Env | Meaning |
|---|---|
| `SC_MANIFEST_SIGNING_KEY` | base64 32-byte Ed25519 seed. Set → coordinator signs every job and serves `GET /v1/manifest-key`. Empty → disabled. |
| `SC_MANIFEST_SIGNER_ID` | signer identity in each manifest (default `signer-v1`). |

`GET /v1/manifest-key` → `{signer_id, public_key, algorithm, manifest_version}` (404 when signing is off).

Provider node (`provider-daemon` flags / env, and `MobileConfig` fields):

| Env | Meaning |
|---|---|
| `SC_MANIFEST_VERIFY_KEY` | comma-separated `signer-id:<base64>` (or bare `<base64>`) keys the node will accept. Empty → don't verify. |
| `SC_REQUIRE_MANIFEST` | `1` → reject any job that arrives without a valid signed manifest. |

## What v1 does NOT do yet

- `runtime_hash` / `model_hash` are deterministic derivations, not registry hashes
  — the node structurally validates them but does not yet cross-check against the
  signed model registry (that lands with the registry integration).
- Nonce single-use is not persisted across node restarts.
- The signer key is a plain env seed, not KMS/HSM custody with rotation.

See `security/threat-model/SECURITY_V0.1.md` (AYNI-001) and
`docs/AGENT-COORDINATION-SECURITY-COUNCIL.md`.
