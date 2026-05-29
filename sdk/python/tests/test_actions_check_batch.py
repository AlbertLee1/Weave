"""Round-110 SDK BDD for c.actions.check_batch — sync + async mirror
of round-109 backend POST /api/v2/me/checks/actionTypes.

Contract under test:
- ``c.actions.check_batch(ontology, action_types) -> ActionCheckBatchResponse``
- Both transports POST {ontologyApiName, actionTypeApiNames:[]}
- Response order matches input order
- Per-entry .found discriminator distinguishes missing-from-config
  vs missing-perm
- found=false entries surface can_apply=False regardless of caller
  perms (same contract as round-108 OT bulk SDK)
"""
from __future__ import annotations

import json
import unittest

from weave_client import (
    ActionCheckBatchEntry,
    ActionCheckBatchResponse,
    Client,
    WeaveAsyncClient,
)

from tests.test_client import _StubServer


_PAYLOAD = {
    "ontologyApiName": "northwind",
    "results": [
        {"actionTypeApiName": "createCustomer", "found": True,
         "actionTypeRid": "ri.at.cc", "canApply": True},
        {"actionTypeApiName": "ghostAction", "found": False, "canApply": False},
        {"actionTypeApiName": "createOrder", "found": True,
         "actionTypeRid": "ri.at.co", "canApply": False},
    ],
}


class SyncCheckBatchTests(unittest.TestCase):

    def test_check_batch_returns_typed_response_preserving_order(self):
        routes = {"POST /api/v2/me/checks/actionTypes":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            resp = c.actions.check_batch(
                "northwind", ["createCustomer", "ghostAction", "createOrder"])
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent, {
            "ontologyApiName": "northwind",
            "actionTypeApiNames": ["createCustomer", "ghostAction", "createOrder"],
        })
        self.assertIsInstance(resp, ActionCheckBatchResponse)
        self.assertEqual(resp.ontology_api_name, "northwind")
        self.assertEqual(len(resp.results), 3)
        self.assertEqual(resp.results[0].action_type_api_name, "createCustomer")
        self.assertEqual(resp.results[1].action_type_api_name, "ghostAction")
        self.assertEqual(resp.results[2].action_type_api_name, "createOrder")

    def test_check_batch_found_discriminator(self):
        routes = {"POST /api/v2/me/checks/actionTypes":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            resp = c.actions.check_batch(
                "northwind", ["createCustomer", "ghostAction", "createOrder"])
        self.assertTrue(resp.results[0].found)
        self.assertFalse(resp.results[1].found)
        self.assertTrue(resp.results[2].found)
        # found=false MUST report can_apply=False regardless of caller perms.
        self.assertFalse(resp.results[1].can_apply)

    def test_check_batch_per_entry_apply_matrix(self):
        # createCustomer admin-applicable, createOrder no-perm —
        # exercise the can_apply split across entries.
        routes = {"POST /api/v2/me/checks/actionTypes":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            resp = c.actions.check_batch(
                "northwind", ["createCustomer", "ghostAction", "createOrder"])
        self.assertTrue(resp.results[0].can_apply)
        self.assertEqual(resp.results[0].action_type_rid, "ri.at.cc")
        self.assertFalse(resp.results[2].can_apply)
        self.assertEqual(resp.results[2].action_type_rid, "ri.at.co")

    def test_check_batch_empty_array_raises_on_server(self):
        # Wrapper stays thin — empty array flows to server, surfaces
        # as WeaveError via the standard 400 mapping.
        from weave_client.exceptions import WeaveError
        routes = {"POST /api/v2/me/checks/actionTypes":
                  (400, '{"errorCode":"INVALID_REQUEST_BODY",'
                        '"errorName":"InvalidRequestBody",'
                        '"errorInstanceId":"x","parameters":{}}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            with self.assertRaises(WeaveError):
                c.actions.check_batch("northwind", [])


class AsyncCheckBatchTests(unittest.IsolatedAsyncioTestCase):

    async def test_async_check_batch_returns_typed_response(self):
        routes = {"POST /api/v2/me/checks/actionTypes":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                resp = await c.actions.check_batch(
                    "northwind", ["createCustomer", "ghostAction", "createOrder"])
                sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent["ontologyApiName"], "northwind")
        self.assertEqual(len(resp.results), 3)
        self.assertIsInstance(resp.results[0], ActionCheckBatchEntry)
        self.assertTrue(resp.results[0].found)
        self.assertFalse(resp.results[1].found)

    async def test_async_check_batch_per_entry_matrix(self):
        routes = {"POST /api/v2/me/checks/actionTypes":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                resp = await c.actions.check_batch(
                    "northwind", ["createCustomer", "ghostAction", "createOrder"])
        # Async transport doesn't mutate wire payload — same split.
        self.assertTrue(resp.results[0].can_apply)
        self.assertFalse(resp.results[2].can_apply)


if __name__ == "__main__":
    unittest.main()
