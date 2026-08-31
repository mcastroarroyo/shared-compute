"""Tamper-evident, content-minimizing Council decision records."""

from __future__ import annotations

from dataclasses import asdict, dataclass
from hashlib import sha256
import json
import time
from typing import Any, Mapping


def canonical_json(value: Any) -> bytes:
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()


@dataclass(frozen=True)
class AuditRecord:
    index: int
    event_type: str
    created_at: int
    payload_digest: str
    previous_hash: str
    record_hash: str


class AuditLog:
    """Append-only hash chain.

    Only a payload digest is retained, preventing customer plaintext from entering the
    governance log. A production sink should additionally anchor heads in immutable
    object storage or a transparency service.
    """

    def __init__(self) -> None:
        self._records: list[AuditRecord] = []

    @property
    def records(self) -> tuple[AuditRecord, ...]:
        return tuple(self._records)

    def append(self, event_type: str, payload: Mapping[str, Any], *, now: int | None = None) -> AuditRecord:
        if not event_type:
            raise ValueError("event_type is required")
        created_at = int(time.time() if now is None else now)
        payload_digest = sha256(canonical_json(payload)).hexdigest()
        previous_hash = self._records[-1].record_hash if self._records else "0" * 64
        body = {
            "index": len(self._records),
            "event_type": event_type,
            "created_at": created_at,
            "payload_digest": payload_digest,
            "previous_hash": previous_hash,
        }
        record_hash = sha256(canonical_json(body)).hexdigest()
        record = AuditRecord(record_hash=record_hash, **body)
        self._records.append(record)
        return record

    def verify(self) -> bool:
        previous = "0" * 64
        for expected_index, record in enumerate(self._records):
            if record.index != expected_index or record.previous_hash != previous:
                return False
            body = asdict(record)
            claimed = body.pop("record_hash")
            if sha256(canonical_json(body)).hexdigest() != claimed:
                return False
            previous = claimed
        return True
