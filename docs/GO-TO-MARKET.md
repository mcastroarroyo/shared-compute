# Ayni — go-to-market readiness

Status of the push to a hardened, market-ready state. Updated 2026-09-02 (evening).

## Done — shipped & verified in production

### Web surfaces (Cloudflare Pages, auto-deploy on push to `main`)
| URL | Project | Root | Status |
|---|---|---|---|
| `ayni-ai.com` | `ayni` | `site` | live |
| **`app.ayni-ai.com`** | `ayni-app` | `webapp` | **live** — built from `main`, `NEXT_PUBLIC_API_URL=https://api.ayni-ai.com` baked in |
| **`demo.ayni-ai.com`** | `ayni-demo` | `demo` | **live** |
| `ayni-console.pages.dev` | `ayni-console` | `console` | live |

### Coordinator (`api.ayni-ai.com`, Fly `ayni-coordinator`)
- Deployed at `main` (installer fix, `/v1/demo/*`, Council-in-the-loop, manifest signing,
  request-body ceiling + prompt-size guard).
- `SC_GITHUB_CLIENT_ID` set to the real value (`Ov23liLR5kTzRtS6gS1y`). Only the
  **client secret** remains — see user list.
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
  confirmed in enforcing mode. `release-provider.yml` now also builds
  **`provider-daemon-windows-x64.exe`** (MSVC + CMake + Ninja, same `--features llama`
  build as macOS/Linux) — first run in progress; there's no one-line Windows installer
  yet (`install/provider.sh` is bash/curl), so this is "a real binary exists," not
  "click-to-join" parity with Mac/Linux.
- **Phone (Pixel)**: registers as `device_attested`, serves real llama.cpp inference,
  ~14 tok/s, counters + earnings estimate update live. Now has real CI coverage
  (`.github/workflows/android.yml`, mock-backend build + unit tests — the llama.cpp
  cross-compile stays a manual/hardware step) so a Kotlin/manifest/bindings break
  doesn't ship silently.
- **Reconnect**: the desktop daemon reconnects with capped backoff instead of exiting on
  a drop (was: every coordinator deploy silently removed all providers). The Android
  app's own reconnect only covered a dropped WebSocket, not the OS killing the whole
  process — added a `BootReceiver` (resumes sharing after a reboot if it was on) and a
  battery-optimization-exemption prompt while sharing is active.

### Android — known gap worth a deliberate decision
`app/build.gradle.kts` sets `minSdk = 26` (Android 8.0+), but the uniffi-generated
Kotlin bindings (`uniffi/sc_mobile/sc_mobile.kt`, from uniffi 0.29) use
`java.lang.ref.Cleaner`, which needs **API 33**. `lintDebug` catches this (3 `NewApi`
errors) — it's excluded from the new CI job for now rather than silenced, because the
right fix is a choice: raise `minSdk` (cuts off real API 26–32 devices — a lot of the
phones this project is pitched at), or find/patch a uniffi version whose generated
Android bindings don't need `Cleaner` below 33. Not yet hit on the one physical device
tested (a current Pixel), but it's a real crash risk on older hardware.

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

### Council — a second real opinion, not just a second vote
`security_critic` and `node_safety` used to run the identical structural-bounds
check under different names — they always agreed, so the "second seat" added
no real coverage. Now 3 genuinely distinct deterministic reviewers run every
NORMAL-risk workload (the policy's target seat count):
- `security_critic` — structural bounds (item/redundancy/token sanity).
- `node_safety` — device-trust proportionality (large batches on an unattested
  tier with no redundancy; over-redundant spot workloads).
- `cost_governance` (new) — economic sanity (non-positive price blocks; a quote
  priced far under its token volume flags for operator review — catches a
  pricing-path bug, not an attack, before it reaches a provider).

### Council ledger now survives a redeploy
Was in-process only — every deploy silently reset `/v1/council/decisions` and
`/v1/council/run-reviews` to just the seeded demo rows. Now persisted to
Postgres (own small pool, JSONB rows, `internal/api/council_persist.go`),
hydrated at boot before serving traffic so the hash chain links onto the real
tail. **Verified live**: recorded a decision + run review, restarted the
machine, both were still there (`live_count` and `run_reviews.count` unchanged,
`chain_valid: true`).

### Load check (light, against the live coordinator)
- 100 requests to public `/v1/quote`, 20 concurrent → 6×200 then 94×429
  (6/min-per-IP limiter holds), **zero 500s**, p95 ≈ 370 ms.
- 60 concurrent reads across `/healthz` + `/v1/council/roster` → 60×200,
  p95 ≈ 377 ms, no errors.
- 30 concurrent `/v1/chat/completions` against a ~$0.04 test-credit balance →
  all completed cleanly, no 500s; a genuine load/soak test at production
  volume is still open (see deferred list).

## User-gated — the short list (everything else is done)

Each is an account/credential action Claude cannot perform. Do these and the
remaining surfaces light up.

1. **GitHub OAuth app — the client secret only** (app + client ID + callback URL are all set)
   - github.com → Settings → Developer settings → OAuth Apps → **Ayni** → *Generate a new client secret*
   - Copy it, then:
     `fly secrets set --app ayni-coordinator SC_GITHUB_CLIENT_SECRET=<the-secret>`
     then `fly secrets deploy --app ayni-coordinator`
   - Test: open `https://app.ayni-ai.com` → Continue with GitHub → should land on `/dashboard/`.
   - Optional: Google OAuth (new app, callback `https://api.ayni-ai.com/auth/google/callback`),
     `SC_GOOGLE_CLIENT_ID` / `SC_GOOGLE_CLIENT_SECRET`.

2. **Stripe LIVE mode**
   - `fly secrets set --app ayni-coordinator SC_STRIPE_SECRET_KEY=sk_live_… SC_STRIPE_WEBHOOK_SECRET=whsec_… SC_STRIPE_CONNECT_WEBHOOK_SECRET=whsec_…`
   - Stripe Dashboard → Webhooks: `https://api.ayni-ai.com/billing/webhook` (`checkout.session.completed`)
     and `https://api.ayni-ai.com/payouts/webhook` (Connect thin events)
   - Provider payouts stay operator-approved: `POST /admin/payouts/run?commit=1` (you run it)

3. **Council model reviewer** (optional, recommended)
   - `fly secrets set --app ayni-coordinator SC_COUNCIL_MODEL_API=anthropic SC_COUNCIL_MODEL_KEY=sk-ant-… SC_COUNCIL_MODEL_ID=claude-haiku-4-5-20251001`
   - Without it the 3 deterministic reviewers run and the loop is fully functional.

4. **Deploy secrets** after any `fly secrets set`: `fly secrets deploy --app ayni-coordinator`

5. **Keep a demo provider online** for `demo.ayni-ai.com` — [`infra/demo-provider/`](../infra/demo-provider/)
   on a small always-on box (or your Mac/Pixel while showing it).

## Explicitly deferred (not blockers to a soft launch, real blockers to scale)

- **External third-party pen test + security audit** before broad GA.
- **Legal**: ToS, privacy policy, DPA, and review of the Darkbloom non-compete clause.
- **Load/soak testing at production volume** (a light concurrency check is done — see above);
  multi-region coordinator + Postgres HA + Redis for scheduler/rate-limit state.
- **SOC 2** readiness (only if selling to enterprises).
- **Windows provider**: binary now builds in CI (see above) — a one-line installer
  (PowerShell, not bash) and a real machine to smoke-test it on are still open.
  **Confidential (Tier 2)** compute.
- **Android minSdk vs uniffi `Cleaner`** (API 26 vs 33 — see above): needs a decision,
  not a silent patch to generated code.
- A real signed Council roster + live model providers for every seat (until then the
  `/v1/council/roster` observatory stays labelled "demonstration data" even though the
  3 reviewers actually gating money today are genuinely distinct and decisions persist).
  Worth considering: having `/v1/council/roster` show which of the 10 seats are real
  vs. placeholder, instead of an all-fictional cast — deliberately not done without you
  weighing in, since it changes what the public trust page claims.
- Provider payout self-serve onboarding UI (operator triggers `/admin/payouts/connect` today —
  deliberate, so a payout is never one click for anyone but you).
