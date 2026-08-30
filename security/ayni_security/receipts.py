"""Signed result-receipt reference model used by the malicious-node harness."""

from __future__ import annotations

from base64 import b64decode, b64encode
from dataclasses import asdict, dataclass
import json
from typing import Mapping

from cryptography.exceptions import InvalidSignature
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey, Ed25519PublicKey

from .replay import SettlementGuard


class ReceiptError(ValueError):
    pass


def _sha256(value: str, field: str) -> None:
    if len(value) != 64 or any(c not in "0123456789abcdef" for c in value):
        raise ReceiptError(f"{field} must be a lowercase SHA-256")


@dataclass(frozen=True)
class JobAssignment:
    job_id: str
    lease_id: str
    device_pk: str
    workload_hash: str
    nonce: str
    expires_at: int
    max_output_bytes: int

    def __post_init__(self) -> None:
        if not all((self.job_id, self.lease_id, self.device_pk, self.nonce)):
            raise ReceiptError("assignment identifiers are required")
        _sha256(self.workload_hash, "workload_hash")
        if self.expires_at <= 0 or self.max_output_bytes <= 0:
            raise ReceiptError("assignment limits must be positive")


@dataclass(frozen=True)
class ResultReceipt:
    job_id: str
    lease_id: str
    device_pk: str
    workload_hash: str
    result_hash: str
    nonce: str
    final_sequence: int
    output_bytes: int
    completed_at: int

    def __post_init__(self) -> None:
        if not all((self.job_id, self.lease_id, self.device_pk, self.nonce)):
            raise ReceiptError("receipt identifiers are required")
        _sha256(self.workload_hash, "workload_hash")
        _sha256(self.result_hash, "result_hash")
        if self.final_sequence < 0 or self.output_bytes < 0 or self.completed_at <= 0:
            raise ReceiptError("receipt counters and timestamp are invalid")

    def canonical_bytes(self) -> bytes:
        return json.dumps(asdict(self), sort_keys=True, separators=(",", ":")).encode()


@dataclass(frozen=True)
class SignedResultReceipt:
    receipt: ResultReceipt
    signature: str

    @classmethod
    def sign(cls, receipt: ResultReceipt, private_key: Ed25519PrivateKey) -> "SignedResultReceipt":
        return cls(receipt=receipt, signature=b64encode(private_key.sign(receipt.canonical_bytes())).decode())


class ReceiptVerifier:
    """Verifies assignment binding before atomically consuming a payable event."""

    def __init__(self, device_keys: Mapping[str, Ed25519PublicKey]) -> None:
        self._device_keys = dict(device_keys)
        self._settlements = SettlementGuard()

    def verify_and_consume(
        self, signed: SignedResultReceipt, assignment: JobAssignment, *, now: int
    ) -> ResultReceipt:
        receipt = signed.receipt
        public_key = self._device_keys.get(assignment.device_pk)
        if public_key is None:
            raise ReceiptError("device identity is not trusted")
        if receipt.device_pk != assignment.device_pk:
            raise ReceiptError("receipt is bound to a different device")
        try:
            signature = b64decode(signed.signature, validate=True)
            public_key.verify(signature, receipt.canonical_bytes())
        except (InvalidSignature, ValueError) as exc:
            raise ReceiptError("invalid device signature") from exc
        if receipt.job_id != assignment.job_id or receipt.lease_id != assignment.lease_id:
            raise ReceiptError("receipt is not bound to this job lease")
        if receipt.workload_hash != assignment.workload_hash or receipt.nonce != assignment.nonce:
            raise ReceiptError("receipt is not bound to this workload challenge")
        if now > assignment.expires_at or receipt.completed_at > assignment.expires_at:
            raise ReceiptError("receipt or lease expired")
        if receipt.completed_at > now + 30:
            raise ReceiptError("receipt timestamp is in the future")
        if receipt.output_bytes > assignment.max_output_bytes:
            raise ReceiptError("receipt exceeds output ceiling")
        if not self._settlements.claim(receipt.job_id, receipt.device_pk):
            raise ReceiptError("payable event replay")
        return receipt
