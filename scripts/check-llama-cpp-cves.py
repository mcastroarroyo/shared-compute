#!/usr/bin/env python3
"""Alert on new llama.cpp CVEs affecting the C++ we bundle on provider devices.

Why this exists
---------------
provider-core links llama.cpp as C++ source vendored inside the `llama-cpp-sys-2`
crate. None of our other scanners see it: cargo-audit covers RustSec advisories for
Rust crates, govulncheck covers Go, npm audit covers JavaScript, pip-audit covers
Python. The vendored C++ belongs to no package ecosystem, so it had no watcher at
all -- and it is the code that parses model files and prompt text on someone's
phone.

What this does
--------------
Pulls every llama.cpp CVE from NVD and diffs it against a reviewed baseline. A CVE
nobody has triaged fails the build. Triaging means adding it to the baseline with a
disposition and a reason, which is a deliberate human step.

This does NOT prove we are unaffected. It proves no llama.cpp CVE has gone unread.
Reachability is argued per-CVE in the baseline file.
"""
from __future__ import annotations

import argparse
import json
import pathlib
import sys
import urllib.error
import urllib.request

NVD = "https://services.nvd.nist.gov/rest/json/cves/2.0?keywordSearch=llama.cpp&resultsPerPage=200"
BASELINE = pathlib.Path(__file__).resolve().parent.parent / "security" / "llama-cpp-cve-baseline.json"
TIMEOUT = 60


def fetch() -> list[dict]:
    req = urllib.request.Request(NVD, headers={"User-Agent": "ayni-supply-chain-check"})
    with urllib.request.urlopen(req, timeout=TIMEOUT) as r:
        data = json.load(r)
    out = []
    for v in data.get("vulnerabilities", []):
        c = v["cve"]
        sev = ""
        metrics = c.get("metrics", {})
        for k in ("cvssMetricV40", "cvssMetricV31", "cvssMetricV30"):
            if metrics.get(k):
                sev = metrics[k][0]["cvssData"].get("baseSeverity", "")
                break
        desc = next((d["value"] for d in c.get("descriptions", []) if d["lang"] == "en"), "")
        out.append(
            {
                "id": c["id"],
                "published": c.get("published", "")[:10],
                "severity": sev,
                "summary": " ".join(desc.split())[:200],
            }
        )
    return sorted(out, key=lambda x: x["published"])


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument(
        "--strict",
        action="store_true",
        help="treat an unreachable NVD as a failure (used by the scheduled run)",
    )
    ap.add_argument(
        "--write-baseline",
        action="store_true",
        help="record every current CVE as untriaged; you still have to fill in each reason",
    )
    args = ap.parse_args()

    try:
        live = fetch()
    except (urllib.error.URLError, TimeoutError, json.JSONDecodeError) as e:
        # A flaky NVD must not block every pull request, but it must not silently
        # hide a new advisory on the weekly sweep either.
        print(f"could not reach NVD: {e}", file=sys.stderr)
        return 1 if args.strict else 0

    if args.write_baseline:
        BASELINE.parent.mkdir(parents=True, exist_ok=True)
        BASELINE.write_text(
            json.dumps(
                {
                    "_comment": "Every llama.cpp CVE, reviewed. disposition: not-compiled | not-reachable | affected | fixed-upstream.",
                    "reviewed": {c["id"]: {"disposition": "TODO", "reason": "TODO", "severity": c["severity"], "published": c["published"]} for c in live},
                },
                indent=2,
            )
            + "\n"
        )
        print(f"wrote {len(live)} CVEs to {BASELINE}")
        return 0

    if not BASELINE.exists():
        print(f"missing baseline {BASELINE}; run with --write-baseline", file=sys.stderr)
        return 1

    baseline = json.loads(BASELINE.read_text())
    reviewed = baseline.get("reviewed", {})

    new = [c for c in live if c["id"] not in reviewed]
    todo = [k for k, v in reviewed.items() if v.get("disposition") in (None, "", "TODO")]
    affected = [k for k, v in reviewed.items() if v.get("disposition") == "affected"]

    print(f"llama.cpp CVEs on NVD: {len(live)}   reviewed: {len(reviewed)}")

    if new:
        print("\nNEW, untriaged llama.cpp CVEs:", file=sys.stderr)
        for c in new:
            print(f"  {c['id']}  {c['published']}  {c['severity']}\n      {c['summary']}", file=sys.stderr)
        print(
            f"\nTriage each one in {BASELINE.name}: set a disposition and say why.",
            file=sys.stderr,
        )

    if todo:
        print(f"\nCVEs still marked TODO: {', '.join(sorted(todo))}", file=sys.stderr)

    if affected:
        print(f"\nCVEs marked affected and not yet fixed: {', '.join(sorted(affected))}", file=sys.stderr)

    return 1 if (new or todo or affected) else 0


if __name__ == "__main__":
    sys.exit(main())
