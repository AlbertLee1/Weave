"""Vertex API surface (VTX-109).

Adds ``client.vertex.scenarios.create / run / apply_to_main`` plus a
``scenario_id`` parameter on :meth:`weave_client.objects.ObjectsAPI.get`.
``run()`` starts a scenario run and polls the persisted run record until it
reaches a terminal status. The mounted server contract returns
``{"runRid": "...", "status": "pending"}`` from ``POST /runs``; clients then
read ``GET /runs/{runRid}``.
"""
from __future__ import annotations

import time
import urllib.parse
from typing import Any, Callable, Dict, Optional, TYPE_CHECKING

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
        streaming: bool = False,
        poll_interval: float = 1.0,
        timeout: Optional[float] = 60.0,
        sleep: Callable[[float], None] = time.sleep,
        monotonic: Callable[[], float] = time.monotonic,
    ) -> Dict[str, Any]:
        """Run a scenario.

        Blocks for the terminal Run record by starting the run and polling the
        mounted ``GET /runs/{runRid}`` route. Streaming is rejected until a
        stream endpoint is mounted and documented.
        """
        if streaming:
            raise NotImplementedError("vertex.scenarios.run streaming is not mounted; use polling")
        accepted = self.start_run(scenario_rid)
        run_rid = str(accepted.get("runRid", "")).strip()
        if not run_rid:
            raise RuntimeError("vertex.scenarios.run start response missing runRid")
        return self.wait_for_run(
            scenario_rid,
            run_rid,
            poll_interval=poll_interval,
            timeout=timeout,
            sleep=sleep,
            monotonic=monotonic,
        )

    def start_run(self, scenario_rid: str) -> Dict[str, Any]:
        """Start a scenario run and return ``{"runRid", "status"}``."""
        path = f"/api/vertex/v1/scenarios/{scenario_rid}/runs"
        return self._client._request("POST", path, json_body={}) or {}

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


def _scenario_run_record_path(scenario_rid: str, run_rid: str) -> str:
    return (
        "/api/vertex/v1/scenarios/"
        + urllib.parse.quote(scenario_rid, safe="")
        + "/runs/"
        + urllib.parse.quote(run_rid, safe="")
    )


def _is_terminal_run_status(status: str) -> bool:
    return status in {"succeeded", "failed", "canceled"}


__all__ = ["VertexAPI", "ScenariosAPI"]
