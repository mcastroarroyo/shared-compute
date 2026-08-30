"""Executable reference model for Ayni's financial invariants."""

from __future__ import annotations

from dataclasses import dataclass
from threading import Lock

from .replay import SettlementGuard


class LedgerError(ValueError):
    pass


@dataclass(frozen=True)
class LedgerSnapshot:
    funded_micros: int
    customer_balances_micros: int
    charged_micros: int
    provider_earned_micros: int
    provider_paid_micros: int


class ReferenceLedger:
    """Small state machine used for randomized adversarial traces.

    It is not a production database. It defines conservation and idempotency properties
    that every production store must satisfy under retries and concurrency.
    """

    def __init__(self) -> None:
        self._lock = Lock()
        self._settlements = SettlementGuard()
        self._funded = 0
        self._charged = 0
        self._earned = 0
        self._paid = 0
        self._balances: dict[str, int] = {}
        self._provider_balances: dict[str, int] = {}
        self._charged_jobs: set[str] = set()

    def fund(self, account_id: str, amount_micros: int, payment_id: str, seen_payments: set[str]) -> bool:
        if not account_id or not payment_id or amount_micros <= 0:
            raise LedgerError("invalid funding event")
        with self._lock:
            if payment_id in seen_payments:
                return False
            seen_payments.add(payment_id)
            self._funded += amount_micros
            self._balances[account_id] = self._balances.get(account_id, 0) + amount_micros
            return True

    def charge(self, account_id: str, job_id: str, gross_micros: int) -> bool:
        if not account_id or not job_id or gross_micros < 0:
            raise LedgerError("invalid charge")
        with self._lock:
            if job_id in self._charged_jobs:
                return False
            if self._balances.get(account_id, 0) < gross_micros:
                raise LedgerError("insufficient credit")
            self._charged_jobs.add(job_id)
            self._balances[account_id] -= gross_micros
            self._charged += gross_micros
            return True

    def accrue(
        self,
        *,
        job_id: str,
        device_pk: str,
        provider_id: str,
        gross_micros: int,
        provider_micros: int,
    ) -> bool:
        if not provider_id or gross_micros < 0 or provider_micros < 0:
            raise LedgerError("invalid accrual")
        if provider_micros > gross_micros:
            raise LedgerError("provider earnings exceed job gross")
        with self._lock:
            if job_id not in self._charged_jobs:
                raise LedgerError("unbilled job cannot create earnings")
            if not self._settlements.claim(job_id, device_pk):
                return False
            self._earned += provider_micros
            self._provider_balances[provider_id] = (
                self._provider_balances.get(provider_id, 0) + provider_micros
            )
            return True

    def payout(self, provider_id: str, amount_micros: int) -> None:
        if amount_micros <= 0:
            raise LedgerError("invalid payout")
        with self._lock:
            owed = self._provider_balances.get(provider_id, 0)
            if amount_micros > owed:
                raise LedgerError("payout exceeds accrued balance")
            self._provider_balances[provider_id] = owed - amount_micros
            self._paid += amount_micros

    def snapshot(self) -> LedgerSnapshot:
        with self._lock:
            return LedgerSnapshot(
                funded_micros=self._funded,
                customer_balances_micros=sum(self._balances.values()),
                charged_micros=self._charged,
                provider_earned_micros=self._earned,
                provider_paid_micros=self._paid,
            )

    def assert_invariants(self) -> None:
        snap = self.snapshot()
        if snap.funded_micros != snap.customer_balances_micros + snap.charged_micros:
            raise AssertionError("customer credits were created or destroyed")
        if snap.provider_earned_micros > snap.charged_micros:
            raise AssertionError("provider earnings exceed customer charges")
        if snap.provider_paid_micros > snap.provider_earned_micros:
            raise AssertionError("payouts exceed provider earnings")
