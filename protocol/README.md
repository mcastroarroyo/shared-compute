# Protocol `v0`

The language-neutral contract between the **coordinator** and a **provider**. Both the Go
mirror (`coordinator/internal/protocol`) and the Rust mirror (`provider-core/sc-protocol`)
must match these schemas exactly. Changing anything here bumps `VERSION` and updates both
mirrors in the same change.

## Layers

| Layer | Transport | Encryption |
|---|---|---|
| Consumer → Coordinator | HTTPS (OpenAI-compatible REST, SSE for streaming) | TLS; optional outer NaCl seal to the coordinator's published key |
| Coordinator ↔ Provider | One WebSocket, provider dials out (WSS in prod) | TLS + **mandatory** `crypto_box` on job payloads, fresh ephemeral keypair per job |
| Provider inference | in-process `llama.cpp` | plaintext only in memory, never logged or written to disk |

## WebSocket framing

Every frame is a UTF-8 JSON object matching `schemas/message.json`:

```json
{ "v": "0", "type": "<message-type>", "id": "<uuid>", "ts": 1724970000, "...": "type-specific fields" }
```

- `v` — protocol version, currently `"0"`. Mismatched major version ⇒ connection closed
  with `register_ack.error = "protocol-version"`.
- `id` — unique per message; `heartbeat_ack` / `register_ack` echo the request `id` in
  `re`.
- Unknown `type` or unknown fields ⇒ ignored by receivers (forward-compatible), except
  during `register` where strictness is enforced.

## Message types

### provider → coordinator

| type | purpose | schema |
|---|---|---|
| `register` | advertise capabilities + public key + optional attestation | `schemas/register.json` |
| `heartbeat` | liveness + dynamic telemetry | `schemas/heartbeat.json` |
| `model_ready` | a model finished downloading and verified | `schemas/model_ready.json` |
| `job_chunk` | **sealed** streaming output chunk | `schemas/job_chunk.json` |
| `job_done` | job finished, with usage metadata | `schemas/job_done.json` |
| `job_error` | job failed | `schemas/job_error.json` |
| `benchmark_report` | prefill/decode tok/s per model+backend | `schemas/benchmark_report.json` |

### coordinator → provider

| type | purpose | schema |
|---|---|---|
| `register_ack` | assign `provider_id`, session nonce, accepted models, trust tier | `schemas/register_ack.json` |
| `heartbeat_ack` | echo + server time + optional directives | `schemas/heartbeat_ack.json` |
| `model_pull` | instruct provider to download+verify a model | `schemas/model_pull.json` |
| `job_request` | **sealed** inference request | `schemas/job_request.json` |
| `cancel` | cancel an in-flight job | `schemas/cancel.json` |

## Sealing

`schemas/sealed_payload.json`:

```json
{
  "alg": "crypto_box-x25519-xsalsa20poly1305",
  "epk": "<base64: coordinator ephemeral public key for this job>",
  "nonce": "<base64: 24 bytes>",
  "ciphertext": "<base64: NaCl box output>"
}
```

- **Coordinator → provider (`job_request`):**
  `box(plaintext, nonce, provider_static_pk, coord_ephemeral_sk)`.
  The provider opens with `box_open(ct, nonce, epk, provider_static_sk)`.
- **Provider → coordinator (`job_chunk`):**
  `box(plaintext, nonce, epk, provider_static_sk)`.
  The coordinator opens with `box_open(ct, nonce, provider_static_pk, coord_ephemeral_sk)`.
- The coordinator discards `coord_ephemeral_sk` when the job ends. One keypair per job.
- Any open failure ⇒ job aborts with a generic `job_error.code = "decrypt"`.

### Sealed plaintexts

`job_request` plaintext — `schemas/job_request_plaintext.json`:

```json
{
  "job_id": "…",
  "model": "llama-3.2-1b-instruct-q4_k_m",
  "messages": [{ "role": "system|user|assistant", "content": "…" }],
  "params": { "max_tokens": 512, "temperature": 0.7, "top_p": 0.95,
              "top_k": 40, "repeat_penalty": 1.1, "stop": [], "seed": null },
  "stream": true
}
```

`job_chunk` plaintext — `schemas/job_chunk_plaintext.json`:

```json
{ "job_id": "…", "seq": 0, "delta": "…token text…", "done": false }
```

## Consumer-facing REST (coordinator)

OpenAI-compatible subset. Full OpenAPI added at M4.

| Method | Path | Notes |
|---|---|---|
| `POST` | `/v1/chat/completions` | `stream: true` ⇒ `text/event-stream`; supports `X-Provider-Trust-Level` |
| `GET` | `/v1/models` | from the signed registry manifest |
| `GET` | `/healthz` | liveness |
| `GET` | `/metrics` | Prometheus (added M2) |

Auth: `Authorization: Bearer <api-key>`.
