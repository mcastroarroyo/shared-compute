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
from typing import Any

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", "clients", "python"))

from ayni import Ayni, AyniError, CouncilBlocked, InsufficientCredit, Quote  # noqa: E402

from ayni.triage import (  # noqa: E402
    LABELLED_SAMPLE,
    SEVERITY_RANK,
    TRIAGE_TEMPLATE,
    parse_verdict,
    read_entries,
    score,
)


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
        if os.path.exists(args.labels):
            rows = [json.loads(l) for l in open(args.labels, encoding="utf-8") if l.strip()]
        else:
            rows = LABELLED_SAMPLE
        client = Ayni(api_key=os.environ.get("AYNI_API_KEY"), trust=args.trust)
        res = client.run(
            prompts=[TRIAGE_TEMPLATE.format(input=r["log"]) for r in rows],
            max_tokens=args.max_tokens, max_price_usd=args.max_price, on_quote=show_quote,
        )
        verdicts = [parse_verdict(out, row["log"])[0] for out, row in zip(res.contents(), rows)]
        sc = score(verdicts, rows, args.min_severity)
        for m in sc["misses"]:
            print(f"  MISS want={m['want']:<6} got={m['got']:<8} {m['log'][:58]}")
        print(f"\n{sc['lines']} labelled lines at --min-severity {args.min_severity}")
        print(f"  recall     {sc['recall']:.0%}  ({sc['true_positives']}/{sc['true_positives'] + sc['false_negatives']} of the lines that needed review were caught)")
        print(f"  precision  {sc['precision']:.0%}  ({sc['false_positives']} false alarm(s))")
        print(f"  cost       ${res.charged_usd:.4f}")
        print(f"  distinct answers: {sc['distinct_answers']} of 5 possible ({', '.join(sorted(set(verdicts)))})")
        print(f"\n  VERDICT: {sc['verdict']}.")
        if not sc["fit"]:
            print("  Measured on the model in production today. Do not put this in front of a SIEM")
            print("  until both numbers hold up on your own labelled lines.")
            return 1
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
        toks = n * (args.avg_input_tokens + args.max_tokens)
        print(f"  per 1M tokens  ${q.total_usd / max(toks, 1) * 1_000_000:.4f}"
              f"   (per 1M lines ${q.total_usd / max(n, 1) * 1_000_000:.2f}"
              f" at {args.avg_input_tokens + args.max_tokens} tokens/line)")
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
