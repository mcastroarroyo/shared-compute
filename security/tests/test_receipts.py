from dataclasses import replace
from hashlib import sha256
import unittest

from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

from ayni_security.receipts import (
    JobAssignment,
    ReceiptError,
    ReceiptVerifier,
    ResultReceipt,
    SignedResultReceipt,
)


def fixture():
    key = Ed25519PrivateKey.generate()
    assignment = JobAssignment(
        job_id="job-1",
        lease_id="lease-1",
        device_pk="device-a",
        workload_hash=sha256(b"workload").hexdigest(),
        nonce="challenge-1",
        expires_at=1_800_000_120,
        max_output_bytes=4_096,
    )
    receipt = ResultReceipt(
        job_id=assignment.job_id,
        lease_id=assignment.lease_id,
        device_pk=assignment.device_pk,
        workload_hash=assignment.workload_hash,
        result_hash=sha256(b"result").hexdigest(),
        nonce=assignment.nonce,
        final_sequence=4,
        output_bytes=2_048,
        completed_at=1_800_000_060,
    )
    return key, assignment, receipt


class ResultReceiptTests(unittest.TestCase):
    def test_valid_receipt_is_payable_once(self):
        key, assignment, receipt = fixture()
        signed = SignedResultReceipt.sign(receipt, key)
        verifier = ReceiptVerifier({assignment.device_pk: key.public_key()})
        self.assertEqual(
            verifier.verify_and_consume(signed, assignment, now=1_800_000_061), receipt
        )
        with self.assertRaisesRegex(ReceiptError, "replay"):
            verifier.verify_and_consume(signed, assignment, now=1_800_000_061)

    def test_forged_key_and_post_signature_tamper_fail(self):
        key, assignment, receipt = fixture()
        forged = SignedResultReceipt.sign(receipt, Ed25519PrivateKey.generate())
        verifier = ReceiptVerifier({assignment.device_pk: key.public_key()})
        with self.assertRaisesRegex(ReceiptError, "signature"):
            verifier.verify_and_consume(forged, assignment, now=1_800_000_061)

        signed = SignedResultReceipt.sign(receipt, key)
        tampered = replace(signed, receipt=replace(receipt, output_bytes=1))
        with self.assertRaisesRegex(ReceiptError, "signature"):
            verifier.verify_and_consume(tampered, assignment, now=1_800_000_061)

    def test_wrong_assignment_expiry_and_output_ceiling_fail(self):
        key, assignment, receipt = fixture()
        verifier = ReceiptVerifier({assignment.device_pk: key.public_key()})
        wrong_lease = SignedResultReceipt.sign(replace(receipt, lease_id="lease-evil"), key)
        with self.assertRaisesRegex(ReceiptError, "job lease"):
            verifier.verify_and_consume(wrong_lease, assignment, now=1_800_000_061)

        too_large = SignedResultReceipt.sign(replace(receipt, output_bytes=4_097), key)
        with self.assertRaisesRegex(ReceiptError, "output ceiling"):
            verifier.verify_and_consume(too_large, assignment, now=1_800_000_061)

        valid = SignedResultReceipt.sign(receipt, key)
        with self.assertRaisesRegex(ReceiptError, "expired"):
            verifier.verify_and_consume(valid, assignment, now=1_800_000_121)


if __name__ == "__main__":
    unittest.main()
