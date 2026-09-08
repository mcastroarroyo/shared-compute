"""
Security-log triage helpers shared by the example monitor and the MCP server.

A 0.5B model pattern-matches; it does not reason its way to a verdict. Two rules earn
most of the accuracy here: ask for exactly one token from a closed set, and give worked
examples. The category is NOT asked of the model (small models copy stray context words
into free-form fields); it is derived deterministically below, which is free and exact.

Standard library only.
"""

from __future__ import annotations

import json
import sys
from typing import Any, Iterator

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

SEVERITY_RANK = {"NONE": 0, "LOW": 1, "MEDIUM": 2, "HIGH": 3, "CRITICAL": 4}

# Twelve labelled lines used to measure whether the model in production can triage at
# all. Mirrors examples/log-security-monitor/labelled-sample.jsonl.
LABELLED_SAMPLE: list[dict[str, Any]] = [
    {"review": True, "log": "sshd[4412]: Failed password for invalid user admin from 203.0.113.44 port 51422 ssh2"},
    {"review": True, "log": "sshd[4413]: Failed password for invalid user root from 203.0.113.44 port 51423 ssh2"},
    {"review": False, "log": "sshd[4420]: Accepted publickey for deploy from 10.0.2.15 port 43122 ssh2"},
    {"review": False, "log": "GET /healthz 200 1.2ms"},
    {"review": False, "log": "connection refused: too many connections (max_connections=100)"},
    {"review": True, "log": "storage.objects.list by contractor@external.example on projects/_/buckets/finance-exports"},
    {"review": True, "log": "storage.objects.get by contractor@external.example on projects/_/buckets/finance-exports/objects/payroll-2026-q3.csv"},
    {"review": False, "log": "cron: nightly backup completed in 42s"},
    {"review": True, "log": "SetIamPolicy by svc-ci@project.iam.gserviceaccount.com on projects/prod"},
    {"review": False, "log": "systemd[1]: Started Daily apt download activities."},
    {"review": False, "log": "kernel: Out of memory: Killed process 8821 (node) total-vm:2104856kB"},
    {"review": True, "log": "GET /admin/../../etc/passwd 404 from 198.51.100.7 ua=curl/8.4.0"},
]


def categorize(line: str) -> str:
    """Pick a category from the log text itself. First match wins; order is by severity of concern."""
    low = line.lower()
    for name, needles in CATEGORY_RULES:
        if any(n in low for n in needles):
            return name
    return "other"


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


def parse_text(raw: str) -> Iterator[tuple[str, Any]]:
    """Yield (message, original_entry) from JSONL, a JSON array, or plain text."""
    if not raw or not raw.strip():
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


def read_entries(path: str) -> Iterator[tuple[str, Any]]:
    """Yield (message, original_entry) from a file path, or stdin when path is '-'."""
    if path == "-":
        raw = sys.stdin.read()
    else:
        with open(path, encoding="utf-8", errors="replace") as f:
            raw = f.read()
    yield from parse_text(raw)


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


def score(verdicts: list[str], rows: list[dict[str, Any]], min_severity: str) -> dict[str, Any]:
    """Recall/precision of one-word verdicts against labelled rows, with the degenerate check.

    A model that answers the same thing every time scores high recall for free; that is
    caught here before it reads as competence.
    """
    floor = SEVERITY_RANK[min_severity]
    tp = fp = tn = fn = 0
    misses: list[dict[str, Any]] = []
    for sev, row in zip(verdicts, rows):
        flagged = SEVERITY_RANK[sev] >= floor
        want = bool(row["review"])
        tp += flagged and want
        fp += flagged and not want
        tn += (not flagged) and (not want)
        fn += (not flagged) and want
        if flagged != want:
            misses.append({"want": "review" if want else "ignore", "got": sev, "log": row["log"]})
    recall = tp / (tp + fn) if (tp + fn) else 0.0
    precision = tp / (tp + fp) if (tp + fp) else 0.0
    spread = len(set(verdicts))
    flagged_all = (tp + fp) == len(rows)
    flagged_none = (tp + fp) == 0
    degenerate = spread == 1 or flagged_all or flagged_none or precision < 0.5
    if degenerate and spread == 1:
        verdict = ("not fit for triage: the model returned one answer for every line; any recall "
                   "it scores is an artifact of the threshold, not skill")
    elif degenerate and flagged_all:
        verdict = (f"not fit for triage: every line was flagged at min_severity {min_severity}; "
                   "that is a pass-through, not a filter. Raise min_severity and measure again")
    elif degenerate and flagged_none:
        verdict = (f"not fit for triage: nothing was flagged at min_severity {min_severity}; "
                   "real incidents would pass through")
    elif degenerate:
        verdict = ("not fit for triage: precision this low means most alerts are noise, and an "
                   "on-call rota will stop reading them")
    elif recall < 0.8:
        verdict = "not fit for triage: real incidents are passing through"
    else:
        verdict = "usable as a first-pass filter; keep measuring on your own labelled data"
    return {
        "lines": len(rows),
        "min_severity": min_severity,
        "recall": round(recall, 3),
        "precision": round(precision, 3),
        "true_positives": tp, "false_positives": fp, "true_negatives": tn, "false_negatives": fn,
        "distinct_answers": spread,
        "flagged": tp + fp,
        "fit": not degenerate and recall >= 0.8,
        "verdict": verdict,
        "misses": misses,
    }
