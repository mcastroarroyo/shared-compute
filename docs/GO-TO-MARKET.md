# Ayni — go-to-market readiness

Status of the push to a hardened, market-ready state. Updated 2026-09-05.

## GCP production (new) — `ayni1-507216`, us-east1

Built and verified 2026-09-05; runbook in `infra/gcp/README.md`.

| Piece | State |
|---|---|
| Cloud Run `ayni-coordinator` | deployed at `main`; min 1 instance, session affinity, 3600 s WS timeout, private VPC egress, Cloud SQL attached. **Verified with an authenticated call**: serves the KMS manifest key, the migrated Council ledger, roster; security headers present. |
| Cloud SQL PG17 `ayni-pg` | private IP only, PITR + 7 backups, deletion protection. **Data migrated from Fly** (all 21 tables, row counts match). |
| Secret Manager | one secret per `SC_*`; SA has `secretAccessor` only. `SC_DATABASE_URL`, consumer keys, provider tokens, admin token, GitHub client id populated. **Empty (user):** `SC_GITHUB_CLIENT_SECRET`, `SC_STRIPE_SECRET_KEY`, `SC_STRIPE_WEBHOOK_SECRET`, `SC_STRIPE_CONNECT_WEBHOOK_SECRET`, `SC_COUNCIL_MODEL_KEY`. |
| Cloud KMS `ayni/manifest-signer` | EC_SIGN_ED25519; coordinator signs Workload Manifests via `AsymmetricSign` (`SC_MANIFEST_KMS_KEY`). Public key `6QPfm1yfsgQJp2s5sRb+jqUPZC/fLXJJNnEwgWZb4vY=`, signer id `ayni-coordinator-kms-v1`. |
| Edge | global external HTTPS LB on `34.160.126.149`, Cloud Armor (300 req/min/IP → 429; SQLi/XSS/LFI/RCE/protocol/scanner WAF), TLS ≥1.2 MODERN policy, HTTP→HTTPS redirect. Certificate Manager cert for `api.ayni-ai.com` via DNS authorization (CNAME added in Cloudflare) — **ACTIVE**. Backend service `ayni-api-backend-sl` (serverless NEG attached); WAF policy attach pending (see below). |
| Audit | Data Access audit logs on Secret Manager, KMS, Cloud Run, IAM. |
| Monitoring | email channel → uptime check on `/health` + "API down" and "5xx rate" alert policies. |
| CI | new `supply-chain.yml`: govulncheck, cargo-audit, npm audit (shipped deps), gitleaks, pip-audit; weekly. |

### Blocked on you (in order)
1. ~~Org policy~~ **Done (2026-09-05).** The project-level override of `iam.allowedPolicyMemberDomains` is `allowAll`, `allUsers` has `roles/run.invoker`, and the Cloud Run URL answers publicly (`/health`, `/v1/manifest-key`). The LB backend service was recreated as `ayni-api-backend-sl` (the original had `portName=https`, which serverless NEGs reject, so the edge returned `no healthy upstream`). One command is left for you because the auto-mode classifier refuses it — it attaches the Cloud Armor WAF/rate-limit policy to the new backend:
   ```bash
   gcloud compute backend-services update ayni-api-backend-sl --global --security-policy=ayni-api-policy --project=ayni1-507216
   ```
   (Then optionally delete the unused `ayni-api-backend`.)
2. ~~Secrets~~ **Done (2026-09-05).** Live values added by the owner through the Secret Manager console: GitHub client secret, Stripe live secret key, and the signing secrets of two new Stripe event destinations (`ayni-billing` → `/billing/webhook`, snapshot events `checkout.session.completed` + `checkout.session.async_payment_succeeded`; `ayni-payouts` → `/payouts/webhook`, thin events `v2.core.account[requirements].updated` + `v2.core.account[configuration.recipient].capability_status_updated`). Verified from GCP: live Checkout session created (200), Connect module mounted, both webhook routes reject bad signatures (400), GitHub OAuth redirect works. `deploy.sh` now pins each secret to its newest *enabled* version (Cloud Run's `latest` alias resolves to a disabled version and fails the revision).
3. ~~Cutover~~ **Done (2026-09-05).** `api.ayni-ai.com` is an `A` record → `34.160.126.149` (DNS only) in Cloudflare; resolvers return the GCP IP. Fly stays up as a fallback until 24 h clean, then scale to zero. Providers on `SC_REQUIRE_MANIFEST=1` must switch `SC_MANIFEST_VERIFY_KEY=ayni-coordinator-kms-v1:6QPfm1yfsgQJp2s5sRb+jqUPZC/fLXJJNnEwgWZb4vY=`.
   **Post-cutover verification (2026-09-05):** resolvers return the GCP IP; edge serves the Google Trust Services cert with `via: 1.1 google`; `/v1/manifest-key` returns the KMS key; all `/v1/council/*` routes 200. The Mac provider reconnected on its own but carried the old Fly verify key, so it was restarted with `SC_MANIFEST_VERIFY_KEY=ayni-coordinator-kms-v1:…` (`infra/supervise-provider.sh`); a chat completion through `api.ayni-ai.com` then succeeded end to end under `SC_REQUIRE_MANIFEST=1`, i.e. the KMS-signed Workload Manifest verified on the node. **Any other provider on REQUIRE_MANIFEST must be restarted with the new key** (the Pixel app when it comes back online).
4. GitHub OAuth end-to-end: GitHub disables its **Authorize** button for automated clicks — open `app.ayni-ai.com`, Continue with GitHub, click Authorize yourself.

### QA this round
- GCP public Cloud Run URL, full `scripts/qa-prod.sh`: **31/35** (re-run after Stripe went live: still 31/35, `billing/checkout -> 200`). The 4 misses are expected for the new stack: 2× `device_attested` (no phone connected to GCP yet) and 2× `insufficient_credit` (the GCP consumer key has a zero balance under `SC_BILLING_ENFORCE=1`; it gets funded through the first live Stripe checkout once the Stripe secrets land).
- Local: Go (race) / Rust / Python harness (23/23) / all four web builds / schema + prompt-logging guards — **all green** after fixing the one failure found: the Android attestation fixture had aged out (Google RKP intermediates live ~2 weeks) — tests now verify at a pinned time; production keeps wall-clock.
- Live prod (Fly): `scripts/qa-prod.sh` **33/35**; the 2 misses are the `device_attested` cases (the Pixel was offline; only the Mac provider was connected).
- Demo (`demo.ayni-ai.com`): sample → $0.01 quote → 3-seat Council APPROVE → summary returned by a Mac in 1.7 s, content-free record.
- App (`app.ayni-ai.com`): sign-in page renders, GitHub OAuth redirects correctly to `api.ayni-ai.com/auth/github/callback` (final Authorize click is human-only).
- Cloud Run: KMS sign+verify against the real key; deployment serves KMS key / Council ledger / roster with correct security headers.

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

### Android — Play Store launch (in progress, 2026-09-05)
- **Build:** `android-app` v0.2.0 (versionCode 2), signed release AAB with the `upload.jks` upload key (CN=Ayni Provider Upload, O=Zalesgen LLC). Pins the production KMS manifest key and requires signed manifests (fail-closed, like the desktop daemon). Lint enforced; the uniffi `Cleaner` NewApi warning is a static false positive (runtime reflection fallback to the JNA cleaner below API 33) and is scoped out in `app/lint.xml`, so **minSdk stays 26 — decided**.
- **Attestation expiry (found on the real Pixel, fixed):** the phone came back as `community` with "android_key: intermediate cert[1] outside validity window". Android key-attestation chains are fixed at key-generation time and the RKP intermediate is short-lived, so a device that generated its key weeks ago silently drops to Tier 0. `HardwareIdentity.ensureKey` now regenerates the hardware key when any cert in the chain is within 24 h of expiry. Same root cause as the coordinator test-fixture failure earlier today.
- **Site prerequisites:** `/privacy/` and `/terms/` pages live (footer-linked). Lawyer review still pending.
- **Assets (scratchpad `play-assets/`):** `icon-512.png`, `feature-1024x500.png`, `listing.md` (name, short/full description, data-safety, content-rating, target-audience, financial-features answers, release notes), the AAB. Phone screenshots still to capture from the Pixel.
- **Play Console blockers (owner):** developer account shows "Finish setting up" → verify organisation website (Search Console DNS TXT `google-site-verification=…` on `ayni-ai.com`, record value in Search Console; the Cloudflare TXT add and Search Console "Verify" are the owner's clicks) and verify phone numbers. Then: Create app → store listing → data safety → content rating → internal testing release with the AAB.

### Android — minSdk decision (resolved above; history)
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
