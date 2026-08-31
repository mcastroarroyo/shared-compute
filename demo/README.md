# Ayni live demo — `demo.ayni-ai.com`

A one-link, no-sign-up page for investors and prospective providers. It runs the
full loop against the live coordinator for a single fixed workload (summarize a
pasted document with the 0.5B model): **quote → Council review → encrypted
execution on a real provider → the content-free record**. Nobody is billed.

Static Next.js export, same shape as `site/` and `webapp/`.

## Local

```bash
cd demo
npm install
NEXT_PUBLIC_API_URL=http://localhost:8080 npm run dev   # -> http://localhost:3000
```

## Build

```bash
npm run build      # output: ./out
```

## Deploy — Cloudflare Pages project `ayni-demo`

| Setting | Value |
|---|---|
| Project name | `ayni-demo` |
| Production branch | `main` |
| Framework preset | Next.js (Static HTML Export) |
| Root directory | `demo` |
| Build command | `npm run build` |
| Build output directory | `out` |
| `NEXT_PUBLIC_API_URL` | `https://api.ayni-ai.com` |
| `NEXT_PUBLIC_CALENDAR_URL` | your Cal.com / Calendly link (optional; falls back to `mailto:hello@ayni-ai.com`) |

Add the custom domain `demo.ayni-ai.com` in the Pages project. CI builds `demo/`
on every PR via the `site` job matrix in `.github/workflows/ci.yml`.

## Coordinator side

The page calls two **public, unauthenticated** endpoints, both gated by
`SC_DEMO_ENABLED=1` on the coordinator (404 otherwise):

| Endpoint | What it does |
|---|---|
| `POST /v1/demo/summarize` `{text}` | estimate + price, `council.ForWorkload` + `Evaluate`, then `batch.Run` on eligible providers. Meters the job to key id `demo` (never debited). Rate-limited 3/min per IP, text capped at 6000 chars. Returns quote + council (with the exact `facts`) + result + a content-free `record`. If no provider is online it returns `result: null` and the last real run as `fallback`. |
| `GET /v1/demo/last-job` | the content-free record of the most recent successful demo run (in-memory; resets on deploy). |

```bash
fly secrets set --app ayni-coordinator SC_DEMO_ENABLED=1
```

## Demo capacity (keep the "Run" step live)

Every click needs a provider online for `qwen2.5-0.5b-instruct-q4_k_m`. Keep 1–2
always-on nodes — see [`../infra/demo-provider/`](../infra/demo-provider/). Your
Mac or Pixel joining ad hoc also works; when nothing is connected the page
degrades to the recorded last run (clearly labelled, never faked).
