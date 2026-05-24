"""BDD acceptance tests for the round-76 DashboardsAPI Python wrapper.

The Go server has 6 dashboards endpoints (CRUD + round-62 Duplicate).
This module wraps them so Python callers don't have to hand-build
URLs or remember the partial-update DTO semantics.
"""
from __future__ import annotations

import json
import unittest

from weave_client import Client, Dashboard

from tests.test_client import _StubServer


def _route(payload, status=200):
    return (status, json.dumps(payload))


class DashboardsAPITests(unittest.TestCase):

    def test_list_returns_typed_dashboards(self):
        payload = {
            "dashboards": [
                {
                    "id": "d-1", "name": "Sales", "createdBy": "u",
                    "isPublic": False, "definition": {"widgets": []},
                    "createdAt": "2026-05-25T00:00:00Z",
                    "updatedAt": "2026-05-25T00:00:00Z",
                },
                {
                    "id": "d-2", "name": "Ops", "createdBy": "u",
                    "isPublic": True, "definition": {},
                    "createdAt": "2026-05-25T00:00:00Z",
                    "updatedAt": "2026-05-25T00:00:00Z",
                },
            ],
        }
        routes = {"GET /api/v2/dashboards": _route(payload)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            items = c.dashboards.list()
        self.assertEqual(len(items), 2)
        self.assertIsInstance(items[0], Dashboard)
        self.assertEqual(items[0].id, "d-1")
        self.assertEqual(items[0].name, "Sales")
        self.assertFalse(items[0].is_public)
        self.assertTrue(items[1].is_public)

    def test_list_empty_dashboards_returns_empty_list(self):
        routes = {"GET /api/v2/dashboards": _route({"dashboards": None})}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            items = c.dashboards.list()
        self.assertEqual(items, [])

    def test_create_returns_typed_dashboard(self):
        payload = {
            "id": "d-new", "name": "Sales", "createdBy": "u",
            "isPublic": False, "definition": {"widgets": [{"id": "w1"}]},
            "createdAt": "2026-05-25T00:00:00Z",
            "updatedAt": "2026-05-25T00:00:00Z",
        }
        routes = {"POST /api/v2/dashboards": (201, json.dumps(payload))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            d = c.dashboards.create("Sales", definition={"widgets": [{"id": "w1"}]})
        self.assertEqual(d.id, "d-new")
        self.assertEqual(d.name, "Sales")
        body = json.loads(srv.requests[0]["body"])
        self.assertEqual(body["name"], "Sales")
        self.assertEqual(body["definition"], {"widgets": [{"id": "w1"}]})
        # isPublic omitted defaults to False on the server side; wrapper
        # may either send false explicitly or omit it — assert it's
        # not True.
        self.assertNotEqual(body.get("isPublic"), True)

    def test_create_with_is_public_propagates(self):
        payload = {
            "id": "d-pub", "name": "Public", "createdBy": "u",
            "isPublic": True, "definition": {},
            "createdAt": "2026-05-25T00:00:00Z",
            "updatedAt": "2026-05-25T00:00:00Z",
        }
        routes = {"POST /api/v2/dashboards": (201, json.dumps(payload))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.dashboards.create("Public", is_public=True)
        body = json.loads(srv.requests[0]["body"])
        self.assertTrue(body["isPublic"])

    def test_get_returns_typed_dashboard(self):
        payload = {
            "id": "d-1", "name": "Sales", "createdBy": "u",
            "isPublic": False, "definition": {},
            "createdAt": "2026-05-25T00:00:00Z",
            "updatedAt": "2026-05-25T00:00:00Z",
        }
        routes = {"GET /api/v2/dashboards/d-1": _route(payload)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            d = c.dashboards.get("d-1")
        self.assertEqual(d.id, "d-1")
        self.assertEqual(d.name, "Sales")

    def test_update_partial_only_sends_supplied_fields(self):
        # Partial update DTO: nil pointer fields preserve the existing
        # value (server-side semantic). Wrapper passes only the kwargs
        # the caller supplied so omitted fields stay absent from the body.
        payload = {
            "id": "d-1", "name": "Sales v2", "createdBy": "u",
            "isPublic": False, "definition": {},
            "createdAt": "2026-05-25T00:00:00Z",
            "updatedAt": "2026-05-25T00:01:00Z",
        }
        routes = {"PUT /api/v2/dashboards/d-1": _route(payload)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            updated = c.dashboards.update("d-1", name="Sales v2")
        self.assertEqual(updated.name, "Sales v2")
        body = json.loads(srv.requests[0]["body"])
        self.assertEqual(body["name"], "Sales v2")
        # definition + isPublic NOT supplied — must NOT appear in body
        # (None means preserve, not clear).
        self.assertNotIn("definition", body)
        self.assertNotIn("isPublic", body)

    def test_update_definition_only(self):
        payload = {
            "id": "d-1", "name": "Sales", "createdBy": "u",
            "isPublic": False, "definition": {"widgets": [{"id": "new"}]},
            "createdAt": "2026-05-25T00:00:00Z",
            "updatedAt": "2026-05-25T00:01:00Z",
        }
        routes = {"PUT /api/v2/dashboards/d-1": _route(payload)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.dashboards.update("d-1", definition={"widgets": [{"id": "new"}]})
        body = json.loads(srv.requests[0]["body"])
        self.assertNotIn("name", body)
        self.assertEqual(body["definition"], {"widgets": [{"id": "new"}]})

    def test_update_is_public_toggle(self):
        # isPublic is a bool tristate — None=preserve, True/False sends.
        payload = {
            "id": "d-1", "name": "Sales", "createdBy": "u",
            "isPublic": True, "definition": {},
            "createdAt": "2026-05-25T00:00:00Z",
            "updatedAt": "2026-05-25T00:01:00Z",
        }
        routes = {"PUT /api/v2/dashboards/d-1": _route(payload)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.dashboards.update("d-1", is_public=True)
        body = json.loads(srv.requests[0]["body"])
        self.assertNotIn("name", body)
        self.assertNotIn("definition", body)
        self.assertEqual(body["isPublic"], True)

    def test_delete_returns_none(self):
        routes = {"DELETE /api/v2/dashboards/d-1": (204, "")}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.dashboards.delete("d-1")
        self.assertIsNone(result)

    def test_duplicate_returns_new_dashboard(self):
        # Round 62 surface: POST .../{id}/duplicate returns a fresh
        # Dashboard owned by caller with "(copy)" name suffix.
        payload = {
            "id": "d-copy", "name": "Sales (copy)", "createdBy": "u",
            "isPublic": False, "definition": {"widgets": []},
            "createdAt": "2026-05-25T00:01:00Z",
            "updatedAt": "2026-05-25T00:01:00Z",
        }
        routes = {"POST /api/v2/dashboards/d-1/duplicate": (201, json.dumps(payload))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            d = c.dashboards.duplicate("d-1")
        self.assertEqual(d.id, "d-copy")
        self.assertEqual(d.name, "Sales (copy)")

    def test_path_quote_handles_special_chars(self):
        routes = {}  # any request → 404 from stub
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            try:
                c.dashboards.get("d/with/slash")
            except Exception:
                pass
            self.assertIn("d%2Fwith%2Fslash", srv.requests[0]["path"])


if __name__ == "__main__":
    unittest.main()
