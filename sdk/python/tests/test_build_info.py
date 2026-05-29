"""Round-124 SDK BDD for c.build_info — sync + async mirror of
round-123 backend GET /api/v2/build-info.

Contract under test:
- ``c.build_info() -> BuildInfo`` (sync, top-level Client method)
- ``await c.build_info() -> BuildInfo`` (async, top-level
  WeaveAsyncClient method)
- Request is GET path-only, no body, no auth header (endpoint is
  public per round-123 backend convention — Foundry-parity)
- BuildInfo carries version / commit / go_version / build_time
  with snake_case ↔ camelCase wire aliases via _CamelModel
"""
from __future__ import annotations

import json
import unittest

from weave_client import BuildInfo, Client, WeaveAsyncClient

from tests.test_client import _StubServer


_PAYLOAD = {
    "version": "1.2.3",
    "commit": "abc123",
    "goVersion": "go1.22.3",
    "buildTime": "2026-05-25T10:00:00Z",
}


class SyncBuildInfoTests(unittest.TestCase):

    def test_build_info_returns_typed_model(self):
        routes = {"GET /api/v2/build-info": (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            info = c.build_info()
        self.assertIsInstance(info, BuildInfo)
        self.assertEqual(info.version, "1.2.3")
        self.assertEqual(info.commit, "abc123")
        self.assertEqual(info.go_version, "go1.22.3")
        self.assertEqual(info.build_time, "2026-05-25T10:00:00Z")

    def test_build_info_sends_no_auth_header(self):
        # Endpoint is public per round-123 backend convention — the
        # SDK must NOT attach Authorization. Mirror of round-117
        # public-endpoint pattern; the wire test catches future
        # PRs that accidentally route build-info through the
        # bearer-token path.
        routes = {"GET /api/v2/build-info": (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.build_info()
        req = srv.requests[0]
        self.assertEqual(req["auth"], "",
                         f"build_info sent auth={req['auth']!r}; "
                         "endpoint is public, no Authorization expected")
        self.assertEqual(req["method"], "GET")
        self.assertEqual(req["body"], "")
        self.assertEqual(req["path"], "/api/v2/build-info")

    def test_build_info_handles_default_unknown_values(self):
        # Round-123 backend defaults all 3 ldflags-overridable
        # fields to "unknown" in local dev. The SDK must surface
        # those literally, not coerce to None or empty.
        payload = {
            "version": "unknown",
            "commit": "unknown",
            "goVersion": "go1.22.3",
            "buildTime": "unknown",
        }
        routes = {"GET /api/v2/build-info": (200, json.dumps(payload))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            info = c.build_info()
        self.assertEqual(info.version, "unknown")
        self.assertEqual(info.commit, "unknown")
        self.assertEqual(info.build_time, "unknown")

    def test_build_info_works_with_no_credentials(self):
        # Mirror of round-117 / r123 public-endpoint contract — a
        # Client built without any token should still reach the
        # endpoint successfully (no 401 raised).
        routes = {"GET /api/v2/build-info": (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url)  # no access_token, no api_key
            info = c.build_info()
        self.assertEqual(info.version, "1.2.3")


class AsyncBuildInfoTests(unittest.IsolatedAsyncioTestCase):

    async def test_async_build_info_returns_typed_model(self):
        routes = {"GET /api/v2/build-info": (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                info = await c.build_info()
        self.assertIsInstance(info, BuildInfo)
        self.assertEqual(info.version, "1.2.3")
        self.assertEqual(info.go_version, "go1.22.3")

    async def test_async_build_info_sends_no_auth_header(self):
        # Async parity guard — same public-endpoint contract on
        # the async path.
        routes = {"GET /api/v2/build-info": (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                await c.build_info()
        self.assertEqual(srv.requests[0]["auth"], "")


if __name__ == "__main__":
    unittest.main()
