"""VTX-109 — Python SDK Vertex client unit tests."""
from __future__ import annotations

from typing import Any

import pytest

from weave_client._http import HTTPResponse
from weave_client.client import Client
from weave_client.vertex import _parse_sse_stream


class _StubTransport:
    """Records the last request and replies with caller-supplied JSON."""

    def __init__(self, reply: Any):
        self.reply = reply
        self.replies = list(reply) if isinstance(reply, list) else None
        self.last_method = ""
        self.last_url = ""
        self.last_headers: dict = {}
        self.last_body: Any = None
        self.requests: list[dict[str, Any]] = []

    def request(self, method, url, *, headers, json_body):
        self.last_method = method
        self.last_url = url
        self.last_headers = dict(headers)
        self.last_body = json_body
        self.requests.append({"method": method, "url": url, "headers": dict(headers), "body": json_body})
        reply = self.replies.pop(0) if self.replies is not None else self.reply
        import json as _json
        return HTTPResponse(200, _json.dumps(reply), {})


def _make_client(reply: Any) -> tuple[Client, _StubTransport]:
    t = _StubTransport(reply)
    c = Client("http://x", api_key="wvk_test", transport=t)
    return c, t


def test_vertex_scenarios_create_posts_to_scenarios_endpoint():
    c, t = _make_client({"rid": "ri.vertex.main.scenario.s1"})
    res = c.vertex.scenarios.create(
        case_study_rid="ri.vertex.main.case-study.cs1",
        name="snowstorm",
        parent_ontology_commit="commit-A",
    )
    assert res["rid"] == "ri.vertex.main.scenario.s1"
    assert t.last_method == "POST"
    assert t.last_url.endswith("/api/vertex/v1/scenarios")
    assert t.last_body == {
        "caseStudyRid": "ri.vertex.main.case-study.cs1",
        "name": "snowstorm",
        "parentOntologyCommit": "commit-A",
    }


def test_vertex_scenarios_apply_to_main_posts_to_apply_endpoint():
    c, t = _make_client({"ontologyCommit": "commit-B"})
    res = c.vertex.scenarios.apply_to_main("ri.vertex.main.scenario.s1")
    assert res["ontologyCommit"] == "commit-B"
    assert t.last_url.endswith("/api/vertex/v1/scenarios/ri.vertex.main.scenario.s1/apply")


def test_vertex_scenarios_run_streaming_false_returns_terminal_record():
    c, t = _make_client({"scenarioRunRid": "ri.vertex.main.scenario-run.r1", "status": "succeeded"})
    res = c.vertex.scenarios.run("ri.vertex.main.scenario.s1", streaming=False)
    assert isinstance(res, dict)
    assert res["status"] == "succeeded"
    assert t.last_url.endswith("/api/vertex/v1/scenarios/ri.vertex.main.scenario.s1/runs")


def test_vertex_scenarios_wait_for_run_polls_get_route_until_failed_terminal_record():
    c, t = _make_client(
        [
            {
                "rid": "ri.vertex.main.scenario-run.r1",
                "scenarioRid": "ri.vertex.main.scenario.s1",
                "status": "pending",
            },
            {
                "rid": "ri.vertex.main.scenario-run.r1",
                "scenarioRid": "ri.vertex.main.scenario.s1",
                "status": "failed",
                "error": "scoring failed",
                "checkpoint": {
                    "runRid": "ri.vertex.main.scenario-run.r1",
                    "scenarioRid": "ri.vertex.main.scenario.s1",
                    "status": "failed",
                    "attemptsById": {"score": 3},
                    "error": "scoring failed",
                    "updatedAt": "2026-05-20T00:00:00Z",
                },
            },
        ]
    )
    sleeps: list[float] = []

    res = c.vertex.scenarios.wait_for_run(
        "ri.vertex.main.scenario.s1",
        "ri.vertex.main.scenario-run.r1",
        poll_interval=0.25,
        timeout=10.0,
        sleep=sleeps.append,
    )

    assert [r["method"] for r in t.requests] == ["GET", "GET"]
    assert all(
        r["url"].endswith(
            "/api/vertex/v1/scenarios/ri.vertex.main.scenario.s1/runs/ri.vertex.main.scenario-run.r1"
        )
        for r in t.requests
    )
    assert sleeps == [0.25]
    assert res["status"] == "failed"
    assert res["error"] == "scoring failed"
    assert res["checkpoint"]["attemptsById"]["score"] == 3


def test_vertex_scenarios_wait_for_run_returns_canceled_record():
    c, _ = _make_client(
        {
            "rid": "ri.vertex.main.scenario-run.r1",
            "scenarioRid": "ri.vertex.main.scenario.s1",
            "status": "canceled",
            "error": "operator canceled",
            "checkpoint": {
                "runRid": "ri.vertex.main.scenario-run.r1",
                "scenarioRid": "ri.vertex.main.scenario.s1",
                "status": "canceled",
                "attemptsById": {},
                "error": "operator canceled",
                "updatedAt": "2026-05-20T00:00:00Z",
            },
        }
    )

    res = c.vertex.scenarios.wait_for_run("ri.vertex.main.scenario.s1", "ri.vertex.main.scenario-run.r1")

    assert res["status"] == "canceled"
    assert res["error"] == "operator canceled"


def test_vertex_scenarios_wait_for_run_reports_timeout_without_real_sleep():
    c, _ = _make_client(
        [
            {
                "rid": "ri.vertex.main.scenario-run.r1",
                "scenarioRid": "ri.vertex.main.scenario.s1",
                "status": "running",
            },
            {
                "rid": "ri.vertex.main.scenario-run.r1",
                "scenarioRid": "ri.vertex.main.scenario.s1",
                "status": "running",
            },
        ]
    )
    ticks = iter([0.0, 0.1, 0.1])

    with pytest.raises(TimeoutError):
        c.vertex.scenarios.wait_for_run(
            "ri.vertex.main.scenario.s1",
            "ri.vertex.main.scenario-run.r1",
            poll_interval=1.0,
            timeout=0.1,
            sleep=lambda _: None,
            monotonic=lambda: next(ticks),
        )


def test_objects_get_with_scenario_id_sets_x_scenario_id_header():
    c, t = _make_client({"id": "JFK", "properties": {"capacity": 150}})
    c.objects.get("aviation", "Airport", "JFK", scenario_id="ri.vertex.main.scenario.s1")
    assert t.last_headers.get("X-Scenario-Id") == "ri.vertex.main.scenario.s1"


def test_objects_get_without_scenario_id_does_not_set_header():
    c, t = _make_client({"id": "JFK"})
    c.objects.get("aviation", "Airport", "JFK")
    assert "X-Scenario-Id" not in t.last_headers


def test_parse_sse_stream_yields_event_dicts_in_order():
    chunks = [
        b'data: {"kind": "progress", "percent": 25}\n\n',
        b'data: {"kind": "progress", "percent": 100}\n\ndata: {"kind": "completed", "scenarioRunRid": "r1"}\n\n',
    ]
    out = list(_parse_sse_stream(chunks))
    assert len(out) == 3
    assert out[0]["kind"] == "progress" and out[0]["percent"] == 25
    assert out[2]["kind"] == "completed"


def test_parse_sse_stream_drops_malformed_events():
    chunks = [b'data: not-json\n\ndata: {"kind": "progress", "percent": 10}\n\n']
    out = list(_parse_sse_stream(chunks))
    assert len(out) == 1
    assert out[0]["percent"] == 10
