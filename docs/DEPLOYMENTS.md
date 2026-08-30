# Live deployments

| Service | URL | Host |
|---|---|---|
| Coordinator API | https://api.ayni-ai.com | Fly.io `ayni-coordinator` + Fly Postgres |
| Model registry | https://models.ayni-ai.com | Cloudflare R2 bucket `models` |
| Operator console | https://ayni-console.pages.dev | Cloudflare Pages `ayni-console` (auto-deploy on push to `main`) |
| Source | github.com/mcastroarroyo/shared-compute (private) | GitHub Actions CI on every push |

## Console — M4

Cloudflare Pages project **`ayni-console`**, connected to the GitHub repo:
- Root directory `console`, build `npm run build`, output `out`
- Env: `NEXT_PUBLIC_COORDINATOR_URL = https://api.ayni-ai.com`
- Every push to `main` triggers a rebuild + deploy
- First visit: **Settings** → paste the admin token (`.env.local` → `SC_ADMIN_TOKEN`) and a
  consumer key; both are kept in that browser's `localStorage` only

## Coordinator — M2

| | |
|---|---|
| Provider | Fly.io, org `personal`, region `iad` |
| App | `ayni-coordinator` |
| URL | **https://api.ayni-ai.com** (Let's Encrypt cert, verified) — raw `https://ayni-coordinator.fly.dev` also works |
| DNS | Cloudflare `ayni-ai.com`: `CNAME api → yjj6lzr.ayni-coordinator.fly.dev`, DNS-only (grey cloud) |
| Database | Fly Postgres `ayni-coordinator-db`, attached as `SC_DATABASE_URL` |
| Secrets | `SC_CONSUMER_API_KEYS`, `SC_PROVIDER_TOKENS`, `SC_DATABASE_URL` (via `fly secrets`) |
| Credentials | local `.env.local` (gitignored) |
| Deploy | `SC_FLY_APP=ayni-coordinator ./infra/deploy-coordinator.sh` |
| Logs | `fly logs --app ayni-coordinator` |
| Metrics | `curl https://ayni-coordinator.fly.dev/metrics` (Prometheus) |

### Smoke test (verified 2026-08-30)

Mac provider (`./infra/run-provider.sh`, llama.cpp/Metal, Qwen2.5-0.5B) → registered over
`wss://` → streaming + non-streaming `/v1/chat/completions` returned real completions →
`401` without a key → usage metered (`sc_jobs_total{result="ok"} 2`, 47 tokens).

## Run this machine as a provider

```bash
source .env.local
SC_COORDINATOR_URL="wss://ayni-coordinator.fly.dev/ws/provider" \
SC_REGISTRATION_TOKEN="$SC_REGISTRATION_TOKEN" \
  ./infra/run-provider.sh
```

(Switch the URL to `wss://api.ayni-ai.com/ws/provider` once the cert is verified.)
