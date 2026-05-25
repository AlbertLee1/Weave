"""Round-100 SDK BDD — milestone! Mirror of round-99 backend
GET /api/v2/me/ontologies.

Contract under test:
- ``c.ontologies.list_me() -> List[MeOntologiesEntry]``
- ``await c.ontologies.list_me() -> List[MeOntologiesEntry]``
- GET path-only, no body
- Returns typed entries with .rid, .api_name, .display_name, .role
- Empty response is [] (not None) so callers can iterate without
  nil-checks (round-99 backend contract regression guard)
"""
from __future__ import annotations

import json
import unittest

from weave_client import Client, MeOntologiesEntry, WeaveAsyncClient

from tests.test_client import _StubServer


_BASE_PAYLOAD = {
    "ontologies": [
        {
            "rid": "ri.ontology.main.ontology.northwind",
            "apiName": "northwind",
            "displayName": "Northwind",
            "role": "ontology-editor",
        },
        {
            "rid": "ri.ontology.main.ontology.chinook",
            "apiName": "chinook",
            "displayName": "Chinook",
            "role": "ontology-admin",
        },
    ]
}


class SyncListMeTests(unittest.TestCase):

    def test_list_me_returns_typed_entries(self):
        routes = {"GET /api/v2/me/ontologies": (200, json.dumps(_BASE_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            entries = c.ontologies.list_me()
        self.assertEqual(len(entries), 2)
        self.assertIsInstance(entries[0], MeOntologiesEntry)
        self.assertEqual(entries[0].rid, "ri.ontology.main.ontology.northwind")
        self.assertEqual(entries[0].api_name, "northwind")
        self.assertEqual(entries[0].display_name, "Northwind")
        self.assertEqual(entries[0].role, "ontology-editor")
        self.assertEqual(entries[1].role, "ontology-admin")

    def test_list_me_handles_empty_response(self):
        routes = {"GET /api/v2/me/ontologies": (200, '{"ontologies":[]}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            entries = c.ontologies.list_me()
        self.assertEqual(entries, [],
                         "empty response must yield [], never None")
        # Iteration safety — never raises on empty.
        for _ in entries:
            self.fail("empty entries should not iterate")

    def test_list_me_request_is_get_with_no_body(self):
        routes = {"GET /api/v2/me/ontologies": (200, json.dumps(_BASE_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.ontologies.list_me()
        req = srv.requests[0]
        self.assertEqual(req["method"], "GET")
        self.assertEqual(req["body"], "")
        self.assertEqual(req["path"], "/api/v2/me/ontologies")


class AsyncListMeTests(unittest.IsolatedAsyncioTestCase):

    async def test_async_list_me_returns_typed_entries(self):
        routes = {"GET /api/v2/me/ontologies": (200, json.dumps(_BASE_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                entries = await c.ontologies.list_me()
        self.assertEqual(len(entries), 2)
        self.assertIsInstance(entries[0], MeOntologiesEntry)
        self.assertEqual(entries[0].api_name, "northwind")
        self.assertEqual(entries[1].role, "ontology-admin")

    async def test_async_list_me_handles_empty_response(self):
        routes = {"GET /api/v2/me/ontologies": (200, '{"ontologies":[]}')}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                entries = await c.ontologies.list_me()
        self.assertEqual(entries, [])


if __name__ == "__main__":
    unittest.main()
