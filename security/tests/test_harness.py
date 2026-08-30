import unittest

from ayni_security.harness import run_all_attacks, run_manifest_attacks, run_receipt_attacks


class AdversarialHarnessTests(unittest.TestCase):
    def test_all_hostile_manifests_are_rejected(self):
        report = run_manifest_attacks(1_800_000_000)
        report.assert_secure()
        self.assertGreaterEqual(len(report.results), 10)

    def test_all_malicious_node_receipts_are_rejected(self):
        report = run_receipt_attacks(1_800_000_000)
        report.assert_secure()
        self.assertGreaterEqual(len(report.results), 10)

    def test_combined_offline_harness(self):
        report = run_all_attacks(1_800_000_000)
        report.assert_secure()
        self.assertGreaterEqual(len(report.results), 20)


if __name__ == "__main__":
    unittest.main()
