# Workload runners

A **workload runner** is a program that sends batch inference to Ayni over the API:
a nightly log triage, a classification sweep, an enrichment pass. You describe the work,
Ayni prices it up front, a Council reviews its shape, it fans out across idle benchmarked
devices, and you are charged once at the quoted price.

This guide is the whole contract: quote, queue, run, collect, and what to change when you
move a pipeline off a hosted API or your own GPUs.

- API base: `https://api.ayni-ai.com`
- Client: [`clients/python`](../clients/python) (standard library only)
- Worked example: [`examples/log-security-monitor`](../examples/log-security-monitor)
- No code at all: the [MCP server](MCP-SERVER.md) gives Claude Desktop, Claude Code or Cursor the same quote, run and collect steps with a price cap on every spend

---

## 1. The shape of it

```
  price it            decide            run it                collect
  ────────            ──────            ──────                ───────
  POST /v1/quote  →   (your budget) →   POST /v1/workloads  →  GET /v1/runs/{id}
  no key, no                            POST .../accept        GET /v1/runs/{id}/results
  content sent                          {"async": true}        or a signed webhook
```

Two rules worth knowing before you write any code:

1. **Nothing runs until you accept.** A quote is a price and a completion estimate. It
   expires (ten minutes by default) and costs nothing.
2. **You are charged the quoted price, once** — not the metered sum. If every item fails,
   you are charged nothing.

## 2. Price the move before you move it

`POST /v1/quote` is public: no API key, and you send counts rather than content. Use it to
compare against your current bill honestly, before anything leaves your network.

```bash
curl -s https://api.ayni-ai.com/v1/quote \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen2.5-0.5b-instruct-q4_k_m",
       "estimate":{"count":5000,"avg_prompt_tokens":180,"avg_completion_tokens":48},
       "spot":false}' | jq '.price.total_usd, .estimate'
```

With the Python client, including volumes above the 5,000-item preview cap (priced from a
representative slice and scaled — pricing is linear in tokens):

```python
from ayni import Ayni

q = Ayni(api_key="").estimate(items=50_000, avg_input_tokens=180, avg_output_tokens=48)
print(q.total_usd, q.breakdown_usd)
```

The breakdown is the actual cost build-up — compute paid to devices, coordination, a
failure buffer, payment fees, Ayni's margin — not a bid plus a markup.

Compare prices **per million tokens**, the unit every vendor uses: MICRO is about $0.03 per
million input tokens and $0.13 per million output on demand, 60% of that on spot. A
per-item or per-line price is only meaningful next to the tokens per item behind it; a
220-token log line with a one-word answer is about 226 tokens, so "$6 per million lines" is
"$0.027 per million tokens".

## 3. Get a key and credit

Sign in at [app.ayni-ai.com](https://app.ayni-ai.com), then **Add $10** on the dashboard.
Your key is on the same account; an operator can mint additional keys from the console.
Send it as `Authorization: Bearer sc_live_...`. At zero balance the API returns
`402 insufficient_credit` and nothing runs.

## 4. Run a workload

### Synchronous — small batches, interactive tools

```python
from ayni import Ayni

ayni = Ayni()                                   # reads AYNI_API_KEY
result = ayni.run(
    prompts=[f"Classify this log line: {line}" for line in lines],
    max_tokens=48,
    max_price_usd=5.00,        # refuse to run if the quote comes back higher
)
print(result.ok_count, result.charged_usd)
for item in result.items:
    print(item.index, item.content)
```

The connection stays open for the whole fan-out. Good to a few hundred items; past that,
queue it.

### Asynchronous — the runner path

```python
run = ayni.submit(
    prompts=prompts,
    label="logs-2026-09-07T08",
    webhook_url="https://ops.example.com/hooks/ayni",   # optional
    max_price_usd=20.00,
)
print(run.id, run.status)        # run_ab12…  queued
print(run.webhook_secret)        # shown once — store it to verify callbacks

run = ayni.wait(run.id)          # or wait for the webhook and skip polling
if run.status == "succeeded":
    results = ayni.results(run.id)
```

Raw HTTP:

```bash
# 1. quote
WL=$(curl -s https://api.ayni-ai.com/v1/workloads \
  -H "Authorization: Bearer $AYNI_API_KEY" -H "Content-Type: application/json" \
  -d '{"model":"qwen2.5-0.5b-instruct-q4_k_m","items":[{"messages":[{"role":"user","content":"…"}]}]}' \
  | jq -r .id)

# 2. queue it
RUN=$(curl -s -X POST "https://api.ayni-ai.com/v1/workloads/$WL/accept" \
  -H "Authorization: Bearer $AYNI_API_KEY" -H "Content-Type: application/json" \
  -d '{"async":true,"label":"nightly"}' | jq -r .id)

# 3. poll, then collect
curl -s "https://api.ayni-ai.com/v1/runs/$RUN"          -H "Authorization: Bearer $AYNI_API_KEY"
curl -s "https://api.ayni-ai.com/v1/runs/$RUN/results"  -H "Authorization: Bearer $AYNI_API_KEY"
```

## 5. Collecting results — read this before you build

**Results live in coordinator memory, never on disk.** Ayni's architecture forbids writing
prompt or completion content to storage; async runs keep that promise rather than quietly
breaking it for convenience. What this means for you:

- Persisted per run: status, item counts, tokens, timings, cost. **Never content.**
- Results are held for `SC_RUN_RESULTS_TTL_SECONDS` (one hour by default) and served over
  TLS to the owning key, then dropped.
- After that, `GET /v1/runs/{id}/results` returns `410 results_expired`, and the run still
  reports `succeeded` with `results_available: false`. Re-run to regenerate.
- A coordinator restart drops in-flight results too. Collect promptly; treat results as a
  stream to drain into your own store, not as a durable Ayni-side archive.

Write results into your system as you collect them, exactly as you would with a queue you
must acknowledge.

## 6. Webhooks

Supply `webhook_url` (https only) and Ayni POSTs a **content-free** notice when the run
finishes: status, counts, cost, and a `results_url` you then fetch with your key. Content
is never pushed to a URL, only pulled over an authenticated TLS call.

```
X-Ayni-Signature: t=1757222400,v1=9f2b…
```

`v1` is `HMAC-SHA256(secret, "{t}.{raw_body}")` with the `webhook_secret` returned once at
submission — the same scheme Stripe uses.

```python
from ayni import verify_webhook

if not verify_webhook(request.body, request.headers["X-Ayni-Signature"], SECRET):
    return 400
```

Reject anything that fails verification or whose `t` is far from now. Delivery is
best-effort and attempted once; polling remains the source of truth.

## 7. Endpoint reference

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/quote` | Public price/ETA from counts. No key, no content. Max 5,000 items. |
| `POST` | `/v1/workloads` | Quote real items. Returns `id`, price breakdown, Council verdict. |
| `GET` | `/v1/workloads/{id}` | Re-read a quote before accepting. |
| `POST` | `/v1/workloads/{id}/accept` | Empty body runs synchronously. `{"async":true}` queues it → `202`. |
| `GET` | `/v1/runs` | Your recent runs, newest first (`?limit=`). |
| `GET` | `/v1/runs/{id}` | Status and progress. |
| `GET` | `/v1/runs/{id}/results` | Items, usage, stats. `409` while running, `410` once expired. |
| `POST` | `/v1/batch` | One-shot fan-out, metered per token, no quote step. |
| `POST` | `/v1/chat/completions` | OpenAI-compatible single request, streaming or blocking. |

Accept body: `{"async": bool, "webhook_url": "https://…", "label": "free text"}`.

Run status: `queued → running → succeeded | failed`.

### Errors

| Status | Code | What to do |
|---|---|---|
| 402 | `insufficient_credit` | Add credit; the quote is untouched until it expires. |
| 409 | `council_blocked` | The Council refused the workload's shape. Read `council.reviews`. |
| 409 | `not_finished` | Still running. Honour `Retry-After`. |
| 409 | `already_accepted` | A quote runs once. Get a new one. |
| 410 | `results_expired` | Results left memory. Re-run. |
| 429 | `too_many_runs` | You are at your concurrent-run ceiling (4 by default). |
| 429 | `rate_limited` | 120 req/min per key. Honour `Retry-After`. |
| 409 | `no_eligible_provider` | No online device meets your trust tier or model. |

The Python client raises `InsufficientCredit`, `CouncilBlocked` and `AyniError` (with
`.code`), and retries 429/5xx with backoff.

## 8. Choosing tier, spot and redundancy

| You want | Set | Effect |
|---|---|---|
| Sensitive input | `X-Provider-Trust-Level: device_attested` | Hardware-verified devices only; ×1.4; `409` if none online |
| Cost over speed | `"spot": true` | 60% of on-demand, wider completion band, may requeue |
| Cross-checked output | `"redundancy": 2` or `3` | 2× or 3× compute; 3 takes a majority vote |

Batches over 1,000 items at redundancy 1 on unattested devices draw a `CONDITIONAL`
Council note: one bad device can quietly corrupt a whole sweep. For security triage,
either raise redundancy or pin `device_attested`.

## 9. Moving a pipeline off GCP

The worked example is a workspace log security monitor:
[`examples/log-security-monitor`](../examples/log-security-monitor). The pattern
generalises to any per-record classification or enrichment job.

**What changes:** the call that classifies a record.
**What does not:** where logs come from, how findings are stored, your alerting.

```diff
- resp = vertex_model.predict(instances=[{"content": prompt}])
- verdict = resp.predictions[0]
+ result = ayni.run(prompts=[prompt], max_tokens=48, max_price_usd=5.00)
+ verdict = result.items[0].content
```

A sensible migration:

1. **Price it.** `--estimate --count N` with your real volumes. No content leaves your network.
2. **Shadow it.** Run one hour of logs through both and diff the findings. A 0.5B model
   with a tight, closed-form prompt does well at triage; it is not a reasoning model.
3. **Cut over the batch tier first.** Nightly and hourly sweeps tolerate seconds per item.
   Keep anything interactive where it is until you have measured it.
4. **Set a ceiling.** `max_price_usd` per submission plus prepaid credit bounds spend
   at two levels; there is no surprise invoice.

**What you gain:** one itemised quote instead of per-request billing; prompts sealed to a
single device with a one-time key and never logged; a hardware-attested tier you can
require per call; open source you can read end to end.

**What you give up today:** one 0.5B model in production, seconds not milliseconds per
item, no uptime SLA, and results you must collect within the retention window. Interactive
or frontier-model work should stay where it is.

**Measure the model before you trust it.** The production model is small, and small models
fail in ways that are easy to mistake for working. Measured on the log-triage example
(2026-09-07): at a `LOW` reporting floor it kept every line that mattered while halving the
volume — useful — but it produced only two distinct answers across twelve lines, and asked
a direct yes/no it caught none of six real issues. It is a volume filter, not a classifier.
The example ships a `--selftest` that scores recall and precision against labelled lines and
exits non-zero when the result is not fit for purpose. Run it on your own labelled data,
at your own threshold, before anything you build depends on the verdicts. See
[the comparison](https://ayni-ai.com/testers) and be honest with yourself about which tier
of work you are moving.

## 10. Operating a runner

- **Keep a ledger of your own.** Store `run_id`, `label` and the items you submitted, so a
  restart or an expired result window is a re-run, not a mystery.
- **Idempotency is yours.** Ayni charges one accept once, but if your job resubmits the
  same logs you pay twice. Key your work by source record id.
- **Watch `failed_count`.** Items fail individually; the run still succeeds. Re-submit
  failures rather than the whole batch.
- **Ceilings.** Four concurrent runs per key by default, 120 requests per minute per key,
  5,000 items per workload. Chunk and pipeline within those.
- **Spend.** Prepaid credit is the hard stop. `max_price_usd` is the soft one. Set both.

## If you already run a LAN cluster

Teams that route local inference through a LAN router such as NVIDIA's Personal AI Router keep the
same request shape when they move batch work to Ayni: both speak the OpenAI Chat Completions
contract. Keep interactive, in-building work on the cluster and send the queue to `POST /v1/workloads`
or the Python client. A cluster can also become supply: run the Ayni provider daemon on one member
with its backend pointed at the cluster's loopback proxy and the whole cluster joins as one
`community`-tier node (see `docs/RELATED-WORK.md`).
