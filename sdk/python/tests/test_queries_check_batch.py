"""Round-116 SDK BDD for c.queries.check_batch — sync + async mirror
of round-115 backend POST /api/v2/me/checks/queryTypes.

Closes the SDK three-axis bulk-check parity:
- c.objects.check_batch (round 108)
- c.actions.check_batch (round 110)
- c.queries.check_batch (this round)

Contract under test:
- ``c.queries.check_batch(ontology, query_types) -> QueryCheckBatchResponse``
- Both transports POST {ontologyApiName, queryTypeApiNames:[]}
- Response order matches input order
- Per-entry .found discriminator + can_execute follows found-gate
  rule (found=false ⇒ can_execute=False regardless of perms)
"""
from __future__ import annotations

import json
import unittest

from weave_client import (
    Client,
    QueryCheckBatchEntry,
    QueryCheckBatchResponse,
    WeaveAsyncClient,
)

from tests.test_client import _StubServer


_PAYLOAD = {
    "ontologyApiName": "northwind",
    "results": [
        {"queryTypeApiName": "topCustomers", "found": True,
         "queryTypeRid": "ri.qt.top", "canExecute": True},
        {"queryTypeApiName": "ghostQuery", "found": False, "canExecute": False},
        {"queryTypeApiName": "lateShipments", "found": True,
         "queryTypeRid": "ri.qt.late", "canExecute": False},
    ],
}


class SyncCheckBatchTests(unittest.TestCase):

    def test_check_batch_returns_typed_preserving_order(self):
        routes = {"POST /api/v2/me/checks/queryTypes":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            resp = c.queries.check_batch(
                "northwind", ["topCustomers", "ghostQuery", "lateShipments"])
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent, {
            "ontologyApiName": "northwind",
            "queryTypeApiNames": ["topCustomers", "ghostQuery", "lateShipments"],
        })
        self.assertIsInstance(resp, QueryCheckBatchResponse)
        self.assertEqual(resp.ontology_api_name, "northwind")
        self.assertEqual(len(resp.results), 3)
        # Order preserved row N → row N.
        self.assertEqual(resp.results[0].query_type_api_name, "topCustomers")
        self.assertEqual(resp.results[1].query_type_api_name, "ghostQuery")
        self.assertEqual(resp.results[2].query_type_api_name, "lateShipments")

    def test_check_batch_found_discriminator(self):
        routes = {"POST /api/v2/me/checks/queryTypes":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            resp = c.queries.check_batch(
                "northwind", ["topCustomers", "ghostQuery", "lateShipments"])
        self.assertTrue(resp.results[0].found)
        self.assertFalse(resp.results[1].found)
        self.assertTrue(resp.results[2].found)
        # found=false MUST report can_execute=False regardless of caller perms.
        self.assertFalse(resp.results[1].can_execute)

    def test_check_batch_per_entry_can_execute_split(self):
        # topCustomers can execute, lateShipments cannot — split
        # surfaces transparently.
        routes = {"POST /api/v2/me/checks/queryTypes":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            resp = c.queries.check_batch(
                "northwind", ["topCustomers", "ghostQuery", "lateShipments"])
        self.assertTrue(resp.results[0].can_execute)
        self.assertEqual(resp.results[0].query_type_rid, "ri.qt.top")
        self.assertFalse(resp.results[2].can_execute)
        self.assertEqual(resp.results[2].query_type_rid, "ri.qt.late")

    def test_check_batch_empty_array_raises_on_server(self):
        # Wrapper stays thin — empty array flows to server 400.
        from weave_client.exceptions import WeaveError
        routes = {"POST /api/v2/me/checks/queryTypes":
                  (400, '{"errorCode":"INVALID_REQUEST_BODY",'
                        '"errorName":"InvalidRequestBody",'
                        '"errorInstanceId":"x","parameters":{}}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            with self.assertRaises(WeaveError):
                c.queries.check_batch("northwind", [])


class AsyncCheckBatchTests(unittest.IsolatedAsyncioTestCase):

    async def test_async_check_batch_returns_typed(self):
        routes = {"POST /api/v2/me/checks/queryTypes":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                resp = await c.queries.check_batch(
                    "northwind", ["topCustomers", "ghostQuery", "lateShipments"])
                sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent["ontologyApiName"], "northwind")
        self.assertEqual(len(resp.results), 3)
        self.assertIsInstance(resp.results[0], QueryCheckBatchEntry)
        self.assertTrue(resp.results[0].found)
        self.assertFalse(resp.results[1].found)

    async def test_async_check_batch_per_entry_can_execute(self):
        routes = {"POST /api/v2/me/checks/queryTypes":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                resp = await c.queries.check_batch(
                    "northwind", ["topCustomers", "ghostQuery", "lateShipments"])
        self.assertTrue(resp.results[0].can_execute)
        self.assertFalse(resp.results[2].can_execute)


if __name__ == "__main__":
    unittest.main()
