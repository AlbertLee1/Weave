"""Round-96 SDK BDD for ontologies.get_me — sync + async mirror of
round-95 backend GET /api/v2/ontologies/{ontologyApiName}/me.

Contract under test:
- ``c.ontologies.get_me(ontology) -> OntologyMe``
- ``await c.ontologies.get_me(ontology) -> OntologyMe``
- Both transports POST nothing; GET path-only, ontology url-quoted
- Returned OntologyMe carries ontology_rid, ontology_api_name, role
  ("" when no scoped role), permissions ([] when none), markings
  ([] when none) — every list field always non-None so callers can
  write ``for p in me.permissions`` without nil-checks.
"""
from __future__ import annotations

import json
import unittest

from weave_client import Client, OntologyMe, WeaveAsyncClient

from tests.test_client import _StubServer


_BASE_PAYLOAD = {
    "ontologyRid": "ri.ontology.main.ontology.northwind",
    "ontologyApiName": "northwind",
    "role": "ontology-editor",
    "permissions": ["objectType.read", "objectType.create", "action.apply"],
    "markings": ["ACME", "PII"],
}


class SyncOntologyMeTests(unittest.TestCase):

    def test_get_me_returns_typed_model_with_all_fields(self):
        routes = {"GET /api/v2/ontologies/northwind/me":
                  (200, json.dumps(_BASE_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            me = c.ontologies.get_me("northwind")
        self.assertIsInstance(me, OntologyMe)
        self.assertEqual(me.ontology_rid, _BASE_PAYLOAD["ontologyRid"])
        self.assertEqual(me.ontology_api_name, "northwind")
        self.assertEqual(me.role, "ontology-editor")
        self.assertEqual(me.permissions,
                         ["objectType.read", "objectType.create", "action.apply"])
        self.assertEqual(me.markings, ["ACME", "PII"])

    def test_get_me_handles_empty_role(self):
        # role="" is the contract for users with global perms only —
        # the wrapper must surface the empty string, not None.
        payload = {**_BASE_PAYLOAD, "role": "", "permissions": [], "markings": []}
        routes = {"GET /api/v2/ontologies/northwind/me":
                  (200, json.dumps(payload))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            me = c.ontologies.get_me("northwind")
        self.assertEqual(me.role, "")
        self.assertEqual(me.permissions, [])
        self.assertEqual(me.markings, [])

    def test_get_me_ontology_path_is_url_quoted(self):
        # SDK path-builder must call quote_path so a slash-containing
        # ontology key never produces wrong-host requests.
        routes = {"GET /api/v2/ontologies/nw%2Fchild/me":
                  (200, json.dumps(_BASE_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.ontologies.get_me("nw/child")
        self.assertEqual(srv.requests[0]["path"], "/api/v2/ontologies/nw%2Fchild/me")

    def test_get_me_request_is_get_with_no_body(self):
        routes = {"GET /api/v2/ontologies/northwind/me":
                  (200, json.dumps(_BASE_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.ontologies.get_me("northwind")
        self.assertEqual(srv.requests[0]["method"], "GET")
        # GET requests carry no body — confirm the wrapper isn't
        # accidentally POSTing.
        self.assertEqual(srv.requests[0]["body"], "")


class AsyncOntologyMeTests(unittest.IsolatedAsyncioTestCase):

    async def test_async_get_me_returns_typed_model(self):
        routes = {"GET /api/v2/ontologies/northwind/me":
                  (200, json.dumps(_BASE_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                me = await c.ontologies.get_me("northwind")
        self.assertIsInstance(me, OntologyMe)
        self.assertEqual(me.ontology_rid, _BASE_PAYLOAD["ontologyRid"])
        self.assertEqual(me.role, "ontology-editor")
        self.assertEqual(len(me.permissions), 3)

    async def test_async_get_me_handles_empty_role(self):
        payload = {**_BASE_PAYLOAD, "role": "", "permissions": [], "markings": []}
        routes = {"GET /api/v2/ontologies/northwind/me":
                  (200, json.dumps(payload))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                me = await c.ontologies.get_me("northwind")
        self.assertEqual(me.role, "")
        self.assertEqual(me.permissions, [])

    async def test_async_get_me_ontology_path_is_url_quoted(self):
        routes = {"GET /api/v2/ontologies/nw%2Fchild/me":
                  (200, json.dumps(_BASE_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                await c.ontologies.get_me("nw/child")
        self.assertEqual(srv.requests[0]["path"], "/api/v2/ontologies/nw%2Fchild/me")


if __name__ == "__main__":
    unittest.main()
