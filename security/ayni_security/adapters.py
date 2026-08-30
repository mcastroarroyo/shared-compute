"""Constrained, provider-neutral adapters for Council model responses.

This module performs no network calls and knows no vendor credentials. A deployment may
provide a transport implementation, but only the structured request and response schema
defined here crosses that boundary.
"""

from __future__ import annotations

from dataclasses import dataclass
from hashlib import sha256
import json
from typing import Any, Mapping, Protocol

from .council import (
    CouncilDecision,
    MemberReview,
    Proposal,
    SeatAssignment,
    Severity,
)


MAX_RESPONSE_BYTES = 64 * 1024
RESPONSE_KEYS = frozenset(
    {
        "decision",
        "severity",
        "confidence",
        "attack_scenarios",
        "required_controls",
        "evidence_digests",
    }
)


class AdapterError(ValueError):
    """Raised when a model or transport violates the Council adapter contract."""


class CouncilTransport(Protocol):
    def complete(self, request: Mapping[str, Any], *, timeout_seconds: float) -> str | bytes | Mapping[str, Any]: ...


def _string_list(value: Any, field: str) -> tuple[str, ...]:
    if not isinstance(value, list):
        raise AdapterError(f"{field} must be a list")
    if len(value) > 50:
        raise AdapterError(f"{field} contains too many items")
    result: list[str] = []
    for item in value:
        if not isinstance(item, str) or not item or len(item) > 512:
            raise AdapterError(f"{field} contains an invalid item")
        result.append(item)
    return tuple(result)


def parse_member_review(payload: str | bytes | Mapping[str, Any], *, seat: str) -> MemberReview:
    """Parse one strict model response into the only object the Council can consume."""

    if isinstance(payload, bytes):
        if len(payload) > MAX_RESPONSE_BYTES:
            raise AdapterError("model response exceeds byte limit")
        try:
            decoded: Any = json.loads(payload.decode("utf-8"))
        except (UnicodeDecodeError, json.JSONDecodeError) as exc:
            raise AdapterError("model response is not valid UTF-8 JSON") from exc
    elif isinstance(payload, str):
        if len(payload.encode("utf-8")) > MAX_RESPONSE_BYTES:
            raise AdapterError("model response exceeds byte limit")
        try:
            decoded = json.loads(payload)
        except json.JSONDecodeError as exc:
            raise AdapterError("model response is not valid JSON") from exc
    elif isinstance(payload, Mapping):
        decoded = dict(payload)
        try:
            encoded = json.dumps(decoded, sort_keys=True, separators=(",", ":")).encode()
        except (TypeError, ValueError) as exc:
            raise AdapterError("model response is not JSON serializable") from exc
        if len(encoded) > MAX_RESPONSE_BYTES:
            raise AdapterError("model response exceeds byte limit")
    else:
        raise AdapterError("model response must be JSON or an object")

    if not isinstance(decoded, dict):
        raise AdapterError("model response must be a JSON object")
    if set(decoded) != RESPONSE_KEYS:
        missing = sorted(RESPONSE_KEYS - set(decoded))
        unknown = sorted(set(decoded) - RESPONSE_KEYS)
        raise AdapterError(f"response schema mismatch; missing={missing}, unknown={unknown}")
    confidence = decoded["confidence"]
    if isinstance(confidence, bool) or not isinstance(confidence, (int, float)):
        raise AdapterError("confidence must be numeric")

    evidence = _string_list(decoded["evidence_digests"], "evidence_digests")
    if any(len(item) != 64 or any(c not in "0123456789abcdef" for c in item) for item in evidence):
        raise AdapterError("evidence_digests must contain lowercase SHA-256 values")
    try:
        return MemberReview(
            seat=seat,
            decision=CouncilDecision(decoded["decision"]),
            severity=Severity(decoded["severity"]),
            confidence=float(confidence),
            attack_scenarios=_string_list(decoded["attack_scenarios"], "attack_scenarios"),
            required_controls=_string_list(decoded["required_controls"], "required_controls"),
            evidence_digests=evidence,
        )
    except (TypeError, ValueError) as exc:
        raise AdapterError("model response contains an invalid review value") from exc


@dataclass
class StructuredModelReviewer:
    """Council Reviewer backed by an injected transport and a least-privilege schema."""

    transport: CouncilTransport
    constitution_digest: str
    timeout_seconds: float = 30.0
    last_response_digest: str | None = None

    def __post_init__(self) -> None:
        if len(self.constitution_digest) != 64 or any(
            c not in "0123456789abcdef" for c in self.constitution_digest
        ):
            raise ValueError("constitution_digest must be a lowercase SHA-256")
        if not 0 < self.timeout_seconds <= 120:
            raise ValueError("timeout_seconds must be in (0, 120]")

    def review(self, proposal: Proposal, assignment: SeatAssignment) -> MemberReview:
        request = {
            "protocol": "ayni-council-review-v1",
            "constitution_digest": self.constitution_digest,
            "seat": assignment.seat,
            "proposal": {
                "proposal_id": proposal.proposal_id,
                "title": proposal.title,
                "risk_class": proposal.risk_class.value,
                "facts": proposal.facts,
                "source_digest": proposal.source_digest,
            },
            "response_schema": sorted(RESPONSE_KEYS),
        }
        raw = self.transport.complete(request, timeout_seconds=self.timeout_seconds)
        canonical = (
            bytes(raw)
            if isinstance(raw, bytes)
            else raw.encode()
            if isinstance(raw, str)
            else json.dumps(raw, sort_keys=True, separators=(",", ":")).encode()
        )
        self.last_response_digest = sha256(canonical).hexdigest()
        return parse_member_review(raw, seat=assignment.seat)
