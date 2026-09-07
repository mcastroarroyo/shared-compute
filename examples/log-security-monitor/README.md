# Workspace log security monitor on Ayni

Triage security logs with a small model spread across idle consumer devices instead of a
hosted model API or your own GPUs. Reads the Cloud Logging JSON your pipeline already
produces, writes findings as JSONL for your SIEM.

This directory is a working runner, not a slide. It includes a `--selftest` that measures
whether the model is actually good enough for your logs — read
[the measured results](#measured-quality-read-this-first) before you wire it to anything.

## Quick start

```bash
pip install -e ../../clients/python          # or just copy clients/python/ayni next to this file
export AYNI_API_KEY=sc_live_...              # from app.ayni-ai.com

# 1. What would it cost? Sends counts, not logs. No API key needed.
python monitor.py --estimate --count 50000

# 2. Is the model good enough for this? Scores it against labelled lines.
python monitor.py --selftest --min-severity LOW

# 3. Triage a file
python monitor.py --input sample-logs.jsonl --out findings.jsonl

# 4. Straight from GCP, in place of your current cron
gcloud logging read 'severity>=WARNING' --format=json --limit=5000 \
  | python monitor.py --input - --out findings.jsonl

# 5. Hourly sweep: queue it, take a callback, collect later
python monitor.py --input hour.jsonl --async --webhook https://ops.example.com/ayni
```

## Measured quality — read this first

Measured on 2026-09-07 against `qwen2.5-0.5b-instruct-q4_k_m`, the only model in Ayni
production today, over the 12 labelled lines in `labelled-sample.jsonl`:

| Threshold | Recall | Precision | Verdict |
|---|---|---|---|
| `--min-severity LOW` | 100% (6/6) | 50% (6 false alarms) | Usable as a **volume filter** |
| `--min-severity MEDIUM` | 83% | 45% | **Not fit** — the tool says so and exits 1 |

What that means honestly:

- **This model is not a severity classifier.** It returned only two distinct answers across
  twelve lines. Asked a direct yes/no ("should a human review this?") in testing, it
  answered *no* to everything and caught **zero** of six real issues, including an SSH brute
  force and a path-traversal probe.
- **It is usable for volume reduction at `LOW`**, where it kept everything that mattered and
  halved what a human or an expensive model would otherwise read. That is a real saving on
  a large log stream, and it is all this size of model should be asked to do.
- **Do not point this at a SIEM as a severity source** until `--selftest` shows good recall
  *and* precision on *your* labelled data. Twelve lines is a smoke test, not evidence.
- The pipeline itself is model-agnostic. Ayni prices `MEDIUM` and `LARGE` classes already;
  when devices serving them come online, change `--model` and re-run `--selftest`.

Three prompt designs were tried before publishing this: a five-level severity plus category
taxonomy, a binary yes/no, and the current one-token form with worked examples. The current
design is the best of the three, and its limits are the model's, not the prompt's.

### Design notes that mattered

- **Ask for one token.** A 0.5B model holds a five-way choice; it does not hold a five by
  seven taxonomy.
- **Do not feed it stray context.** Appending `[resource=gce_instance]` made it emit
  `gce_instance` as the category. That annotation now stays on the finding, out of the prompt.
- **Derive what you can.** The category comes from deterministic keyword rules: free, instant,
  and never wrong in the way a small model is wrong. Only the judgement call goes to the model.

## What it does

1. Reads Cloud Logging JSON (`textPayload`, `jsonPayload`, `protoPayload`), plain JSONL, or
   text, from a file or stdin.
2. Builds one tightly anchored prompt per line.
3. Quotes the batch, shows the price, and refuses to run above `--max-price`.
4. Runs it on Ayni, synchronously or queued with `--async`.
5. Writes one JSON object per finding, with the serving device and its trust tier.

```json
{"severity":"HIGH","category":"recon","reason":"HIGH","log":"GET /admin/../../etc/passwd 404 from 198.51.100.7",
 "timestamp":"2026-09-07T08:20:41Z","insertId":"a12","served_by":"85b34912","trust_level":"community"}
```

## Options worth knowing

| Flag | Why |
|---|---|
| `--estimate --count N` | Price the move before sending anything. |
| `--selftest` | Score the model on labelled lines. Exits 1 when unfit. |
| `--async --webhook URL` | Queue large sweeps; collect on a signed callback. |
| `--spot` | 60% of on-demand for work that can wait. |
| `--trust device_attested` | Hardware-verified devices only, ×1.4 price. |
| `--max-price 10.0` | Hard ceiling per batch; nothing runs above it. |
| `--min-severity` | Reporting floor. `LOW` is the volume-filter setting. |

## Replacing a GCP call

What changes is the classify call. Where logs come from, how findings are stored, and your
alerting all stay put.

```diff
- resp = vertex_model.predict(instances=[{"content": prompt}])
- verdict = resp.predictions[0]
+ result = ayni.run(prompts=[prompt], max_tokens=6, max_price_usd=5.00)
+ verdict = result.items[0].content
```

Sensible order: price it, `--selftest` it, shadow it against your current tool for a day,
then cut over the batch tier only. Keep interactive detection where it is.

## Privacy

Each line is sealed to one device with a one-time key; the coordinator relays ciphertext and
a CI check fails the build if any code path could log prompt content. Async results are held
in coordinator memory for a bounded window and fetched over TLS, never written to disk. See
[docs/WORKLOAD-RUNNERS.md](../../docs/WORKLOAD-RUNNERS.md).
