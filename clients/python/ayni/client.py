"""Ayni API client for workload runners. Standard library only."""

from __future__ import annotations

import hashlib
import hmac
import json
import os
import time
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from typing import Any, Callable, Iterable, Sequence

DEFAULT_BASE = os.environ.get("AYNI_BASE_URL", "https://api.ayni-ai.com")
DEFAULT_MODEL = "qwen2.5-0.5b-instruct-q4_k_m"

# The coordinator caps a single workload; chunk above this and run the parts in order.
MAX_ITEMS_PER_WORKLOAD = 5000


class AyniError(RuntimeError):
    """An API call failed. `code` is Ayni's machine-readable error code."""

    def __init__(self, message: str, *, status: int = 0, code: str = "") -> None:
        super().__init__(message)
        self.status = status
        self.code = code


class InsufficientCredit(AyniError):
    """The quoted price exceeds the key's credit balance (HTTP 402)."""


class CouncilBlocked(AyniError):
    """The Ayni Council refused the workload on its shape, before it ran."""


@dataclass
class Quote:
    """A price and completion estimate. Nothing runs until you accept it."""

    id: str
    total_usd: float
    eta_seconds: int
    eligible_nodes: int
    council_decision: str
    raw: dict[str, Any] = field(repr=False, default_factory=dict)

    @property
    def breakdown_usd(self) -> dict[str, float]:
        return self.raw.get("price", {}).get("breakdown_usd", {})

    @property
    def supply_online(self) -> bool:
        return bool(self.raw.get("estimate", {}).get("supply_online", False))


@dataclass
class RunItem:
    """One completed item, in submission order."""

    index: int
    content: str
    provider: str = ""
    trust_level: str = ""
    latency_ms: int = 0
    error: str = ""

    @property
    def ok(self) -> bool:
        return not self.error


@dataclass
class RunResult:
    """Everything a finished workload returned."""

    id: str
    items: list[RunItem]
    charged_usd: float
    quoted_usd: float
    stats: dict[str, Any] = field(default_factory=dict)
    raw: dict[str, Any] = field(repr=False, default_factory=dict)

    @property
    def ok_count(self) -> int:
        return sum(1 for i in self.items if i.ok)

    @property
    def failed_count(self) -> int:
        return sum(1 for i in self.items if not i.ok)

    def contents(self) -> list[str]:
        """Just the text, in submission order. Failed items yield ''."""
        return [i.content for i in self.items]


@dataclass
class Run:
    """A queued or finished asynchronous run."""

    id: str
    status: str
    workload_id: str = ""
    total: int = 0
    done: int = 0
    ok: int = 0
    failed: int = 0
    charged_usd: float = 0.0
    error: str = ""
    webhook_secret: str = ""
    raw: dict[str, Any] = field(repr=False, default_factory=dict)

    @property
    def finished(self) -> bool:
        return self.status in ("succeeded", "failed")

    @property
    def results_available(self) -> bool:
        return bool(self.raw.get("results_available", False))


def _run_from(payload: dict[str, Any]) -> Run:
    p = payload.get("progress", {})
    return Run(
        id=payload.get("id", ""),
        status=payload.get("status", ""),
        workload_id=payload.get("workload_id", ""),
        total=p.get("total", 0),
        done=p.get("done", 0),
        ok=p.get("ok", 0),
        failed=p.get("failed", 0),
        charged_usd=payload.get("charged_usd", 0.0),
        error=payload.get("error", ""),
        webhook_secret=payload.get("webhook_secret", ""),
        raw=payload,
    )


def _result_from(payload: dict[str, Any]) -> RunResult:
    items = [
        RunItem(
            index=it.get("index", n),
            content=(it.get("message") or {}).get("content", "") or "",
            provider=it.get("provider", ""),
            trust_level=it.get("trust_level", ""),
            latency_ms=it.get("latency_ms", 0),
            error=it.get("error", "") or it.get("err_code", "") or "",
        )
        for n, it in enumerate(payload.get("items", []))
    ]
    return RunResult(
        id=payload.get("id", ""),
        items=items,
        charged_usd=payload.get("charged_usd", 0.0),
        quoted_usd=payload.get("quoted_usd", 0.0),
        stats=payload.get("stats", {}),
        raw=payload,
    )


def verify_webhook(body: bytes, signature_header: str, secret: str, *, tolerance: int = 300) -> bool:
    """Check an X-Ayni-Signature header against the run's webhook secret.

    The header is `t=<unix>,v1=<hex>`, signed over `"{t}.{body}"` with HMAC-SHA256,
    the same shape Stripe uses. `tolerance` bounds replay in seconds.
    """
    parts = dict(p.split("=", 1) for p in signature_header.split(",") if "=" in p)
    ts, sig = parts.get("t", ""), parts.get("v1", "")
    if not ts or not sig:
        return False
    try:
        if tolerance and abs(time.time() - int(ts)) > tolerance:
            return False
    except ValueError:
        return False
    expected = hmac.new(
        secret.encode(), f"{ts}.".encode() + body, hashlib.sha256
    ).hexdigest()
    return hmac.compare_digest(expected, sig)


class Ayni:
    """Client for the Ayni workload API.

    Args:
        api_key: consumer API key. Defaults to $AYNI_API_KEY.
        base_url: coordinator origin. Defaults to $AYNI_BASE_URL or production.
        model: model id used when a call does not name one.
        trust: optional hardware trust floor — "community", "device_attested"
            or "confidential". Requests fail with 409 if no such device is online.
        timeout: per-HTTP-call timeout in seconds.
    """

    def __init__(
        self,
        api_key: str | None = None,
        *,
        base_url: str = DEFAULT_BASE,
        model: str = DEFAULT_MODEL,
        trust: str | None = None,
        timeout: float = 60.0,
        max_retries: int = 4,
    ) -> None:
        # An empty key is allowed: `estimate()` calls the public quote endpoint, so a
        # runner can price the move before signing up. Authenticated calls then 401.
        self.api_key = api_key if api_key is not None else os.environ.get("AYNI_API_KEY", "")
        self.base_url = base_url.rstrip("/")
        self.model = model
        self.trust = trust
        self.timeout = timeout
        self.max_retries = max_retries

    # --- transport ---

    def _request(self, method: str, path: str, body: dict[str, Any] | None = None) -> dict[str, Any]:
        url = self.base_url + path
        data = json.dumps(body).encode() if body is not None else None
        headers = {"Content-Type": "application/json", "User-Agent": "ayni-python/0.1"}
        if self.api_key:
            headers["Authorization"] = f"Bearer {self.api_key}"
        if self.trust:
            headers["X-Provider-Trust-Level"] = self.trust

        delay = 1.0
        last: Exception | None = None
        for attempt in range(self.max_retries + 1):
            req = urllib.request.Request(url, data=data, headers=headers, method=method)
            try:
                with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                    raw = resp.read()
                    return json.loads(raw) if raw else {}
            except urllib.error.HTTPError as e:  # noqa: PERF203 — retry needs the handler
                payload = _safe_json(e.read())
                err = payload.get("error", {}) if isinstance(payload, dict) else {}
                code = err.get("code", "") if isinstance(err, dict) else ""
                msg = err.get("message", str(e)) if isinstance(err, dict) else str(e)

                if e.code == 402:
                    raise InsufficientCredit(msg, status=402, code=code or "insufficient_credit") from None
                if code == "council_blocked":
                    raise CouncilBlocked(msg, status=e.code, code=code) from None
                # 429 and 5xx are worth another go; everything else is the caller's problem.
                if e.code not in (429, 500, 502, 503, 504) or attempt == self.max_retries:
                    raise AyniError(msg, status=e.code, code=code) from None
                wait = float(e.headers.get("Retry-After") or delay)
                last = e
            except (urllib.error.URLError, TimeoutError) as e:
                if attempt == self.max_retries:
                    raise AyniError(f"network error calling {path}: {e}") from None
                wait, last = delay, e
            time.sleep(min(wait, 30.0))
            delay = min(delay * 2, 30.0)
        raise AyniError(f"request failed after retries: {last}")

    # --- quoting ---

    @staticmethod
    def _items(prompts: Sequence[str] | None, items: Sequence[dict] | None, max_tokens: int | None) -> list[dict]:
        if items is not None:
            return list(items)
        if prompts is None:
            raise AyniError("pass prompts=[...] or items=[...]")
        out: list[dict] = []
        for p in prompts:
            item: dict[str, Any] = {"messages": [{"role": "user", "content": p}]}
            if max_tokens:
                item["max_tokens"] = max_tokens
            out.append(item)
        return out

    def quote(
        self,
        prompts: Sequence[str] | None = None,
        *,
        items: Sequence[dict] | None = None,
        model: str | None = None,
        max_tokens: int | None = None,
        redundancy: int = 1,
        spot: bool = False,
    ) -> Quote:
        """Price a workload. Nothing runs and nothing is stored until you accept."""
        body = {
            "model": model or self.model,
            "items": self._items(prompts, items, max_tokens),
            "redundancy": redundancy,
            "spot": spot,
        }
        if self.trust:
            body["tier"] = self.trust  # also sent as X-Provider-Trust-Level by _request
        p = self._request("POST", "/v1/workloads", body)
        est, council = p.get("estimate", {}), p.get("council", {})
        return Quote(
            id=p.get("id", ""),
            total_usd=p.get("price", {}).get("total_usd", 0.0),
            eta_seconds=est.get("eta_seconds", 0),
            eligible_nodes=est.get("eligible_nodes", 0),
            council_decision=council.get("decision", "") if isinstance(council, dict) else "",
            raw=p,
        )

    def estimate(
        self,
        *,
        items: int,
        avg_input_tokens: int = 250,
        avg_output_tokens: int = 64,
        model: str | None = None,
        redundancy: int = 1,
        spot: bool = False,
    ) -> Quote:
        """Price a hypothetical workload from counts alone. No API key, no content sent.

        Use this to compare against your current bill before moving anything. The public
        preview endpoint prices up to MAX_ITEMS_PER_WORKLOAD at a time, so larger volumes
        are priced from a representative slice and scaled — pricing is linear in tokens.
        """
        if items <= 0:
            raise AyniError("items must be positive")
        sample = min(items, MAX_ITEMS_PER_WORKLOAD)
        p = self._request(
            "POST",
            "/v1/quote",
            {
                "model": model or self.model,
                "estimate": {
                    "count": sample,
                    "avg_prompt_tokens": avg_input_tokens,
                    "avg_completion_tokens": avg_output_tokens,
                },
                "redundancy": redundancy,
                "spot": spot,
                **({"tier": self.trust} if self.trust else {}),
            },
        )
        scale = items / sample
        est = p.get("estimate", {})
        return Quote(
            id=p.get("id", ""),
            total_usd=round(p.get("price", {}).get("total_usd", 0.0) * scale, 6),
            eta_seconds=int(est.get("eta_seconds", 0) * scale),
            eligible_nodes=est.get("eligible_nodes", 0),
            council_decision="",
            raw={
                **p,
                "price": {
                    **p.get("price", {}),
                    "total_usd": round(p.get("price", {}).get("total_usd", 0.0) * scale, 6),
                    "breakdown_usd": {
                        k: round(v * scale, 6)
                        for k, v in (p.get("price", {}).get("breakdown_usd", {}) or {}).items()
                    },
                },
                "scaled_from_items": sample,
                "scaled_to_items": items,
            },
        )

    # --- synchronous run ---

    def accept(self, quote_id: str) -> RunResult:
        """Run an accepted quote and wait for it. Use `submit` for large batches."""
        return _result_from(self._request("POST", f"/v1/workloads/{quote_id}/accept"))

    def run(
        self,
        prompts: Sequence[str] | None = None,
        *,
        items: Sequence[dict] | None = None,
        model: str | None = None,
        max_tokens: int | None = None,
        redundancy: int = 1,
        spot: bool = False,
        max_price_usd: float | None = None,
        on_quote: Callable[[Quote], None] | None = None,
    ) -> RunResult:
        """Quote, then run, in one call.

        `max_price_usd` is a guard rail: if the quote comes back above it, nothing runs
        and the quote simply expires. `on_quote` sees the quote before it is accepted.
        """
        q = self.quote(
            prompts, items=items, model=model, max_tokens=max_tokens,
            redundancy=redundancy, spot=spot,
        )
        if on_quote:
            on_quote(q)
        if max_price_usd is not None and q.total_usd > max_price_usd:
            raise AyniError(
                f"quote ${q.total_usd:.2f} exceeds max_price_usd ${max_price_usd:.2f}; nothing ran",
                code="over_budget",
            )
        return self.accept(q.id)

    # --- asynchronous run ---

    def submit(
        self,
        prompts: Sequence[str] | None = None,
        *,
        items: Sequence[dict] | None = None,
        model: str | None = None,
        max_tokens: int | None = None,
        redundancy: int = 1,
        spot: bool = False,
        webhook_url: str | None = None,
        label: str = "",
        max_price_usd: float | None = None,
        on_quote: Callable[[Quote], None] | None = None,
    ) -> Run:
        """Queue a workload and return immediately.

        Poll with `wait()` or take the webhook, then collect with `results()`. Keep the
        returned `webhook_secret`: it is shown once and verifies the callback signature.
        """
        q = self.quote(
            prompts, items=items, model=model, max_tokens=max_tokens,
            redundancy=redundancy, spot=spot,
        )
        if on_quote:
            on_quote(q)
        if max_price_usd is not None and q.total_usd > max_price_usd:
            raise AyniError(
                f"quote ${q.total_usd:.2f} exceeds max_price_usd ${max_price_usd:.2f}; nothing was queued",
                code="over_budget",
            )
        body: dict[str, Any] = {"async": True}
        if webhook_url:
            body["webhook_url"] = webhook_url
        if label:
            body["label"] = label
        return _run_from(self._request("POST", f"/v1/workloads/{q.id}/accept", body))

    def get_run(self, run_id: str) -> Run:
        return _run_from(self._request("GET", f"/v1/runs/{run_id}"))

    def list_runs(self, limit: int = 50) -> list[Run]:
        p = self._request("GET", f"/v1/runs?limit={int(limit)}")
        return [_run_from(r) for r in p.get("data", [])]

    def wait(
        self,
        run_id: str,
        *,
        poll_seconds: float = 3.0,
        timeout_seconds: float = 3600.0,
        on_progress: Callable[[Run], None] | None = None,
    ) -> Run:
        """Poll until the run finishes. Returns the final Run (check `.status`)."""
        deadline = time.time() + timeout_seconds
        while True:
            run = self.get_run(run_id)
            if on_progress:
                on_progress(run)
            if run.finished:
                return run
            if time.time() >= deadline:
                raise AyniError(f"run {run_id} still {run.status} after {timeout_seconds:.0f}s",
                                code="wait_timeout")
            time.sleep(poll_seconds)

    def results(self, run_id: str) -> RunResult:
        """Collect a finished run's output.

        Results live in coordinator memory for a bounded window and are never written to
        disk, so fetch them promptly; after that this raises with code `results_expired`.
        """
        return _result_from(self._request("GET", f"/v1/runs/{run_id}/results"))

    # --- convenience for the common "classify every line" shape ---

    def map(
        self,
        inputs: Iterable[str],
        template: str,
        *,
        max_tokens: int = 64,
        chunk_size: int = MAX_ITEMS_PER_WORKLOAD,
        max_price_usd: float | None = None,
        spot: bool = False,
        on_quote: Callable[[Quote], None] | None = None,
    ) -> list[str]:
        """Apply one prompt template over many inputs and return the outputs in order.

        `template` must contain `{input}`. Inputs are chunked to fit a workload, and each
        chunk is quoted and run in turn, so `max_price_usd` applies per chunk.
        """
        if "{input}" not in template:
            raise AyniError("template must contain a {input} placeholder")
        rows = list(inputs)
        out: list[str] = []
        for start in range(0, len(rows), chunk_size):
            chunk = rows[start : start + chunk_size]
            res = self.run(
                prompts=[template.format(input=x) for x in chunk],
                max_tokens=max_tokens,
                spot=spot,
                max_price_usd=max_price_usd,
                on_quote=on_quote,
            )
            out.extend(res.contents())
        return out


def _safe_json(raw: bytes) -> Any:
    try:
        return json.loads(raw)
    except Exception:
        return {}
