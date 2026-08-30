# Crypto envelope — `v0`

## Primitives

| Purpose | Primitive |
|---|---|
| Job sealing (both directions) | NaCl `crypto_box` = X25519 key agreement + XSalsa20-Poly1305 AEAD |
| Nonce | 24 random bytes per sealed message, never reused under one keypair |
| Provider identity key | X25519 static keypair. Tier 0: on disk (0600). Tier 1+: hardware-bound (StrongBox / TPM / VBS) |
| Coordinator per-job key | X25519 **ephemeral** keypair, generated per job, secret discarded at job end |
| Manifest signing | Ed25519 (`model-registry` signing key; public key pinned in provider + coordinator) |
| API keys | random 32 bytes, shown once, stored as SHA-256 (M2) |
| Registration token | random 32 bytes issued out-of-band to a provider operator (M2) |

## Handshake

```
provider                                   coordinator
   |  WSS connect (TLS)                         |
   |------------------------------------------> |
   |  register { capabilities, static_pk,       |
   |             attestation? }                 |
   |------------------------------------------> |
   |                     verify attestation (tier)
   |                     persist provider + static_pk
   |  register_ack { provider_id, tier,         |
   |                 accepted_models, hb_sec }  |
   | <------------------------------------------ |
   |  heartbeat (every hb_sec) { telemetry }    |
   | <----------------------------------------> |
```

## Per-job flow

```
consumer --POST /v1/chat/completions--> coordinator
                                        pick provider P (scheduler)
                                        gen ephemeral (epk, esk)
                                        pt = job_request_plaintext
                                        n1 = random24
                                        ct1 = box(pt, n1, P.static_pk, esk)
        job_request { sealed: {epk, nonce:n1, ciphertext:ct1} } --> P
                                        P: pt = box_open(ct1, n1, epk, P.static_sk)
                                        P: run llama.cpp, for each delta:
                                           n = random24
                                           ct = box(chunk_pt, n, epk, P.static_sk)
        <-- job_chunk { sealed: {epk, nonce:n, ciphertext:ct} }
                                        coordinator: box_open(ct, n, P.static_pk, esk)
        <-- SSE data: {choices:[{delta:{content}}]}  (to consumer)
        <-- job_done { usage, finish_reason }
                                        coordinator: discard esk; write usage_event
```

## Invariants (enforced in code + CI)

1. `esk` never leaves the coordinator process and is zeroized after `job_done`/`job_error`.
2. A nonce is never reused for a given `(sender_sk, recipient_pk)` pair — always fresh
   random 24 bytes.
3. Prompt/completion plaintext is never logged, traced, put in metrics, or written to disk
   by either side. The only place it exists as plaintext on the provider is the argument to
   / return from the `llama.cpp` call.
4. `box_open` failure ⇒ abort job, emit `job_error {code:"decrypt"}`, no detail leaked.
5. Provider verifies the Ed25519 manifest signature and every file SHA-256 before it
   advertises a model in `register.capabilities.models` or `model_ready`.
6. Coordinator authenticates every consumer request (Bearer API key) and every provider
   socket (registration token) before doing any work.

## Key rotation

- Provider static key: rotate by re-`register`ing with a new `static_pk`; coordinator keeps
  the old key valid until in-flight jobs drain.
- Manifest signing key: publish new public key via a signed `keyset.json`; providers and
  coordinator ship with a pinned bootstrap key and accept a rotation signed by the current
  key.
