# console

Next.js operator console for the shared-compute network. **Fully static** (`output:
"export"`) — every call goes from the browser straight to the coordinator.

## Pages

| Route | What | Auth |
|---|---|---|
| `/` | coordinator health, connected providers, model catalog, 24h usage | admin token |
| `/keys` | create / disable consumer API keys | admin token |
| `/playground` | streaming chat against `/v1/chat/completions` | consumer key |
| `/settings` | coordinator URL + tokens (stored in this browser only) | — |

Single-operator for now: the admin token (`SC_ADMIN_TOKEN` on the coordinator) is entered
once in Settings and kept in `localStorage`. Real multi-user auth (Clerk / Auth.js) is M9.

## Develop

```bash
cd console
npm install
NEXT_PUBLIC_COORDINATOR_URL=https://api.ayni-ai.com npm run dev
```

## Build (static)

```bash
npm run build          # -> console/out/
```

## Deploy — Cloudflare Pages

- Cloudflare dash → **Workers & Pages → Create → Pages → Connect to Git**
- Project root: `console`
- Build command: `npm run build`
- Build output dir: `out`
- Env var: `NEXT_PUBLIC_COORDINATOR_URL = https://api.ayni-ai.com`
- (optional) custom domain `console.ayni-ai.com`

Or Vercel: import the repo, root `console`, framework Next.js, same env var.
