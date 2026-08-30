"""Public-safe, tamper-evident Council decision records.

The publication boundary is intentionally narrower than the internal Council record.
It has no field capable of carrying prompts, chain-of-thought, exploit reproductions,
credentials, customer content, or unrestricted evidence.
"""

from __future__ import annotations

from dataclasses import asdict, dataclass, replace
from hashlib import sha256
import json
from typing import Mapping, Sequence

from .council import CouncilDecision, RiskClass, Severity


class PublicationError(ValueError):
    pass


PROTOCOL = "ayni-council-public-record-v1"
ALLOWED_ACTION_STATES = frozenset({"OPEN", "IN_PROGRESS", "VERIFIED", "CLOSED", "SUPERSEDED"})
FORBIDDEN_MARKERS = (
    "chain of thought",
    "chain-of-thought",
    "reasoning trace",
    "system prompt",
    "api key",
    "private key",
    "bearer token",
    "exploit payload",
    "customer prompt",
    "customer content",
)


def _digest(value: str, field: str, *, allow_empty: bool = False) -> None:
    if allow_empty and value == "":
        return
    if len(value) != 64 or any(c not in "0123456789abcdef" for c in value):
        raise PublicationError(f"{field} must be a lowercase SHA-256")


def _public_text(value: str, field: str, maximum: int) -> None:
    if not value or len(value) > maximum:
        raise PublicationError(f"{field} is empty or oversized")
    lowered = value.lower()
    if any(marker in lowered for marker in FORBIDDEN_MARKERS):
        raise PublicationError(f"{field} contains restricted disclosure material")


@dataclass(frozen=True)
class VoteTotals:
    approve: int
    conditional: int
    block: int
    missing: int = 0

    def __post_init__(self) -> None:
        if any(isinstance(value, bool) or not isinstance(value, int) or value < 0 for value in asdict(self).values()):
            raise PublicationError("vote totals must be non-negative integers")
        if sum(asdict(self).values()) > 10:
            raise PublicationError("vote totals exceed Council seats")


@dataclass(frozen=True)
class PublicDissent:
    seat: str
    decision: CouncilDecision
    severity: Severity
    summary: str
    evidence_digests: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        _public_text(self.seat, "dissent.seat", 64)
        _public_text(self.summary, "dissent.summary", 512)
        if len(self.evidence_digests) > 20:
            raise PublicationError("too many dissent evidence digests")
        for digest in self.evidence_digests:
            _digest(digest, "dissent.evidence_digest")


@dataclass(frozen=True)
class PublicAction:
    action_id: str
    control: str
    owner_role: str
    status: str
    due_at: int | None = None

    def __post_init__(self) -> None:
        _public_text(self.action_id, "action.action_id", 64)
        _public_text(self.control, "action.control", 256)
        _public_text(self.owner_role, "action.owner_role", 128)
        if self.status not in ALLOWED_ACTION_STATES:
            raise PublicationError("invalid public action status")
        if self.due_at is not None and self.due_at <= 0:
            raise PublicationError("action due_at must be a positive Unix timestamp")


@dataclass(frozen=True)
class PublicDecisionRecord:
    decision_id: str
    proposal_id: str
    title: str
    risk_class: RiskClass
    decision: CouncilDecision
    severity: Severity
    votes: VoteTotals
    required_controls: tuple[str, ...]
    dissent: tuple[PublicDissent, ...]
    actions: tuple[PublicAction, ...]
    evidence_digests: tuple[str, ...]
    roster_digest: str
    constitution_digest: str
    previous_record_hash: str
    published_at: int
    record_hash: str = ""
    protocol: str = PROTOCOL

    def __post_init__(self) -> None:
        if self.protocol != PROTOCOL:
            raise PublicationError("unsupported public record protocol")
        _public_text(self.decision_id, "decision_id", 64)
        _public_text(self.proposal_id, "proposal_id", 128)
        _public_text(self.title, "title", 256)
        if self.published_at <= 0:
            raise PublicationError("published_at must be a positive Unix timestamp")
        if len(self.required_controls) > 50 or len(self.dissent) > 10 or len(self.actions) > 100:
            raise PublicationError("public record collections exceed limits")
        for control in self.required_controls:
            _public_text(control, "required_control", 256)
        if len(self.evidence_digests) > 50:
            raise PublicationError("too many public evidence digests")
        for digest in self.evidence_digests:
            _digest(digest, "evidence_digest")
        _digest(self.roster_digest, "roster_digest")
        _digest(self.constitution_digest, "constitution_digest")
        _digest(self.previous_record_hash, "previous_record_hash", allow_empty=True)
        _digest(self.record_hash, "record_hash", allow_empty=True)
        expected_votes = {RiskClass.NORMAL: 3, RiskClass.HIGH: 5, RiskClass.SECURITY_CRITICAL: 10}[self.risk_class]
        if sum(asdict(self.votes).values()) != expected_votes:
            raise PublicationError("published vote total does not match required quorum")

    def canonical_payload(self) -> Mapping[str, object]:
        payload = asdict(self)
        payload.pop("record_hash")
        return payload

    def canonical_bytes(self) -> bytes:
        return json.dumps(self.canonical_payload(), sort_keys=True, separators=(",", ":")).encode()

    def verify(self) -> bool:
        return bool(self.record_hash) and sha256(self.canonical_bytes()).hexdigest() == self.record_hash


def seal_public_record(record: PublicDecisionRecord) -> PublicDecisionRecord:
    if record.record_hash:
        raise PublicationError("record is already sealed")
    return replace(record, record_hash=sha256(record.canonical_bytes()).hexdigest())


def verify_public_chain(records: Sequence[PublicDecisionRecord]) -> bool:
    previous = ""
    for record in records:
        if record.previous_record_hash != previous or not record.verify():
            return False
        previous = record.record_hash
    return True
