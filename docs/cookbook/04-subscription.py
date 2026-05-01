"""Cookbook chapter 4 — SSE subscription with resume.

Opens a long-lived SSE stream against a saved ObjectSet, persists
``lastEventId`` so disconnects can resume from the ring buffer, and
applies exponential backoff between reconnect attempts.

Run::

    export WEAVE_BASE_URL=http://localhost:9117
    export WEAVE_TOKEN=...
    export WEAVE_ONTOLOGY=northwind
    export WEAVE_OBJECT_SET_RID=ri.os.main.objectSet.<uuid>
    python3 docs/cookbook/04-subscription.py

The script imports ``httpx`` only when invoked — module-load is import-light
so ``py_compile`` works in environments without the runtime dep installed.
"""
from __future__ import annotations

import json
import os
import sys
import time
from typing import Iterator, Optional


def url_with_resume(base_url: str, ontology: str, rid: str, last_id: Optional[str]) -> str:
    """Build the subscribe URL with an optional ``?lastEventId=`` resume hint."""
    suffix = f"?lastEventId={last_id}" if last_id else ""
    return (
        f"{base_url.rstrip('/')}"
        f"/api/v2/ontologies/{ontology}/objectSets/{rid}/subscribe{suffix}"
    )


def parse_sse_frames(line_iter: Iterator[str]) -> Iterator[tuple[str, dict]]:
    """Yield ``(event_id, payload)`` for each complete SSE frame.

    Implements the bare-minimum SSE parser: ``id: <n>`` updates the
    current event id, ``data: <json>`` lines accumulate, comment lines
    (``:ping``) are ignored, and a blank line terminates a frame.
    """
    event_id = ""
    data_lines: list[str] = []
    for raw in line_iter:
        if raw == "":
            if data_lines:
                payload_text = "\n".join(data_lines)
                try:
                    yield event_id, json.loads(payload_text)
                except ValueError:
                    pass  # malformed — skip rather than crash the loop
                data_lines = []
                event_id = ""
            continue
        if raw.startswith(":"):
            continue  # heartbeat
        if raw.startswith("id: "):
            event_id = raw[4:].strip()
        elif raw.startswith("data: "):
            data_lines.append(raw[6:])


def handle(payload: dict) -> None:
    """User callback — replace with your application logic."""
    obj = payload.get("object") or {}
    print(
        f"  event={payload.get('eventType')!s:<18} "
        f"pk={obj.get('__primaryKey')} "
        f"type={obj.get('__apiName')}"
    )


def consume_session(
    base_url: str,
    ontology: str,
    rid: str,
    token: Optional[str],
    last_event_id: Optional[str],
) -> Optional[str]:
    """Run one SSE session until the server hangs up.

    Returns the most recent ``lastEventId`` so the caller can resume.
    """
    import httpx  # imported lazily so py_compile passes without the dep installed

    headers: dict[str, str] = {}
    if token:
        headers["Authorization"] = f"Bearer {token}"

    url = url_with_resume(base_url, ontology, rid, last_event_id)
    print(f"connecting {url}")
    with httpx.stream("GET", url, headers=headers, timeout=None) as resp:
        resp.raise_for_status()
        for event_id, payload in parse_sse_frames(resp.iter_lines()):
            if event_id:
                last_event_id = event_id
            handle(payload)
    return last_event_id


def main() -> int:
    base_url = os.environ.get("WEAVE_BASE_URL", "http://localhost:9117")
    ontology = os.environ.get("WEAVE_ONTOLOGY")
    rid = os.environ.get("WEAVE_OBJECT_SET_RID")
    token = os.environ.get("WEAVE_TOKEN") or None

    if not ontology or not rid:
        print(
            "WEAVE_ONTOLOGY and WEAVE_OBJECT_SET_RID must be set — "
            "create a temporary ObjectSet first via client.objectsets.create_temporary().",
            file=sys.stderr,
        )
        return 2

    last_event_id: Optional[str] = None
    delay = 1.0
    while True:
        try:
            last_event_id = consume_session(base_url, ontology, rid, token, last_event_id)
            delay = 1.0  # successful session — reset
        except KeyboardInterrupt:
            print("\nshutdown requested", file=sys.stderr)
            return 0
        except Exception as err:  # noqa: BLE001 — broad catch is intentional for the reconnect loop
            print(f"stream lost: {err!r} (retry in {delay:.1f}s)", file=sys.stderr)
            time.sleep(delay)
            delay = min(delay * 2, 30.0)


if __name__ == "__main__":
    sys.exit(main())
