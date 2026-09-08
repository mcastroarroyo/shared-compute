# Ayni MCP server: run workloads from Claude, Cursor or your own agent

This guide is for a small security team that wants to run batch work on Ayni without
writing code: point an MCP client at the Ayni server, ask in plain language, approve the
price, get the results. It covers install, the account steps, the tool set, a worked
security-log triage, the money guard rails, and what the server does not do.

The server is part of the Python client in [`clients/python`](../clients/python). It uses
only the Python standard library, talks to the same API as [the runner guide](WORKLOAD-RUNNERS.md),
and speaks the Model Context Protocol over stdio. Your API key stays on your machine; the
server never prints it.

---

## 1. What you can do through it

| You say | The client calls | Spends credit? |
|---|---|---|
| "Is Ayni up? How much credit do I have?" | `ayni_status` | no |
| "What would 50,000 log lines a day cost?" | `ayni_estimate` | no, and no content is sent |
| "Price these prompts" | `ayni_quote` | no (a quote is free and expires in ten minutes) |
| "Run that quote, cap at $2" | `ayni_run` | yes, once, at the quoted price |
| "How is the run going?" / "Wait for it" / "Get the results" | `ayni_run_status`, `ayni_wait`, `ayni_results` | no |
| "Show my recent runs" | `ayni_list_runs` | no |
| "Add $20 of credit" | `ayni_topup_link` returns a Stripe page for you to pay on | not by the server |
| "Triage this log export" | `ayni_triage_logs` (price first, run on confirm) | on confirm only |
| "Is the model good enough for my logs?" | `ayni_triage_selftest` | about one cent |

Two prompt templates ship with the server for clients that show them: **Triage security
logs on Ayni** (the guided flow in section 5) and **Price moving a batch job to Ayni**.

## 2. Install

You need Python 3.10 or newer and an Ayni API key (section 3).

**From the repository, no install step:**

```bash
git clone https://github.com/mcastroarroyo/shared-compute
```

The command for every client below is then:

```
python3 -m ayni.mcp
```

with the environment variable `PYTHONPATH` set to `<clone>/clients/python`.

**As a package (gives you an `ayni-mcp` command):**

```bash
pip install "git+https://github.com/mcastroarroyo/shared-compute#subdirectory=clients/python"
```

**Check it works** before wiring a client. This prints what `ayni_status` would return:

```bash
AYNI_API_KEY=sc_live_... ayni-mcp --selfcheck
```

### Claude Desktop

Settings → Developer → Edit Config, add the server, restart Claude Desktop:

```json
{
  "mcpServers": {
    "ayni": {
      "command": "ayni-mcp",
      "env": {
        "AYNI_API_KEY": "sc_live_...",
        "AYNI_MAX_PRICE_USD": "10"
      }
    }
  }
}
```

If you did not install the package, use `"command": "python3", "args": ["-m", "ayni.mcp"]`
and add `"PYTHONPATH": "/path/to/shared-compute/clients/python"` to `env`.

### Claude Code

```bash
claude mcp add ayni -e AYNI_API_KEY=sc_live_... -e AYNI_MAX_PRICE_USD=10 -- ayni-mcp
```

### Cursor and other MCP clients

Any client that launches a stdio server works. Command `ayni-mcp`, environment as above.

### Environment variables

| Variable | Meaning | Default |
|---|---|---|
| `AYNI_API_KEY` | Consumer API key. Required for everything except estimates. | unset |
| `AYNI_MAX_PRICE_USD` | Hard cap per spending call. No tool argument can exceed it. | `25` |
| `AYNI_BASE_URL` | Coordinator origin. | `https://api.ayni-ai.com` |
| `AYNI_MODEL` | Default model id. | the production model |
| `AYNI_TRUST` | Default trust floor: `community`, `device_attested`, `confidential`. | `community` |

## 3. Account steps (once)

1. Sign in at `https://app.ayni-ai.com` with GitHub or Google.
2. Create an API key on the **API keys** page. It is shown once; put it in the MCP config,
   never in a chat message.
3. Add credit on the **Billing** page, or later ask the assistant for a top-up link.
   Credit is prepaid USD through Stripe; there is no token and no invoice surprise.
4. In your MCP client, run "Call ayni_status". You should see the key id, the credit
   balance, the served models and how many devices are online.

## 4. The workflow, in the order the tools expect

```
 ayni_status ─▶ ayni_estimate ─▶ ayni_quote ─▶ ayni_run ─▶ results
   (free)         (free, no        (free,        (spends,     sync: in the reply
                   content)         10 min)       capped)     async: ayni_wait → ayni_results
```

**Estimate** prices from counts: how many items, average prompt and completion length in
tokens. Nothing but numbers leaves your machine and no key is needed. Use it to compare
against a current bill.

**Quote** sends the real prompts and returns one itemised price, an ETA, how many
devices are eligible, and the Council's decision on the workload's shape. A quote is free,
single-use, and expires after about ten minutes. Nothing runs.

**Run** accepts a quote. It needs `max_price_usd`; if the quote is above it the server
refuses and nothing runs. Sync mode returns the outputs in the reply. Async mode returns a
run id, and `ayni_wait` blocks up to two minutes at a time while reporting progress.

**Results** of an async run are held in the coordinator's memory for a bounded window
(one hour by default) and are never written to disk. Collect them promptly. After the
window they are gone and the tool returns `results_expired`.

**Charges**: you pay the quoted price once when you accept, or nothing if every item
failed. The reply shows `charged_usd` next to `quoted_usd` so the two can be compared.

## 5. Worked example: triage a security-log export

The `ayni_triage_logs` tool reads a local file (a Cloud Logging JSON export from
`gcloud logging read --format=json`, JSONL, or plain text), asks the network for a
one-word severity per line, derives a category deterministically from the text, and
returns findings at or above a severity floor. It always prices first.

A conversation in Claude Desktop with the **Triage security logs on Ayni** prompt
selected, or typed out:

> Triage `~/exports/prod-warnings.json` on Ayni. Budget $2. Show me the price before running.

The assistant then:

1. Calls `ayni_status`. Stops if the key is invalid, credit is zero, or no device is online.
2. Calls `ayni_triage_selftest`. This runs twelve labelled lines and reports recall,
   precision and a plain verdict. **Read the verdict.** The model in production today is a
   0.5B model; on the labelled sample it flags nearly everything at the LOW floor, which
   is a pass-through, not triage. The selftest says so rather than hiding it.
3. Calls `ayni_triage_logs` with `confirm=false`. You see the line count, the price, the
   ETA and whether the price is within budget. No log content has left your machine yet.
4. Asks you to confirm the price.
5. On yes, calls `ayni_triage_logs` with `confirm=true` and a `findings_path` next to the
   input. Progress is reported per chunk of 500 lines. Findings are returned sorted by
   severity, with counts by severity and category, the total charged, and the path of the
   JSONL file with every finding for your SIEM.

What a finding looks like:

```json
{"severity": "HIGH", "category": "auth", "reason": "HIGH",
 "log": "sshd[4412]: Failed password for invalid user admin from 203.0.113.44 port 51422 ssh2",
 "timestamp": "2026-09-06T02:14:11Z", "insertId": "…", "served_by": "b5fea573", "trust_level": "community"}
```

Useful arguments: `min_severity` (default LOW), `spot: true` for 60% of the price and a
looser ETA, `trust: "device_attested"` to require hardware-verified devices, `max_lines`
(default 5,000 per call; larger sweeps belong in the [monitor CLI](../examples/log-security-monitor)).

## 6. Money guard rails

Because an assistant is spending money on your behalf:

- Every tool that can spend requires `max_price_usd`. The server compares the quote to
  it before accepting and refuses if higher.
- `AYNI_MAX_PRICE_USD` is a hard cap per call set by whoever configured the server. No
  argument in a conversation can raise it.
- Nothing runs without a quote, and the server's instructions tell the assistant to show
  the price before accepting. Ask it to confirm every spend if you want that behaviour
  enforced in the client as well.
- Top-ups are never made by the server. `ayni_topup_link` creates a Stripe Checkout page;
  a person opens it and pays.
- The API key is read from the environment and never appears in any tool output.

## 7. Privacy

- Estimates and dry runs send counts, not content.
- Quotes and runs send prompts to the coordinator over TLS, which seals each item to one
  device's key and relays ciphertext. A CI check fails the build if any code path could
  log a prompt.
- Results are held in coordinator memory for a bounded window and never written to disk.
- The server writes to your disk only when you pass `findings_path`.
- Log lines are truncated to 1,200 characters before they are sent, to keep prompts small.

## 8. Quality: measure before you trust

`ayni_triage_selftest` exists because a small model can look competent by answering the
same word every time. It reports recall, precision, how many distinct answers the model
gave, how many lines it flagged, and one of these verdicts:

| Verdict | Meaning |
|---|---|
| usable as a first-pass filter | recall at least 80%, precision at least 50%, and the model discriminated between lines |
| not fit: every line was flagged | the floor is below the model's noise; raise `min_severity` and re-measure |
| not fit: nothing was flagged | the floor is above everything the model says; real incidents pass through |
| not fit: one answer for every line | not classification at all |
| not fit: precision too low | most alerts would be noise |

Measured on the production model on 2026-09-07: two distinct answers (LOW, MEDIUM) across
the twelve lines, every line flagged at LOW. Use it as a volume filter or wait for larger
model classes; the tool will report the improvement when supply arrives. Bring your own
labelled lines for a measurement that means something for your logs.

## 9. Errors you may see

| `error` | What happened | What to do |
|---|---|---|
| `bad_request` | a missing or invalid argument, or the file was not found | fix the argument |
| `over_budget` | the quote is above `max_price_usd` | raise the cap or reduce the work |
| `insufficient_credit` | the key has no credit for this quote | `ayni_topup_link`, pay, retry |
| `council_blocked` | the Council refused the workload's shape | fewer items or lower redundancy |
| `not_found` | the quote expired or was used | quote again |
| `estimate_only` | the quote came from counts, not items | quote real prompts |
| `already_accepted` | that quote already ran | look it up in `ayni_list_runs` |
| `no_provider` | no eligible device online | try later or relax `trust` |
| `not_finished` | results asked for too early | `ayni_wait` |
| `results_expired` | the results window closed | re-run |
| `http_401` | key missing or wrong | check the MCP config |
| `edge_blocked` | a firewall in front of the API matched the prompt text | see the note below |

**Known issue (2026-09-07).** The production edge firewall currently refuses prompts that
contain attack-looking strings such as `../../etc/passwd` or `UNION SELECT`, which security
logs contain by nature. The tools report this as `edge_blocked`. A scoped rule change is
written up in `docs/SECURITY.md` and awaits the operator; until it lands, triage of such lines
fails at the edge instead of running. Lines without such strings run normally.

## 10. What the server does not do

- It does not run a network service. It is a local process your MCP client starts, one
  per client. A hosted MCP endpoint with OAuth is on the roadmap.
- It does not keep state. Quotes, runs and results live in the coordinator; restart the
  client freely.
- It does not retry failed items; they come back marked and are not charged if every item
  failed.
- It does not replace the runner API for always-on pipelines. For a continuous log stream
  use the [runner guide](WORKLOAD-RUNNERS.md) and the monitor CLI.

## 11. Reference

Tools and arguments (all objects; `*` marks required):

- `ayni_status` ()
- `ayni_estimate` (`items`*, `avg_input_tokens`, `avg_output_tokens`, `model`, `redundancy`, `spot`, `trust`)
- `ayni_quote` (`prompts` or `items`, `model`, `max_tokens`, `redundancy`, `spot`, `trust`)
- `ayni_run` (`quote_id`*, `max_price_usd`*, `mode` sync|async, `label`, `webhook_url`, `limit`)
- `ayni_run_status` (`run_id`*)
- `ayni_wait` (`run_id`*, `timeout_seconds` ≤ 120)
- `ayni_results` (`run_id`*, `offset`, `limit`, `include_failed`)
- `ayni_list_runs` (`limit`)
- `ayni_topup_link` (`amount_usd`* ≥ 0.50)
- `ayni_triage_logs` (`max_price_usd`*, `path` or `lines`, `confirm`, `min_severity`, `spot`, `trust`, `chunk`, `max_lines`, `max_findings`, `findings_path`)
- `ayni_triage_selftest` (`max_price_usd`, `min_severity`, `trust`)

Resources: `ayni://guide/quickstart`, and when run from a clone, `ayni://docs/workload-runners`
and `ayni://docs/mcp-server`. Prompts: `triage_security_logs`, `price_a_migration`.

Tests: `python3 -m unittest clients/python/tests/test_mcp.py` runs the server over stdio
against a fake coordinator with the real response shapes.
