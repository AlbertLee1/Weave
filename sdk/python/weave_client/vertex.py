"""Vertex API surface (VTX-109).

Adds ``client.vertex.scenarios.create / run / apply_to_main`` plus a
``scenario_id`` parameter on :meth:`weave_client.objects.ObjectsAPI.get`.
``run()`` returns a generator that yields progress dicts parsed from the
server's SSE stream, matching the BDD acceptance criteria in PRD VTX-109.

The Vertex endpoints themselves are owned by VTX-044 in another stream;
this module is a thin pass-through that documents the contract and works
the moment the server side ships.
"""
from __future__ import annotations

import json
import time
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, Callable, Dict, Generator, Iterable, Optional, TYPE_CHECKING

if TYPE_CHECKING:  # avoid runtime circular import
    from .client import Client


class VertexAPI:
    """Top-level Vertex namespace exposed as ``client.vertex``."""

    def __init__(self, client: "Client"):
        self._client = client
        self.scenarios = ScenariosAPI(client)


class ScenariosAPI:
    """Scenario create / run / apply_to_main."""

    def __init__(self, client: "Client"):
        self._client = client

    def create(
        self,
        *,
        case_study_rid: str,
        name: str,
        parent_ontology_commit: str,
    ) -> Dict[str, Any]:
        body = {
            "caseStudyRid": case_study_rid,
            "name": name,
            "parentOntologyCommit": parent_ontology_commit,
        }
        return self._client._request("POST", "/api/vertex/v1/scenarios", json_body=body) or {}

    def apply_to_main(self, scenario_rid: str) -> Dict[str, Any]:
        path = f"/api/vertex/v1/scenarios/{scenario_rid}/apply"
        return self._client._request("POST", path, json_body={}) or {}

    def run(
        self,
        scenario_rid: str,
        *,
        streaming: bool = True,
    ) -> "Generator[Dict[str, Any], None, None] | Dict[str, Any]":
        """Run a scenario.

        When ``streaming=True`` (the default), returns a generator yielding
        SSE event dicts (``{"kind": "progress", "percent": 25}`` etc.).
        When ``streaming=False``, blocks for the terminal Run record and
        returns it as a dict.
        """
        path = f"/api/vertex/v1/scenarios/{scenario_rid}/runs"
        if not streaming:
            return self._client._request("POST", path, json_body={}) or {}
        return _sse_generator(self._client.base_url + path, headers=self._client._headers())

    def get_run(self, scenario_rid: str, run_rid: str) -> Dict[str, Any]:
        """Fetch a persisted scenario-run record."""
        path = _scenario_run_record_path(scenario_rid, run_rid)
        return self._client._request("GET", path, json_body=None) or {}

    def wait_for_run(
        self,
        scenario_rid: str,
        run_rid: str,
        *,
        poll_interval: float = 1.0,
        timeout: Optional[float] = 60.0,
        sleep: Callable[[float], None] = time.sleep,
        monotonic: Callable[[], float] = time.monotonic,
    ) -> Dict[str, Any]:
        """Poll a scenario run until it reaches a terminal status.

        Returns failed and canceled terminal records as-is so callers can inspect
        ``error`` and ``checkpoint`` details instead of treating completion as
        success-only.
        """
        deadline = None if timeout is None else monotonic() + max(0.0, timeout)
        interval = max(0.0, poll_interval)
        while True:
            if deadline is not None and monotonic() >= deadline:
                raise TimeoutError(f"vertex.scenarios.wait_for_run timed out after {timeout} seconds")
            run = self.get_run(scenario_rid, run_rid)
            if _is_terminal_run_status(str(run.get("status", ""))):
                return run
            if deadline is not None:
                remaining = deadline - monotonic()
                if remaining <= 0:
                    raise TimeoutError(f"vertex.scenarios.wait_for_run timed out after {timeout} seconds")
                delay = min(interval, remaining)
            else:
                delay = interval
            if delay > 0:
                sleep(delay)


def _sse_generator(url: str, *, headers: Dict[str, str]) -> Generator[Dict[str, Any], None, None]:
    """Open url, POST empty body, parse SSE event blocks into dicts.

    This intentionally does not run through Transport.request: SSE needs a
    streaming response and Transport.request consumes the body eagerly.
    """
    req = urllib.request.Request(
        url=url,
        data=b"{}",
        method="POST",
        headers={**headers, "Accept": "text/event-stream", "Content-Type": "application/json"},
    )
    try:
        resp = urllib.request.urlopen(req)
    except urllib.error.HTTPError as e:  # pragma: no cover - server may 404 in dev
        raise RuntimeError(f"vertex.scenarios.run: {e.code} {e.reason}")
    buf = ""
    try:
        for chunk in iter(lambda: resp.read(4096), b""):
            buf += chunk.decode("utf-8", errors="replace")
            while True:
                idx = buf.find("\n\n")
                if idx == -1:
                    break
                block = buf[:idx]
                buf = buf[idx + 2 :]
                data_lines = [
                    line[len("data:") :].lstrip()
                    for line in block.splitlines()
                    if line.startswith("data:")
                ]
                if not data_lines:
                    continue
                try:
                    yield json.loads("\n".join(data_lines))
                except json.JSONDecodeError:
                    continue
    finally:
        resp.close()


def _scenario_run_record_path(scenario_rid: str, run_rid: str) -> str:
    return (
        "/api/vertex/v1/scenarios/"
        + urllib.parse.quote(scenario_rid, safe="")
        + "/runs/"
        + urllib.parse.quote(run_rid, safe="")
    )


def _is_terminal_run_status(status: str) -> bool:
    return status in {"succeeded", "failed", "canceled"}


# ---------------------------------------------------------------------------
# Test helper — drives the generator off an injected raw iterable so the unit
# test suite can hit the parser without a live server. Not part of the public
# API; kept lower-cased to discourage external use.
# ---------------------------------------------------------------------------


def _parse_sse_stream(raw_chunks: Iterable[bytes]) -> Generator[Dict[str, Any], None, None]:
    buf = ""
    for chunk in raw_chunks:
        buf += chunk.decode("utf-8", errors="replace")
        while True:
            idx = buf.find("\n\n")
            if idx == -1:
                break
            block = buf[:idx]
            buf = buf[idx + 2 :]
            data_lines = [
                line[len("data:") :].lstrip()
                for line in block.splitlines()
                if line.startswith("data:")
            ]
            if not data_lines:
                continue
            try:
                yield json.loads("\n".join(data_lines))
            except json.JSONDecodeError:
                continue


__all__ = ["VertexAPI", "ScenariosAPI"]
