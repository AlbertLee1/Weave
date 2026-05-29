"""Round-104 SDK BDD for c.actions.check — sync + async mirror
of round-103 backend GET /api/v2/ontologies/{ont}/actions/{action}/check.

Contract under test:
- ``c.actions.check(ontology, action) -> ActionCheckResponse``
- ``await c.actions.check(...) -> ActionCheckResponse``
- GET path-only with url-quoted ontology + action keys
- Returned ActionCheckResponse always carries canApply, ontology_api_name,
  action_api_name, action_rid — wire-format regression guard mirrors
  the round-103 backend BDD's response-shape assertion
"""
from __future__ import annotations

import json
import unittest

from weave_client import ActionCheckResponse, Client, WeaveAsyncClient

from tests.test_client import _StubServer


_PAYLOAD = {
    "ontologyApiName": "northwind",
    "actionApiName": "createCustomer",
    "actionRid": "ri.action-type.main.createCustomer",
    "canApply": True,
}


class SyncActionsCheckTests(unittest.TestCase):

    def test_check_returns_typed_response_with_canApply_true(self):
        routes = {"GET /api/v2/ontologies/northwind/actions/createCustomer/check":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            resp = c.actions.check("northwind", "createCustomer")
        self.assertIsInstance(resp, ActionCheckResponse)
        self.assertTrue(resp.can_apply)
        self.assertEqual(resp.ontology_api_name, "northwind")
        self.assertEqual(resp.action_api_name, "createCustomer")
        self.assertEqual(resp.action_rid, "ri.action-type.main.createCustomer")

    def test_check_carries_false_canApply(self):
        # Probe returns 200 with canApply=false when caller lacks
        # permission — NOT 403. SDK must surface the boolean
        # transparently so the SPA can use it for UI gating.
        payload = {**_PAYLOAD, "canApply": False}
        routes = {"GET /api/v2/ontologies/northwind/actions/createCustomer/check":
                  (200, json.dumps(payload))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            resp = c.actions.check("northwind", "createCustomer")
        self.assertFalse(resp.can_apply)

    def test_check_url_quotes_both_path_segments(self):
        # Defense in depth: ontology AND action keys both flow through
        # quote_path so a slash in either never produces a wrong-host
        # request.
        routes = {"GET /api/v2/ontologies/nw%2Fchild/actions/act%2Fwith%2Fslash/check":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.actions.check("nw/child", "act/with/slash")
        self.assertEqual(
            srv.requests[0]["path"],
            "/api/v2/ontologies/nw%2Fchild/actions/act%2Fwith%2Fslash/check")

    def test_check_is_get_with_no_body(self):
        routes = {"GET /api/v2/ontologies/northwind/actions/createCustomer/check":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.actions.check("northwind", "createCustomer")
        req = srv.requests[0]
        self.assertEqual(req["method"], "GET")
        self.assertEqual(req["body"], "")


class AsyncActionsCheckTests(unittest.IsolatedAsyncioTestCase):

    async def test_async_check_returns_typed_response(self):
        routes = {"GET /api/v2/ontologies/northwind/actions/createCustomer/check":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                resp = await c.actions.check("northwind", "createCustomer")
        self.assertIsInstance(resp, ActionCheckResponse)
        self.assertTrue(resp.can_apply)
        self.assertEqual(resp.action_rid, "ri.action-type.main.createCustomer")

    async def test_async_check_carries_false_canApply(self):
        payload = {**_PAYLOAD, "canApply": False}
        routes = {"GET /api/v2/ontologies/northwind/actions/createCustomer/check":
                  (200, json.dumps(payload))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                resp = await c.actions.check("northwind", "createCustomer")
        self.assertFalse(resp.can_apply)


if __name__ == "__main__":
    unittest.main()
