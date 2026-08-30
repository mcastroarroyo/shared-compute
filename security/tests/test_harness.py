import unittest

from ayni_security.harness import run_manifest_attacks


class AdversarialHarnessTests(unittest.TestCase):
    def test_all_hostile_manifests_are_rejected(self):
        report = run_manifest_attacks(1_800_000_000)
        report.assert_secure()
        self.assertGreaterEqual(len(report.results), 10)


if __name__ == "__main__":
    unittest.main()
