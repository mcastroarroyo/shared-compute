"""
Ayni workload runner client.

Run batch inference on the Ayni network — a marketplace of idle, benchmarked consumer
devices — with one quote up front and one charge at the quoted price.

Standard library only, so it vendors cleanly into a Cloud Function, a Lambda, or a
cron box:

    from ayni import Ayni

    ayni = Ayni(api_key="sc_live_...")
    result = ayni.run(
        prompts=["Classify this log line: ..."],
        max_price_usd=5.00,          # refuse to run if the quote comes back higher
    )
    for item in result.items:
        print(item.content)

Full guide: https://ayni-ai.com/runners
"""

from .client import (  # noqa: F401
    Ayni,
    AyniError,
    CouncilBlocked,
    InsufficientCredit,
    Quote,
    Run,
    RunItem,
    RunResult,
    verify_webhook,
)

__all__ = [
    "Ayni",
    "AyniError",
    "CouncilBlocked",
    "InsufficientCredit",
    "Quote",
    "Run",
    "RunItem",
    "RunResult",
    "verify_webhook",
]
__version__ = "0.1.0"
