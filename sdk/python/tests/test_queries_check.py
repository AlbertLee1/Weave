"""Round-114 SDK BDD for c.queries.check — sync + async mirror
of round-113 backend GET /api/v2/ontologies/{ont}/queryTypes/{qt}/check.

Closes the three-axis check-family parity on the SDK side:
- c.objects.check (round 106)
- c.actions.check (round 104)
- c.queries.check (this round)

Contract under test:
- ``c.queries.check(ontology, query_type) -> QueryCheckResponse``
- ``await c.queries.check(...) -> QueryCheckResponse``
- GET path-only with url-quoted ontology + query_type
- Returned QueryCheckResponse carries can_execute boolean +
  ontology_api_name + query_type_api_name + query_type_rid
"""
from __future__ import annotations

import json
import unittest

from weave_client import Client, QueryCheckResponse, WeaveAsyncClient

from tests.test_client import _StubServer


_PAYLOAD = {
    "ontologyApiName": "northwind",
    "queryTypeApiName": "topCustomers",
    "queryTypeRid": "ri.qt.topCustomers",
    "canExecute": True,
}


class SyncQueriesCheckTests(unittest.TestCase):

    def test_check_returns_typed_response_canExecute_true(self):
        routes = {"GET /api/v2/ontologies/northwind/queryTypes/topCustomers/check":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            resp = c.queries.check("northwind", "topCustomers")
        self.assertIsInstance(resp, QueryCheckResponse)
        self.assertTrue(resp.can_execute)
        self.assertEqual(resp.ontology_api_name, "northwind")
        self.assertEqual(resp.query_type_api_name, "topCustomers")
        self.assertEqual(resp.query_type_rid, "ri.qt.topCustomers")

    def test_check_carries_false_canExecute(self):
        # 200 with canExecute=false when caller lacks queryType.read;
        # NEVER 403 — probe is informational so the SPA can disable
        # the Run-Query button cleanly.
        payload = {**_PAYLOAD, "canExecute": False}
        routes = {"GET /api/v2/ontologies/northwind/queryTypes/topCustomers/check":
                  (200, json.dumps(payload))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            resp = c.queries.check("northwind", "topCustomers")
        self.assertFalse(resp.can_execute)

    def test_check_url_quotes_both_path_segments(self):
        # Defense in depth — both ontology AND query keys flow
        # through quote_path.
        routes = {"GET /api/v2/ontologies/nw%2Fchild/queryTypes/qt%2Fwith%2Fslash/check":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.queries.check("nw/child", "qt/with/slash")
        self.assertEqual(
            srv.requests[0]["path"],
            "/api/v2/ontologies/nw%2Fchild/queryTypes/qt%2Fwith%2Fslash/check")

    def test_check_is_get_with_no_body(self):
        routes = {"GET /api/v2/ontologies/northwind/queryTypes/topCustomers/check":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.queries.check("northwind", "topCustomers")
        req = srv.requests[0]
        self.assertEqual(req["method"], "GET")
        self.assertEqual(req["body"], "")


class AsyncQueriesCheckTests(unittest.IsolatedAsyncioTestCase):

    async def test_async_check_returns_typed_response(self):
        routes = {"GET /api/v2/ontologies/northwind/queryTypes/topCustomers/check":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                resp = await c.queries.check("northwind", "topCustomers")
        self.assertIsInstance(resp, QueryCheckResponse)
        self.assertTrue(resp.can_execute)
        self.assertEqual(resp.query_type_rid, "ri.qt.topCustomers")

    async def test_async_check_carries_false_canExecute(self):
        payload = {**_PAYLOAD, "canExecute": False}
        routes = {"GET /api/v2/ontologies/northwind/queryTypes/topCustomers/check":
                  (200, json.dumps(payload))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                resp = await c.queries.check("northwind", "topCustomers")
        self.assertFalse(resp.can_execute)


if __name__ == "__main__":
    unittest.main()
