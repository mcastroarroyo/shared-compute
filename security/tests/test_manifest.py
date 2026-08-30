from dataclasses import asdict
import unittest

from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

from ayni_security.harness import sample_fixture
from ayni_security.manifest import ManifestError, SignedManifest, WorkloadManifest
from ayni_security.replay import ReplayGuard


class WorkloadManifestTests(unittest.TestCase):
    def test_valid_signed_manifest_is_accepted_once(self):
        now, key, device_pk, policy, manifest = sample_fixture(1_800_000_000)
        signed = SignedManifest.sign(manifest, "kms-v1", key)
        guard = ReplayGuard()
        signed.verify_and_consume(
            {"kms-v1": key.public_key()}, policy, guard,
            expected_device_pk=device_pk, now=now,
        )
        with self.assertRaisesRegex(ManifestError, "replay"):
            signed.verify_and_consume(
                {"kms-v1": key.public_key()}, policy, guard,
                expected_device_pk=device_pk, now=now,
            )

    def test_signature_from_untrusted_authority_is_rejected(self):
        now, key, device_pk, policy, manifest = sample_fixture(1_800_000_000)
        signed = SignedManifest.sign(manifest, "unknown", key)
        with self.assertRaisesRegex(ManifestError, "untrusted"):
            signed.verify_and_consume(
                {"kms-v1": Ed25519PrivateKey.generate().public_key()},
                policy, ReplayGuard(), expected_device_pk=device_pk, now=now,
            )

    def test_unknown_fields_fail_closed(self):
        _, _, _, _, manifest = sample_fixture(1_800_000_000)
        body = asdict(manifest)
        body["execute"] = "rm -rf"
        with self.assertRaisesRegex(ManifestError, "unknown fields"):
            WorkloadManifest.from_mapping(body)

    def test_canonical_bytes_are_stable(self):
        _, _, _, _, manifest = sample_fixture(1_800_000_000)
        reparsed = WorkloadManifest.from_mapping(asdict(manifest))
        self.assertEqual(manifest.canonical_bytes(), reparsed.canonical_bytes())


if __name__ == "__main__":
    unittest.main()
