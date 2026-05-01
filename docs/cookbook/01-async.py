"""Cookbook chapter 1 — Async.

Demonstrates concurrent fan-out using ``WeaveAsyncClient`` and
``asyncio.gather``. The script picks an ontology, lists its ObjectTypes,
hydrates the full metadata of each one in parallel, and prints a brief
summary. Bound by the server's concurrency, not the SDK's.

Run::

    export WEAVE_BASE_URL=http://localhost:9117
    export WEAVE_TOKEN=...        # optional under AUTH_MODE=dev
    python3 docs/cookbook/01-async.py
"""
from __future__ import annotations

import asyncio
import os
import sys
import time

from weave_client import WeaveAsyncClient, WeaveError


async def hydrate(client: WeaveAsyncClient, ontology: str, names: list[str]) -> dict[str, dict]:
    """Fetch full metadata for every object type concurrently.

    The ``asyncio.gather`` call dispatches all coroutines onto the same
    event loop. httpx pools connections per host so reused sockets stay
    warm across the fan-out.
    """
    coros = [
        client.ontologies.get_object_type_full_metadata(ontology, name)
        for name in names
    ]
    payloads = await asyncio.gather(*coros)
    return dict(zip(names, payloads))


async def main() -> int:
    base_url = os.environ.get("WEAVE_BASE_URL", "http://localhost:9117")
    token = os.environ.get("WEAVE_TOKEN") or None

    async with WeaveAsyncClient(base_url, access_token=token) as client:
        ontologies = await client.ontologies.list()
        if not ontologies:
            print(
                "No ontologies on this server — load a fixture (testdata/northwind) "
                "before re-running this recipe.",
                file=sys.stderr,
            )
            return 0

        ont = ontologies[0].api_name
        types = await client.ontologies.list_object_types(ont)
        names = [t.api_name for t in types[:8]]
        if not names:
            print(f"Ontology {ont!r} has no ObjectTypes.", file=sys.stderr)
            return 0

        started = time.perf_counter()
        full = await hydrate(client, ont, names)
        elapsed = time.perf_counter() - started

        print(f"Hydrated {len(full)} object types from {ont!r} in {elapsed * 1000:.0f}ms")
        for name, payload in full.items():
            props = payload.get("properties") or {}
            print(f"  {name}: {len(props)} properties")
        return 0


if __name__ == "__main__":
    try:
        sys.exit(asyncio.run(main()))
    except WeaveError as err:
        print(f"weave error: {err}", file=sys.stderr)
        sys.exit(1)
