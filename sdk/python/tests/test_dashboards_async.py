"""BDD acceptance tests for the round-78 AsyncDashboardsAPI mirror.

Round 76 added the sync DashboardsAPI; this round mirrors it on
WeaveAsyncClient. Reuses Dashboard dataclass via lazy import
(round-61/74 pattern).
"""
from __future__ import annotations

import json
import unittest

from weave_client import Dashboard, WeaveAsyncClient

from tests.test_client import _StubServer


def _route(payload, status=200):
    return (status, json.dumps(payload))


def _dashboard_payload(**overrides):
    base = {
        "id": "d-1", "name": "Sales", "createdBy": "u",
        "isPublic": False, "definition": {},
        "createdAt": "2026-05-25T00:00:00Z",
        "updatedAt": "2026-05-25T00:00:00Z",
    }
    base.update(overrides)
    return base


class AsyncDashboardsAPITests(unittest.IsolatedAsyncioTestCase):

    async def test_list_returns_typed_dashboards(self):
        payload = {"dashboards": [_dashboard_payload(id="d-1"), _dashboard_payload(id="d-2", isPublic=True)]}
        routes = {"GET /api/v2/dashboards": _route(payload)}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                items = await c.dashboards.list()
        self.assertEqual(len(items), 2)
        self.assertIsInstance(items[0], Dashboard)
        self.assertEqual(items[0].id, "d-1")
        self.assertTrue(items[1].is_public)

    async def test_list_empty_returns_empty_list(self):
        routes = {"GET /api/v2/dashboards": _route({"dashboards": None})}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                items = await c.dashboards.list()
        self.assertEqual(items, [])

    async def test_create_propagates_fields_in_body(self):
        routes = {"POST /api/v2/dashboards": (201, json.dumps(_dashboard_payload(id="d-new", name="Sales")))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                d = await c.dashboards.create("Sales", definition={"widgets": [{"id": "w1"}]}, is_public=True)
        self.assertEqual(d.id, "d-new")
        body = json.loads(srv.requests[0]["body"])
        self.assertEqual(body["name"], "Sales")
        self.assertEqual(body["definition"], {"widgets": [{"id": "w1"}]})
        self.assertTrue(body["isPublic"])

    async def test_get_returns_typed_dashboard(self):
        routes = {"GET /api/v2/dashboards/d-1": _route(_dashboard_payload(id="d-1"))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                d = await c.dashboards.get("d-1")
        self.assertEqual(d.id, "d-1")

    async def test_update_partial_only_sends_supplied_fields(self):
        routes = {"PUT /api/v2/dashboards/d-1": _route(_dashboard_payload(id="d-1", name="Sales v2"))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                updated = await c.dashboards.update("d-1", name="Sales v2")
        self.assertEqual(updated.name, "Sales v2")
        body = json.loads(srv.requests[0]["body"])
        self.assertEqual(body["name"], "Sales v2")
        # Same preserve-on-None invariant as sync sibling.
        self.assertNotIn("definition", body)
        self.assertNotIn("isPublic", body)

    async def test_delete_returns_none(self):
        routes = {"DELETE /api/v2/dashboards/d-1": (204, "")}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                result = await c.dashboards.delete("d-1")
        self.assertIsNone(result)

    async def test_duplicate_returns_new_dashboard(self):
        routes = {"POST /api/v2/dashboards/d-1/duplicate":
                  (201, json.dumps(_dashboard_payload(id="d-copy", name="Sales (copy)")))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                d = await c.dashboards.duplicate("d-1")
        self.assertEqual(d.id, "d-copy")
        self.assertEqual(d.name, "Sales (copy)")

    async def test_lifecycle_create_update_delete(self):
        # End-to-end async lifecycle on a single client connection.
        routes = {
            "POST /api/v2/dashboards": (201, json.dumps(_dashboard_payload(id="d-life"))),
            "PUT /api/v2/dashboards/d-life": _route(_dashboard_payload(id="d-life", name="Renamed")),
            "DELETE /api/v2/dashboards/d-life": (204, ""),
        }
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                d = await c.dashboards.create("Sales")
                self.assertEqual(d.id, "d-life")
                updated = await c.dashboards.update(d.id, name="Renamed")
                self.assertEqual(updated.name, "Renamed")
                await c.dashboards.delete(d.id)
        methods = [r["method"] for r in srv.requests]
        self.assertEqual(methods, ["POST", "PUT", "DELETE"])


if __name__ == "__main__":
    unittest.main()
