from hashlib import sha256
import json
import unittest

from ayni_security.adapters import AdapterError, StructuredModelReviewer, parse_member_review
from ayni_security.council import Proposal, RiskClass, SeatAssignment


def valid_response():
    return {
        "decision": "BLOCK",
        "severity": "CRITICAL",
        "confidence": 0.97,
        "attack_scenarios": ["arbitrary egress could create an abuse proxy"],
        "required_controls": ["deny egress by default"],
        "evidence_digests": [sha256(b"evidence").hexdigest()],
    }


class FakeTransport:
    def __init__(self, response):
        self.response = response
        self.request = None

    def complete(self, request, *, timeout_seconds):
        self.request = request
        self.timeout_seconds = timeout_seconds
        return self.response


class AdapterTests(unittest.TestCase):
    def test_strict_response_is_converted_to_seat_bound_review(self):
        transport = FakeTransport(json.dumps(valid_response()))
        reviewer = StructuredModelReviewer(transport, sha256(b"constitution").hexdigest())
        proposal = Proposal(
            proposal_id="change-1",
            title="Network policy change",
            risk_class=RiskClass.SECURITY_CRITICAL,
            facts={"network_policy": "NONE", "source": "pull-request"},
            source_digest=sha256(b"source").hexdigest(),
        )
        review = reviewer.review(
            proposal, SeatAssignment("red_team", "model-1", "provider-1")
        )
        self.assertEqual(review.seat, "red_team")
        self.assertEqual(review.decision.value, "BLOCK")
        self.assertEqual(transport.request["protocol"], "ayni-council-review-v1")
        self.assertNotIn("model_id", transport.request)
        self.assertEqual(len(reviewer.last_response_digest), 64)

    def test_unknown_output_fields_are_rejected(self):
        payload = valid_response()
        payload["tool_call"] = {"deploy": True}
        with self.assertRaisesRegex(AdapterError, "schema mismatch"):
            parse_member_review(payload, seat="security_critic")

    def test_invalid_digest_and_oversized_output_are_rejected(self):
        payload = valid_response()
        payload["evidence_digests"] = ["not-a-digest"]
        with self.assertRaisesRegex(AdapterError, "SHA-256"):
            parse_member_review(payload, seat="privacy")
        with self.assertRaisesRegex(AdapterError, "byte limit"):
            parse_member_review("x" * (64 * 1024 + 1), seat="privacy")

    def test_boolean_confidence_is_rejected(self):
        payload = valid_response()
        payload["confidence"] = True
        with self.assertRaisesRegex(AdapterError, "numeric"):
            parse_member_review(payload, seat="reliability")


if __name__ == "__main__":
    unittest.main()
