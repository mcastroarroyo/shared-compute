# Related work and literature

Systems and references that Ayni builds on, borrows from, or is often compared with. Each entry
says what the system is, how it differs from Ayni, and what, if anything, Ayni takes from it.
Facts about third-party projects are as read on the date given; check the linked source before
relying on them.

## NVIDIA Personal AI Router (PAIR)

Source: https://github.com/NVIDIA/Personal-AI-Router · Apache 2.0 · read 2026-09-07.

**What it is.** A local inference router for a group of computers on one network. It discovers
participating machines, manages the inference engines installed on them (Ollama and LM Studio),
and presents Ollama-compatible (port 11434) and OpenAI-compatible (port 1234) proxy endpoints to
applications on each machine. Windows 11, Linux and macOS, x64 and arm64, shipped as signed
installers with a desktop app (Electron) and a terminal interface. The backend is thirteen Go
services behind a JSON-RPC broker: node scanner (mDNS, `_nvpair-node._tcp`), job scheduler,
cluster manager (certificate pinning), engine manager, error sync, and the two proxies.

**How it routes.** Every request is served whole by one node. PAIR does not pool GPU memory,
shard a model across machines, or split one request between nodes. A proxy filters the cluster to
nodes that advertise the requested model, orders them by the scheduler's ranking, tries the local
engine first, then peers over mutual TLS. A peer request is served by that peer's own engine, never
re-routed a second time. The ranking combines pending work per node with a smoothed GPU-utilisation
signal quantised to 0–3 units at 40/70/85% with hysteresis, plus a local reservation added at
dispatch time so a burst does not pile onto one idle node. The authors list the limits plainly:
the scheduler sees pressure, not capacity (no VRAM, GPU type, model warmness or request cost), and
the cluster view is eventually consistent.

**How it trusts.** Pairing is a six-digit PIN shown on one machine and typed on another; the
exchange pins each node's self-signed certificate and sets a shared cluster id. After that, cluster
surfaces (model inventory, workload replication, remote engine control, error sync) require mutual
TLS. Proxies serve plaintext HTTP to loopback and mTLS to peers on the same port by inspecting the
first byte of the connection; plaintext from a non-loopback address gets 403. Telemetry stays
plaintext and unauthenticated on purpose so anything on the subnet can observe utilisation.

**Where it sits next to Ayni.**

| | PAIR | Ayni |
|---|---|---|
| Scope | One LAN, machines you own or trust | Internet marketplace over devices other people own |
| Who runs the model | Ollama or LM Studio the user installed | llama.cpp embedded in the provider, models from a signed registry |
| Devices | Desktops and laptops with GPUs | Phones, laptops, desktops; ACU self-benchmarked |
| Trust | Peer trust bootstrapped by PIN, then mTLS; peers see plaintext | Job sealed to one device's key; coordinator relays ciphertext; attestation tiers |
| Scheduling signal | Pending work + GPU pressure | Measured tokens/s per node, tier, thermal and battery headroom, free RAM |
| Economics | None; all machines are yours | Quotes, prepaid credits, 70% to device owners |
| Client contract | Ollama and OpenAI shapes on loopback | OpenAI shape plus batch, workloads, async runs |
| Request granularity | One request to one node | One request to one node; workloads fan out per item |

The two agree on the thing that matters most for consumer hardware: do not shard, route whole
requests to interchangeable copies of a model. They disagree on the boundary. PAIR stops at the
router of the house or office and trusts everything inside it. Ayni starts where that trust ends.

**How PAIR fits into Ayni's architecture.**

1. *A PAIR cluster as one Ayni provider.* A cluster owner runs the Ayni provider daemon on any
   member and points its inference backend at PAIR's loopback OpenAI-compatible proxy instead of the
   embedded llama.cpp. The whole cluster then joins Ayni as a single node whose ACU benchmark
   measures the cluster through the proxy, and PAIR spreads Ayni's jobs across the machines. This
   is the fastest way for a small GPU fleet that already exists to become supply, including the
   7B-class supply security workloads need. It needs one new provider-core backend (remote
   OpenAI-compatible engine) and a manifest entry that maps a registry model to the Ollama or LM
   Studio model name so the advertised model is the verified one. Because plaintext leaves the
   provider process for other machines on the LAN, such a node is `community` tier only; the
   per-job seal covers the cluster, not one device.
2. *Ayni as overflow behind PAIR.* A team that keeps sensitive interactive work on its PAIR
   cluster can send batch or overflow work to Ayni without changing request shape, since both speak
   the OpenAI contract. PAIR cannot route to Ayni as a peer today because its peers are mDNS-found
   and mTLS-pinned on the LAN; that would need either a bridge process that presents Ayni as a
   local node or a remote-node feature upstream. Until then the split happens in the client:
   local endpoint for chat, Ayni workload for the queue.
3. *Ideas adopted into the scheduler.* Hysteresis on load signals so ranks do not thrash;
   reservation at dispatch so concurrent fan-outs do not all pick the same idle node; tolerance of
   several missed heartbeats, with a liveness check, before evicting a phone on flaky Wi-Fi; and
   the rule that a forwarded job is served, never forwarded again. Ayni's relay is already single
   hop; the other three are tracked in the roadmap.
4. *Ideas for provider onboarding.* PAIR's engine manager detects and adopts an Ollama the user
   already has rather than installing a second one. An "adopt your existing Ollama" path would cut
   provider friction for people who already run models, at the cost of registry verification; it is
   a candidate, not a plan.

## Other systems

- **llama.cpp** — https://github.com/ggml-org/llama.cpp. The inference engine embedded in the Ayni
  provider core (Metal, CUDA, Vulkan, CPU). Ayni's hardware classes follow what llama.cpp can serve
  on each device.
- **Ollama** — https://github.com/ollama/ollama and **LM Studio** — https://lmstudio.ai. Desktop
  model runners with local HTTP APIs; the engines PAIR manages, and the shape of the "adopt an
  existing engine" idea above.
- **Petals** — https://github.com/bigscience-workshop/petals. Runs one large model sharded across
  volunteer machines over the internet. The opposite bet to PAIR and Ayni: it pools devices to
  serve a model none could hold alone, and pays for it in latency and in every shard seeing
  activations. Ayni serves whole models on whole devices for privacy and predictable ETA.
- **exo** — https://github.com/exo-explore/exo. Splits a model across the devices on a LAN
  (pipeline parallel) so consumer hardware can run bigger models locally. LAN-scoped like PAIR,
  sharded like Petals; no marketplace.
- **Token-settled compute networks** (Akash, io.net, Render and similar). Marketplaces that settle in
  a native token, mostly for GPU rental. Ayni settles in prepaid USD through Stripe and has no token;
  see the comparison guide for the full table.

## Standards and primitives

- OpenAI Chat Completions request and response shape, used as the de-facto client contract by Ayni,
  PAIR, Ollama and LM Studio.
- X25519 (RFC 7748) and NaCl `box` (libsodium, https://doc.libsodium.org) for the per-job seal.
- Ed25519 (RFC 8032) for the signed model manifest.
- Android hardware-backed key attestation,
  https://developer.android.com/privacy-and-security/security-key-attestation, the basis of the
  `device_attested` tier.
- Model Context Protocol, https://modelcontextprotocol.io, the planned agent-facing surface for
  capacity, estimate, run and results.
