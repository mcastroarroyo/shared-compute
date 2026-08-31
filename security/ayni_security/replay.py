"""Concurrency-safe replay protection reference implementations."""

from __future__ import annotations

from threading import Lock


class ReplayGuard:
    """Atomically consumes single-use values.

    Production implementations must persist this state transactionally. This in-memory
    implementation is the executable reference model used by the cyber harness.
    """

    def __init__(self) -> None:
        self._seen: set[str] = set()
        self._lock = Lock()

    def consume(self, value: str) -> bool:
        if not value:
            return False
        with self._lock:
            if value in self._seen:
                return False
            self._seen.add(value)
            return True

    def __len__(self) -> int:
        with self._lock:
            return len(self._seen)


class SettlementGuard:
    """Enforces: one verified (job_id, device_pk) can create earnings once."""

    def __init__(self) -> None:
        self._claims = ReplayGuard()

    def claim(self, job_id: str, device_pk: str) -> bool:
        if not job_id or not device_pk:
            return False
        return self._claims.consume(f"{job_id}\x00{device_pk}")

    def __len__(self) -> int:
        return len(self._claims)
