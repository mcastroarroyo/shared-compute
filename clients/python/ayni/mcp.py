#!/usr/bin/env python3
"""
Ayni MCP server: quote, run and collect workloads on the Ayni network from any MCP
client (Claude Desktop, Claude Code, Cursor, an in-house agent).

Standard library only. Speaks the Model Context Protocol over stdio (newline-delimited
JSON-RPC 2.0); logs go to stderr, never stdout.

    export AYNI_API_KEY=sc_live_...
    python3 -m ayni.mcp            # or the `ayni-mcp` console script

Money guard rails, because an agent is spending it:
  * every tool that can spend takes a required `max_price_usd`;
  * `AYNI_MAX_PRICE_USD` (default 25) is a hard cap per call that no argument can raise;
  * nothing runs until a quote is accepted, and quotes are shown before acceptance;
  * the API key is read from the environment and never appears in any tool output.

Full guide: docs/MCP-SERVER.md in the repository.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import threading
import time
from typing import Any, Callable

from . import __version__
from .client import (
    MAX_ITEMS_PER_WORKLOAD,
    Ayni,
    AyniError,
    CouncilBlocked,
    InsufficientCredit,
    Quote,
    RunResult,
)
from .triage import (
    LABELLED_SAMPLE,
    SEVERITY_RANK,
    TRIAGE_TEMPLATE,
    parse_text,
    parse_verdict,
    read_entries,
    score,
)

SUPPORTED_PROTOCOLS = ("2025-06-18", "2025-03-26", "2024-11-05")
DEFAULT_PROTOCOL = "2025-06-18"
HARD_CAP_DEFAULT = 25.0
TRIAGE_MAX_TOKENS = 6
TRUST_LEVELS = ("community", "device_attested", "confidential")

INSTRUCTIONS = """Ayni runs batch inference on a marketplace of idle, benchmarked consumer devices,
priced up front and charged once at the quoted price. Use these tools in this order:

1. ayni_status: is the coordinator up, is the key valid, how much credit, what supply is online.
2. ayni_estimate: price a job from counts only (no content leaves the machine, no key needed).
3. ayni_quote: price the real items. Nothing runs. Show the user the price, ETA and council decision.
4. ayni_run: accept a quote. Requires max_price_usd; refuses if the quote is above it.
5. ayni_run_status / ayni_wait / ayni_results: for async runs. Results live in coordinator memory
   for a bounded window and are never written to disk, so collect them promptly.

For security logs use ayni_triage_logs (dry run first, then confirm=true) and run
ayni_triage_selftest before trusting the verdicts: the model in production today is a
small one and measured quality is published in the guide. Report prices per million
tokens (price_per_1m_tokens_usd), which is how every vendor quotes; mention a per-item price
only together with the tokens per item behind it. Never spend without telling the user the
quoted price first. If a call returns insufficient_credit, offer ayni_topup_link;
the user opens the link and pays themselves."""

QUICKSTART = """# Ayni quickstart (MCP)

1. ayni_status — connectivity, key id, credit, models, devices online.
2. ayni_estimate {items, avg_input_tokens, avg_output_tokens} — price from counts, keyless.
3. ayni_quote {prompts[]} — real price + ETA + council decision. Expires in ~10 minutes. Free.
4. ayni_run {quote_id, max_price_usd} — runs it. mode "sync" returns results; mode "async"
   returns a run id: poll with ayni_run_status, block briefly with ayni_wait, collect with
   ayni_results (memory-only; fetch promptly).
5. Out of credit? ayni_topup_link {amount_usd} returns a Stripe Checkout page for a person to open.

Pricing is per million tokens (MICRO on demand: about $0.03 input / $0.13 output; spot 60% of
that); every price view carries price_per_1m_tokens_usd. Build-up: compute paid to devices x redundancy, +15% coordination, +8%/redundancy failure buffer,
+30% margin, +3% payment, $0.01 floor. Spot = 60% of on-demand, wider ETA. Tier
device_attested x1.4, confidential x3.0. You are charged the quoted price once, or nothing if
every item failed.

Security logs: ayni_triage_logs {path | lines, max_price_usd, confirm} and ayni_triage_selftest.
"""


# --------------------------------------------------------------------------------------
# helpers
# --------------------------------------------------------------------------------------

def _log(msg: str) -> None:
    print(f"ayni-mcp: {msg}", file=sys.stderr, flush=True)


def _quote_view(q: Quote) -> dict[str, Any]:
    raw = q.raw or {}
    est = raw.get("estimate", {}) or {}
    council = raw.get("council") if isinstance(raw.get("council"), dict) else {}
    reviews = [
        {"seat": r.get("seat"), "decision": r.get("decision"), "note": r.get("note", "")}
        for r in (council.get("reviews") or [])
    ]
    # Market-comparable number: every vendor quotes per million tokens. Per-item figures
    # only make sense next to the token count that produced them.
    tokens = int(est.get("prompt_tokens") or 0) + int(est.get("completion_tokens") or 0)
    sf, st = raw.get("scaled_from_items"), raw.get("scaled_to_items")
    if tokens and sf and st:
        tokens = int(tokens * (st / sf))
    per_1m_tokens = round(q.total_usd / tokens * 1_000_000, 4) if tokens else None
    return {
        "quote_id": q.id,
        "model": raw.get("model"),
        "tokens_priced": tokens or None,
        "price_per_1m_tokens_usd": per_1m_tokens,
        "tier": raw.get("tier"),
        "spot": raw.get("spot", False),
        "redundancy": raw.get("redundancy", 1),
        "runnable": raw.get("runnable", True),
        "price_usd": q.total_usd,
        "breakdown_usd": q.breakdown_usd,
        "eta_seconds": q.eta_seconds,
        "eta_seconds_max": est.get("eta_seconds_max"),
        "eligible_nodes": q.eligible_nodes,
        "supply_online": q.supply_online,
        "council": {
            "decision": council.get("decision", q.council_decision),
            "risk_class": council.get("risk_class"),
            "reviews": reviews,
            "audit_hash": council.get("audit_hash"),
        } if council else {"decision": q.council_decision},
        "expires_at": raw.get("expires_at"),
        "note": raw.get("note"),
        "scaled_from_items": raw.get("scaled_from_items"),
        "scaled_to_items": raw.get("scaled_to_items"),
    }


def _result_view(res: RunResult, *, offset: int = 0, limit: int = 200, include_failed: bool = True) -> dict[str, Any]:
    items = res.items if include_failed else [i for i in res.items if i.ok]
    window = items[offset: offset + limit]
    return {
        "run_id": res.id,
        "items_total": len(res.items),
        "items_ok": res.ok_count,
        "items_failed": res.failed_count,
        "charged_usd": res.charged_usd,
        "quoted_usd": res.quoted_usd,
        "stats": res.stats,
        "offset": offset,
        "returned": len(window),
        "truncated": offset + len(window) < len(items),
        "items": [
            {"index": i.index, "content": i.content, "error": i.error or None,
             "provider": i.provider, "trust_level": i.trust_level, "latency_ms": i.latency_ms}
            for i in window
        ],
    }


def _err(code: str, message: str, **extra: Any) -> dict[str, Any]:
    out = {"error": code, "message": message}
    out.update(extra)
    return out


def _api_error(e: AyniError) -> dict[str, Any]:
    code = e.code or (f"http_{e.status}" if e.status else "error")
    hint = ""
    if isinstance(e, InsufficientCredit) or code == "insufficient_credit":
        hint = "Add credit with ayni_topup_link (a person opens the link and pays), then retry."
    elif isinstance(e, CouncilBlocked) or code == "council_blocked":
        hint = "The Council refused this workload's shape; reduce items or redundancy and re-quote."
    elif code == "not_found":
        hint = "Quotes expire after about ten minutes and are single-use; call ayni_quote again."
    elif code == "results_expired":
        hint = "Results are held in memory for a bounded window; re-run the workload."
    elif code == "not_finished":
        hint = "Still running; use ayni_wait or ayni_run_status."
    elif code == "no_provider":
        hint = "No eligible device is online for this model or tier; try later, or relax trust."
    elif code == "edge_blocked":
        hint = ("A firewall in front of the API matched the prompt text (for example a path "
                "traversal or SQL string inside a log line). Report it to Ayni with the run label; "
                "as a workaround split the file and retry without the offending lines.")
    elif e.status == 401:
        hint = "AYNI_API_KEY is missing or invalid; set it in the MCP server's environment."
    return _err(code, str(e), status=e.status, hint=hint)


class BudgetError(Exception):
    pass


# --------------------------------------------------------------------------------------
# the server
# --------------------------------------------------------------------------------------

class AyniMCP:
    def __init__(self, *, api_key: str | None, base_url: str, model: str, trust: str | None,
                 hard_cap_usd: float, timeout: float = 600.0) -> None:
        self.base_url = base_url
        self.model = model
        self.trust = trust
        self.hard_cap = hard_cap_usd
        self._key = api_key
        self._timeout = timeout
        self._out_lock = threading.Lock()
        self._client_protocol = DEFAULT_PROTOCOL

    # --- clients -----------------------------------------------------------------------

    def _client(self, *, trust: str | None = None, keyless: bool = False) -> Ayni:
        return Ayni(
            "" if keyless else self._key,
            base_url=self.base_url,
            model=self.model,
            trust=trust or self.trust,
            timeout=self._timeout,
        )

    def _budget(self, max_price_usd: Any) -> float:
        try:
            b = float(max_price_usd)
        except (TypeError, ValueError):
            raise BudgetError("max_price_usd is required (a number in USD)") from None
        if b <= 0:
            raise BudgetError("max_price_usd must be positive")
        if b > self.hard_cap:
            raise BudgetError(
                f"max_price_usd {b:.2f} is above this server's hard cap of ${self.hard_cap:.2f} "
                "(AYNI_MAX_PRICE_USD); ask the operator to raise it"
            )
        return b

    @staticmethod
    def _trust_arg(v: Any) -> str | None:
        if v in (None, ""):
            return None
        if v not in TRUST_LEVELS:
            raise BudgetError(f"trust must be one of {', '.join(TRUST_LEVELS)}")
        return str(v)

    # --- tools -------------------------------------------------------------------------

    def tool_status(self, a: dict[str, Any], progress: Callable) -> dict[str, Any]:
        out: dict[str, Any] = {
            "base_url": self.base_url,
            "server_version": __version__,
            "hard_cap_usd": self.hard_cap,
            "default_model": self.model,
            "default_trust": self.trust or "community",
            "key_configured": bool(self._key),
        }
        try:
            out["coordinator"] = self._client(keyless=True).health()
        except AyniError as e:
            out["coordinator"] = _api_error(e)
        if self._key:
            try:
                bal = self._client().balance()
                out["key_id"] = bal.get("key_id")
                out["credit_usd"] = bal.get("credit_usd")
                out["billing_enforced"] = bal.get("enforced")
            except AyniError as e:
                out["key"] = _api_error(e)
            try:
                out["models"] = [
                    {"id": m.get("id"), "hardware_class": m.get("hardware_class"),
                     "context_length": m.get("context_length")}
                    for m in self._client().models()
                ]
            except AyniError as e:
                out["models"] = _api_error(e)
        try:
            q = self._client(keyless=True).estimate(items=100, avg_input_tokens=220, avg_output_tokens=6)
            out["supply"] = {
                "eligible_nodes": q.eligible_nodes,
                "supply_online": q.supply_online,
                "eta_seconds_for_100_short_items": q.eta_seconds,
                "price_usd_for_100_short_items": q.total_usd,
            }
        except AyniError as e:
            out["supply"] = _api_error(e)
        return out

    def tool_estimate(self, a: dict[str, Any], progress: Callable) -> dict[str, Any]:
        items = int(a.get("items") or 0)
        if items <= 0:
            return _err("bad_request", "items must be a positive integer")
        q = self._client(trust=self._trust_arg(a.get("trust")), keyless=True).estimate(
            items=items,
            avg_input_tokens=int(a.get("avg_input_tokens") or 250),
            avg_output_tokens=int(a.get("avg_output_tokens") or 64),
            model=a.get("model") or None,
            redundancy=int(a.get("redundancy") or 1),
            spot=bool(a.get("spot", False)),
        )
        v = _quote_view(q)
        v.pop("quote_id", None)
        v["estimate_only"] = True
        v["items"] = items
        v["tokens_per_item"] = int(a.get("avg_input_tokens") or 250) + int(a.get("avg_output_tokens") or 64)
        v["price_per_1m_items_usd"] = round(q.total_usd / items * 1_000_000, 2)
        v["pricing_note"] = ("Compare on price_per_1m_tokens_usd; the per-item figure assumes "
                             f"{v['tokens_per_item']} tokens per item.")
        return v

    def tool_quote(self, a: dict[str, Any], progress: Callable) -> dict[str, Any]:
        prompts = a.get("prompts")
        items = a.get("items")
        if not prompts and not items:
            return _err("bad_request", "pass prompts: [string, ...] or items: [{messages: [...]}, ...]")
        n = len(prompts or items)
        if n > MAX_ITEMS_PER_WORKLOAD:
            return _err("too_many_items", f"one workload holds at most {MAX_ITEMS_PER_WORKLOAD} items; split it")
        q = self._client(trust=self._trust_arg(a.get("trust"))).quote(
            prompts,
            items=items,
            model=a.get("model") or None,
            max_tokens=int(a["max_tokens"]) if a.get("max_tokens") else None,
            redundancy=int(a.get("redundancy") or 1),
            spot=bool(a.get("spot", False)),
        )
        v = _quote_view(q)
        v["items"] = n
        if v.get("tokens_priced"):
            v["tokens_per_item"] = round(v["tokens_priced"] / n)
        v["next"] = ("ayni_run with this quote_id and a max_price_usd at or above price_usd"
                     if v.get("council", {}).get("decision") != "BLOCK" else
                     "council blocked this shape; nothing can run from this quote")
        return v

    def tool_run(self, a: dict[str, Any], progress: Callable) -> dict[str, Any]:
        quote_id = str(a.get("quote_id") or "")
        if not quote_id:
            return _err("bad_request", "quote_id is required (from ayni_quote)")
        budget = self._budget(a.get("max_price_usd"))
        client = self._client()
        q = client.get_quote(quote_id)  # 404 once expired: surfaced with a hint
        if q.total_usd > budget:
            return _err("over_budget",
                        f"quote is ${q.total_usd:.4f}, above max_price_usd ${budget:.4f}; nothing ran",
                        price_usd=q.total_usd)
        if not q.raw.get("runnable", True):
            return _err("estimate_only", "this quote was built from counts; quote real items to run")
        mode = str(a.get("mode") or "sync")
        if mode == "async":
            run = client.accept_async(quote_id, webhook_url=a.get("webhook_url") or None,
                                      label=str(a.get("label") or ""))
            out = {
                "run_id": run.id, "status": run.status, "quoted_usd": q.total_usd,
                "next": "ayni_wait or ayni_run_status, then ayni_results (memory-only; collect promptly)",
            }
            if run.webhook_secret:
                out["webhook_secret"] = run.webhook_secret
                out["webhook_note"] = "shown once; verifies X-Ayni-Signature on the callback"
            return out
        res = client.accept(quote_id)
        return _result_view(res, limit=int(a.get("limit") or 200))

    def tool_run_status(self, a: dict[str, Any], progress: Callable) -> dict[str, Any]:
        run = self._client().get_run(str(a.get("run_id") or ""))
        return dict(run.raw)

    def tool_wait(self, a: dict[str, Any], progress: Callable) -> dict[str, Any]:
        run_id = str(a.get("run_id") or "")
        timeout = min(max(float(a.get("timeout_seconds") or 60), 1.0), 120.0)
        client = self._client()
        deadline = time.time() + timeout
        run = client.get_run(run_id)
        while not run.finished and time.time() < deadline:
            progress(run.done, run.total, f"{run.status} {run.done}/{run.total}")
            time.sleep(min(3.0, max(0.5, deadline - time.time())))
            run = client.get_run(run_id)
        out = dict(run.raw)
        out["finished"] = run.finished
        if not run.finished:
            out["hint"] = f"still {run.status} after {timeout:.0f}s; call ayni_wait again"
        return out

    def tool_results(self, a: dict[str, Any], progress: Callable) -> dict[str, Any]:
        res = self._client().results(str(a.get("run_id") or ""))
        return _result_view(res, offset=int(a.get("offset") or 0), limit=int(a.get("limit") or 200),
                            include_failed=bool(a.get("include_failed", True)))

    def tool_list_runs(self, a: dict[str, Any], progress: Callable) -> dict[str, Any]:
        runs = self._client().list_runs(limit=int(a.get("limit") or 20))
        return {"runs": [r.raw for r in runs]}

    def tool_topup_link(self, a: dict[str, Any], progress: Callable) -> dict[str, Any]:
        amount = float(a.get("amount_usd") or 0)
        if amount < 0.5:
            return _err("bad_request", "amount_usd must be at least 0.50")
        url = self._client().checkout_url(amount)
        return {"checkout_url": url, "amount_usd": amount,
                "note": "Open this in a browser and pay there. Nothing was charged by creating it."}

    # --- security-log triage -------------------------------------------------------------

    def _load_lines(self, a: dict[str, Any]) -> list[tuple[str, Any]]:
        if a.get("lines"):
            return [(m, e) for m, e in parse_text("\n".join(str(x) for x in a["lines"]))]
        path = a.get("path")
        if not path:
            raise BudgetError("pass path (a local JSONL/JSON/text file) or lines: [string, ...]")
        path = os.path.expanduser(str(path))
        if not os.path.isfile(path):
            raise BudgetError(f"no such file: {path}")
        return list(read_entries(path))

    def tool_triage(self, a: dict[str, Any], progress: Callable) -> dict[str, Any]:
        entries = self._load_lines(a)
        if not entries:
            return _err("empty", "no log lines found on input")
        max_lines = int(a.get("max_lines") or 5000)
        if len(entries) > max_lines:
            return _err("too_many_lines",
                        f"{len(entries)} lines exceed max_lines={max_lines}; raise max_lines "
                        "(one call holds up to 5000 per chunk) or use the monitor CLI for bulk sweeps",
                        lines=len(entries))
        min_sev = str(a.get("min_severity") or "LOW").upper()
        if min_sev not in SEVERITY_RANK:
            return _err("bad_request", f"min_severity must be one of {', '.join(SEVERITY_RANK)}")
        budget = self._budget(a.get("max_price_usd"))
        spot = bool(a.get("spot", False))
        trust = self._trust_arg(a.get("trust"))
        chunk = max(1, min(int(a.get("chunk") or 500), MAX_ITEMS_PER_WORKLOAD))
        prompts_all = [TRIAGE_TEMPLATE.format(input=msg) for msg, _ in entries]
        avg_tokens = max(1, sum(len(p) for p in prompts_all) // max(1, len(prompts_all)) // 4)

        if not a.get("confirm", False):
            q = self._client(trust=trust, keyless=True).estimate(
                items=len(entries), avg_input_tokens=avg_tokens, avg_output_tokens=TRIAGE_MAX_TOKENS,
                spot=spot)
            v = _quote_view(q)
            v.pop("quote_id", None)
            return {
                "dry_run": True,
                "lines": len(entries),
                "chunks": (len(entries) + chunk - 1) // chunk,
                "estimate": v,
                "within_budget": q.total_usd <= budget,
                "max_price_usd": budget,
                "note": ("No log content was sent; this is priced from counts. Call again with "
                         "confirm=true to run. Quality caveat: run ayni_triage_selftest first."),
            }

        client = self._client(trust=trust)
        findings: list[dict[str, Any]] = []
        by_sev: dict[str, int] = {k: 0 for k in SEVERITY_RANK}
        by_cat: dict[str, int] = {}
        spent = 0.0
        ok = failed = 0
        chunks_run = 0
        stopped = ""
        out_path = a.get("findings_path")
        fh = open(os.path.expanduser(str(out_path)), "w", encoding="utf-8") if out_path else None
        try:
            for start in range(0, len(entries), chunk):
                part = entries[start: start + chunk]
                prompts = prompts_all[start: start + chunk]
                remaining = budget - spent
                try:
                    res = client.run(prompts=prompts, max_tokens=TRIAGE_MAX_TOKENS, spot=spot,
                                     max_price_usd=remaining)
                except AyniError as e:
                    stopped = f"chunk {chunks_run + 1} not run ({e.code or e.status}): {e}"
                    break
                chunks_run += 1
                spent += res.charged_usd
                ok += res.ok_count
                failed += res.failed_count
                for item, (msg, entry) in zip(res.items, part):
                    if not item.ok:
                        continue
                    sev, cat, reason = parse_verdict(item.content, msg)
                    by_sev[sev] += 1
                    if SEVERITY_RANK[sev] < SEVERITY_RANK[min_sev]:
                        continue
                    by_cat[cat] = by_cat.get(cat, 0) + 1
                    f = {
                        "severity": sev, "category": cat, "reason": reason, "log": msg,
                        "timestamp": entry.get("timestamp") if isinstance(entry, dict) else None,
                        "insertId": entry.get("insertId") if isinstance(entry, dict) else None,
                        "served_by": item.provider, "trust_level": item.trust_level,
                    }
                    findings.append(f)
                    if fh:
                        fh.write(json.dumps(f) + "\n")
                progress(min(start + chunk, len(entries)), len(entries),
                         f"{min(start + chunk, len(entries))}/{len(entries)} lines, ${spent:.4f}")
        finally:
            if fh:
                fh.close()
        findings.sort(key=lambda f: -SEVERITY_RANK[f["severity"]])
        max_findings = int(a.get("max_findings") or 100)
        return {
            "dry_run": False,
            "lines": len(entries),
            "chunks_run": chunks_run,
            "items_ok": ok,
            "items_failed": failed,
            "charged_usd": round(spent, 6),
            "max_price_usd": budget,
            "min_severity": min_sev,
            "findings_count": len(findings),
            "by_severity": by_sev,
            "by_category": by_cat,
            "findings": findings[:max_findings],
            "findings_truncated": len(findings) > max_findings,
            "findings_path": os.path.expanduser(str(out_path)) if out_path else None,
            "stopped": stopped or None,
            "quality_note": ("Verdicts come from the small model in production; measured on labelled "
                             "lines it is a volume filter at LOW, not a classifier. See ayni_triage_selftest."),
        }

    def tool_selftest(self, a: dict[str, Any], progress: Callable) -> dict[str, Any]:
        budget = self._budget(a.get("max_price_usd", 0.25))
        min_sev = str(a.get("min_severity") or "LOW").upper()
        if min_sev not in SEVERITY_RANK:
            return _err("bad_request", f"min_severity must be one of {', '.join(SEVERITY_RANK)}")
        rows = LABELLED_SAMPLE
        res = self._client(trust=self._trust_arg(a.get("trust"))).run(
            prompts=[TRIAGE_TEMPLATE.format(input=r["log"]) for r in rows],
            max_tokens=TRIAGE_MAX_TOKENS, max_price_usd=budget,
        )
        verdicts = [parse_verdict(out, row["log"])[0] for out, row in zip(res.contents(), rows)]
        sc = score(verdicts, rows, min_sev)
        sc["charged_usd"] = res.charged_usd
        sc["answers"] = sorted(set(verdicts))
        sc["model"] = self.model
        return sc

    # --- MCP surface -----------------------------------------------------------------------

    def tools(self) -> list[dict[str, Any]]:
        ro = {"readOnlyHint": True, "destructiveHint": False, "idempotentHint": True, "openWorldHint": True}
        spend = {"readOnlyHint": False, "destructiveHint": False, "idempotentHint": False, "openWorldHint": True}
        trust_p = {"type": "string", "enum": list(TRUST_LEVELS),
                   "description": "Hardware trust floor. device_attested x1.4 price, confidential x3.0."}
        return [
            {"name": "ayni_status", "title": "Ayni status",
             "description": "Check the coordinator, the API key, prepaid credit, served models and how many devices are online. Call this first.",
             "inputSchema": {"type": "object", "properties": {}, "additionalProperties": False},
             "annotations": ro},
            {"name": "ayni_estimate", "title": "Estimate a workload from counts",
             "description": "Price a hypothetical workload from counts only. No content is sent and no API key is needed. Use it to compare against a current bill before moving anything.",
             "inputSchema": {"type": "object", "required": ["items"], "properties": {
                 "items": {"type": "integer", "minimum": 1, "description": "Number of prompts."},
                 "avg_input_tokens": {"type": "integer", "default": 250},
                 "avg_output_tokens": {"type": "integer", "default": 64},
                 "model": {"type": "string"},
                 "redundancy": {"type": "integer", "minimum": 1, "maximum": 3, "default": 1,
                                "description": "2 or 3 runs each item on that many devices and majority-votes."},
                 "spot": {"type": "boolean", "default": False, "description": "60% price, best-effort ETA."},
                 "trust": trust_p,
             }, "additionalProperties": False},
             "annotations": ro},
            {"name": "ayni_quote", "title": "Quote real items",
             "description": "Price a real workload: the exact prompts, one itemised price, an ETA and the Council's decision. Nothing runs and nothing is stored. The quote expires in about ten minutes and is single-use. Free.",
             "inputSchema": {"type": "object", "properties": {
                 "prompts": {"type": "array", "items": {"type": "string"}, "maxItems": MAX_ITEMS_PER_WORKLOAD,
                             "description": "One user prompt per item."},
                 "items": {"type": "array", "items": {"type": "object"},
                           "description": "Alternative to prompts: OpenAI-style {messages:[...], max_tokens} objects."},
                 "model": {"type": "string"},
                 "max_tokens": {"type": "integer", "minimum": 1, "description": "Completion cap per item; priced as if fully used."},
                 "redundancy": {"type": "integer", "minimum": 1, "maximum": 3, "default": 1},
                 "spot": {"type": "boolean", "default": False},
                 "trust": trust_p,
             }, "additionalProperties": False},
             "annotations": ro},
            {"name": "ayni_run", "title": "Run a quoted workload",
             "description": "Accept a quote and run it. Spends credit: charged once at the quoted price (nothing if every item fails). Refuses if the quote is above max_price_usd. mode sync returns the results; mode async returns a run id to poll.",
             "inputSchema": {"type": "object", "required": ["quote_id", "max_price_usd"], "properties": {
                 "quote_id": {"type": "string"},
                 "max_price_usd": {"type": "number", "exclusiveMinimum": 0,
                                   "description": "Refuse to run if the quote is above this. Tell the user the quoted price first."},
                 "mode": {"type": "string", "enum": ["sync", "async"], "default": "sync"},
                 "label": {"type": "string", "description": "async only: a name shown in run listings."},
                 "webhook_url": {"type": "string", "description": "async only: public https URL called when the run finishes (content-free, signed)."},
                 "limit": {"type": "integer", "default": 200, "description": "sync only: items to include in the reply."},
             }, "additionalProperties": False},
             "annotations": spend},
            {"name": "ayni_run_status", "title": "Run status",
             "description": "Progress of an asynchronous run: status, done/ok/failed counts, charged amount, whether results are available.",
             "inputSchema": {"type": "object", "required": ["run_id"], "properties": {"run_id": {"type": "string"}},
                             "additionalProperties": False},
             "annotations": ro},
            {"name": "ayni_wait", "title": "Wait for a run",
             "description": "Block up to timeout_seconds (max 120) until an asynchronous run finishes, reporting progress. Call again if it is still running.",
             "inputSchema": {"type": "object", "required": ["run_id"], "properties": {
                 "run_id": {"type": "string"},
                 "timeout_seconds": {"type": "number", "default": 60, "maximum": 120}},
                 "additionalProperties": False},
             "annotations": ro},
            {"name": "ayni_results", "title": "Collect results",
             "description": "Fetch a finished run's outputs in submission order. Results are held in coordinator memory for a bounded window (default one hour) and never written to disk: collect them promptly.",
             "inputSchema": {"type": "object", "required": ["run_id"], "properties": {
                 "run_id": {"type": "string"},
                 "offset": {"type": "integer", "minimum": 0, "default": 0},
                 "limit": {"type": "integer", "minimum": 1, "default": 200},
                 "include_failed": {"type": "boolean", "default": True}},
                 "additionalProperties": False},
             "annotations": ro},
            {"name": "ayni_list_runs", "title": "List runs",
             "description": "This key's most recent asynchronous runs, newest first (metadata only).",
             "inputSchema": {"type": "object", "properties": {"limit": {"type": "integer", "default": 20, "maximum": 100}},
                             "additionalProperties": False},
             "annotations": ro},
            {"name": "ayni_topup_link", "title": "Create a top-up link",
             "description": "Create a Stripe Checkout link to add prepaid credit to this key. Nothing is charged by creating it; a person opens the link and pays.",
             "inputSchema": {"type": "object", "required": ["amount_usd"], "properties": {
                 "amount_usd": {"type": "number", "minimum": 0.5}}, "additionalProperties": False},
             "annotations": {"readOnlyHint": False, "destructiveHint": False, "idempotentHint": False, "openWorldHint": True}},
            {"name": "ayni_triage_logs", "title": "Triage security logs",
             "description": "Security-log triage on Ayni. Reads a local file (Cloud Logging JSON export, JSONL or plain text) or inline lines, asks the network for a one-word severity per line, derives a category deterministically, and returns findings at or above min_severity. Without confirm=true it only prices the job from counts (no log content leaves the machine). Spends credit when confirm=true, never above max_price_usd. Run ayni_triage_selftest first: the production model is small and its measured quality is a volume filter, not a classifier.",
             "inputSchema": {"type": "object", "required": ["max_price_usd"], "properties": {
                 "path": {"type": "string", "description": "Local file: JSONL, a JSON array (gcloud logging read --format=json) or plain text."},
                 "lines": {"type": "array", "items": {"type": "string"}, "description": "Inline log lines instead of a path."},
                 "max_price_usd": {"type": "number", "exclusiveMinimum": 0, "description": "Total budget for this call."},
                 "confirm": {"type": "boolean", "default": False, "description": "false = price only; true = run and spend."},
                 "min_severity": {"type": "string", "enum": list(SEVERITY_RANK), "default": "LOW"},
                 "spot": {"type": "boolean", "default": False},
                 "trust": trust_p,
                 "chunk": {"type": "integer", "default": 500, "maximum": MAX_ITEMS_PER_WORKLOAD, "description": "Lines per workload."},
                 "max_lines": {"type": "integer", "default": 5000},
                 "max_findings": {"type": "integer", "default": 100, "description": "Findings included in the reply (all are written to findings_path)."},
                 "findings_path": {"type": "string", "description": "Optional local JSONL file to write every finding to, for a SIEM."},
             }, "additionalProperties": False},
             "annotations": spend},
            {"name": "ayni_triage_selftest", "title": "Measure triage quality",
             "description": "Run twelve labelled log lines through the network and report recall, precision and whether the model is fit for triage. Costs about a cent. Do this before trusting ayni_triage_logs.",
             "inputSchema": {"type": "object", "properties": {
                 "max_price_usd": {"type": "number", "default": 0.25},
                 "min_severity": {"type": "string", "enum": list(SEVERITY_RANK), "default": "LOW"},
                 "trust": trust_p}, "additionalProperties": False},
             "annotations": spend},
        ]

    def resources(self) -> list[dict[str, Any]]:
        out = [{"uri": "ayni://guide/quickstart", "name": "Ayni quickstart", "mimeType": "text/markdown",
                "description": "The tool order, pricing rules and the results-in-memory rule."}]
        for name, rel in (("workload-runners", "WORKLOAD-RUNNERS.md"), ("mcp-server", "MCP-SERVER.md")):
            if self._repo_doc(rel):
                out.append({"uri": f"ayni://docs/{name}", "name": f"docs/{rel}", "mimeType": "text/markdown",
                            "description": f"Repository guide {rel}."})
        return out

    @staticmethod
    def _repo_doc(rel: str) -> str | None:
        p = os.path.join(os.path.dirname(__file__), "..", "..", "..", "docs", rel)
        return os.path.abspath(p) if os.path.isfile(p) else None

    def read_resource(self, uri: str) -> dict[str, Any]:
        if uri == "ayni://guide/quickstart":
            return {"contents": [{"uri": uri, "mimeType": "text/markdown", "text": QUICKSTART}]}
        if uri.startswith("ayni://docs/"):
            rel = {"workload-runners": "WORKLOAD-RUNNERS.md", "mcp-server": "MCP-SERVER.md"}.get(uri.split("/")[-1])
            p = self._repo_doc(rel) if rel else None
            if p:
                with open(p, encoding="utf-8") as f:
                    return {"contents": [{"uri": uri, "mimeType": "text/markdown", "text": f.read()}]}
        raise KeyError(uri)

    def prompts(self) -> list[dict[str, Any]]:
        return [
            {"name": "triage_security_logs", "title": "Triage security logs on Ayni",
             "description": "Guided workflow: check status, measure model fitness, price the file, confirm, run, summarise findings.",
             "arguments": [
                 {"name": "path", "description": "Local log file (JSONL, JSON array or text)", "required": True},
                 {"name": "budget_usd", "description": "Maximum spend for this triage", "required": False},
                 {"name": "min_severity", "description": "LOW, MEDIUM, HIGH or CRITICAL", "required": False}]},
            {"name": "price_a_migration", "title": "Price moving a batch job to Ayni",
             "description": "Estimate what an existing batch job would cost on Ayni, from counts only.",
             "arguments": [
                 {"name": "items_per_month", "description": "Prompts per month", "required": True},
                 {"name": "avg_input_tokens", "description": "Average prompt length in tokens", "required": False},
                 {"name": "avg_output_tokens", "description": "Average completion length in tokens", "required": False}]},
        ]

    def get_prompt(self, name: str, args: dict[str, Any]) -> dict[str, Any]:
        if name == "triage_security_logs":
            path = args.get("path", "<path>")
            budget = args.get("budget_usd", "2")
            sev = args.get("min_severity", "LOW")
            text = (
                f"Triage the security logs in {path} on the Ayni network.\n\n"
                "Steps:\n"
                "1. Call ayni_status. Stop and tell me if the key is invalid, credit is zero, or no device is online.\n"
                f"2. Call ayni_triage_selftest (max_price_usd 0.25). Report recall, precision and the verdict plainly.\n"
                f"3. Call ayni_triage_logs with path={path}, min_severity={sev}, max_price_usd={budget}, confirm=false. "
                "Show me the line count, the price and the ETA.\n"
                "4. Ask me to confirm the price. Only after I say yes, call it again with confirm=true and a "
                "findings_path next to the input file.\n"
                "5. Summarise: counts by severity and category, the top findings with their log lines, what it cost, "
                "and the quality caveat from the selftest.\n"
                "Never spend above the budget, and never call a spending tool without showing me the price first."
            )
            return {"description": "Guided security-log triage", "messages": [{"role": "user", "content": {"type": "text", "text": text}}]}
        if name == "price_a_migration":
            n = args.get("items_per_month", "100000")
            text = (
                f"Estimate the monthly cost of running {n} prompts on Ayni "
                f"(avg_input_tokens {args.get('avg_input_tokens', 250)}, avg_output_tokens {args.get('avg_output_tokens', 64)}). "
                "Call ayni_estimate for on-demand and again with spot=true, then present both prices with the "
                "breakdown, the per-item price, the ETA, and whether supply was online when priced. "
                "Say clearly that the estimate assumes about four characters per token and prices the "
                "completion cap, not actual output."
            )
            return {"description": "Price a migration", "messages": [{"role": "user", "content": {"type": "text", "text": text}}]}
        raise KeyError(name)

    # --- JSON-RPC plumbing -----------------------------------------------------------------

    def _send(self, msg: dict[str, Any]) -> None:
        line = json.dumps(msg, separators=(",", ":"), ensure_ascii=False)
        with self._out_lock:
            sys.stdout.write(line + "\n")
            sys.stdout.flush()

    def _call_tool(self, name: str, args: dict[str, Any], progress_token: Any) -> dict[str, Any]:
        handlers: dict[str, Callable[[dict[str, Any], Callable], dict[str, Any]]] = {
            "ayni_status": self.tool_status,
            "ayni_estimate": self.tool_estimate,
            "ayni_quote": self.tool_quote,
            "ayni_run": self.tool_run,
            "ayni_run_status": self.tool_run_status,
            "ayni_wait": self.tool_wait,
            "ayni_results": self.tool_results,
            "ayni_list_runs": self.tool_list_runs,
            "ayni_topup_link": self.tool_topup_link,
            "ayni_triage_logs": self.tool_triage,
            "ayni_triage_selftest": self.tool_selftest,
        }
        h = handlers.get(name)
        if h is None:
            raise KeyError(name)

        def progress(done: int, total: int, message: str = "") -> None:
            if progress_token is None:
                return
            self._send({"jsonrpc": "2.0", "method": "notifications/progress",
                        "params": {"progressToken": progress_token, "progress": done, "total": total,
                                   "message": message}})

        try:
            payload = h(args or {}, progress)
            is_error = "error" in payload and "message" in payload and len(payload) <= 5
        except BudgetError as e:
            payload, is_error = _err("bad_request", str(e)), True
        except AyniError as e:
            payload, is_error = _api_error(e), True
        except (OSError, ValueError) as e:
            payload, is_error = _err("local_error", str(e)), True
        text = json.dumps(payload, indent=2, ensure_ascii=False)
        return {"content": [{"type": "text", "text": text}], "structuredContent": payload, "isError": is_error}

    def handle(self, req: dict[str, Any]) -> dict[str, Any] | None:
        method = req.get("method")
        rid = req.get("id")
        params = req.get("params") or {}
        is_notification = "id" not in req

        def ok(result: Any) -> dict[str, Any]:
            return {"jsonrpc": "2.0", "id": rid, "result": result}

        def err(code: int, message: str) -> dict[str, Any] | None:
            if is_notification:
                return None
            return {"jsonrpc": "2.0", "id": rid, "error": {"code": code, "message": message}}

        try:
            if method == "initialize":
                want = params.get("protocolVersion")
                self._client_protocol = want if want in SUPPORTED_PROTOCOLS else DEFAULT_PROTOCOL
                return ok({
                    "protocolVersion": self._client_protocol,
                    "capabilities": {"tools": {"listChanged": False}, "resources": {"subscribe": False, "listChanged": False},
                                     "prompts": {"listChanged": False}},
                    "serverInfo": {"name": "ayni", "title": "Ayni workloads", "version": __version__},
                    "instructions": INSTRUCTIONS,
                })
            if method in ("notifications/initialized", "notifications/cancelled", "notifications/roots/list_changed"):
                return None
            if method == "ping":
                return ok({})
            if method == "tools/list":
                return ok({"tools": self.tools()})
            if method == "tools/call":
                name = params.get("name", "")
                token = (params.get("_meta") or {}).get("progressToken")
                try:
                    return ok(self._call_tool(name, params.get("arguments") or {}, token))
                except KeyError:
                    return err(-32602, f"unknown tool: {name}")
            if method == "resources/list":
                return ok({"resources": self.resources()})
            if method == "resources/templates/list":
                return ok({"resourceTemplates": []})
            if method == "resources/read":
                try:
                    return ok(self.read_resource(str(params.get("uri", ""))))
                except KeyError:
                    return err(-32002, f"resource not found: {params.get('uri')}")
            if method == "prompts/list":
                return ok({"prompts": self.prompts()})
            if method == "prompts/get":
                try:
                    return ok(self.get_prompt(str(params.get("name", "")), params.get("arguments") or {}))
                except KeyError:
                    return err(-32602, f"unknown prompt: {params.get('name')}")
            return err(-32601, f"method not found: {method}")
        except Exception as e:  # noqa: BLE001 — a bug must not kill the transport
            _log(f"internal error in {method}: {e!r}")
            return err(-32603, f"internal error: {e}")

    def serve(self) -> int:
        _log(f"ready (base {self.base_url}, model {self.model}, cap ${self.hard_cap:.2f}, key {'set' if self._key else 'missing'})")
        for raw in sys.stdin:
            raw = raw.strip()
            if not raw:
                continue
            try:
                msg = json.loads(raw)
            except json.JSONDecodeError:
                self._send({"jsonrpc": "2.0", "id": None, "error": {"code": -32700, "message": "parse error"}})
                continue
            if isinstance(msg, list):  # JSON-RPC batch (allowed by 2025-03-26 clients)
                replies = [r for r in (self.handle(m) for m in msg if isinstance(m, dict)) if r is not None]
                if replies:
                    self._send_batch(replies)
                continue
            if not isinstance(msg, dict):
                continue
            # Long tool calls run on a thread so pings and cancellations still get answered.
            if msg.get("method") == "tools/call":
                threading.Thread(target=self._handle_and_send, args=(msg,), daemon=True).start()
            else:
                self._handle_and_send(msg)
        return 0

    def _send_batch(self, replies: list[dict[str, Any]]) -> None:
        line = json.dumps(replies, separators=(",", ":"), ensure_ascii=False)
        with self._out_lock:
            sys.stdout.write(line + "\n")
            sys.stdout.flush()

    def _handle_and_send(self, msg: dict[str, Any]) -> None:
        reply = self.handle(msg)
        if reply is not None:
            self._send(reply)


# --------------------------------------------------------------------------------------
# entry point
# --------------------------------------------------------------------------------------

def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(prog="ayni-mcp", description="Ayni MCP server (stdio).")
    ap.add_argument("--base-url", default=os.environ.get("AYNI_BASE_URL", "https://api.ayni-ai.com"))
    ap.add_argument("--model", default=os.environ.get("AYNI_MODEL", "qwen2.5-0.5b-instruct-q4_k_m"))
    ap.add_argument("--trust", default=os.environ.get("AYNI_TRUST") or None, choices=[None, *TRUST_LEVELS])
    ap.add_argument("--max-price-usd", type=float, default=float(os.environ.get("AYNI_MAX_PRICE_USD", HARD_CAP_DEFAULT)),
                    help="hard cap per spending call; arguments cannot exceed it")
    ap.add_argument("--selfcheck", action="store_true", help="print ayni_status as JSON and exit")
    args = ap.parse_args(argv)

    key = os.environ.get("AYNI_API_KEY") or None
    srv = AyniMCP(api_key=key, base_url=args.base_url.rstrip("/"), model=args.model, trust=args.trust,
                  hard_cap_usd=args.max_price_usd)
    if args.selfcheck:
        print(json.dumps(srv.tool_status({}, lambda *a: None), indent=2))
        return 0
    if not key:
        _log("AYNI_API_KEY is not set: estimates work, everything else will return 401")
    try:
        return srv.serve()
    except KeyboardInterrupt:
        return 130


if __name__ == "__main__":
    sys.exit(main())
