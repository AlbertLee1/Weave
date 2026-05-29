"""Round-128 SDK BDD for c.build_info_features — sync + async
mirror of round-127 backend GET /api/v2/build-info/features.

Contract:
- ``c.build_info_features() -> List[Feature]``
- ``await c.build_info_features() -> List[Feature]``
- Anonymous (no Authorization header — public endpoint)
- Each Feature carries name/enabled + optional description/reason
- Empty response yields [] not None
"""
from __future__ import annotations

import json
import unittest

from weave_client import Client, Feature, WeaveAsyncClient

from tests.test_client import _StubServer


_PAYLOAD = {
    "features": [
        {"name": "mcp", "enabled": True,
         "description": "MCP JSON-RPC 2.0 endpoint at /mcp for AI agents."},
        {"name": "rid-versioning", "enabled": True,
         "description": "RID @vN parser + Get-endpoint @vN guards."},
        {"name": "sessions", "enabled": False,
         "description": "Session inventory + revoke endpoints.",
         "reason": "SessionStore not configured (no PG)"},
    ]
}


class SyncFeaturesTests(unittest.TestCase):

    def test_returns_typed_feature_list_in_order(self):
        routes = {"GET /api/v2/build-info/features":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            features = c.build_info_features()
        self.assertEqual(len(features), 3)
        self.assertIsInstance(features[0], Feature)
        self.assertEqual(features[0].name, "mcp")
        self.assertTrue(features[0].enabled)
        self.assertEqual(features[2].name, "sessions")
        self.assertFalse(features[2].enabled)
        self.assertEqual(features[2].reason, "SessionStore not configured (no PG)")
        # Enabled features have empty reason — round-127 backend
        # uses json:omitempty so the field is absent on the wire,
        # SDK Pydantic default "" surfaces it consistently.
        self.assertEqual(features[0].reason, "")

    def test_sends_no_auth_header(self):
        # Public endpoint — SDK must NOT attach Authorization.
        routes = {"GET /api/v2/build-info/features":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.build_info_features()
        req = srv.requests[0]
        self.assertEqual(req["auth"], "")
        self.assertEqual(req["method"], "GET")
        self.assertEqual(req["body"], "")

    def test_handles_empty_features_array(self):
        # Round-127 backend defensive contract: [] not null when
        # registry is empty. SDK must surface that as a list.
        routes = {"GET /api/v2/build-info/features":
                  (200, '{"features":[]}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            features = c.build_info_features()
        self.assertEqual(features, [])

    def test_works_with_no_credentials(self):
        # No-token client still reaches the public endpoint.
        routes = {"GET /api/v2/build-info/features":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url)
            features = c.build_info_features()
        self.assertEqual(len(features), 3)


class AsyncFeaturesTests(unittest.IsolatedAsyncioTestCase):

    async def test_async_returns_typed_feature_list(self):
        routes = {"GET /api/v2/build-info/features":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                features = await c.build_info_features()
        self.assertEqual(len(features), 3)
        self.assertIsInstance(features[0], Feature)
        self.assertFalse(features[2].enabled)

    async def test_async_sends_no_auth_header(self):
        routes = {"GET /api/v2/build-info/features":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                await c.build_info_features()
        self.assertEqual(srv.requests[0]["auth"], "")


if __name__ == "__main__":
    unittest.main()
