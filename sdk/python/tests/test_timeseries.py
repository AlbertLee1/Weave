"""BDD acceptance tests for the round-44 TimeSeriesAPI (PRD-V2 Gap-D2)."""
from __future__ import annotations

import json
import unittest
from datetime import datetime, timezone

from weave_client import Client, TimeSeriesPoint

from tests.test_client import _StubServer


class TimeSeriesReadEndpointsTests(unittest.TestCase):
    """get_first_point / get_last_point / get_latest_value /
    stream_points / stream_values."""

    def test_get_first_point_parses_response(self):
        resp = json.dumps({"time": "2026-01-01T00:00:00Z", "value": 1.5})
        routes = {
            "GET /api/v2/ontologies/nw/objects/Reading/sensor-001/timeseries/temperature/firstPoint": (200, resp),
        }
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            pt = c.timeseries.get_first_point("nw", "Reading", "sensor-001", "temperature")
        self.assertIsInstance(pt, TimeSeriesPoint)
        self.assertEqual(pt.value, 1.5)
        self.assertEqual(pt.time, datetime(2026, 1, 1, 0, 0, tzinfo=timezone.utc))

    def test_get_last_point_uses_correct_path(self):
        resp = json.dumps({"time": "2026-04-01T00:00:00Z", "value": 99.9})
        routes = {
            "GET /api/v2/ontologies/nw/objects/Reading/sensor-001/timeseries/temperature/lastPoint": (200, resp),
        }
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            pt = c.timeseries.get_last_point("nw", "Reading", "sensor-001", "temperature")
        self.assertEqual(pt.value, 99.9)

    def test_get_latest_value_uses_latestValue_path(self):
        # TimeSeriesValueBankProperty endpoint — different URL even
        # though the wire shape matches get_last_point.
        resp = json.dumps({"time": "2026-05-01T00:00:00Z", "value": "active"})
        routes = {
            "GET /api/v2/ontologies/nw/objects/State/dev-1/timeseries/status/latestValue": (200, resp),
        }
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            pt = c.timeseries.get_latest_value("nw", "State", "dev-1", "status")
        self.assertEqual(pt.value, "active")

    def test_stream_points_parses_array(self):
        resp = json.dumps([
            {"time": "2026-01-01T00:00:00Z", "value": 1.0},
            {"time": "2026-01-02T00:00:00Z", "value": 2.0},
            {"time": "2026-01-03T00:00:00Z", "value": 3.0},
        ])
        routes = {
            "POST /api/v2/ontologies/nw/objects/Reading/sensor-001/timeseries/temperature/streamPoints": (200, resp),
        }
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            pts = c.timeseries.stream_points("nw", "Reading", "sensor-001", "temperature")
        self.assertEqual(len(pts), 3)
        self.assertEqual([p.value for p in pts], [1.0, 2.0, 3.0])

    def test_stream_points_handles_empty_array(self):
        routes = {
            "POST /api/v2/ontologies/nw/objects/Reading/sensor-001/timeseries/temperature/streamPoints": (200, "[]"),
        }
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            pts = c.timeseries.stream_points("nw", "Reading", "sensor-001", "temperature")
        self.assertEqual(pts, [])

    def test_stream_values_uses_streamValues_path(self):
        resp = json.dumps([
            {"time": "2026-01-01T00:00:00Z", "value": "ok"},
            {"time": "2026-01-02T00:00:00Z", "value": "err"},
        ])
        routes = {
            "POST /api/v2/ontologies/nw/objects/State/dev-1/timeseries/status/streamValues": (200, resp),
        }
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            pts = c.timeseries.stream_values("nw", "State", "dev-1", "status")
        self.assertEqual([p.value for p in pts], ["ok", "err"])


class TimeSeriesAppendPointTests(unittest.TestCase):

    def test_append_point_with_iso_string(self):
        routes = {
            "POST /api/v2/ontologies/nw/objects/Reading/sensor-001/timeseries/temperature/points": (204, ""),
        }
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.timeseries.append_point(
                "nw", "Reading", "sensor-001", "temperature",
                time="2026-04-01T00:00:00Z", value=42.5,
            )
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent, {"time": "2026-04-01T00:00:00Z", "value": 42.5})

    def test_append_point_with_datetime_aware_tz(self):
        routes = {
            "POST /api/v2/ontologies/nw/objects/Reading/sensor-001/timeseries/temperature/points": (204, ""),
        }
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            t = datetime(2026, 4, 1, 12, 0, 0, tzinfo=timezone.utc)
            c.timeseries.append_point(
                "nw", "Reading", "sensor-001", "temperature",
                time=t, value=42.5,
            )
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent["value"], 42.5)
        # ISO format with timezone preserved.
        self.assertIn("2026-04-01T12:00:00", sent["time"])

    def test_append_point_with_datetime_naive_assumes_utc(self):
        routes = {
            "POST /api/v2/ontologies/nw/objects/Reading/sensor-001/timeseries/temperature/points": (204, ""),
        }
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            t = datetime(2026, 4, 1, 12, 0, 0)  # naive
            c.timeseries.append_point(
                "nw", "Reading", "sensor-001", "temperature",
                time=t, value=42.5,
            )
            sent = json.loads(srv.requests[0]["body"])
        self.assertTrue(sent["time"].endswith("Z"))


class TimeSeriesTransformTests(unittest.TestCase):

    def test_transform_with_inline_points(self):
        resp = json.dumps([
            {"time": "2026-01-01T00:00:00Z", "value": 10.0},
            {"time": "2026-01-02T00:00:00Z", "value": 20.0},
        ])
        routes = {
            "POST /api/v2/ontologies/nw/timeseries/transform": (200, resp),
        }
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            out = c.timeseries.transform(
                "nw",
                points=[
                    {"time": "2026-01-01T00:00:00Z", "value": 5.0},
                    {"time": "2026-01-02T00:00:00Z", "value": 10.0},
                ],
                steps=[{"op": "scale", "params": {"factor": 2.0}}],
            )
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(len(out), 2)
        self.assertEqual(out[1].value, 20.0)
        self.assertIn("points", sent)
        self.assertIn("steps", sent)
        self.assertNotIn("source", sent)

    def test_transform_with_source(self):
        routes = {
            "POST /api/v2/ontologies/nw/timeseries/transform": (200, "[]"),
        }
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.timeseries.transform(
                "nw",
                source={
                    "objectType": "Reading",
                    "primaryKey": "sensor-001",
                    "property": "temperature",
                },
                steps=[{"op": "smooth", "params": {"window": 5}}],
            )
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent["source"]["objectType"], "Reading")
        self.assertNotIn("points", sent)

    def test_transform_rejects_both_source_and_points(self):
        c = Client("http://nowhere", access_token="t")
        with self.assertRaises(ValueError):
            c.timeseries.transform(
                "nw",
                source={"objectType": "X", "primaryKey": "1", "property": "p"},
                points=[{"time": "2026-01-01T00:00:00Z", "value": 1.0}],
                steps=[],
            )

    def test_transform_rejects_neither_source_nor_points(self):
        c = Client("http://nowhere", access_token="t")
        with self.assertRaises(ValueError):
            c.timeseries.transform("nw", steps=[])


if __name__ == "__main__":
    unittest.main()
