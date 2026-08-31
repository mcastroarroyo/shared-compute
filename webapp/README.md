# Ayni app — `app.ayni-ai.com`

The signed-in web app: OAuth sign-in, share your computer, request a quote,
accept + pay, watch your earnings. Static Next.js export, same shape as `site/`.

## Local

```bash
cd webapp
npm install
NEXT_PUBLIC_API_URL=http://localhost:8080 npm run dev   # -> http://localhost:3000
```

The app is a pure client of the coordinator API (`app/lib/api.ts`). Every call is
`credentials: "include"`, so the coordinator must send back a credentialed
`Access-Control-Allow-Origin` for the app's origin — set `SC_APP_URL` on the
coordinator to wherever the app is served (localhost is always allowed).

## Build

```bash
npm run build      # output: ./out  (output:"export", trailingSlash:true)
```

## Deploy — Cloudflare Pages project `ayni-app`

Connect the GitHub repo, then:

| Setting | Value |
|---|---|
| Project name | `ayni-app` |
| Production branch | `main` |
| Framework preset | Next.js (Static HTML Export) |
| Root directory | `webapp` |
| Build command | `npm run build` |
| Build output directory | `out` |
| Env var | `NEXT_PUBLIC_API_URL = https://api.ayni-ai.com` |

Then add a **custom domain** `app.ayni-ai.com` in the Pages project (Cloudflare
adds the CNAME automatically since `ayni-ai.com` is on the same account).

Every push to `main` rebuilds and redeploys. CI also builds `webapp/` on every
PR (the `site` job matrix in `.github/workflows/ci.yml`).

## Coordinator env this app depends on

| Env | Why |
|---|---|
| `SC_DATABASE_URL` | accounts live in Postgres; without it `/auth/*` and `/v1/me` 404 |
| `SC_GITHUB_CLIENT_ID` / `SC_GITHUB_CLIENT_SECRET` | GitHub sign-in |
| `SC_GOOGLE_CLIENT_ID` / `SC_GOOGLE_CLIENT_SECRET` | Google sign-in (optional) |
| `SC_AUTH_CALLBACK_BASE` | `https://api.ayni-ai.com` — OAuth redirect target |
| `SC_APP_URL` | `https://app.ayni-ai.com` — post-login redirect + CORS origin |
| `SC_COOKIE_DOMAIN` | `.ayni-ai.com` — shares the session cookie across `api.`/`app.` |
| `SC_STRIPE_SECRET_KEY` | "Add credit" checkout + charging on accept |
| `SC_BILLING_ENFORCE=1` | require a positive balance to run a workload |
| `SC_COUNCIL_MODEL_API` / `_KEY` / `_ID` | optional real model seat on the Council |

The GitHub OAuth app's **Authorization callback URL** must be exactly
`https://api.ayni-ai.com/auth/github/callback` (and the Google one
`.../auth/google/callback`).
