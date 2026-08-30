from dataclasses import replace
from hashlib import sha256
import unittest

from ayni_security.council import CouncilDecision, RiskClass, Severity
from ayni_security.publication import (
    PublicAction,
    PublicDecisionRecord,
    PublicDissent,
    PublicationError,
    VoteTotals,
    seal_public_record,
    verify_public_chain,
)


def digest(value: str) -> str:
    return sha256(value.encode()).hexdigest()


def record(previous: str = "", decision_id: str = "ACD-1") -> PublicDecisionRecord:
    return PublicDecisionRecord(
        decision_id=decision_id,
        proposal_id="proposal-1",
        title="Custom model registry admission",
        risk_class=RiskClass.SECURITY_CRITICAL,
        decision=CouncilDecision.BLOCK,
        severity=Severity.CRITICAL,
        votes=VoteTotals(approve=5, conditional=3, block=2),
        required_controls=("isolate the parser", "enforce registry signatures"),
        dissent=(PublicDissent("dissenter", CouncilDecision.BLOCK, Severity.CRITICAL, "Model extraction remains unresolved", (digest("evidence"),)),),
        actions=(PublicAction("action-1", "isolate the parser", "runtime_security", "OPEN"),),
        evidence_digests=(digest("report"),),
        roster_digest=digest("roster"),
        constitution_digest=digest("constitution"),
        previous_record_hash=previous,
        published_at=1_800_000_000,
    )


class PublicRecordTests(unittest.TestCase):
    def test_public_record_is_sealed_and_chain_verifies(self):
        first = seal_public_record(record())
        second = seal_public_record(record(first.record_hash, "ACD-2"))
        self.assertTrue(first.verify())
        self.assertTrue(verify_public_chain((first, second)))

    def test_tamper_breaks_public_proof(self):
        sealed = seal_public_record(record())
        self.assertFalse(replace(sealed, title="Changed after publication").verify())

    def test_restricted_discussion_markers_are_rejected(self):
        with self.assertRaisesRegex(PublicationError, "restricted disclosure"):
            PublicDissent("red_team", CouncilDecision.BLOCK, Severity.CRITICAL, "The chain-of-thought contains an exploit payload")

    def test_wrong_quorum_and_invalid_action_state_are_rejected(self):
        with self.assertRaisesRegex(PublicationError, "quorum"):
            replace(record(), votes=VoteTotals(approve=2, conditional=0, block=0))
        with self.assertRaisesRegex(PublicationError, "action status"):
            PublicAction("action-1", "fix it", "security", "HIDDEN")


if __name__ == "__main__":
    unittest.main()
