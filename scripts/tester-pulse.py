#!/usr/bin/env python3
"""
Tester pulse: when a new or returning device shows up on the Ayni network, send it a
small paid job so the tester sees real work (and real cents) within minutes of joining.

Runs from cron or launchd every few minutes. Standard library only.

  python3 scripts/tester-pulse.py            # one pass
  python3 scripts/tester-pulse.py --status   # what it has done today

Reads SC_CONSUMER_KEY and SC_ADMIN_TOKEN from the repo's .env.local (or the environment).
State and logs live in ~/.ayni/pulse/. Spend is capped per day (default $0.50).
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
sys.path.insert(0, os.path.join(REPO, "clients", "python"))
from ayni import Ayni, AyniError  # noqa: E402

API = os.environ.get("AYNI_BASE_URL", "https://api.ayni-ai.com")
STATE_DIR = os.path.expanduser("~/.ayni/pulse")
STATE = os.path.join(STATE_DIR, "state.json")
LOG = os.path.join(STATE_DIR, "pulse.jsonl")
DAILY_CAP_USD = float(os.environ.get("PULSE_DAILY_CAP_USD", "0.50"))
MAX_PRICE_PER_JOB = 0.05
QUIET_MINUTES = 20            # do not re-pulse the same device within this window

PROMPTS = [
    "Reply with one word: hello.",
    "What is 3 + 4? Answer with the number only.",
    "Name one colour. One word.",
    "Say OK.",
    "Complete: The sky is",
    "Reply with the word yes.",
]


def load_env() -> None:
    p = os.path.join(REPO, ".env.local")
    if not os.path.exists(p):
        return
    with open(p, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            k, v = line.split("=", 1)
            os.environ.setdefault(k.strip(), v.strip().strip('"').strip("'"))


def admin_get(path: str) -> dict:
    req = urllib.request.Request(API + path, headers={"Authorization": "Bearer " + os.environ["SC_ADMIN_TOKEN"]})
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.loads(r.read())


def load_state() -> dict:
    try:
        with open(STATE, encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError):
        return {"seen": {}, "spend": {}}


def save_state(st: dict) -> None:
    os.makedirs(STATE_DIR, exist_ok=True)
    tmp = STATE + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(st, f, indent=1)
    os.replace(tmp, STATE)


def log(event: dict) -> None:
    os.makedirs(STATE_DIR, exist_ok=True)
    event["ts"] = datetime.now(timezone.utc).isoformat(timespec="seconds")
    with open(LOG, "a", encoding="utf-8") as f:
        f.write(json.dumps(event) + "\n")
    print(json.dumps(event))


def today() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%d")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--status", action="store_true")
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()
    load_env()
    st = load_state()
    spent_today = st["spend"].get(today(), 0.0)

    if args.status:
        print(f"spent today ${spent_today:.4f} of ${DAILY_CAP_USD:.2f}; devices seen: {len(st['seen'])}")
        if os.path.exists(LOG):
            with open(LOG, encoding="utf-8") as f:
                tail = f.readlines()[-10:]
            print("".join(tail))
        return 0

    if not os.environ.get("SC_ADMIN_TOKEN") or not os.environ.get("SC_CONSUMER_KEY"):
        log({"event": "error", "message": "SC_ADMIN_TOKEN / SC_CONSUMER_KEY not set"})
        return 1

    try:
        ov = admin_get("/admin/overview")
        nodes = admin_get("/admin/nodes")
    except (urllib.error.URLError, ValueError, KeyError) as e:
        log({"event": "error", "message": f"admin api: {e}"})
        return 1

    online = ov.get("providers", {}).get("online", 0)
    if online == 0:
        log({"event": "idle", "online": 0})
        return 0

    rows = nodes if isinstance(nodes, list) else nodes.get("nodes") or nodes.get("data") or []
    now = time.time()
    fresh: list[str] = []
    for r in rows:
        pk = r.get("static_pk", "")
        upd = r.get("updated_at", "")
        try:
            t = datetime.fromisoformat(upd.replace("Z", "+00:00")).timestamp()
        except ValueError:
            continue
        # A benchmark row updated in the last heartbeat window is a device that is online now.
        if now - t > 15 * 60:
            continue
        last = st["seen"].get(pk, 0)
        if now - last < QUIET_MINUTES * 60:
            continue
        fresh.append(pk)

    if not fresh:
        log({"event": "quiet", "online": online})
        return 0
    if spent_today >= DAILY_CAP_USD:
        log({"event": "capped", "online": online, "fresh": len(fresh), "spent_today": spent_today})
        return 0

    # Enough items that every online device is likely to get at least one.
    n_items = max(3, min(online * 3, 12))
    prompts = [PROMPTS[i % len(PROMPTS)] for i in range(n_items)]
    if args.dry_run:
        log({"event": "dry_run", "online": online, "fresh": [p[:12] for p in fresh], "items": n_items})
        return 0

    client = Ayni(api_key=os.environ["SC_CONSUMER_KEY"], base_url=API, timeout=120)
    try:
        res = client.run(prompts=prompts, max_tokens=8, max_price_usd=min(MAX_PRICE_PER_JOB, DAILY_CAP_USD - spent_today))
    except AyniError as e:
        log({"event": "job_failed", "code": e.code, "status": e.status, "message": str(e)})
        return 1

    served = sorted({i.provider for i in res.items if i.ok})
    for pk in fresh:
        st["seen"][pk] = now
    st["spend"][today()] = round(spent_today + res.charged_usd, 6)
    save_state(st)
    log({"event": "job", "online": online, "fresh": len(fresh), "items": n_items, "ok": res.ok_count,
         "failed": res.failed_count, "charged_usd": res.charged_usd, "served_by": served,
         "wall_ms": res.stats.get("wall_ms"), "fanout": res.stats.get("fanout")})
    return 0


if __name__ == "__main__":
    sys.exit(main())
