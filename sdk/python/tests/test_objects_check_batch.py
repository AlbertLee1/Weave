"""Round-108 SDK BDD for c.objects.check_batch — sync + async mirror
of round-107 backend POST /api/v2/me/checks/objectTypes.

Contract under test:
- ``c.objects.check_batch(ontology, object_types) -> ObjectCheckBatchResponse``
- Both transports POST {ontologyApiName, objectTypeApiNames:[]}
- Response order matches input order
- Per-entry .found discriminator distinguishes missing-from-config
  vs missing-perm
- found=false entries surface can_read=can_write=False regardless
  of caller perms
"""
from __future__ import annotations

import json
import unittest

from weave_client import (
    Client,
    ObjectCheckBatchEntry,
    ObjectCheckBatchResponse,
    WeaveAsyncClient,
)

from tests.test_client import _StubServer


_PAYLOAD = {
    "ontologyApiName": "northwind",
    "results": [
        {"objectTypeApiName": "Customer", "found": True,
         "objectTypeRid": "ri.ot.cust", "canRead": True, "canWrite": True},
        {"objectTypeApiName": "GhostType", "found": False,
         "canRead": False, "canWrite": False},
        {"objectTypeApiName": "Order", "found": True,
         "objectTypeRid": "ri.ot.ord", "canRead": True, "canWrite": False},
    ],
}


class SyncCheckBatchTests(unittest.TestCase):

    def test_check_batch_returns_typed_response_preserving_order(self):
        routes = {"POST /api/v2/me/checks/objectTypes":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            resp = c.objects.check_batch(
                "northwind", ["Customer", "GhostType", "Order"])
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent, {
            "ontologyApiName": "northwind",
            "objectTypeApiNames": ["Customer", "GhostType", "Order"],
        })
        self.assertIsInstance(resp, ObjectCheckBatchResponse)
        self.assertEqual(resp.ontology_api_name, "northwind")
        self.assertEqual(len(resp.results), 3)
        # Order preserved row N → row N
        self.assertEqual(resp.results[0].object_type_api_name, "Customer")
        self.assertEqual(resp.results[1].object_type_api_name, "GhostType")
        self.assertEqual(resp.results[2].object_type_api_name, "Order")

    def test_check_batch_found_discriminator(self):
        # The found:bool field is the contract that distinguishes
        # "type removed from config" from "exists but no perm". The
        # SDK must surface it as a typed bool, not a missing-attribute
        # sentinel.
        routes = {"POST /api/v2/me/checks/objectTypes":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            resp = c.objects.check_batch(
                "northwind", ["Customer", "GhostType", "Order"])
        self.assertTrue(resp.results[0].found)
        self.assertFalse(resp.results[1].found)
        self.assertTrue(resp.results[2].found)
        # found=false entries surface perms=false regardless of role.
        self.assertFalse(resp.results[1].can_read)
        self.assertFalse(resp.results[1].can_write)

    def test_check_batch_split_matrix_per_entry(self):
        # Customer (admin perms) vs Order (read-only) — exercise
        # the two-axis matrix on different entries.
        routes = {"POST /api/v2/me/checks/objectTypes":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            resp = c.objects.check_batch(
                "northwind", ["Customer", "GhostType", "Order"])
        # Customer: read+write
        self.assertTrue(resp.results[0].can_read)
        self.assertTrue(resp.results[0].can_write)
        self.assertEqual(resp.results[0].object_type_rid, "ri.ot.cust")
        # Order: read-only
        self.assertTrue(resp.results[2].can_read)
        self.assertFalse(resp.results[2].can_write)

    def test_check_batch_empty_object_types_raises_on_server(self):
        # Wrapper sends the empty array as-is; server returns 400.
        # Pydantic doesn't validate wrapper-side because the API
        # surface stays thin and the server is authoritative.
        from weave_client.exceptions import WeaveError
        routes = {"POST /api/v2/me/checks/objectTypes":
                  (400, '{"errorCode":"INVALID_REQUEST_BODY",'
                        '"errorName":"InvalidRequestBody",'
                        '"errorInstanceId":"x","parameters":{}}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            with self.assertRaises(WeaveError):
                c.objects.check_batch("northwind", [])


class AsyncCheckBatchTests(unittest.IsolatedAsyncioTestCase):

    async def test_async_check_batch_returns_typed_response(self):
        routes = {"POST /api/v2/me/checks/objectTypes":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                resp = await c.objects.check_batch(
                    "northwind", ["Customer", "GhostType", "Order"])
                sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent["ontologyApiName"], "northwind")
        self.assertEqual(len(resp.results), 3)
        self.assertIsInstance(resp.results[0], ObjectCheckBatchEntry)
        self.assertTrue(resp.results[0].found)
        self.assertFalse(resp.results[1].found)

    async def test_async_check_batch_per_entry_matrix(self):
        routes = {"POST /api/v2/me/checks/objectTypes":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                resp = await c.objects.check_batch(
                    "northwind", ["Customer", "GhostType", "Order"])
        # Same per-entry split as sync — async transport doesn't
        # mutate the wire payload.
        self.assertTrue(resp.results[2].can_read)
        self.assertFalse(resp.results[2].can_write)


if __name__ == "__main__":
    unittest.main()
