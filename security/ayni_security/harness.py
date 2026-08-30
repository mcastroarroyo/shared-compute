"""Offline hostile-workload suite. Safe by default: it performs no network traffic."""

from __future__ import annotations

from base64 import b64encode
from dataclasses import dataclass, replace
from hashlib import sha256
import os
import time
from uuid import uuid4

from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

from .manifest import (
    ManifestError,
    NodeSafetyPolicy,
    ResourceLimits,
    SignedManifest,
    WorkloadManifest,
)
from .replay import ReplayGuard


@dataclass(frozen=True)
class AttackResult:
    name: str
    rejected: bool


@dataclass(frozen=True)
class HarnessReport:
    results: tuple[AttackResult, ...]

    @property
    def passed(self) -> bool:
        return all(result.rejected for result in self.results)

    def assert_secure(self) -> None:
        failures = [result.name for result in self.results if not result.rejected]
        if failures:
            raise AssertionError(f"security harness accepted hostile cases: {failures}")


def _hash(label: str) -> str:
    return sha256(label.encode()).hexdigest()


def sample_fixture(now: int | None = None):
    current = int(time.time() if now is None else now)
    private_key = Ed25519PrivateKey.generate()
    device_pk = b64encode(os.urandom(32)).decode()
    runtime_hash = _hash("approved-runtime")
    model_hash = _hash("approved-model")
    policy = NodeSafetyPolicy(
        allowed_runtimes={"llama.cpp": runtime_hash},
        allowed_models={"llama-3.2-1b-q4": model_hash},
    )
    manifest = WorkloadManifest(
        manifest_version=1,
        job_id=str(uuid4()),
        lease_id=str(uuid4()),
        workload_id=str(uuid4()),
        customer_id="customer-hash-1",
        device_pk=device_pk,
        runtime_id="llama.cpp",
        runtime_version="approved-2026-08",
        runtime_hash=runtime_hash,
        model_id="llama-3.2-1b-q4",
        model_hash=model_hash,
        operation="inference",
        input_hash=_hash("encrypted-input"),
        resource_limits=ResourceLimits(
            max_runtime_ms=60_000,
            max_memory_mb=4_096,
            max_storage_mb=8_192,
            max_input_bytes=262_144,
            max_output_tokens=1_024,
            max_cpu_duty_cycle_pct=70,
        ),
        network_policy="NONE",
        created_at=current,
        expires_at=current + 120,
        nonce=b64encode(os.urandom(24)).decode(),
    )
    return current, private_key, device_pk, policy, manifest


def run_manifest_attacks(now: int | None = None) -> HarnessReport:
    current, private_key, device_pk, policy, manifest = sample_fixture(now)
    trusted = {"signer-v1": private_key.public_key()}

    mutations = {
        "unknown-runtime": replace(manifest, runtime_id="shell"),
        "runtime-hash-tamper": replace(manifest, runtime_hash=_hash("evil-runtime")),
        "unknown-model": replace(manifest, model_id="evil.gguf"),
        "model-hash-tamper": replace(manifest, model_hash=_hash("evil-model")),
        "arbitrary-egress": replace(manifest, network_policy="CUSTOM"),
        "shell-operation": replace(manifest, operation="execute-shell"),
        "expired-lease": replace(manifest, created_at=current - 200, expires_at=current - 1),
        "oversized-runtime": replace(
            manifest,
            resource_limits=replace(manifest.resource_limits, max_runtime_ms=999_999_999),
        ),
        "oversized-memory": replace(
            manifest,
            resource_limits=replace(manifest.resource_limits, max_memory_mb=999_999),
        ),
        "unbounded-cpu": replace(
            manifest,
            resource_limits=replace(manifest.resource_limits, max_cpu_duty_cycle_pct=100),
        ),
        "wrong-device": replace(manifest, device_pk=b64encode(os.urandom(32)).decode()),
    }
    results: list[AttackResult] = []
    for name, mutated in mutations.items():
        signed = SignedManifest.sign(mutated, "signer-v1", private_key)
        try:
            signed.verify_and_consume(
                trusted, policy, ReplayGuard(), expected_device_pk=device_pk, now=current
            )
            rejected = False
        except ManifestError:
            rejected = True
        results.append(AttackResult(name=name, rejected=rejected))

    valid = SignedManifest.sign(manifest, "signer-v1", private_key)
    guard = ReplayGuard()
    valid.verify_and_consume(trusted, policy, guard, expected_device_pk=device_pk, now=current)
    try:
        valid.verify_and_consume(trusted, policy, guard, expected_device_pk=device_pk, now=current)
        replay_rejected = False
    except ManifestError:
        replay_rejected = True
    results.append(AttackResult(name="signed-manifest-replay", rejected=replay_rejected))

    signed_original = SignedManifest.sign(manifest, "signer-v1", private_key)
    tampered = replace(signed_original, manifest=replace(manifest, model_id="evil.gguf"))
    try:
        tampered.verify_and_consume(
            trusted, policy, ReplayGuard(), expected_device_pk=device_pk, now=current
        )
        tamper_rejected = False
    except ManifestError:
        tamper_rejected = True
    results.append(AttackResult(name="post-signature-tamper", rejected=tamper_rejected))
    return HarnessReport(tuple(results))


def main() -> int:
    report = run_manifest_attacks()
    for result in report.results:
        print(f"{'PASS' if result.rejected else 'FAIL'} {result.name}")
    return 0 if report.passed else 1


if __name__ == "__main__":
    raise SystemExit(main())
