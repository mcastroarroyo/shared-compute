"""
End-to-end test of the MCP server over stdio against a fake coordinator.

Runs with the standard library only:  python3 -m unittest clients/python/tests/test_mcp.py
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

HERE = os.path.dirname(os.path.abspath(__file__))
PKG = os.path.abspath(os.path.join(HERE, ".."))
sys.path.insert(0, PKG)


class FakeCoordinator(BaseHTTPRequestHandler):
    """Just enough of the Ayni API for the server's tools, with the real response shapes."""

    quotes: dict[str, dict] = {}
    runs: dict[str, dict] = {}
    results: dict[str, dict] = {}
    credit_usd = 3.0
    calls: list[tuple[str, str, dict | None]] = []

    def log_message(self, *a):  # quiet
        pass

    def _json(self, code: int, body: dict) -> None:
        raw = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def _body(self) -> dict | None:
        n = int(self.headers.get("Content-Length") or 0)
        return json.loads(self.rfile.read(n)) if n else None

    def _authed(self) -> bool:
        return self.headers.get("Authorization") == "Bearer sc_test_key"

    @staticmethod
    def _price(items: int, prompt_tokens: int, completion_tokens: int, spot: bool) -> dict:
        per = (prompt_tokens * 0.02 + completion_tokens * 0.08) / 1_000_000
        compute = per * items
        coord, fail = compute * 0.15, compute * 0.08
        sub = compute + coord + fail
        margin, pay = sub * 0.30, sub * 0.03
        total = max(sub + margin + pay, 0.01) * (0.6 if spot else 1.0)
        return {"currency": "usd", "total_usd": round(total, 2), "breakdown_usd": {
            "compute_acquisition": round(compute, 4), "coordination": round(coord, 4),
            "expected_failure": round(fail, 4), "payment_processing": round(pay, 4),
            "ayni_margin": round(margin, 4)}}

    def _quote(self, body: dict, runnable: bool) -> dict:
        qid = f"wl_{len(self.quotes) + 1:04d}"
        if runnable:
            items = body.get("items") or []
            n = len(items)
            ptoks = sum(len(m["content"]) for it in items for m in it["messages"]) // 4 // max(n, 1)
            ctoks = items[0].get("max_tokens", 512) if items else 512
        else:
            est = body["estimate"]
            n, ptoks, ctoks = est["count"], est["avg_prompt_tokens"], est["avg_completion_tokens"]
        q = {
            "id": qid, "object": "workload.quote", "model": body.get("model"), "status": "quoted",
            "class": "MICRO", "spot": body.get("spot", False), "tier": body.get("tier", "community"),
            "redundancy": body.get("redundancy", 1), "runnable": runnable,
            "estimate": {"items": n, "prompt_tokens": n * ptoks, "completion_tokens": n * ctoks,
                         "eta_seconds": 3, "eligible_nodes": 2, "supply_online": True},
            "price": self._price(n, ptoks, ctoks, body.get("spot", False)),
            "council": {"proposal_id": qid, "risk_class": "standard", "decision": "APPROVE",
                        "reviews": [{"seat": "security_critic", "decision": "APPROVE"}], "audit_hash": "abc"},
            "created_at": "2026-09-07T00:00:00Z", "expires_at": "2026-09-07T00:10:00Z",
            "_items": n,
        }
        if runnable:
            q["accept_url"] = f"/v1/workloads/{qid}/accept"
        self.quotes[qid] = q
        return q

    @staticmethod
    def _public(q: dict) -> dict:
        return {k: v for k, v in q.items() if not k.startswith("_")}

    def do_GET(self):
        p = self.path
        self.calls.append(("GET", p, None))
        if p in ("/healthz", "/health"):
            return self._json(200, {"status": "ok"})
        if not self._authed():
            return self._json(401, {"error": {"code": "unauthorized", "message": "bad key"}})
        if p == "/v1/models":
            return self._json(200, {"object": "list", "data": [
                {"id": "qwen2.5-0.5b-instruct-q4_k_m", "hardware_class": "MICRO", "context_length": 4096}]})
        if p == "/billing/balance":
            return self._json(200, {"key_id": "key_test", "credit_micros": int(self.credit_usd * 1e6),
                                    "credit_usd": self.credit_usd, "enforced": True})
        if p.startswith("/v1/workloads/"):
            q = self.quotes.get(p.split("/")[3])
            if not q:
                return self._json(404, {"error": {"code": "not_found", "message": "no such workload quote (it may have expired)"}})
            return self._json(200, self._public(q))
        if p.startswith("/v1/runs?"):
            return self._json(200, {"object": "list", "data": list(self.runs.values())})
        if p.startswith("/v1/runs/") and p.endswith("/results"):
            rid = p.split("/")[3]
            if rid not in self.runs:
                return self._json(404, {"error": {"code": "not_found", "message": "no such run"}})
            if self.runs[rid]["status"] != "succeeded":
                return self._json(409, {"error": {"code": "not_finished", "message": "still running"}})
            return self._json(200, self.results[rid])
        if p.startswith("/v1/runs/"):
            r = self.runs.get(p.split("/")[3])
            return self._json(200, r) if r else self._json(404, {"error": {"code": "not_found", "message": "no such run"}})
        self._json(404, {"error": {"code": "not_found", "message": p}})

    def do_POST(self):
        p = self.path
        body = self._body()
        self.calls.append(("POST", p, body))
        if p == "/v1/quote":
            return self._json(200, self._public(self._quote(body, runnable=False)))
        if not self._authed():
            return self._json(401, {"error": {"code": "unauthorized", "message": "bad key"}})
        if p == "/v1/workloads":
            return self._json(200, self._public(self._quote(body, runnable=True)))
        if p == "/billing/checkout":
            return self._json(200, {"url": f"https://checkout.stripe.com/c/pay/test_{body['amount_usd']}", "session_id": "cs_test"})
        if p.startswith("/v1/workloads/") and p.endswith("/accept"):
            qid = p.split("/")[3]
            q = self.quotes.get(qid)
            if not q:
                return self._json(404, {"error": {"code": "not_found", "message": "no such workload quote (it may have expired)"}})
            if q.get("_accepted"):
                return self._json(409, {"error": {"code": "already_accepted", "message": "already accepted"}})
            if not q["runnable"]:
                return self._json(400, {"error": {"code": "estimate_only", "message": "estimate only"}})
            if q["price"]["total_usd"] > FakeCoordinator.credit_usd:
                return self._json(402, {"error": {"code": "insufficient_credit", "message": "add credit"}})
            q["_accepted"] = True
            FakeCoordinator.credit_usd = round(FakeCoordinator.credit_usd - q["price"]["total_usd"], 6)
            n = q["_items"]
            # Alternate HIGH/NONE so a triage over the fake looks like a classifier, not a constant.
            items = [{"index": i, "message": {"role": "assistant", "content": "HIGH" if i % 2 == 0 else "NONE"},
                      "provider": "fake-1", "trust_level": "community", "latency_ms": 12} for i in range(n)]
            result = {"id": qid, "object": "workload.result", "items": items,
                      "charged_usd": q["price"]["total_usd"], "quoted_usd": q["price"]["total_usd"],
                      "stats": {"ok": n, "failed": 0, "wall_ms": 40, "fanout": 2}}
            if body and body.get("async"):
                rid = f"run_{qid[3:]}"
                run = {"id": rid, "object": "workload.run", "workload_id": qid, "status": "succeeded",
                       "progress": {"total": n, "done": n, "ok": n, "failed": 0},
                       "charged_usd": q["price"]["total_usd"], "results_available": True,
                       "poll_url": f"/v1/runs/{rid}", "results_url": f"/v1/runs/{rid}/results"}
                if body.get("webhook_url"):
                    run["webhook_secret"] = "whsec_test"
                self.runs[rid] = run
                result["id"] = rid
                self.results[rid] = result
                return self._json(202, {**run, "status": "queued"})
            return self._json(200, result)
        self._json(404, {"error": {"code": "not_found", "message": p}})


class MCPClient:
    """Minimal stdio client: one request, one response, by id."""

    def __init__(self, env: dict[str, str]):
        self.proc = subprocess.Popen(
            [sys.executable, "-m", "ayni.mcp"], cwd=PKG, env=env, text=True, bufsize=1,
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        )
        self.n = 0
        self.notifications: list[dict] = []

    def request(self, method: str, params: dict | None = None) -> dict:
        self.n += 1
        rid = self.n
        self.proc.stdin.write(json.dumps({"jsonrpc": "2.0", "id": rid, "method": method, "params": params or {}}) + "\n")
        self.proc.stdin.flush()
        while True:
            line = self.proc.stdout.readline()
            if not line:
                raise RuntimeError("server closed stdout: " + self.proc.stderr.read())
            msg = json.loads(line)
            if "id" in msg and msg["id"] == rid:
                return msg
            self.notifications.append(msg)

    def notify(self, method: str, params: dict | None = None) -> None:
        self.proc.stdin.write(json.dumps({"jsonrpc": "2.0", "method": method, "params": params or {}}) + "\n")
        self.proc.stdin.flush()

    def call(self, name: str, **args) -> dict:
        r = self.request("tools/call", {"name": name, "arguments": args, "_meta": {"progressToken": f"pt{self.n}"}})
        assert "result" in r, r
        return r["result"]

    def close(self) -> None:
        self.proc.stdin.close()
        self.proc.wait(timeout=10)
        self.proc.stdout.close()
        self.proc.stderr.close()


class TestMCPServer(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.httpd = ThreadingHTTPServer(("127.0.0.1", 0), FakeCoordinator)
        cls.port = cls.httpd.server_address[1]
        threading.Thread(target=cls.httpd.serve_forever, daemon=True).start()
        cls.base = f"http://127.0.0.1:{cls.port}"

    @classmethod
    def tearDownClass(cls):
        cls.httpd.shutdown()

    def env(self, **extra) -> dict[str, str]:
        e = {"PATH": os.environ.get("PATH", ""), "AYNI_API_KEY": "sc_test_key", "AYNI_BASE_URL": self.base,
             "AYNI_MAX_PRICE_USD": "1.00", "PYTHONPATH": PKG}
        e.update(extra)
        return e

    def start(self, **extra) -> MCPClient:
        c = MCPClient(self.env(**extra))
        init = c.request("initialize", {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "test", "version": "0"}})
        self.assertEqual(init["result"]["protocolVersion"], "2025-06-18")
        self.assertIn("tools", init["result"]["capabilities"])
        c.notify("notifications/initialized")
        self.addCleanup(c.close)
        return c

    def test_handshake_lists_and_ping(self):
        c = self.start()
        self.assertEqual(c.request("ping")["result"], {})
        tools = c.request("tools/list")["result"]["tools"]
        names = {t["name"] for t in tools}
        self.assertEqual(names, {"ayni_status", "ayni_estimate", "ayni_quote", "ayni_run", "ayni_run_status",
                                 "ayni_wait", "ayni_results", "ayni_list_runs", "ayni_topup_link",
                                 "ayni_triage_logs", "ayni_triage_selftest"})
        for t in tools:
            self.assertEqual(t["inputSchema"]["type"], "object")
            self.assertIn("annotations", t)
        run_tool = next(t for t in tools if t["name"] == "ayni_run")
        self.assertIn("max_price_usd", run_tool["inputSchema"]["required"])
        self.assertFalse(run_tool["annotations"]["readOnlyHint"])
        self.assertIn("ayni://guide/quickstart", [r["uri"] for r in c.request("resources/list")["result"]["resources"]])
        text = c.request("resources/read", {"uri": "ayni://guide/quickstart"})["result"]["contents"][0]["text"]
        self.assertIn("ayni_run", text)
        prompts = {p["name"] for p in c.request("prompts/list")["result"]["prompts"]}
        self.assertIn("triage_security_logs", prompts)
        got = c.request("prompts/get", {"name": "triage_security_logs", "arguments": {"path": "/tmp/x.jsonl", "budget_usd": "3"}})
        self.assertIn("/tmp/x.jsonl", got["result"]["messages"][0]["content"]["text"])
        self.assertEqual(c.request("nope")["error"]["code"], -32601)

    def test_status_estimate_quote_run_sync(self):
        c = self.start()
        st = c.call("ayni_status")["structuredContent"]
        self.assertEqual(st["key_id"], "key_test")
        self.assertTrue(st["key_configured"])
        self.assertEqual(st["supply"]["eligible_nodes"], 2)
        self.assertNotIn("sc_test_key", json.dumps(st))

        est = c.call("ayni_estimate", items=50_000, avg_input_tokens=200, avg_output_tokens=6)["structuredContent"]
        self.assertTrue(est["estimate_only"])
        self.assertGreater(est["price_usd"], 0)
        self.assertEqual(est["scaled_to_items"], 50_000)
        self.assertEqual(est["tokens_per_item"], 206)
        self.assertEqual(est["tokens_priced"], 50_000 * 206)
        self.assertAlmostEqual(est["price_per_1m_tokens_usd"], est["price_usd"] / (50_000 * 206) * 1e6, places=3)

        q = c.call("ayni_quote", prompts=["a", "b", "c"], max_tokens=6)["structuredContent"]
        self.assertTrue(q["quote_id"].startswith("wl_"))
        self.assertEqual(q["council"]["decision"], "APPROVE")
        self.assertEqual(q["items"], 3)
        self.assertGreater(q["tokens_priced"], 0)
        self.assertGreater(q["price_per_1m_tokens_usd"], 0)

        over = c.call("ayni_run", quote_id=q["quote_id"], max_price_usd=0.001)
        self.assertTrue(over["isError"])
        self.assertEqual(over["structuredContent"]["error"], "over_budget")

        cap = c.call("ayni_run", quote_id=q["quote_id"], max_price_usd=5.0)
        self.assertTrue(cap["isError"])
        self.assertIn("hard cap", cap["structuredContent"]["message"])

        res = c.call("ayni_run", quote_id=q["quote_id"], max_price_usd=0.5)
        self.assertFalse(res["isError"], res)
        sc = res["structuredContent"]
        self.assertEqual(sc["items_ok"], 3)
        self.assertEqual([i["content"] for i in sc["items"]], ["HIGH", "NONE", "HIGH"])

        again = c.call("ayni_run", quote_id=q["quote_id"], max_price_usd=0.5)
        self.assertTrue(again["isError"])
        self.assertEqual(again["structuredContent"]["error"], "already_accepted")

        gone = c.call("ayni_run", quote_id="wl_nope", max_price_usd=0.5)["structuredContent"]
        self.assertEqual(gone["error"], "not_found")
        self.assertIn("expire", gone["hint"])

    def test_async_run_wait_results_list(self):
        c = self.start()
        q = c.call("ayni_quote", prompts=["x"] * 4)["structuredContent"]
        run = c.call("ayni_run", quote_id=q["quote_id"], max_price_usd=0.5, mode="async",
                     webhook_url="https://ops.example.com/hook", label="t")["structuredContent"]
        self.assertTrue(run["run_id"].startswith("run_"))
        self.assertEqual(run["webhook_secret"], "whsec_test")
        st = c.call("ayni_run_status", run_id=run["run_id"])["structuredContent"]
        self.assertEqual(st["status"], "succeeded")
        w = c.call("ayni_wait", run_id=run["run_id"], timeout_seconds=5)["structuredContent"]
        self.assertTrue(w["finished"])
        r = c.call("ayni_results", run_id=run["run_id"], limit=2)["structuredContent"]
        self.assertEqual(r["returned"], 2)
        self.assertTrue(r["truncated"])
        self.assertEqual(r["items_total"], 4)
        runs = c.call("ayni_list_runs")["structuredContent"]["runs"]
        self.assertTrue(any(x["id"] == run["run_id"] for x in runs))

    def test_estimate_only_quote_cannot_run_and_topup_and_credit(self):
        c = self.start()
        link = c.call("ayni_topup_link", amount_usd=5)["structuredContent"]
        self.assertTrue(link["checkout_url"].startswith("https://checkout.stripe.com/"))
        FakeCoordinator.credit_usd = 0.0
        try:
            q = c.call("ayni_quote", prompts=["p"])["structuredContent"]
            r = c.call("ayni_run", quote_id=q["quote_id"], max_price_usd=0.5)
            self.assertTrue(r["isError"])
            self.assertEqual(r["structuredContent"]["error"], "insufficient_credit")
            self.assertIn("ayni_topup_link", r["structuredContent"]["hint"])
        finally:
            FakeCoordinator.credit_usd = 3.0

    def test_triage_dry_run_then_confirm_and_selftest(self):
        c = self.start()
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "logs.jsonl")
            rows = [
                {"textPayload": "sshd[1]: Failed password for invalid user admin from 203.0.113.9 port 2 ssh2"},
                {"jsonPayload": {"message": "GET /healthz 200 1.2ms"}},
                {"protoPayload": {"methodName": "storage.objects.get",
                                  "authenticationInfo": {"principalEmail": "contractor@external.example"},
                                  "resourceName": "projects/_/buckets/finance-exports/objects/payroll.csv"}},
                {"textPayload": "systemd[1]: Started Daily apt download activities."},
            ]
            with open(path, "w") as f:
                for r in rows:
                    f.write(json.dumps(r) + "\n")
            calls_before = len(FakeCoordinator.calls)
            dry = c.call("ayni_triage_logs", path=path, max_price_usd=0.5)["structuredContent"]
            self.assertTrue(dry["dry_run"])
            self.assertEqual(dry["lines"], 4)
            self.assertTrue(dry["within_budget"])
            # Dry run priced from counts only: the fake saw a /v1/quote with an estimate, no items.
            sent = [b for m, p, b in FakeCoordinator.calls[calls_before:] if p == "/v1/quote"]
            self.assertTrue(sent and "estimate" in sent[0] and "items" not in sent[0])

            out = os.path.join(d, "findings.jsonl")
            real = c.call("ayni_triage_logs", path=path, max_price_usd=0.5, confirm=True, findings_path=out)
            self.assertFalse(real["isError"], real)
            sc = real["structuredContent"]
            self.assertFalse(sc["dry_run"])
            self.assertEqual(sc["items_ok"], 4)
            self.assertGreater(sc["charged_usd"], 0)
            # fake answers HIGH for even indexes: lines 0 and 2 -> auth and exfiltration
            cats = {f["category"] for f in sc["findings"]}
            self.assertEqual(cats, {"auth", "exfiltration"})
            with open(out) as f:
                self.assertEqual(sum(1 for _ in f), sc["findings_count"])
            self.assertTrue(any(m.get("method") == "notifications/progress" for m in c.notifications))

        st = c.call("ayni_triage_selftest", max_price_usd=0.25)["structuredContent"]
        self.assertEqual(st["lines"], 12)
        self.assertIn("recall", st)
        self.assertIn("verdict", st)

    def test_no_key_estimate_still_works(self):
        c = self.start(AYNI_API_KEY="")
        est = c.call("ayni_estimate", items=10)["structuredContent"]
        self.assertGreater(est["price_usd"], 0)
        st = c.call("ayni_status")["structuredContent"]
        self.assertFalse(st["key_configured"])
        q = c.call("ayni_quote", prompts=["p"])
        self.assertTrue(q["isError"])
        self.assertIn("AYNI_API_KEY", q["structuredContent"]["hint"])


if __name__ == "__main__":
    unittest.main()
