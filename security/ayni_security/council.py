"""Provider-neutral, fail-closed multi-model governance engine."""

from __future__ import annotations

from dataclasses import dataclass
from enum import StrEnum
from hashlib import sha256
import json
import math
from typing import Any, Mapping, Protocol, Sequence

from .audit import AuditLog


class RiskClass(StrEnum):
    NORMAL = "normal"
    HIGH = "high"
    SECURITY_CRITICAL = "security_critical"


class CouncilDecision(StrEnum):
    APPROVE = "APPROVE"
    CONDITIONAL = "CONDITIONAL"
    BLOCK = "BLOCK"


class Severity(StrEnum):
    LOW = "LOW"
    MEDIUM = "MEDIUM"
    HIGH = "HIGH"
    CRITICAL = "CRITICAL"


SEAT_ROLES: Mapping[str, str] = {
    "security_critic": "Chief Security Critic",
    "red_team": "Red Team Director",
    "privacy": "Privacy Guardian",
    "financial_integrity": "Financial Integrity Guardian",
    "node_safety": "Node and Provider Safety Guardian",
    "renter_abuse": "Renter Abuse Guardian",
    "reliability": "Architecture and Reliability Advisor",
    "human_impact": "Human Impact and Rights Advisor",
    "ai_stewardship": "AI Stewardship Advisor",
    "dissenter": "Independent Dissenter",
}

FORBIDDEN_FACT_KEYS = frozenset(
    {
        "prompt",
        "completion",
        "customer_content",
        "raw_content",
        "raw_input",
        "instructions",
        "system_prompt",
    }
)


@dataclass(frozen=True)
class SeatAssignment:
    seat: str
    model_id: str
    provider: str


def validate_roster(roster: Sequence[SeatAssignment]) -> None:
    if len(roster) != len(SEAT_ROLES):
        raise ValueError("Council requires exactly ten seats")
    seats = {assignment.seat for assignment in roster}
    if seats != set(SEAT_ROLES):
        raise ValueError("Council roster must fill each specialist seat exactly once")
    if len({a.model_id for a in roster}) != len(roster):
        raise ValueError("Council model assignments must be unique")
    provider_counts: dict[str, int] = {}
    for assignment in roster:
        if not assignment.model_id or not assignment.provider:
            raise ValueError("model_id and provider are required")
        provider_counts[assignment.provider] = provider_counts.get(assignment.provider, 0) + 1
    if max(provider_counts.values(), default=0) > 2:
        raise ValueError("Council independence rule: at most two seats per provider")


def _validate_structured_facts(value: Any, path: str = "facts") -> None:
    if isinstance(value, Mapping):
        for key, child in value.items():
            if not isinstance(key, str):
                raise ValueError(f"{path}: keys must be strings")
            if key.lower() in FORBIDDEN_FACT_KEYS:
                raise ValueError(f"{path}.{key}: raw untrusted content is forbidden")
            _validate_structured_facts(child, f"{path}.{key}")
    elif isinstance(value, list):
        if len(value) > 100:
            raise ValueError(f"{path}: list too large")
        for index, child in enumerate(value):
            _validate_structured_facts(child, f"{path}[{index}]")
    elif not isinstance(value, (str, int, float, bool, type(None))):
        raise ValueError(f"{path}: unsupported value type")
    elif isinstance(value, str) and len(value) > 2_048:
        raise ValueError(f"{path}: string too large")


@dataclass(frozen=True)
class Proposal:
    proposal_id: str
    title: str
    risk_class: RiskClass
    facts: Mapping[str, Any]
    source_digest: str

    def __post_init__(self) -> None:
        if not self.proposal_id or not self.title:
            raise ValueError("proposal_id and title are required")
        if len(self.title) > 256:
            raise ValueError("title too large")
        if len(self.source_digest) != 64 or any(c not in "0123456789abcdef" for c in self.source_digest):
            raise ValueError("source_digest must be a lowercase SHA-256")
        _validate_structured_facts(self.facts)


@dataclass(frozen=True)
class MemberReview:
    seat: str
    decision: CouncilDecision
    severity: Severity
    confidence: float
    attack_scenarios: tuple[str, ...] = ()
    required_controls: tuple[str, ...] = ()
    evidence_digests: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        if self.seat not in SEAT_ROLES:
            raise ValueError("unknown Council seat")
        if not 0 <= self.confidence <= 1:
            raise ValueError("confidence must be in [0, 1]")
        for collection in (self.attack_scenarios, self.required_controls, self.evidence_digests):
            if len(collection) > 50 or any(len(item) > 512 for item in collection):
                raise ValueError("review contains oversized structured fields")


class Reviewer(Protocol):
    def review(self, proposal: Proposal, assignment: SeatAssignment) -> MemberReview: ...


@dataclass(frozen=True)
class CouncilOutcome:
    proposal_id: str
    decision: CouncilDecision
    reviews: tuple[MemberReview, ...]
    required_controls: tuple[str, ...]
    dissent: tuple[MemberReview, ...]
    audit_hash: str


@dataclass(frozen=True)
class CouncilPolicy:
    normal_reviewers: int = 3
    high_reviewers: int = 5
    critical_reviewers: int = 10
    critical_objections_to_block: int = 2
    high_objections_to_block: int = 2


class Council:
    """Runs structured reviews and applies deterministic governance policy.

    Models provide intelligence; this class owns the enforceable gate. A model cannot
    deploy, transfer funds, mutate a ledger, or issue node commands through this API.
    """

    def __init__(
        self,
        roster: Sequence[SeatAssignment],
        reviewers: Mapping[str, Reviewer],
        *,
        policy: CouncilPolicy | None = None,
        audit_log: AuditLog | None = None,
    ) -> None:
        validate_roster(roster)
        self._roster = tuple(roster)
        self._reviewers = dict(reviewers)
        self._policy = policy or CouncilPolicy()
        self._audit = audit_log or AuditLog()

    @property
    def audit_log(self) -> AuditLog:
        return self._audit

    def _reviewer_count(self, risk: RiskClass) -> int:
        return {
            RiskClass.NORMAL: self._policy.normal_reviewers,
            RiskClass.HIGH: self._policy.high_reviewers,
            RiskClass.SECURITY_CRITICAL: self._policy.critical_reviewers,
        }[risk]

    def _select(self, proposal: Proposal) -> tuple[SeatAssignment, ...]:
        count = self._reviewer_count(proposal.risk_class)
        if count == len(self._roster):
            return self._roster
        # Stable selection prevents orchestration drift. High-risk reviews always include
        # the independent dissenter; normal reviews rotate all seats.
        digest = sha256(proposal.proposal_id.encode()).digest()
        ranked = sorted(self._roster, key=lambda a: sha256(digest + a.seat.encode()).digest())
        if proposal.risk_class is RiskClass.HIGH:
            dissenter = next(a for a in self._roster if a.seat == "dissenter")
            ranked = [dissenter] + [a for a in ranked if a.seat != "dissenter"]
        return tuple(ranked[:count])

    def evaluate(self, proposal: Proposal) -> CouncilOutcome:
        selected = self._select(proposal)
        reviews: list[MemberReview] = []
        missing: list[str] = []
        for assignment in selected:
            reviewer = self._reviewers.get(assignment.model_id)
            if reviewer is None:
                missing.append(assignment.seat)
                continue
            try:
                review = reviewer.review(proposal, assignment)
                if review.seat != assignment.seat:
                    raise ValueError("review returned for a different seat")
                reviews.append(review)
            except Exception:
                # The audit record intentionally contains no exception text: vendor errors
                # can echo untrusted input. Missing security votes fail closed below.
                missing.append(assignment.seat)

        blocks = [review for review in reviews if review.decision is CouncilDecision.BLOCK]
        critical_blocks = [review for review in blocks if review.severity is Severity.CRITICAL]
        high_blocks = [review for review in blocks if review.severity in (Severity.HIGH, Severity.CRITICAL)]

        if missing:
            decision = CouncilDecision.BLOCK
        elif len(critical_blocks) >= self._policy.critical_objections_to_block:
            decision = CouncilDecision.BLOCK
        elif len(high_blocks) >= self._policy.high_objections_to_block:
            decision = CouncilDecision.BLOCK
        elif any(review.decision is not CouncilDecision.APPROVE for review in reviews):
            decision = CouncilDecision.CONDITIONAL
        else:
            decision = CouncilDecision.APPROVE

        controls = tuple(sorted({control for review in reviews for control in review.required_controls}))
        majority = math.ceil(len(reviews) / 2)
        decision_counts = {d: sum(r.decision is d for r in reviews) for d in CouncilDecision}
        dissent = tuple(r for r in reviews if decision_counts[r.decision] < majority)
        payload = {
            "proposal_id": proposal.proposal_id,
            "source_digest": proposal.source_digest,
            "risk_class": proposal.risk_class,
            "selected_seats": [a.seat for a in selected],
            "missing_seats": missing,
            "decision": decision,
            "reviews": [
                {
                    "seat": r.seat,
                    "decision": r.decision,
                    "severity": r.severity,
                    "confidence": r.confidence,
                    "evidence_digests": r.evidence_digests,
                }
                for r in reviews
            ],
            "required_controls": controls,
        }
        audit_record = self._audit.append("council_decision", payload)
        return CouncilOutcome(
            proposal_id=proposal.proposal_id,
            decision=decision,
            reviews=tuple(reviews),
            required_controls=controls,
            dissent=dissent,
            audit_hash=audit_record.record_hash,
        )


class StaticReviewer:
    """Deterministic adapter used by tests and offline policy exercises."""

    def __init__(self, decision: CouncilDecision, severity: Severity, controls: Sequence[str] = ()) -> None:
        self._decision = decision
        self._severity = severity
        self._controls = tuple(controls)

    def review(self, proposal: Proposal, assignment: SeatAssignment) -> MemberReview:
        digest = sha256(
            json.dumps(proposal.facts, sort_keys=True, separators=(",", ":")).encode()
        ).hexdigest()
        return MemberReview(
            seat=assignment.seat,
            decision=self._decision,
            severity=self._severity,
            confidence=1.0,
            required_controls=self._controls,
            evidence_digests=(digest,),
        )
