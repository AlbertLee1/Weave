"""Cookbook chapter 6 — WebSocket subscription with cursor + replay (US-418).

Opens a long-lived subscription on `WeaveAsyncClient.objects.subscribe`
and prints each event. Auto-reconnect resumes from the most recent
cursor via ``?since=<n>``; a connection-level ``onOutOfDate`` raises
``WeaveOutOfDate`` so the script can refresh full state before
re-subscribing.

Run::

    pip install 'weave-client[ws]'
    export WEAVE_BASE_URL=http://localhost:9117
    export WEAVE_TOKEN=...
    export WEAVE_ONTOLOGY=northwind
    export WEAVE_OBJECT_TYPE=Customer
    python3 docs/cookbook/06-ws-subscription.py

The script imports ``weave_client`` lazily so ``py_compile`` passes in
environments without the SDK installed.
"""
from __future__ import annotations

import asyncio
import json
import os
import sys
from typing import Any, Dict, Optional


def _parse_where(raw: Optional[str]) -> Optional[Dict[str, Any]]:
    if not raw:
        return None
    try:
        clause = json.loads(raw)
    except ValueError:
        print(f"WEAVE_WHERE is not valid JSON: {raw!r}", file=sys.stderr)
        sys.exit(2)
    if not isinstance(clause, dict):
        print("WEAVE_WHERE must decode to a JSON object", file=sys.stderr)
        sys.exit(2)
    return clause


async def run() -> int:
    from weave_client import WeaveAsyncClient, WeaveOutOfDate  # lazy import

    base_url = os.environ.get("WEAVE_BASE_URL", "http://localhost:9117")
    token = os.environ.get("WEAVE_TOKEN") or None
    ontology = os.environ.get("WEAVE_ONTOLOGY")
    object_type = os.environ.get("WEAVE_OBJECT_TYPE")
    where = _parse_where(os.environ.get("WEAVE_WHERE"))
    select_env = os.environ.get("WEAVE_SELECT")
    select = [s.strip() for s in select_env.split(",") if s.strip()] if select_env else None

    if not ontology or not object_type:
        print(
            "WEAVE_ONTOLOGY and WEAVE_OBJECT_TYPE must be set "
            "(e.g. northwind / Customer).",
            file=sys.stderr,
        )
        return 2

    client_kwargs: Dict[str, Any] = {}
    if token:
        client_kwargs["access_token"] = token

    async with WeaveAsyncClient(base_url, **client_kwargs) as client:
        while True:
            try:
                async with client.objects.subscribe(
                    ontology,
                    object_type,
                    where=where,
                    select=select,
                ) as sub:
                    print(
                        f"subscribed to {ontology}/{object_type} "
                        f"(filter={'yes' if where else 'no'}); waiting for events…"
                    )
                    async for evt in sub:
                        pk = evt.object.get("__primaryKey", "?")
                        print(
                            f"  cursor={evt.cursor:>4} state={evt.state:<18} pk={pk}"
                        )
            except WeaveOutOfDate as ood:
                # Cursor fell outside the replay window — refresh full state
                # and re-subscribe. A real consumer would call
                # client.objects.list/search here to reseed local cache.
                print(
                    f"  out-of-date (lastEventId={ood.last_event_id}); "
                    f"re-subscribing from latest…",
                    file=sys.stderr,
                )
                continue
            except KeyboardInterrupt:
                print("\nshutdown requested", file=sys.stderr)
                return 0


def main() -> int:
    try:
        return asyncio.run(run())
    except KeyboardInterrupt:
        return 0


if __name__ == "__main__":
    sys.exit(main())
