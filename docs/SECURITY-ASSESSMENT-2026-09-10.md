# Security assessment, 10 September 2026

Scope: the coordinator, the Rust provider stack, the Android provider app, and the
supply chain under all three. Method: manual review, `semgrep`, `gosec`,
`cargo-audit`, `govulncheck`, live probing of production, and an independent pass by
a local open-weight model.

**This is not a penetration test.** No external party attacked the running system
and no one attempted exploitation. An external test remains an open item.

---

## Finding 1 (critical, live in production): unauthenticated Stripe demo

`internal/connectdemo` was mounted on the production coordinator with no
authentication of any kind. A grep for `withAdmin`, `AdminToken`, `Authorization`
and `SessionUser` across the package returned nothing.

Thirteen routes were exposed, including `POST /connect/accounts`,
`POST /connect/products` and `POST /connect/buy`, and they act against the **live**
Stripe platform keys. Anyone who found the path could create Connect accounts and
products on the real Stripe account.

Confirmed against production with GET requests only. No POST was sent, deliberately,
to avoid creating real Stripe objects:

```
/connect/            200
/connect/store       200
/connect/products    200
```

`gosec` independently flagged five taint findings in the same package (one XSS, four
open redirects), which is corroboration rather than a separate issue.

**Fix** (commit `272c6c8`): the demo is now off unless `SC_CONNECT_DEMO_ENABLED=1`,
and the coordinator logs why it was not mounted. Covered by
`TestConnectDemoOffUnlessExplicitlyEnabled`.

**Status: committed and pushed, NOT YET DEPLOYED.** The endpoint is still reachable
in production. Deploying needs a `gcloud auth login` that only the account owner can
perform.

---

## Finding 2 (high): nothing watched the bundled llama.cpp

`provider-core` links llama.cpp as C++ source vendored inside the `llama-cpp-sys-2`
crate, about 27 MB of it. Every scanner in CI missed it: `cargo-audit` covers RustSec
advisories for Rust crates, `govulncheck` covers Go, `npm audit` covers JavaScript,
`pip-audit` covers Python. Vendored C++ belongs to no package ecosystem, so it had no
watcher at all. It is the code that parses model files and prompt text on a
provider's phone.

NVD lists 38 llama.cpp CVEs. All 38 are now triaged in
`security/llama-cpp-cve-baseline.json`, each with the evidence behind the call.

| Disposition | Count | Why |
|---|---|---|
| not-compiled | 19 | The vulnerable source is not in our build |
| not-reachable | 19 | Compiled, but no call path reaches it |
| affected | 0 | |

**Not compiled.** Three whole components are absent from the vendored tree:

- **The RPC backend.** `ggml/src` contains no `ggml-rpc.cpp`, only the header. This
  clears CVE-2024-42477/42478/42479 and CVE-2026-34159, 78147, 78148, 86317.
- **`llama-server`.** The crate ships only `llama.cpp/tools/mtmd`. This clears
  CVE-2026-43628/43629/43630/43631/43632, 52132 and 21869.
- **The LLaMA-Android JNI wrapper.** Absent; the Android app reaches llama.cpp
  through the Rust `sc-inference` crate via UniFFI, not llama.cpp's own JNI example.
  This clears CVE-2026-43622, 70638, 70639 and 70640.

Every CRITICAL on the list falls into one of those three buckets.

**Not reachable.** The GGUF parser CVEs need a hostile model file, which the model
verification chain prevents (see below). The grammar and JSON-schema CVEs need code
`sc-inference` never calls; its entire llama.cpp surface is `LlamaBatch`,
`llama_backend`, `str_to_token`, `apply_chat_template` and `decode`.
CVE-2026-43627 needs a batch near `INT32_MAX`, and
`provider-core/sc-inference/src/llama.rs:183` rejects a prompt at `n_ctx` before the
batch is built, bounding capacity at the context window.

**Fix.** `scripts/check-llama-cpp-cves.py` diffs NVD against the baseline and fails
CI on any CVE nobody has triaged. It runs in `supply-chain.yml` on every push and
pull request, and weekly with `--strict` so an unreachable NVD fails the sweep rather
than passing quietly. Verified in both directions: green on the current baseline,
red when a CVE is removed from it.

This proves no llama.cpp advisory goes unread. It does not prove we are unaffected.

---

## Finding 3 (medium): the llama.cpp binding was a month stale

`llama-cpp-2` was pinned at 0.1.154 (published 5 August) while 0.1.156 (2 September)
was current. Bumped, which pulls about 160 changed upstream files including GGUF
loading and chat template parsing. Verified by a real build of the bundled C++,
which completed in 8m36s.

---

## Finding 4 (medium): the phone's trust anchors were ordinary settings

The Android app exposed **Manifest URL**, **Registry pubkey** and **Manifest signing
key** as plain editable text fields, with no warning and no distinction from
preferences like the battery floor.

Those three decide which model files the phone will load and whose work it will
accept. Pasting an attacker's values hands over both decisions at once, and the GGUF
parser behind them has a long CVE history. This is a social-engineering surface
("paste this to enable the new fast model"), not a remote one.

**Fix.** The fields are read-only behind a deliberate unlock toggle, carry an
explicit warning that nobody from Ayni will ask for a change, show a message when
they differ from the shipped defaults, and offer a one-tap restore.

---

## Finding 5 (low): self-reported benchmarks were unbounded

`gosec` flagged an unchecked `uint64 -> int64` conversion on `AvailableRAMMB`
(`internal/store/billing_pg.go:122`). That value is reported by the provider about
itself and was stored verbatim, so a crafted report could wrap it negative.

`capability.Sanitize` now bounds every self-reported field before it is scored or
stored, and storage records the same clamped values that scoring used.

**Bounds do not make a claim honest, and the code says so.** Measured ACU is about
1.0 for a mid-range phone, 10 for an M3 Max laptop, 64 for an H100 and 482 for an
8×H100. Any ceiling loose enough to admit a real GPU node is also loose enough to
admit a phone pretending to be one. Since ACU feeds the completion estimate in a
buyer's quote, the weighting of batch fan-out and the public capacity figure, a
dishonest node can still distort those. Fixing it properly needs throughput the
coordinator observes on completed jobs. **Open.**

---

## What held up

The model verification chain is the strongest thing reviewed, and it is what keeps
the llama.cpp GGUF CVEs off the phone:

1. `manifest.json` is verified against an Ed25519 signature using a key pinned in the
   client, not fetched alongside the manifest.
2. Each file streams to a `.part` while being hashed, and its SHA-256 and byte length
   are checked before the rename.
3. A mismatch deletes the `.part` and aborts, so a failed download can never be
   loaded.

Reaching the GGUF parser with hostile bytes therefore requires the registry signing
key itself. The Android app ships both that key and the coordinator signing key
compiled in, with `requireManifest` defaulting to true.

Other checks that came back clean:

- `cargo audit`: no advisories across 300 crates, after the 0.1.156 bump.
- `gosec`: 10 findings over 55 files; 5 in `connectdemo` (Finding 1), 1 previously
  triaged false positive on cookie flags, 3 safe integer conversions, 1 fixed here.
- `/v1/demo/last-job` returns `{"job":null}` in production: no prompt or completion
  content leaks through it.
- A sweep of every mounted GET route against production found no unauthenticated
  endpoint other than `/connect/*` and the intentionally public ones (health probes,
  the manifest key, the install script, the auth provider list).

## Open items

1. **Deploy `272c6c8`.** Finding 1 is live until this ships.
2. **Observed-throughput verification** for capability reports (Finding 5).
3. **An external penetration test.** Nothing in this document substitutes for one.
