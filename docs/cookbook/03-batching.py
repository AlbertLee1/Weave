"""Cookbook chapter 3 — Batching.

Demonstrates ``ActionsAPI.apply_batch`` with client-side chunking. The
recipe builds N synthetic Customer invocations, walks them in groups of
``CHUNK_SIZE``, and submits each chunk in a single round-trip with
``return_edits="NONE"`` so the response payload stays small.

Run::

    export WEAVE_BASE_URL=http://localhost:9117
    export WEAVE_TOKEN=...
    python3 docs/cookbook/03-batching.py
"""
from __future__ import annotations

import os
import sys
import time
from typing import Iterable, Iterator, List

from weave_client import Client, WeaveError


CHUNK_SIZE = 250


def chunked(seq: List[dict], size: int) -> Iterator[List[dict]]:
    """Yield successive ``size``-length slices of ``seq``."""
    for i in range(0, len(seq), size):
        yield seq[i : i + size]


def build_invocations(start: int, count: int) -> List[dict]:
    """Synthesise ``count`` createCustomer parameter envelopes.

    Adapt the keys to whichever Action your ontology actually exposes —
    Northwind ships ``createCustomer`` with at least
    ``customer_id`` / ``company_name``.
    """
    return [
        {
            "parameters": {
                "customer_id": f"BTCH{i:04d}",
                "company_name": f"Cookbook Co {i}",
            },
        }
        for i in range(start, start + count)
    ]


def submit_in_chunks(client: Client, ontology: str, action: str, invocations: List[dict]) -> int:
    """Submit ``invocations`` to ``apply_batch`` in CHUNK_SIZE-sized chunks.

    Returns the number of objects added across every chunk (the server
    returns counts in ``resp.edits``, not a per-row list).
    """
    added = 0
    for chunk in chunked(invocations, CHUNK_SIZE):
        try:
            resp = client.actions.apply_batch(
                ontology, action, chunk, return_edits="NONE",
            )
        except WeaveError as err:
            if err.error_name == "BatchError":
                idx = err.parameters.get("index")
                phase = err.parameters.get("phase")
                cause = err.parameters.get("cause")
                print(
                    f"batch failed at row {idx} during {phase}: {cause}",
                    file=sys.stderr,
                )
            raise
        edits = resp.edits
        chunk_added = edits.added_object_count if edits is not None else 0
        added += chunk_added
        print(f"committed chunk of {len(chunk)} (added so far: {added})")
    return added


def main() -> int:
    base_url = os.environ.get("WEAVE_BASE_URL", "http://localhost:9117")
    token = os.environ.get("WEAVE_TOKEN") or None
    ontology = os.environ.get("WEAVE_ONTOLOGY", "northwind")
    action = os.environ.get("WEAVE_ACTION", "createCustomer")
    rows = int(os.environ.get("WEAVE_BATCH_ROWS", "1000"))

    invocations = build_invocations(start=0, count=rows)
    started = time.perf_counter()

    with Client(base_url, access_token=token) as client:
        try:
            committed = submit_in_chunks(client, ontology, action, invocations)
        except WeaveError as err:
            print(f"weave error: {err}", file=sys.stderr)
            return 1

    elapsed = time.perf_counter() - started
    chunk_count = (len(invocations) + CHUNK_SIZE - 1) // CHUNK_SIZE
    print(
        f"submitted {len(invocations)} {action!r} invocations in {chunk_count} batches "
        f"(server reported +{committed} adds; {elapsed:.1f}s wall-clock)"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
