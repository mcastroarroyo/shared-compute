# CLAUDE.md — project guidance

## What this is

Clean-room decentralized private-inference network. Consumer → OpenAI-compatible API →
Go **coordinator** → end-to-end-encrypted job over WebSocket → Rust **provider** running
`llama.cpp` locally → encrypted token stream back.

Full roadmap: `docs/ROADMAP.md`. Plan of record was approved 2026-08-29.

## Hard rules

- **Clean-room.** Never read, copy, or adapt code from `Layr-Labs/d-inference` / Darkbloom
  or any other proprietary private-inference platform. Generic primitives only.
- **No prompt/completion content** in logs, metrics, traces, or on disk anywhere except
  inside the provider's inference process memory. `make lint` runs a grep check for this.
- **Protocol contract** lives in `protocol/`. It is versioned (`protocol/VERSION`). Any
  change updates the JSON Schemas plus the Go mirror (`coordinator/internal/protocol`) and
  the Rust mirror (`provider-core/sc-protocol`) in one change.
- **Crypto:** NaCl `crypto_box` (X25519 + XSalsa20-Poly1305). Fresh ephemeral keypair per
  job. Coordinator re-seals each job to the target provider's registered public key.

## Build / run

```bash
make dev     # docker compose: coordinator + postgres + minio (R2 stub)
make test    # go test ./... + cargo test
make e2e     # scripted encrypted round-trip
```

Toolchains: Go, Rust, CMake, Ninja, pkg-config, libsodium, OpenJDK, Gradle (Homebrew).
See `docs/DEVELOPMENT.md`.

## Accounts / deploys

Claude does not create accounts or enter credentials/payment info. The human operator does
that, guided step by step. Deploy targets: Fly.io (coordinator), Cloudflare R2 (models),
Cloudflare Pages or Vercel (console), Google Play (Android).

## Conventions

- Go: standard layout, `internal/` packages, `cmd/coordinator`. Errors wrapped with
  `fmt.Errorf("...: %w", err)`. Structured logging via `log/slog`.
- Rust: workspace crates prefixed `sc-`. `thiserror` for lib errors, `anyhow` in the
  daemon binary. `tokio` async runtime.
- Conventional Commits, scoped by component.
