"""Round-106 SDK BDD for c.objects.check — sync + async mirror
of round-105 backend GET /api/v2/ontologies/{ont}/objectTypes/{ot}/check.

Contract under test:
- ``c.objects.check(ontology, object_type) -> ObjectCheckResponse``
- ``await c.objects.check(...) -> ObjectCheckResponse``
- GET path-only with url-quoted ontology + object_type keys
- Returns typed two-axis matrix (can_read + can_write) so the SPA
  gates row visibility separately from edit-pencil visibility.
"""
from __future__ import annotations

import json
import unittest

from weave_client import Client, ObjectCheckResponse, WeaveAsyncClient

from tests.test_client import _StubServer


_PAYLOAD = {
    "ontologyApiName": "northwind",
    "objectTypeApiName": "Customer",
    "objectTypeRid": "ri.object-type.main.Customer",
    "canRead": True,
    "canWrite": True,
}


class SyncObjectsCheckTests(unittest.TestCase):

    def test_check_returns_typed_response_both_true(self):
        routes = {"GET /api/v2/ontologies/northwind/objectTypes/Customer/check":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            resp = c.objects.check("northwind", "Customer")
        self.assertIsInstance(resp, ObjectCheckResponse)
        self.assertTrue(resp.can_read)
        self.assertTrue(resp.can_write)
        self.assertEqual(resp.ontology_api_name, "northwind")
        self.assertEqual(resp.object_type_api_name, "Customer")
        self.assertEqual(resp.object_type_rid, "ri.object-type.main.Customer")

    def test_check_carries_split_read_write_matrix(self):
        # The two-axis matrix is the whole reason this endpoint exists
        # vs the round-103 single-axis action probe. Surface the split
        # transparently so the SPA can gate row visibility (can_read)
        # separately from edit-pencil visibility (can_write).
        payload = {**_PAYLOAD, "canRead": True, "canWrite": False}
        routes = {"GET /api/v2/ontologies/northwind/objectTypes/Customer/check":
                  (200, json.dumps(payload))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            resp = c.objects.check("northwind", "Customer")
        self.assertTrue(resp.can_read)
        self.assertFalse(resp.can_write)

    def test_check_both_false(self):
        # No-role user case — 200 with both false. The SPA hides both
        # row + pencil; the response is informational, never 403.
        payload = {**_PAYLOAD, "canRead": False, "canWrite": False}
        routes = {"GET /api/v2/ontologies/northwind/objectTypes/Customer/check":
                  (200, json.dumps(payload))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            resp = c.objects.check("northwind", "Customer")
        self.assertFalse(resp.can_read)
        self.assertFalse(resp.can_write)

    def test_check_url_quotes_both_path_segments(self):
        routes = {"GET /api/v2/ontologies/nw%2Fchild/objectTypes/ot%2Fwith%2Fslash/check":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.objects.check("nw/child", "ot/with/slash")
        self.assertEqual(
            srv.requests[0]["path"],
            "/api/v2/ontologies/nw%2Fchild/objectTypes/ot%2Fwith%2Fslash/check")

    def test_check_is_get_with_no_body(self):
        routes = {"GET /api/v2/ontologies/northwind/objectTypes/Customer/check":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.objects.check("northwind", "Customer")
        req = srv.requests[0]
        self.assertEqual(req["method"], "GET")
        self.assertEqual(req["body"], "")


class AsyncObjectsCheckTests(unittest.IsolatedAsyncioTestCase):

    async def test_async_check_returns_typed_response(self):
        routes = {"GET /api/v2/ontologies/northwind/objectTypes/Customer/check":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                resp = await c.objects.check("northwind", "Customer")
        self.assertIsInstance(resp, ObjectCheckResponse)
        self.assertTrue(resp.can_read)
        self.assertTrue(resp.can_write)
        self.assertEqual(resp.object_type_rid, "ri.object-type.main.Customer")

    async def test_async_check_carries_split_matrix(self):
        payload = {**_PAYLOAD, "canRead": True, "canWrite": False}
        routes = {"GET /api/v2/ontologies/northwind/objectTypes/Customer/check":
                  (200, json.dumps(payload))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                resp = await c.objects.check("northwind", "Customer")
        self.assertTrue(resp.can_read)
        self.assertFalse(resp.can_write)


if __name__ == "__main__":
    unittest.main()
