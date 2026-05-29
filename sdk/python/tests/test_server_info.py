"""Round-130 SDK BDD for c.server_info — sync + async mirror of
round-129 backend GET /api/v2/server-info.

Closes the 4-of-4 server-observability family on the SDK side:
- c.build_info (round 124)
- c.build_info_dependencies (round 126)
- c.build_info_features (round 128)
- c.server_info (this round)
"""
from __future__ import annotations

import json
import unittest

from weave_client import Client, ServerInfo, WeaveAsyncClient

from tests.test_client import _StubServer


_PAYLOAD = {
    "startedAt": "2026-05-25T12:00:00Z",
    "uptimeSeconds": 11532,
    "goroutineCount": 384,
    "memoryAllocBytes": 1234567890,
    "memorySysBytes": 2345678901,
    "gcCycles": 42,
}


class SyncServerInfoTests(unittest.TestCase):

    def test_returns_typed_response_with_all_6_fields(self):
        routes = {"GET /api/v2/server-info": (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            info = c.server_info()
        self.assertIsInstance(info, ServerInfo)
        self.assertEqual(info.started_at, "2026-05-25T12:00:00Z")
        self.assertEqual(info.uptime_seconds, 11532)
        self.assertEqual(info.goroutine_count, 384)
        self.assertEqual(info.memory_alloc_bytes, 1234567890)
        self.assertEqual(info.memory_sys_bytes, 2345678901)
        self.assertEqual(info.gc_cycles, 42)

    def test_sends_no_auth_header(self):
        # Public endpoint — SDK must NOT attach Authorization.
        routes = {"GET /api/v2/server-info": (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.server_info()
        req = srv.requests[0]
        self.assertEqual(req["auth"], "")
        self.assertEqual(req["method"], "GET")
        self.assertEqual(req["body"], "")

    def test_works_with_no_credentials(self):
        routes = {"GET /api/v2/server-info": (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url)  # no access_token
            info = c.server_info()
        self.assertEqual(info.goroutine_count, 384)

    def test_camel_case_wire_to_snake_case_python(self):
        # Wire is camelCase (Go json tags); Python is snake_case.
        # _CamelModel handles the conversion via Field(alias=...).
        # Verify both spellings work as setters but only snake_case
        # is the canonical Python attr.
        routes = {"GET /api/v2/server-info": (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            info = c.server_info()
        # snake_case attribute access
        self.assertEqual(info.started_at, "2026-05-25T12:00:00Z")
        # The camelCase wire field shouldn't shadow the snake_case
        # attribute — accessing `startedAt` should raise AttributeError
        # in normal Pydantic usage (populate_by_name allows alias on
        # construction, not attribute access).
        with self.assertRaises(AttributeError):
            _ = info.startedAt


class AsyncServerInfoTests(unittest.IsolatedAsyncioTestCase):

    async def test_async_returns_typed_response(self):
        routes = {"GET /api/v2/server-info": (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                info = await c.server_info()
        self.assertIsInstance(info, ServerInfo)
        self.assertEqual(info.uptime_seconds, 11532)

    async def test_async_sends_no_auth_header(self):
        routes = {"GET /api/v2/server-info": (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                await c.server_info()
        self.assertEqual(srv.requests[0]["auth"], "")


if __name__ == "__main__":
    unittest.main()
