"""Retry policy shared by the sync + async SDK clients (US-358).

Retries only idempotent HTTP methods (GET / HEAD / OPTIONS / PUT / DELETE)
and only on the canonical "transient" status set (408, 425, 429, 500, 502,
503, 504) plus transport-level errors. Backoff is exponential with full
jitter, capped by ``max_delay``. The server's ``Retry-After`` header (in
delta-seconds or HTTP-date form) overrides the computed delay so
well-behaved clients yield to explicit guidance.

Non-idempotent methods (POST / PATCH) bypass retries unconditionally — they
may have already taken effect on the server, and replaying them silently
risks double-writes.
"""
from __future__ import annotations

import dataclasses
import email.utils
import random
from typing import Callable, Optional, Sequence

DEFAULT_RETRY_STATUSES = (408, 425, 429, 500, 502, 503, 504)
DEFAULT_RETRY_METHODS = ("GET", "HEAD", "OPTIONS", "PUT", "DELETE")


def header_get_ci(headers: "dict[str, str] | None", name: str) -> Optional[str]:
    """Case-insensitive header lookup against a plain ``dict[str, str]``.

    httpx already returns a case-insensitive mapping but the urllib fallback
    transport hands back a plain dict that preserves the original case sent by
    the server. We coerce to lowercase here so callers can ignore the
    distinction.
    """
    if not headers:
        return None
    target = name.lower()
    for k, v in headers.items():
        if k.lower() == target:
            return v
    return None


@dataclasses.dataclass
class RetryPolicy:
    """Configuration for the SDK's automatic retry behaviour.

    Default is "3 attempts, 100ms..2s exponential with full jitter". Pass
    ``RetryPolicy(max_attempts=1)`` to disable retries entirely.
    """

    max_attempts: int = 3
    base_delay: float = 0.1
    max_delay: float = 2.0
    multiplier: float = 2.0
    retry_statuses: Sequence[int] = DEFAULT_RETRY_STATUSES
    retry_methods: Sequence[str] = DEFAULT_RETRY_METHODS
    rand: Optional[random.Random] = None
    sleep: Optional[Callable[[float], None]] = None

    def attempts(self) -> int:
        return max(1, int(self.max_attempts))

    def is_retriable_method(self, method: str) -> bool:
        return method.upper() in {m.upper() for m in self.retry_methods}

    def is_retriable_status(self, status: int) -> bool:
        return status in set(self.retry_statuses)

    def backoff(self, attempt: int) -> float:
        """Return the delay for the given 0-indexed attempt, full-jitter."""
        cap = min(self.max_delay, self.base_delay * (self.multiplier ** attempt))
        if cap <= 0:
            return 0.0
        rng = self.rand if self.rand is not None else random
        return rng.uniform(0.0, cap)


def parse_retry_after(header: Optional[str], now: float) -> Optional[float]:
    """Parse a ``Retry-After`` header value into seconds, or ``None``.

    Accepts both delta-seconds (``"5"``) and HTTP-date forms
    (``"Wed, 21 Oct 2026 07:28:00 GMT"``). Returns ``None`` for malformed
    inputs so the caller falls back to its computed backoff.
    """
    if not header:
        return None
    h = header.strip()
    if not h:
        return None
    try:
        secs = float(h)
        if secs >= 0:
            return secs
    except ValueError:
        pass
    try:
        dt = email.utils.parsedate_to_datetime(h)
    except (TypeError, ValueError):
        return None
    if dt is None:
        return None
    target = dt.timestamp()
    return max(0.0, target - now)


__all__ = [
    "RetryPolicy",
    "DEFAULT_RETRY_STATUSES",
    "DEFAULT_RETRY_METHODS",
    "header_get_ci",
    "parse_retry_after",
]
