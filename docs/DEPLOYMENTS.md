# Live deployments

## Coordinator — M2

| | |
|---|---|
| Provider | Fly.io, org `personal`, region `iad` |
| App | `ayni-coordinator` |
| URL (raw) | https://ayni-coordinator.fly.dev |
| URL (custom) | https://api.ayni-ai.com — **pending**: CNAME `api` → `ayni-coordinator.fly.dev` (DNS-only) in Cloudflare, then `fly certs check api.ayni-ai.com` |
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
