#!/usr/bin/env python3
"""
Workspace log security monitor, running on the Ayni network.

Triages log lines for security signal with a small model spread across idle consumer
devices, instead of a hosted model API or your own GPUs. Reads the same Cloud Logging
JSON your existing pipeline already produces, so the swap is an output redirect.

    # 1. What would it cost? Prices the job without sending a single log line.
    python monitor.py --estimate --count 50000

    # 2. Triage a file (Cloud Logging export, JSONL, or plain text)
    export AYNI_API_KEY=sc_live_...
    python monitor.py --input sample-logs.jsonl --out findings.jsonl

    # 3. Straight from GCP, same as your current cron
    gcloud logging read 'severity>=WARNING' --format=json --limit=5000 \
      | python monitor.py --input - --out findings.jsonl

    # 4. Large hourly sweep: queue it, get a callback, collect later
    python monitor.py --input hour.jsonl --async --webhook https://ops.example.com/ayni

Findings are written as JSONL, one object per flagged line, ready for your SIEM.
Full guide: https://ayni-ai.com/runners
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
from typing import Any, Iterator

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", "clients", "python"))

from ayni import Ayni, AyniError, CouncilBlocked, InsufficientCredit, Quote  # noqa: E402

# A 0.5B model pattern-matches; it does not reason its way to a verdict. Two rules earn
# most of the accuracy here: ask for exactly one token from a closed set, and give worked
# examples. The category is NOT asked of the model — small models copy stray context words
# into free-form fields — it is derived deterministically below, which is free and exact.
TRIAGE_TEMPLATE = """Answer with ONE word: CRITICAL, HIGH, MEDIUM, LOW, or NONE.
How much of a security concern is this log line?

LOG: sshd[11]: Failed password for invalid user admin from 45.9.1.2 port 22 ssh2
ANSWER: HIGH
LOG: GET /healthz 200 1.2ms
ANSWER: NONE
LOG: GET /admin/../../etc/passwd 404 from 8.8.8.8
ANSWER: HIGH
LOG: storage.objects.get by contractor@outside.example on buckets/payroll/salaries.csv
ANSWER: HIGH
LOG: systemd[1]: Started Daily apt download activities.
ANSWER: NONE
LOG: sshd[20]: Accepted publickey for deploy from 10.0.2.15 port 43122 ssh2
ANSWER: NONE
LOG: kernel: Out of memory: Killed process 8821 (node)
ANSWER: MEDIUM
LOG: {input}
ANSWER:"""

# Category is a lookup, not a judgement call: deterministic, free, and never wrong in the
# way a small model is wrong.
CATEGORY_RULES = [
    ("auth", ("failed password", "authentication fail", "invalid user", "sudo:", "setiampolicy",
              "accepted publickey", "login", "unauthorized", "permission denied")),
    ("exfiltration", ("storage.objects.get", "download", "payroll", "export", "s3:getobject",
                      "bigquery.jobs", "egress")),
    ("recon", ("../", "etc/passwd", "nmap", "scan", "sqlmap", "union select", "404 from")),
    ("malware", ("malware", "virus", "ransom", "cryptomine", "xmrig", "backdoor")),
    ("availability", ("out of memory", "oom", "too many connections", "timeout", "crash",
                      "refused", "unavailable", "throttl")),
    ("misconfig", ("setiampolicy", "allusers", "public access", "0.0.0.0/0", "insecure")),
]


def categorize(line: str) -> str:
    """Pick a category from the log text itself. First match wins; order is by severity of concern."""
    low = line.lower()
    for name, needles in CATEGORY_RULES:
        if any(n in low for n in needles):
            return name
    return "other"


SEVERITY_RANK = {"NONE": 0, "LOW": 1, "MEDIUM": 2, "HIGH": 3, "CRITICAL": 4}


def extract_message(entry: Any) -> str:
    """Pull the human-readable line out of a Cloud Logging entry (or any JSON/text)."""
    if isinstance(entry, str):
        return entry.strip()
    if not isinstance(entry, dict):
        return str(entry)

    # Cloud Logging shapes, in the order they usually carry the signal.
    if isinstance(entry.get("textPayload"), str):
        base = entry["textPayload"]
    elif isinstance(entry.get("jsonPayload"), dict):
        jp = entry["jsonPayload"]
        base = jp.get("message") or jp.get("msg") or json.dumps(jp, separators=(",", ":"))
    elif isinstance(entry.get("protoPayload"), dict):
        pp = entry["protoPayload"]
        who = (pp.get("authenticationInfo") or {}).get("principalEmail", "")
        base = f"{pp.get('methodName', '')} by {who or 'unknown'} on {pp.get('resourceName', '')}"
    else:
        base = entry.get("message") or json.dumps(entry, separators=(",", ":"))

    # Deliberately no [severity=]/[resource=] annotations: a small model copies those
    # tokens straight into its answer. That context is kept on the finding instead.
    return str(base).strip()[:1200]  # keep prompts small: cost is per token


def read_entries(path: str) -> Iterator[tuple[str, Any]]:
    """Yield (message, original_entry). Accepts JSONL, a JSON array, or plain text."""
    stream = sys.stdin if path == "-" else open(path, encoding="utf-8", errors="replace")
    try:
        head = stream.read(1)
        while head and head.isspace():
            head = stream.read(1)
        rest = stream.read()
        raw = head + rest
    finally:
        if stream is not sys.stdin:
            stream.close()
    if not raw.strip():
        return

    # `gcloud logging read --format=json` emits one big array.
    if raw.lstrip().startswith("["):
        for entry in json.loads(raw):
            yield extract_message(entry), entry
        return

    for line in raw.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            entry = json.loads(line)
        except json.JSONDecodeError:
            entry = line
        yield extract_message(entry), entry


def parse_verdict(text: str, log_line: str) -> tuple[str, str, str]:
    """Read the one-word severity; derive the category from the log text."""
    raw = (text or "").strip().upper()
    sev = ""
    for token in ("CRITICAL", "HIGH", "MEDIUM", "LOW", "NONE"):
        if token in raw:      # tolerate "ANSWER: HIGH" or trailing chatter
            sev = token
            break
    if not sev:
        # Unparseable is not the same as safe: surface it for a human rather than drop it.
        sev = "LOW"
    return sev, categorize(log_line), (text or "").strip()[:200]


def show_quote(q: Quote) -> None:
    where = f"{q.eligible_nodes} device(s)" if q.supply_online else "reference estimate"
    print(
        f"  quote ${q.total_usd:.4f} · eta ~{q.eta_seconds}s · {where}"
        + (f" · council {q.council_decision}" if q.council_decision else ""),
        file=sys.stderr,
    )


def main() -> int:
    ap = argparse.ArgumentParser(description="Triage security logs on the Ayni network.")
    ap.add_argument("--input", "-i", help="JSONL/JSON/text file, or - for stdin")
    ap.add_argument("--out", "-o", default="-", help="findings JSONL out (default stdout)")
    ap.add_argument("--min-severity", default="LOW",
                    choices=list(SEVERITY_RANK), help="report at or above this")
    ap.add_argument("--max-price", type=float, default=10.0,
                    help="refuse to run if a quote exceeds this many USD")
    ap.add_argument("--spot", action="store_true",
                    help="60%% of on-demand price; slower, may requeue")
    ap.add_argument("--trust", choices=["community", "device_attested", "confidential"],
                    help="require this hardware trust tier")
    ap.add_argument("--max-tokens", type=int, default=6)
    ap.add_argument("--chunk", type=int, default=500, help="lines per workload")
    ap.add_argument("--async", dest="is_async", action="store_true",
                    help="queue the work and poll instead of holding the connection")
    ap.add_argument("--webhook", help="https URL called when an async run finishes")
    ap.add_argument("--estimate", action="store_true",
                    help="price the job and exit; sends no log content")
    ap.add_argument("--count", type=int, help="line count to price with --estimate")
    ap.add_argument("--avg-input-tokens", type=int, default=220)
    ap.add_argument("--selftest", action="store_true",
                    help="score the current model against labelled lines and exit")
    ap.add_argument("--labels", default=os.path.join(os.path.dirname(__file__), "labelled-sample.jsonl"),
                    help="JSONL of {log, review} used by --selftest")
    args = ap.parse_args()

    # --- selftest: does this model actually triage YOUR logs? measure, do not assume ---
    if args.selftest:
        rows = [json.loads(l) for l in open(args.labels, encoding="utf-8") if l.strip()]
        client = Ayni(api_key=os.environ.get("AYNI_API_KEY"), trust=args.trust)
        res = client.run(
            prompts=[TRIAGE_TEMPLATE.format(input=r["log"]) for r in rows],
            max_tokens=args.max_tokens, max_price_usd=args.max_price, on_quote=show_quote,
        )
        tp = fp = tn = fn = 0
        verdicts: list[str] = []
        for out, row in zip(res.contents(), rows):
            sev, _, _ = parse_verdict(out, row["log"])
            verdicts.append(sev)
            flagged = SEVERITY_RANK[sev] >= SEVERITY_RANK[args.min_severity]
            want = bool(row["review"])
            tp += flagged and want
            fp += flagged and not want
            tn += (not flagged) and (not want)
            fn += (not flagged) and want
            if flagged != want:
                print(f"  MISS want={'review' if want else 'ignore':<6} got={sev:<8} {row['log'][:58]}")
        recall = tp / (tp + fn) if (tp + fn) else 0.0
        precision = tp / (tp + fp) if (tp + fp) else 0.0
        print(f"\n{len(rows)} labelled lines at --min-severity {args.min_severity}")
        print(f"  recall     {recall:.0%}  ({tp}/{tp + fn} of the lines that needed review were caught)")
        print(f"  precision  {precision:.0%}  ({fp} false alarm(s))")
        print(f"  cost       ${res.charged_usd:.4f}")
        # A model that answers the same thing every time scores high recall for free.
        # Catch that before it reads as competence.
        spread = len(set(verdicts))
        degenerate = spread == 1 or precision < 0.5
        if degenerate:
            print(f"\n  distinct answers: {spread} of 5 possible ({', '.join(sorted(set(verdicts)))})")
            print("\n  VERDICT: not fit for triage.")
            if spread == 1:
                print("  The model returned one answer for every line — that is not classification,")
                print("  and any recall it scores is an artifact of the threshold, not skill.")
            else:
                print("  Precision this low means most alerts are noise; an on-call rota will stop")
                print("  reading them, which is worse than no triage at all.")
            print("  Measured on the 0.5B model in production today. This pipeline is ready for a")
            print("  larger model class; do not put it in front of a SIEM until both numbers hold up.")
            return 1
        if recall < 0.8:
            print("\n  VERDICT: not fit for triage. Real incidents are passing through.")
            return 1
        print("\n  VERDICT: usable as a first-pass filter. Keep measuring on your own labelled data.")
        return 0

    # --- estimate mode: compare against your current bill before moving anything ---
    if args.estimate:
        n = args.count
        if n is None:
            if not args.input:
                ap.error("--estimate needs --count N or --input FILE")
            n = sum(1 for _ in read_entries(args.input))
        # /v1/quote is public: price the move with no key and no log content sent.
        est = Ayni(api_key=os.environ.get("AYNI_API_KEY", ""), trust=args.trust)
        q = est.estimate(items=n, avg_input_tokens=args.avg_input_tokens,
                         avg_output_tokens=args.max_tokens, spot=args.spot)
        print(f"{n:,} log lines")
        print(f"  price      ${q.total_usd:.4f}"
              + (f"  (spot)" if args.spot else "  (on-demand)"))
        print(f"  per 1M     ${q.total_usd / max(n, 1) * 1_000_000:.2f}")
        print(f"  eta        ~{q.eta_seconds}s across {q.eligible_nodes} device(s)"
              + ("" if q.supply_online else " (reference estimate; no matching device online)"))
        for k, v in (q.breakdown_usd or {}).items():
            print(f"    {k:<26} ${v:.4f}")
        return 0

    if not args.input:
        ap.error("--input FILE (or -) is required unless you pass --estimate")

    entries = list(read_entries(args.input))
    if not entries:
        print("no log lines on input", file=sys.stderr)
        return 0
    print(f"read {len(entries):,} log lines", file=sys.stderr)

    client = Ayni(api_key=os.environ.get("AYNI_API_KEY"), trust=args.trust)
    out = sys.stdout if args.out == "-" else open(args.out, "w", encoding="utf-8")
    floor = SEVERITY_RANK[args.min_severity]
    flagged = total_cost = 0
    started = time.time()

    try:
        for start in range(0, len(entries), args.chunk):
            chunk = entries[start : start + args.chunk]
            prompts = [TRIAGE_TEMPLATE.format(input=msg) for msg, _ in chunk]
            n_from, n_to = start + 1, start + len(chunk)
            print(f"lines {n_from:,}-{n_to:,}:", file=sys.stderr)

            try:
                if args.is_async:
                    run = client.submit(
                        prompts=prompts, max_tokens=args.max_tokens, spot=args.spot,
                        webhook_url=args.webhook, max_price_usd=args.max_price,
                        label=f"logs-{n_from}-{n_to}", on_quote=show_quote,
                    )
                    print(f"  queued {run.id}", file=sys.stderr)
                    if args.webhook:
                        print(f"  webhook secret (store this): {run.webhook_secret}", file=sys.stderr)
                    run = client.wait(
                        run.id,
                        on_progress=lambda r: print(
                            f"\r  {r.status}: {r.done}/{r.total}", end="", file=sys.stderr, flush=True),
                    )
                    print(file=sys.stderr)
                    if run.status != "succeeded":
                        print(f"  run failed: {run.error}", file=sys.stderr)
                        continue
                    result = client.results(run.id)
                else:
                    result = client.run(
                        prompts=prompts, max_tokens=args.max_tokens, spot=args.spot,
                        max_price_usd=args.max_price, on_quote=show_quote,
                    )
            except CouncilBlocked as e:
                print(f"  council blocked this batch: {e}", file=sys.stderr)
                continue
            except InsufficientCredit as e:
                print(f"  out of credit: {e}\n  add credit at https://app.ayni-ai.com", file=sys.stderr)
                return 2
            except AyniError as e:
                print(f"  batch failed ({e.code or e.status}): {e}", file=sys.stderr)
                continue

            total_cost += result.charged_usd
            for item, (msg, entry) in zip(result.items, chunk):
                if not item.ok:
                    continue
                sev, cat, reason = parse_verdict(item.content, msg)
                if SEVERITY_RANK[sev] < floor:
                    continue
                flagged += 1
                json.dump({
                    "severity": sev,
                    "category": cat,
                    "reason": reason,
                    "log": msg,
                    "timestamp": entry.get("timestamp") if isinstance(entry, dict) else None,
                    "insertId": entry.get("insertId") if isinstance(entry, dict) else None,
                    "served_by": item.provider,
                    "trust_level": item.trust_level,
                }, out)
                out.write("\n")
            out.flush()
            print(f"  {result.ok_count} ok, {result.failed_count} failed,"
                  f" ${result.charged_usd:.4f}", file=sys.stderr)
    finally:
        if out is not sys.stdout:
            out.close()

    elapsed = time.time() - started
    print(f"\ndone: {flagged:,} finding(s) at or above {args.min_severity} "
          f"from {len(entries):,} lines in {elapsed:.1f}s, ${total_cost:.4f} total",
          file=sys.stderr)
    if args.out != "-":
        print(f"findings written to {args.out}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except AyniError as e:  # a clean line beats a traceback for an operator at 3am
        print(f"ayni: {e}" + (f" [{e.code}]" if e.code else ""), file=sys.stderr)
        sys.exit(1)
    except KeyboardInterrupt:
        print("\ninterrupted; work already accepted is still charged", file=sys.stderr)
        sys.exit(130)
