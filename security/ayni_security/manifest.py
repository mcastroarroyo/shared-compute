"""Signed Workload Manifest v1 and immutable node-side safety policy."""

from __future__ import annotations

from base64 import b64decode, b64encode
from dataclasses import asdict, dataclass
import json
import re
import time
from typing import Any, Mapping
from uuid import UUID

from cryptography.exceptions import InvalidSignature
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey, Ed25519PublicKey

from .replay import ReplayGuard


SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
ALLOWED_OPERATIONS = frozenset({"inference"})
ALLOWED_NETWORK_POLICIES = frozenset({"NONE"})


class ManifestError(ValueError):
    pass


def _strict_keys(data: Mapping[str, Any], expected: set[str], where: str) -> None:
    unknown = set(data) - expected
    missing = expected - set(data)
    if unknown:
        raise ManifestError(f"{where}: unknown fields: {sorted(unknown)}")
    if missing:
        raise ManifestError(f"{where}: missing fields: {sorted(missing)}")


def _uuid(value: str, field: str) -> None:
    try:
        UUID(value)
    except (ValueError, AttributeError) as exc:
        raise ManifestError(f"{field}: invalid UUID") from exc


def _sha256(value: str, field: str) -> None:
    if not SHA256_RE.fullmatch(value):
        raise ManifestError(f"{field}: expected lowercase SHA-256")


def _public_key(value: str, field: str = "device_pk") -> None:
    try:
        raw = b64decode(value, validate=True)
    except Exception as exc:
        raise ManifestError(f"{field}: invalid base64") from exc
    if len(raw) != 32:
        raise ManifestError(f"{field}: expected 32 bytes")


@dataclass(frozen=True)
class ResourceLimits:
    max_runtime_ms: int
    max_memory_mb: int
    max_storage_mb: int
    max_input_bytes: int
    max_output_tokens: int
    max_cpu_duty_cycle_pct: int

    @classmethod
    def from_mapping(cls, data: Mapping[str, Any]) -> "ResourceLimits":
        expected = set(cls.__annotations__)
        _strict_keys(data, expected, "resource_limits")
        if any(isinstance(data[k], bool) or not isinstance(data[k], int) for k in expected):
            raise ManifestError("resource_limits: all values must be integers")
        return cls(**{k: data[k] for k in expected})

    def validate_positive(self) -> None:
        for field, value in asdict(self).items():
            if value <= 0:
                raise ManifestError(f"resource_limits.{field}: must be positive")
        if self.max_cpu_duty_cycle_pct > 100:
            raise ManifestError("resource_limits.max_cpu_duty_cycle_pct: exceeds 100")


@dataclass(frozen=True)
class NodeSafetyPolicy:
    """Hard local ceilings that a coordinator cannot remotely relax."""

    allowed_runtimes: Mapping[str, str]
    allowed_models: Mapping[str, str]
    max_runtime_ms: int = 120_000
    max_memory_mb: int = 8_192
    max_storage_mb: int = 16_384
    max_input_bytes: int = 1_048_576
    max_output_tokens: int = 4_096
    max_cpu_duty_cycle_pct: int = 80
    max_lease_seconds: int = 300
    max_clock_skew_seconds: int = 30

    def validate_limits(self, limits: ResourceLimits) -> None:
        limits.validate_positive()
        ceilings = {
            "max_runtime_ms": self.max_runtime_ms,
            "max_memory_mb": self.max_memory_mb,
            "max_storage_mb": self.max_storage_mb,
            "max_input_bytes": self.max_input_bytes,
            "max_output_tokens": self.max_output_tokens,
            "max_cpu_duty_cycle_pct": self.max_cpu_duty_cycle_pct,
        }
        for field, ceiling in ceilings.items():
            if getattr(limits, field) > ceiling:
                raise ManifestError(f"resource_limits.{field}: exceeds immutable node ceiling")


@dataclass(frozen=True)
class WorkloadManifest:
    manifest_version: int
    job_id: str
    lease_id: str
    workload_id: str
    customer_id: str
    device_pk: str
    runtime_id: str
    runtime_version: str
    runtime_hash: str
    model_id: str
    model_hash: str
    operation: str
    input_hash: str
    resource_limits: ResourceLimits
    network_policy: str
    created_at: int
    expires_at: int
    nonce: str

    @classmethod
    def from_mapping(cls, data: Mapping[str, Any]) -> "WorkloadManifest":
        expected = set(cls.__annotations__)
        _strict_keys(data, expected, "manifest")
        resource = data["resource_limits"]
        if not isinstance(resource, Mapping):
            raise ManifestError("resource_limits: expected object")
        values = dict(data)
        values["resource_limits"] = ResourceLimits.from_mapping(resource)
        return cls(**values)

    def to_mapping(self) -> dict[str, Any]:
        return asdict(self)

    def canonical_bytes(self) -> bytes:
        return json.dumps(
            self.to_mapping(), sort_keys=True, separators=(",", ":"), ensure_ascii=False
        ).encode()

    def validate(
        self,
        policy: NodeSafetyPolicy,
        *,
        expected_device_pk: str,
        now: int | None = None,
    ) -> None:
        current = int(time.time() if now is None else now)
        if self.manifest_version != 1:
            raise ManifestError("manifest_version: unsupported")
        for name in ("job_id", "lease_id", "workload_id"):
            _uuid(getattr(self, name), name)
        if not self.customer_id or len(self.customer_id) > 128:
            raise ManifestError("customer_id: invalid")
        _public_key(self.device_pk)
        if self.device_pk != expected_device_pk:
            raise ManifestError("device_pk: manifest is bound to another node")
        if self.runtime_id not in policy.allowed_runtimes:
            raise ManifestError("runtime_id: not allowlisted")
        if policy.allowed_runtimes[self.runtime_id] != self.runtime_hash:
            raise ManifestError("runtime_hash: not allowlisted")
        if not self.runtime_version or len(self.runtime_version) > 64:
            raise ManifestError("runtime_version: invalid")
        if self.model_id not in policy.allowed_models:
            raise ManifestError("model_id: not allowlisted")
        if policy.allowed_models[self.model_id] != self.model_hash:
            raise ManifestError("model_hash: not allowlisted")
        for field in ("runtime_hash", "model_hash", "input_hash"):
            _sha256(getattr(self, field), field)
        if self.operation not in ALLOWED_OPERATIONS:
            raise ManifestError("operation: not allowlisted")
        if self.network_policy not in ALLOWED_NETWORK_POLICIES:
            raise ManifestError("network_policy: outbound network access is denied")
        if not isinstance(self.created_at, int) or not isinstance(self.expires_at, int):
            raise ManifestError("timestamps: expected integer Unix seconds")
        if self.created_at > current + policy.max_clock_skew_seconds:
            raise ManifestError("created_at: too far in the future")
        if self.expires_at <= current:
            raise ManifestError("expires_at: manifest expired")
        if self.expires_at <= self.created_at:
            raise ManifestError("expires_at: must be after created_at")
        if self.expires_at - self.created_at > policy.max_lease_seconds:
            raise ManifestError("expires_at: lease exceeds node ceiling")
        try:
            nonce = b64decode(self.nonce, validate=True)
        except Exception as exc:
            raise ManifestError("nonce: invalid base64") from exc
        if len(nonce) < 16 or len(nonce) > 64:
            raise ManifestError("nonce: expected 16..64 bytes")
        policy.validate_limits(self.resource_limits)


@dataclass(frozen=True)
class SignedManifest:
    key_id: str
    manifest: WorkloadManifest
    signature: str

    @classmethod
    def sign(cls, manifest: WorkloadManifest, key_id: str, key: Ed25519PrivateKey) -> "SignedManifest":
        if not key_id:
            raise ManifestError("key_id is required")
        signature = b64encode(key.sign(manifest.canonical_bytes())).decode()
        return cls(key_id=key_id, manifest=manifest, signature=signature)

    def verify_and_consume(
        self,
        trusted_keys: Mapping[str, Ed25519PublicKey],
        policy: NodeSafetyPolicy,
        replay_guard: ReplayGuard,
        *,
        expected_device_pk: str,
        now: int | None = None,
    ) -> None:
        key = trusted_keys.get(self.key_id)
        if key is None:
            raise ManifestError("key_id: untrusted signing authority")
        try:
            signature = b64decode(self.signature, validate=True)
            key.verify(signature, self.manifest.canonical_bytes())
        except (InvalidSignature, ValueError) as exc:
            raise ManifestError("signature: verification failed") from exc
        self.manifest.validate(policy, expected_device_pk=expected_device_pk, now=now)
        replay_key = f"{self.manifest.job_id}\x00{self.manifest.device_pk}\x00{self.manifest.nonce}"
        if not replay_guard.consume(replay_key):
            raise ManifestError("replay: manifest already consumed")
