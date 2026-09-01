# Ayni — go-to-market readiness

Status of the push to a hardened, market-ready state. Updated 2026-09-01.

## Done — shipped & verified in production

### Coordinator (`api.ayni-ai.com`, Fly `ayni-coordinator`)
- Deployed at `main` (installer fix, `/v1/demo/*`, Council-in-the-loop, manifest signing).
- **Workload Manifest v1 signing ON** — `SC_MANIFEST_SIGNING_KEY` / `SC_MANIFEST_SIGNER_ID=ayni-coordinator-v1`.
  `GET /v1/manifest-key` serves the Ed25519 public key. Every dispatched job carries a
  signed manifest; verified end-to-end against a provider running
  `SC_REQUIRE_MANIFEST=1` (job still ran ⇒ sig + device-binding + expiry + ceilings all check).
- `SC_DEMO_ENABLED=1` — the no-sign-up investor demo endpoints are live.
- Prepaid billing enforced (`SC_BILLING_ENFORCE=1`), Stripe **test** keys wired.

### QA — 35/35 functional checks green (`scratchpad/qa.sh`)
chat (stream + non-stream), `/v1/models`, batch fan-out across 2 heterogeneous nodes,
public `/v1/quote` (on-demand + spot band), full `/v1/workloads` loop
(quote → Council APPROVE → accept → run → meter → settle → run_review CLEAN),
`device_attested` tier pinning, `/v1/demo/summarize` + `last-job`, all six
`/v1/council/*` endpoints (decision hash-chain valid), `/billing/balance`,
admin endpoints (200 with token, 401 without), and the negative set
(bad key 401, unknown model 404, oversized batch 413, missing fields 400).

### Providers
- **Laptop/desktop**: builds with `--features llama`, loads the model on Metal,
  registers as `community`, ~80 tok/s, serves real inference. Manifest verification
  confirmed in enforcing mode.
- **Phone (Pixel)**: registers as `device_attested`, serves real llama.cpp inference,
  ~14 tok/s, counters + earnings estimate update live.
- **Reconnect**: the daemon now reconnects with capped backoff instead of exiting on
  a drop (was: every coordinator deploy silently removed all providers).

### Security review / light pen test — findings fixed
| Check | Result |
|---|---|
| CORS | arbitrary origin gets `*` only, no credentials; app origin credentialed. OK |
| OAuth callback | missing/!match `state` ⇒ 400; redirect target is fixed config, no open redirect |
| Billing input | negative / zero / huge / non-numeric `amount_usd` ⇒ 400 |
| Rate limiting | public quote 6/min per IP enforced (6×200, 34×429) |
| Admin token | `subtle.ConstantTimeCompare` |
| Stripe webhooks | signature verified (`ConstructEventWithOptions` / `ParseEventNotification`) before any effect; no-op if secret unset |
| SQL | all parameterized; no string-built SQL in `store/` |
| Prompt logging | `check-no-prompt-logging.sh` passes; deployed logs carry no prompt/response text |
| `govulncheck` | 0 vulnerabilities affect the code |
| `npm audit` | only `sharp`/libvips — build-time, unused in static export (`images.unoptimized`), not shipped |
| **Body size** | **FIXED** — was 500 / unbounded; now `limitBody` 12 MiB ⇒ 413, plus 256 KiB/item `prompt_too_large` guard |
| **Provider resilience** | **FIXED** — reconnect loop (above) |

## User-gated — the short list (everything else is done)

Each is an account/credential action Claude cannot perform. Do these and the
remaining surfaces light up.

1. **GitHub OAuth app** → sign-in on `app.ayni-ai.com`
   - github.com → Settings → Developer settings → OAuth Apps → New
   - Homepage `https://ayni-ai.com`, callback **exactly** `https://api.ayni-ai.com/auth/github/callback`
   - `fly secrets set --app ayni-coordinator SC_GITHUB_CLIENT_ID=… SC_GITHUB_CLIENT_SECRET=…`
     (an earlier run used literal `...` placeholders — use the real values)
   - Optional: Google, same shape, callback `.../auth/google/callback`, `SC_GOOGLE_CLIENT_*`

2. **Cloudflare Pages — two projects** (connect the GitHub repo)
   | Project | Root dir | Output | Env | Domain |
   |---|---|---|---|---|
   | `ayni-app` | `webapp` | `out` | `NEXT_PUBLIC_API_URL=https://api.ayni-ai.com` | `app.ayni-ai.com` |
   | `ayni-demo` | `demo` | `out` | `NEXT_PUBLIC_API_URL=https://api.ayni-ai.com` (+ optional `NEXT_PUBLIC_CALENDAR_URL`) | `demo.ayni-ai.com` |
   Framework preset: Next.js (Static HTML Export). Auto-deploys on push after that.

3. **Stripe LIVE mode**
   - `fly secrets set --app ayni-coordinator SC_STRIPE_SECRET_KEY=sk_live_… SC_STRIPE_WEBHOOK_SECRET=whsec_… SC_STRIPE_CONNECT_WEBHOOK_SECRET=whsec_…`
   - Stripe Dashboard → Webhooks: `https://api.ayni-ai.com/billing/webhook` (`checkout.session.completed`)
     and `https://api.ayni-ai.com/payouts/webhook` (Connect thin events)
   - Provider payouts stay operator-approved: `POST /admin/payouts/run?commit=1` (you run it)

4. **Council model reviewer** (optional, recommended)
   - `fly secrets set --app ayni-coordinator SC_COUNCIL_MODEL_API=anthropic SC_COUNCIL_MODEL_KEY=sk-ant-… SC_COUNCIL_MODEL_ID=claude-haiku-4-5-20251001`
   - Without it the 2 deterministic reviewers run and the loop is fully functional.

5. **Deploy secret** after any `fly secrets set`: `fly secrets deploy --app ayni-coordinator`

## Explicitly deferred (not blockers to a soft launch, real blockers to scale)

- **External third-party pen test + security audit** before broad GA.
- **Legal**: ToS, privacy policy, DPA, and review of the Darkbloom non-compete clause.
- **Load testing** at scale; multi-region coordinator + Postgres HA + Redis for scheduler/rate-limit state.
- **SOC 2** readiness (only if selling to enterprises).
- **Windows provider** build + signed installer; **confidential (Tier 2)** compute.
- Persisted Council live records (currently in-memory, reset on deploy) and a real signed roster
  (until then the observatory stays labelled "demonstration data").
- Provider payout self-serve onboarding UI (operator triggers `/admin/payouts/connect` today).
