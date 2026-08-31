import unittest

from ayni_security.council import SEAT_ROLES, validate_roster
from ayni_security.qualification import ModelCandidate, select_roster


def candidates():
    providers = ["alpha", "beta", "gamma", "delta", "epsilon"]
    result = []
    for index, seat in enumerate(SEAT_ROLES):
        role_scores = {role: 0.80 for role in SEAT_ROLES}
        role_scores[seat] = 0.99
        result.append(
            ModelCandidate(
                model_id=f"model-{index}",
                provider=providers[index // 2],
                security_score=0.90,
                calibration_score=0.85,
                reliability_score=0.88,
                role_scores=role_scores,
                open_weight=index in (7, 8),
                jurisdiction=f"region-{index % 3}",
            )
        )
    return result


class QualificationTests(unittest.TestCase):
    def test_selects_all_roles_with_provider_and_open_weight_diversity(self):
        pool = candidates()
        selected = select_roster(pool)
        validate_roster(selected.assignments)
        self.assertEqual(len(selected.assignments), 10)
        self.assertEqual(len(selected.selection_digest), 64)
        selected_ids = {assignment.model_id for assignment in selected.assignments}
        self.assertTrue({"model-7", "model-8"}.issubset(selected_ids))

    def test_selection_is_deterministic(self):
        first = select_roster(candidates())
        second = select_roster(list(reversed(candidates())))
        self.assertEqual(first, second)

    def test_rejects_correlated_or_underqualified_pool(self):
        correlated = [
            ModelCandidate(
                model_id=f"single-{index}",
                provider="one-provider",
                security_score=0.9,
                calibration_score=0.9,
                reliability_score=0.9,
                role_scores={seat: 0.9 for seat in SEAT_ROLES},
                open_weight=index < 2,
            )
            for index in range(10)
        ]
        with self.assertRaisesRegex(ValueError, "provider diversity"):
            select_roster(correlated)

        weak = candidates()
        weak[0] = ModelCandidate(
            model_id="weak",
            provider="alpha",
            security_score=0.1,
            calibration_score=0.9,
            reliability_score=0.9,
            role_scores={seat: 0.9 for seat in SEAT_ROLES},
            open_weight=True,
        )
        with self.assertRaisesRegex(ValueError, "fewer than ten"):
            select_roster(weak)


if __name__ == "__main__":
    unittest.main()
