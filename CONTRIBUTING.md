# Contributing

## Ground rules

1. **Clean-room.** Do not copy or paraphrase source code from any proprietary
   private-inference platform (e.g. Darkbloom / `Layr-Labs/d-inference`). Implement from
   public specs and generic primitives only. If you have studied such a codebase, do not
   contribute to the corresponding component.
2. **No prompt data in logs.** Prompt and completion content must never be written to
   logs, metrics, traces, or disk outside the provider inference process. CI enforces a
   grep-based check.
3. **Protocol is versioned.** Changes to `protocol/` require bumping `protocol/VERSION`
   and updating both the Go and Rust type mirrors in the same PR.

## Layout & ownership

| Area | Language | Key dirs |
|---|---|---|
| Coordinator | Go 1.23+ | `coordinator/` |
| Provider core | Rust (stable) | `provider-core/` |
| Android app | Kotlin | `android-app/` |
| Console | TypeScript / Next.js | `console/` |
| Model registry CLI | Rust | `model-registry/` |

## Local checks before pushing

```bash
make fmt      # gofmt + cargo fmt + prettier
make lint     # go vet + golangci-lint + cargo clippy
make test     # unit tests
make e2e      # end-to-end encrypted round-trip
```

## Commit messages

Conventional Commits (`feat:`, `fix:`, `chore:`, `docs:`, `refactor:`, `test:`).
Scope by component, e.g. `feat(coordinator): capability-based scheduler`.
