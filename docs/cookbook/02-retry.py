"""Cookbook chapter 2 — Retry.

Demonstrates configuring ``RetryPolicy`` for two scenarios:

1. A production client that should ride out brief 5xx blips.
2. A test that needs deterministic, instant backoff so the suite stays sub-second.

Run::

    export WEAVE_BASE_URL=http://localhost:9117
    python3 docs/cookbook/02-retry.py
"""
from __future__ import annotations

import os
import random
import sys

from weave_client import Client, RetryPolicy, WeaveError


def production_client(base_url: str, token: str | None) -> Client:
    """Production-shaped retry: 5 attempts, jittered backoff capped at 10s."""
    policy = RetryPolicy(
        max_attempts=5,
        base_delay=0.25,
        max_delay=10.0,
        multiplier=2.0,
    )
    return Client(base_url, access_token=token, retry=policy)


def deterministic_client(base_url: str) -> Client:
    """Test-shaped retry: same logical attempts, but jitter and sleep are fixed."""
    policy = RetryPolicy(
        max_attempts=4,
        base_delay=0.1,
        max_delay=2.0,
        rand=random.Random(0xC00C),  # seeded so jitter is reproducible
        sleep=lambda secs: None,     # collapse waits to zero — for tests
    )
    return Client(base_url, retry=policy)


def disabled_client(base_url: str) -> Client:
    """A client with retries disabled — one attempt only."""
    return Client(base_url, retry=RetryPolicy(max_attempts=1))


def main() -> int:
    base_url = os.environ.get("WEAVE_BASE_URL", "http://localhost:9117")
    token = os.environ.get("WEAVE_TOKEN") or None

    with production_client(base_url, token) as client:
        try:
            ontologies = client.ontologies.list()
        except WeaveError as err:
            print(f"production client failed even after retries: {err}", file=sys.stderr)
            return 1

        if not ontologies:
            print(
                "No ontologies on this server — load a fixture (testdata/northwind) "
                "before re-running this recipe.",
                file=sys.stderr,
            )
            return 0

        print(f"production client succeeded: {len(ontologies)} ontologies")

    with deterministic_client(base_url) as client:
        # Same call, but any retries inside the SDK are instantaneous.
        ontologies = client.ontologies.list()
        print(f"deterministic client succeeded: {len(ontologies)} ontologies")

    print("disabled-client config:", disabled_client(base_url)._transport.retry)
    return 0


if __name__ == "__main__":
    sys.exit(main())
