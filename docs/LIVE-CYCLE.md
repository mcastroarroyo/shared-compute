# First live client cycle — runbook

The end-to-end path a real customer takes today, on laptop/desktop compute
(Android stays a waitlist). Two people:

- **Provider** (test user 1) — shares a laptop, earns.
- **Client** (test user 2) — signs in, gets a quote, accepts, is charged, gets a summary.

Everything below the "operator" line is done once, by you, with real credentials.
Everything above it is the product working on its own.

---

## 0. Operator setup (once)

### 0.1 GitHub OAuth app
GitHub → Settings → Developer settings → **OAuth Apps** → New OAuth App.

| Field | Value |
|---|---|
| Application name | Ayni |
| Homepage URL | `https://ayni-ai.com` |
| Authorization callback URL | `https://api.ayni-ai.com/auth/github/callback` |

Copy the Client ID, generate a Client secret, then:

```bash
fly secrets set --app ayni-coordinator \
  SC_GITHUB_CLIENT_ID=<real-id> \
  SC_GITHUB_CLIENT_SECRET=<real-secret>
```

(Google is optional and identical: callback `https://api.ayni-ai.com/auth/google/callback`,
secrets `SC_GOOGLE_CLIENT_ID` / `SC_GOOGLE_CLIENT_SECRET`.)

Verify: `curl https://api.ayni-ai.com/auth/providers` → `{"providers":["github"]}`.

### 0.2 The app — Cloudflare Pages project `ayni-app`
See [`webapp/README.md`](../webapp/README.md). Root dir `webapp`, output `out`,
env `NEXT_PUBLIC_API_URL=https://api.ayni-ai.com`, custom domain `app.ayni-ai.com`.
No domain purchase — `app.` is a subdomain of `ayni-ai.com`, already on the account.

### 0.3 Billing (live mode)
```bash
fly secrets set --app ayni-coordinator \
  SC_STRIPE_SECRET_KEY=sk_live_... \
  SC_STRIPE_WEBHOOK_SECRET=whsec_... \
  SC_BILLING_ENFORCE=1 \
  SC_PUBLIC_BASE_URL=https://app.ayni-ai.com
```
Stripe Dashboard → Webhooks → add `https://api.ayni-ai.com/billing/webhook`
(events: `checkout.session.completed`) and
`https://api.ayni-ai.com/payouts/webhook` (Connect thin events).

Keep the first cycle cheap: `SC_PRICE_MULTIPLIER` can stay `1.0` — a 0.5B-model
summary quotes at the `$0.01` floor anyway.

### 0.4 Council model seat (optional but wanted)
```bash
fly secrets set --app ayni-coordinator \
  SC_COUNCIL_MODEL_API=anthropic \
  SC_COUNCIL_MODEL_KEY=sk-ant-... \
  SC_COUNCIL_MODEL_ID=claude-haiku-4-5-20251001
```
Without this, the two deterministic reviewers still run and the loop still works.

### 0.5 Provider binaries
The installer pulls from the `provider-latest` GitHub release. It's already
published (darwin-arm64 + linux-x64). To refresh: push a `provider-v*` tag or run
the `release-provider` workflow — it always updates `provider-latest`.

---

## 1. Provider: share a laptop  (test user 1)

1. Go to **app.ayni-ai.com** → "Continue with GitHub" → authorize.
   Lands on `/dashboard/`. A consumer key + a `sc_prov_…` provider token are
   minted server-side on first sign-in.
2. Open **Share your computer**. Copy the one-liner:
   ```bash
   curl -fsSL https://api.ayni-ai.com/install/provider.sh \
     | SC_REGISTRATION_TOKEN=sc_prov_xxx bash
   ```
3. Run it on the laptop. It downloads `provider-daemon`, fetches
   `qwen2.5-0.5b-instruct-q4_k_m.gguf`, and connects to
   `wss://api.ayni-ai.com/ws/provider`.
4. On connect, `wshub` accepts the per-account token and calls
   `LinkProviderDevice` — the device's static X25519 key is now owned by user 1.
5. Confirm: `/dashboard/` → **Earnings** shows `devices: 1`. Operator can also
   check `GET /admin/providers`.

> Payout account: user 1 can't be *paid* until a Stripe Connect recipient account
> exists for that device key. Today that's operator-driven — see step 5 below.
> Earnings accrue regardless (state `accrued`).

---

## 2. Client: quote → accept → charged  (test user 2)

1. **app.ayni-ai.com** → "Continue with GitHub" (a different account) → `/dashboard/`.
2. **Add $10** → Stripe Checkout (`POST /billing/checkout`) → back to the app with
   `credit_usd` updated.
3. **Run a workload** → paste a document → **Get a quote**.
   `POST /v1/workloads` with one chat item, `max_tokens: 320`. Server:
   - estimates tokens + supply, prices bottom-up
     (`compute + coordination 15% + failure 8%/redundancy + margin 30% + payment 3%`, floor `$0.01`),
   - builds a **content-free** Council proposal (`council.ForWorkload` — facts only:
     model class, tier, item count, token totals, redundancy, spot, price, ETA),
   - `council.Evaluate` runs the reviewers; a `BLOCK` returns `409 council_blocked`
     and no quote is created,
   - otherwise a quote is stored with its `council` outcome attached.
4. The card shows price, ETA, "Devices available", the Council decision + each
   reviewer's vote, and the audit hash.
5. **Accept & pay** → `POST /v1/workloads/{id}/accept`. Server re-checks the
   Council outcome is `Acceptable()`, checks credit (`SC_BILLING_ENFORCE`),
   marks the quote accepted (one-shot lock), then fans the item out via
   `batch.Run` → `relay.ExecuteOn` to the provider laptop over the encrypted hop.
6. Settlement: `recordJob` writes `usage_events` + one `provider_earnings` row per
   sub-job (`UNIQUE(job_id, static_pk)` — AYNI-002), then the consumer balance is
   debited **once at the quoted price** (not the metered sum). Nothing ran OK →
   charge is `0`.
7. Response carries `items[0].message.content` (the summary), `charged_usd`, and
   `run_review`.

---

## 3. Council reviews the run

`council.ReviewRun` runs on every accept with **stats only** (no prompt/output):
items, OK/failed, fanout, wall time, prompt/completion tokens, quoted vs charged.
It flags: any failures (Medium, High if >10%), no fan-out on a multi-item batch,
>4 s/item wall time, charged > quoted. Verdict `CLEAN` or `REVIEW`.

Read them back: `GET /v1/council/run-reviews` (last 50, in-memory).
Workload decisions: `GET /v1/council/decisions` (2 demo + live, hash-chained).

---

## 4. Provider gets paid  (operator-approved)

1. Create the Connect recipient account for user 1's device key:
   `POST /admin/payouts/connect` (body: the `static_pk` from
   `GET /admin/payouts/pending`). Send user 1 the returned onboarding link;
   they complete Stripe identity/bank details.
2. Webhook flips the account to `enabled` (or `POST /admin/payouts/connect/refresh`).
3. Dry run: `POST /admin/payouts/run` → lists what would move, `dry_run:true`.
4. **You** move the money:
   ```bash
   curl -XPOST 'https://api.ayni-ai.com/admin/payouts/run?commit=1' \
     -H "Authorization: Bearer $SC_ADMIN_TOKEN"
   ```
   Stripe transfers whole cents; sub-cent dust re-accrues. Rows settle to `paid`
   with the `stripe_transfer_id`.

Claude never runs step 4.

---

## What's still manual / thin

- Provider payout onboarding has no self-serve UI — operator triggers
  `/admin/payouts/connect` and passes the link along.
- `models.ayni-ai.com/qwen2.5-0.5b-instruct-q4_k_m.gguf` must exist (R2). If the
  installer's model fetch 404s, upload it with `sc-modelctl` / `wrangler`.
- Council `run-reviews` / live `decisions` are in-memory — they reset on deploy.
- One model reviewer max wired today; the 10-seat roster is demonstration data.
