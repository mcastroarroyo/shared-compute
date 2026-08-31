from concurrent.futures import ThreadPoolExecutor
import unittest

from ayni_security.replay import ReplayGuard, SettlementGuard


class ReplayTests(unittest.TestCase):
    def test_nonce_is_consumed_once_under_race(self):
        guard = ReplayGuard()
        with ThreadPoolExecutor(max_workers=64) as pool:
            results = list(pool.map(lambda _: guard.consume("same-nonce"), range(2_000)))
        self.assertEqual(sum(results), 1)

    def test_payable_event_is_job_and_device_bound(self):
        guard = SettlementGuard()
        self.assertTrue(guard.claim("job-1", "device-a"))
        self.assertFalse(guard.claim("job-1", "device-a"))
        self.assertTrue(guard.claim("job-1", "device-b"))


if __name__ == "__main__":
    unittest.main()
