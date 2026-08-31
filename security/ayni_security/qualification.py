"""Deterministic qualification and diversity selection for the Ayni Council."""

from __future__ import annotations

from dataclasses import dataclass
from hashlib import sha256
import json
from typing import Mapping, Sequence

from .council import SEAT_ROLES, SeatAssignment, validate_roster


@dataclass(frozen=True)
class ModelCandidate:
    model_id: str
    provider: str
    security_score: float
    calibration_score: float
    reliability_score: float
    role_scores: Mapping[str, float]
    open_weight: bool = False
    jurisdiction: str = "unspecified"

    def __post_init__(self) -> None:
        if not self.model_id or not self.provider:
            raise ValueError("model_id and provider are required")
        for name, score in {
            "security_score": self.security_score,
            "calibration_score": self.calibration_score,
            "reliability_score": self.reliability_score,
            **self.role_scores,
        }.items():
            if isinstance(score, bool) or not isinstance(score, (int, float)) or not 0 <= score <= 1:
                raise ValueError(f"{name} must be a score in [0, 1]")
        unknown = set(self.role_scores) - set(SEAT_ROLES)
        if unknown:
            raise ValueError(f"unknown role scores: {sorted(unknown)}")

    @property
    def baseline_score(self) -> float:
        return (
            0.5 * self.security_score
            + 0.25 * self.calibration_score
            + 0.25 * self.reliability_score
        )

    def score_for(self, seat: str) -> float:
        return 0.65 * self.role_scores.get(seat, self.security_score) + 0.35 * self.baseline_score


@dataclass(frozen=True)
class QualificationPolicy:
    minimum_security: float = 0.75
    minimum_calibration: float = 0.65
    minimum_reliability: float = 0.70
    minimum_providers: int = 5
    minimum_open_weight_seats: int = 2
    maximum_seats_per_provider: int = 2


@dataclass(frozen=True)
class RosterSelection:
    assignments: tuple[SeatAssignment, ...]
    selection_digest: str


def qualified_candidates(
    candidates: Sequence[ModelCandidate], policy: QualificationPolicy | None = None
) -> tuple[ModelCandidate, ...]:
    policy = policy or QualificationPolicy()
    seen: set[str] = set()
    qualified: list[ModelCandidate] = []
    for candidate in candidates:
        if candidate.model_id in seen:
            raise ValueError(f"duplicate model_id: {candidate.model_id}")
        seen.add(candidate.model_id)
        if (
            candidate.security_score >= policy.minimum_security
            and candidate.calibration_score >= policy.minimum_calibration
            and candidate.reliability_score >= policy.minimum_reliability
        ):
            qualified.append(candidate)
    return tuple(sorted(qualified, key=lambda c: (-c.baseline_score, c.provider, c.model_id)))


def select_roster(
    candidates: Sequence[ModelCandidate], policy: QualificationPolicy | None = None
) -> RosterSelection:
    """Select a valid ten-seat roster, preferring role fitness within hard diversity rules."""

    policy = policy or QualificationPolicy()
    if policy.maximum_seats_per_provider > 2:
        raise ValueError("Council constitutional ceiling is two seats per provider")
    pool = qualified_candidates(candidates, policy)
    if len(pool) < len(SEAT_ROLES):
        raise ValueError("fewer than ten candidates passed qualification")
    if len({candidate.provider for candidate in pool}) < policy.minimum_providers:
        raise ValueError("candidate pool lacks provider diversity")
    if sum(candidate.open_weight for candidate in pool) < policy.minimum_open_weight_seats:
        raise ValueError("candidate pool lacks required open-weight representation")

    seats = tuple(SEAT_ROLES)
    ranked = {
        seat: tuple(sorted(pool, key=lambda c: (-c.score_for(seat), c.provider, c.model_id)))
        for seat in seats
    }

    def search(
        index: int,
        used: frozenset[str],
        provider_counts: Mapping[str, int],
        open_count: int,
    ) -> tuple[tuple[str, ModelCandidate], ...] | None:
        if index == len(seats):
            return () if open_count >= policy.minimum_open_weight_seats else None
        remaining_after = len(seats) - index - 1
        seat = seats[index]
        for candidate in ranked[seat]:
            if candidate.model_id in used:
                continue
            if provider_counts.get(candidate.provider, 0) >= policy.maximum_seats_per_provider:
                continue
            next_open = open_count + int(candidate.open_weight)
            if next_open + remaining_after < policy.minimum_open_weight_seats:
                continue
            next_counts = dict(provider_counts)
            next_counts[candidate.provider] = next_counts.get(candidate.provider, 0) + 1
            tail = search(index + 1, used | {candidate.model_id}, next_counts, next_open)
            if tail is not None:
                return ((seat, candidate),) + tail
        return None

    selected = search(0, frozenset(), {}, 0)
    if selected is None:
        raise ValueError("no roster satisfies all qualification and diversity constraints")
    assignments = tuple(
        SeatAssignment(seat=seat, model_id=candidate.model_id, provider=candidate.provider)
        for seat, candidate in selected
    )
    validate_roster(assignments)
    digest_payload = [
        {"seat": a.seat, "model_id": a.model_id, "provider": a.provider}
        for a in assignments
    ]
    digest = sha256(json.dumps(digest_payload, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    return RosterSelection(assignments=assignments, selection_digest=digest)
