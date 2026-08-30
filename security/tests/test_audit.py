import unittest

from ayni_security.audit import AuditLog, AuditRecord


class AuditTests(unittest.TestCase):
    def test_chain_detects_tampering(self):
        log = AuditLog()
        log.append("decision", {"proposal": "one"}, now=1)
        log.append("decision", {"proposal": "two"}, now=2)
        self.assertTrue(log.verify())
        original = log._records[0]  # deliberate adversarial mutation of internal storage
        log._records[0] = AuditRecord(
            index=original.index,
            event_type="tampered",
            created_at=original.created_at,
            payload_digest=original.payload_digest,
            previous_hash=original.previous_hash,
            record_hash=original.record_hash,
        )
        self.assertFalse(log.verify())


if __name__ == "__main__":
    unittest.main()
