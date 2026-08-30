# Payments — consumers → Ayni → providers

How money moves through the network, and what has to be built to operationalise it.
Status: **design**. Only the metering that feeds it (`usage_events`) exists today; the
ledger, Stripe integration, and payout runs are M9.

```
 consumer  ──$──▶  Ayni (platform)  ──$──▶  provider (device owner)
  prepaid credits    keeps a margin       revenue share, paid on a schedule
  or monthly invoice  (infra, egress,     via Stripe Connect
                       fraud, support)
```

Ayni is the merchant of record for consumers and the payer for providers. Providers
never transact with consumers directly.

---

## 1. Consumer side (revenue)

**Launch model — prepaid credits.** An account holds a `credit_micros` balance
(integer micro-USD, never floats). Top up with Stripe Checkout / PaymentIntent; each
request debits the metered cost; at zero the API returns `402 insufficient_credit`.
Chargeback exposure is bounded to recent top-ups and covered by the reserve (§4).

**Later — postpaid metered** for vetted customers: a Stripe Billing metered
subscription, usage reported nightly from `usage_events` via Stripe Meter Events,
invoiced monthly.

**Price card** (per 1M tokens, illustrative — set real numbers before launch):

| Model class | Input | Output | ×`device_attested` | ×`confidential` |
|---|---|---|---|---|
| MICRO (≤3B) | $0.02 | $0.08 | 1.5 | — |
| SMALL (3–8B) | $0.05 | $0.20 | 1.5 | 3 |
| MEDIUM (12–30B q) | $0.15 | $0.60 | 1.4 | 3 |
| LARGE (30–70B) | $0.40 | $1.60 | 1.4 | 3 |
| XL / MoE | $0.90 | $3.60 | 1.3 | 3 |

Tier multipliers apply when the consumer requires `X-Provider-Trust-Level` above
`community`. The published rate is what the consumer pays; the provider gets a share
of it (§2).

**Token counts are coordinator-authored.** The coordinator already decrypts the relay
stream, so it counts prompt/completion tokens itself and writes `usage_events`. The
provider's self-reported `job_done.usage` is advisory only — it can't inflate a bill
or a payout.

---

## 2. Provider side (payout)

**Onboarding — Stripe Connect Express.** The provider completes Stripe-hosted KYC and
bank details; Ayni stores only `stripe_account_id`. No bank data or PII touches our
systems.

**Earnings ledger.** For every completed job the metering path writes one
`provider_earnings` row:

```
gross_micros    = price_card(model_class, io_split) * tokens          # what the consumer paid
provider_micros = gross_micros * REV_SHARE * quality_mult             # what the provider earns
```

- `REV_SHARE` = 0.70 for `community` / `device_attested`; 0.60 for `confidential`
  (more platform infra). Ayni keeps the remainder, out of which it pays Stripe fees,
  egress, and support.
- `quality_mult` ∈ [0.5, 1.1], from a rolling 7‑day window of the node's uptime, p95
  TTFT, error rate, and cancellation rate. Defaults to 1.0; only sustained bad service
  drags it down.
- `community` nodes also get a low absolute base rate — see §5.

**Payout runs.** Weekly cron:

1. Sum `provider_earnings` in state `accrued` per `provider_identity`.
2. Skip identities under the `MIN_PAYOUT` threshold ($10) — rolls to next week.
3. Hold back a 7‑day **reserve** (unvested earnings) against disputes/clawback.
4. Create a Stripe Transfer to the connected account for the vested remainder.
5. Mark rows `paid`, record `stripe_transfer_id`, write a `payouts` row.

**Clawback.** A consumer chargeback or a fraud finding posts a negative adjustment
row; it nets against the reserve first, then future earnings.

---

## 3. Data model (Postgres, all money in integer micro-USD)

```
billing_accounts(id, owner_email, credit_micros, stripe_customer_id, created_at)
credit_ledger(id, account_id, delta_micros, reason,
              stripe_payment_intent, job_id, created_at)
provider_payout_accounts(provider_identity, stripe_account_id, status, created_at)
provider_earnings(id, provider_identity, job_id, model_class, tier,
                  gross_micros, provider_micros, quality_mult,
                  state ∈ {accrued, vested, paid, reversed}, payout_id, created_at)
payouts(id, provider_identity, amount_micros, stripe_transfer_id,
        period_start, period_end, state, created_at)
```

`provider_identity` is the attested hardware key (or X25519 static key for
`community`), so Sybil farming is capped by attestation (§ SECURITY.md) and per-identity
rate limits.

---

## 4. Anti-abuse

- **Inflated usage:** impossible — counts come from the coordinator's own view of the
  decrypted stream, not the provider.
- **Sybil providers:** `device_attested` requires a unique StrongBox/TEE key
  attestation; `community` earns near zero (§5); per-identity earning caps.
- **Consumer fraud:** prepaid credits + Stripe Radar on top-ups + a reserve on the
  payout side absorb chargebacks.
- **Collusion (fake consumer paying own provider):** platform margin + KYC on both
  sides + anomaly detection on self-dealing traffic patterns.

---

## 5. The economics, honestly

At $0.15/M output tokens (current placeholder) a single phone on a 0.5B model earning
the numbers you saw (`$0.00009` for a handful of jobs) would need **~6.7M output
tokens/day — ~77 tok/s sustained around the clock — to make $1/day.** That is not a
real outcome for one phone.

Where the money actually is:

1. **Scale** — hundreds/thousands of devices, each contributing marginal idle time.
2. **Bigger models** — MEDIUM/LARGE price per token is 10–30× MICRO, and those need
   real desktop/GPU providers, not phones.
3. **Premium workloads** — `device_attested` and `confidential` carry 1.4–3× and are
   where paying customers with sensitive traffic live.

**Recommended launch posture:** present `community` (phones, unattested) as an opt-in
contribution that earns a token amount; make `device_attested` desktops/GPUs on
SMALL+ models the tier that actually pays. Revisit the price card with real provider
supply and consumer demand data before enabling payouts.

---

## 6. Build split

**Claude builds:** the schema + migrations, ledger writes in the metering path (behind
a flag, shadow-accruing with no payouts), the rate-card module, the payout batch job
with a mandatory `--dry-run` that prints the transfer list, reconciliation reports,
and the console views (balance, earnings, payout history).

**You do (human-only):** create the Stripe account, enable Connect as a platform,
complete platform verification, set tax settings (W‑8/W‑9 collection, 1099‑K), and
**approve every payout batch**. Claude never initiates a transfer without an explicit,
per-batch go-ahead.

**Rollout:**

1. Ledger shadow-accrues (`provider_earnings`), no real money. Console shows estimated
   earnings. ← unblocks now
2. Consumer prepaid credits live (Stripe Checkout). Inference becomes paid.
3. Provider Connect onboarding + **manual, user-approved monthly payouts**.
4. Automated weekly payout runs with reserve + full dashboards + alerting.
