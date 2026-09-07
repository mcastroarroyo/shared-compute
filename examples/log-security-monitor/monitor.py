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

# A small model needs a tight, closed-form instruction. One line in, one line out.
TRIAGE_TEMPLATE = """You are a security log triage assistant. Classify the log line.
Reply with exactly one line in this form, nothing else:
SEVERITY|CATEGORY|short reason

SEVERITY is one of CRITICAL, HIGH, MEDIUM, LOW, NONE.
CATEGORY is one of auth, exfiltration, malware, misconfig, recon, availability, other.
Use NONE for ordinary, healthy activity.

LOG LINE:
{input}"""

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

    # Carry a little context the model can use, without ballooning the prompt.
    bits = [str(base).strip()]
    if sev := entry.get("severity"):
        bits.append(f"[severity={sev}]")
    res = entry.get("resource")
    if isinstance(res, dict) and (rtype := res.get("type")):
        bits.append(f"[resource={rtype}]")
    line = " ".join(bits)
    return line[:1200]  # keep prompts small: cost is per token


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


def parse_verdict(text: str) -> tuple[str, str, str]:
    """Parse 'SEVERITY|CATEGORY|reason' leniently — small models drift."""
    line = (text or "").strip().splitlines()[0] if (text or "").strip() else ""
    parts = [p.strip() for p in line.split("|")]
    sev = (parts[0] if parts else "").upper()
    for token in SEVERITY_RANK:
        if token in sev:
            sev = token
            break
    else:
        sev = "NONE" if not sev else "LOW"  # unparseable but non-empty: worth a human look
    cat = parts[1].lower() if len(parts) > 1 else "other"
    reason = parts[2] if len(parts) > 2 else line
    return sev, cat, reason[:300]


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
    ap.add_argument("--max-tokens", type=int, default=48)
    ap.add_argument("--chunk", type=int, default=500, help="lines per workload")
    ap.add_argument("--async", dest="is_async", action="store_true",
                    help="queue the work and poll instead of holding the connection")
    ap.add_argument("--webhook", help="https URL called when an async run finishes")
    ap.add_argument("--estimate", action="store_true",
                    help="price the job and exit; sends no log content")
    ap.add_argument("--count", type=int, help="line count to price with --estimate")
    ap.add_argument("--avg-input-tokens", type=int, default=180)
    args = ap.parse_args()

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
                sev, cat, reason = parse_verdict(item.content)
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
