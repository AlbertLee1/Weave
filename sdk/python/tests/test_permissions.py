"""Round-98 SDK BDD for c.permissions.check — sync + async mirror
of round-97 backend POST /api/v2/me/permissions/check.

Contract under test:
- ``c.permissions.check(permissions, ontology=None) -> PermissionsCheckResponse``
- ``await c.permissions.check(...) -> PermissionsCheckResponse``
- Sends POST with body {"permissions": [...], "ontology": "..."} —
  ontology field is omitted entirely when ontology=None so the
  server's global-check branch fires.
- Returns PermissionsCheckResponse with .granted and .denied fields
  that always exactly partition the input (no overlap, no missing).
"""
from __future__ import annotations

import json
import unittest

from weave_client import Client, PermissionsCheckResponse, WeaveAsyncClient

from tests.test_client import _StubServer


class SyncPermissionsCheckTests(unittest.TestCase):

    def test_check_returns_typed_response_with_partition(self):
        payload = {
            "granted": ["objectType.read"],
            "denied": ["objectType.create", "action.apply"],
        }
        routes = {"POST /api/v2/me/permissions/check": (200, json.dumps(payload))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            resp = c.permissions.check(
                ["objectType.read", "objectType.create", "action.apply"])
        self.assertIsInstance(resp, PermissionsCheckResponse)
        self.assertEqual(resp.granted, ["objectType.read"])
        self.assertEqual(resp.denied, ["objectType.create", "action.apply"])
        # Wire contract regression: granted ∪ denied == input
        self.assertEqual(len(resp.granted) + len(resp.denied), 3)

    def test_check_without_ontology_omits_field(self):
        # ontology=None is the "global check" path — must not send
        # ontology field at all, otherwise server would 400 on empty
        # string vs missing field for non-degraded resolvers.
        routes = {"POST /api/v2/me/permissions/check":
                  (200, '{"granted":[],"denied":["x"]}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.permissions.check(["x"])
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent, {"permissions": ["x"]})
        self.assertNotIn("ontology", sent)

    def test_check_with_ontology_includes_field(self):
        routes = {"POST /api/v2/me/permissions/check":
                  (200, '{"granted":["objectType.create"],"denied":[]}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.permissions.check(["objectType.create"], ontology="northwind")
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent, {
            "permissions": ["objectType.create"],
            "ontology": "northwind",
        })

    def test_check_empty_response_lists_are_lists_not_none(self):
        # Pydantic default_factory=list ensures callers can iterate
        # without nil-checks even when server returns "granted":[].
        routes = {"POST /api/v2/me/permissions/check":
                  (200, '{"granted":[],"denied":[]}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            resp = c.permissions.check(["x"])
        self.assertEqual(resp.granted, [])
        self.assertEqual(resp.denied, [])
        # Mutation-safe defaults — each call gets its own list.
        resp.granted.append("test")
        resp2 = PermissionsCheckResponse()
        self.assertEqual(resp2.granted, [], "default_factory must not share state")


class AsyncPermissionsCheckTests(unittest.IsolatedAsyncioTestCase):

    async def test_async_check_returns_typed_response(self):
        payload = {
            "granted": ["objectType.read"],
            "denied": ["objectType.create"],
        }
        routes = {"POST /api/v2/me/permissions/check": (200, json.dumps(payload))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                resp = await c.permissions.check(
                    ["objectType.read", "objectType.create"])
        self.assertIsInstance(resp, PermissionsCheckResponse)
        self.assertEqual(resp.granted, ["objectType.read"])
        self.assertEqual(resp.denied, ["objectType.create"])

    async def test_async_check_with_ontology(self):
        routes = {"POST /api/v2/me/permissions/check":
                  (200, '{"granted":["action.apply"],"denied":[]}')}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                await c.permissions.check(["action.apply"], ontology="nw")
                sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent, {"permissions": ["action.apply"], "ontology": "nw"})

    async def test_async_check_without_ontology_omits_field(self):
        routes = {"POST /api/v2/me/permissions/check":
                  (200, '{"granted":[],"denied":["x"]}')}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                await c.permissions.check(["x"])
                sent = json.loads(srv.requests[0]["body"])
        self.assertNotIn("ontology", sent)


if __name__ == "__main__":
    unittest.main()
