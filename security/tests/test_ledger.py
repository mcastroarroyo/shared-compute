from concurrent.futures import ThreadPoolExecutor
import random
import unittest

from ayni_security.ledger import LedgerError, ReferenceLedger


class LedgerInvariantTests(unittest.TestCase):
    def test_duplicate_completion_accrues_once_under_race(self):
        ledger = ReferenceLedger()
        payments: set[str] = set()
        ledger.fund("customer", 10_000, "pi-1", payments)
        ledger.charge("customer", "job-1", 1_000)
        with ThreadPoolExecutor(max_workers=64) as pool:
            results = list(pool.map(
                lambda _: ledger.accrue(
                    job_id="job-1", device_pk="device-a", provider_id="provider-a",
                    gross_micros=1_000, provider_micros=700,
                ),
                range(2_000),
            ))
        self.assertEqual(sum(results), 1)
        ledger.assert_invariants()
        self.assertEqual(ledger.snapshot().provider_earned_micros, 700)

    def test_unbilled_work_cannot_create_earnings(self):
        ledger = ReferenceLedger()
        with self.assertRaisesRegex(LedgerError, "unbilled"):
            ledger.accrue(
                job_id="ghost", device_pk="device", provider_id="provider",
                gross_micros=100, provider_micros=70,
            )

    def test_randomized_conservation_traces(self):
        for seed in range(100):
            rng = random.Random(seed)
            ledger = ReferenceLedger()
            payments: set[str] = set()
            ledger.fund("customer", 1_000_000, f"pi-{seed}", payments)
            for index in range(100):
                gross = rng.randint(0, 1_000)
                job = f"{seed}-{index}"
                ledger.charge("customer", job, gross)
                ledger.accrue(
                    job_id=job, device_pk="device", provider_id="provider",
                    gross_micros=gross, provider_micros=gross * 7 // 10,
                )
                if rng.random() < 0.2:
                    # Retry storms must be harmless.
                    self.assertFalse(ledger.charge("customer", job, gross))
                    self.assertFalse(ledger.accrue(
                        job_id=job, device_pk="device", provider_id="provider",
                        gross_micros=gross, provider_micros=gross * 7 // 10,
                    ))
                ledger.assert_invariants()


if __name__ == "__main__":
    unittest.main()
