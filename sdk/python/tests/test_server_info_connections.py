"""Round-132 SDK BDD for c.server_info_connections — sync + async
mirror of round-131 backend GET /api/v2/server-info/connections.

Closes 5-of-5 server-observability SDK family:
- c.build_info               (r124)
- c.build_info_dependencies  (r126)
- c.build_info_features      (r128)
- c.server_info              (r130)
- c.server_info_connections  (this round)

Per-service nullability is the critical contract: degraded
backend boot emits {"postgres": null, "nats": null} and the SDK
must surface those as Python None (not crash, not empty dict)
so the SPA can render "service not configured" cleanly.
"""
from __future__ import annotations

import json
import unittest

from weave_client import (
    Client,
    ConnectionStats,
    NATSStats,
    PostgresStats,
    WeaveAsyncClient,
)

from tests.test_client import _StubServer


_FULL_PAYLOAD = {
    "postgres": {
        "acquiredConns": 5,
        "idleConns": 3,
        "totalConns": 8,
        "maxConns": 20,
        "newConnsCount": 100,
        "maxLifetimeDestroyCount": 0,
        "maxIdleDestroyCount": 0,
    },
    "nats": {
        "status": "CONNECTED",
        "serverUrl": "nats://localhost:4222",
        "inMsgs": 1000,
        "outMsgs": 500,
        "reconnects": 0,
    },
}


class SyncConnectionsTests(unittest.TestCase):

    def test_returns_typed_response_both_populated(self):
        routes = {"GET /api/v2/server-info/connections":
                  (200, json.dumps(_FULL_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            stats = c.server_info_connections()
        self.assertIsInstance(stats, ConnectionStats)
        self.assertIsInstance(stats.postgres, PostgresStats)
        self.assertEqual(stats.postgres.acquired_conns, 5)
        self.assertEqual(stats.postgres.max_conns, 20)
        self.assertIsInstance(stats.nats, NATSStats)
        self.assertEqual(stats.nats.status, "CONNECTED")
        self.assertEqual(stats.nats.server_url, "nats://localhost:4222")
        self.assertEqual(stats.nats.in_msgs, 1000)

    def test_degraded_boot_both_null(self):
        # Critical contract — round-131 backend emits {postgres:null,
        # nats:null} on degraded boot. SDK must surface None (not
        # crash, not empty dict).
        routes = {"GET /api/v2/server-info/connections":
                  (200, '{"postgres":null,"nats":null}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            stats = c.server_info_connections()
        self.assertIsNone(stats.postgres)
        self.assertIsNone(stats.nats)

    def test_partial_boot_pg_up_nats_down(self):
        # Per-service nullability — PG configured, NATS not. The
        # SPA reads each pointer independently and surfaces only
        # the unhealthy one.
        payload = {
            "postgres": _FULL_PAYLOAD["postgres"],
            "nats": None,
        }
        routes = {"GET /api/v2/server-info/connections":
                  (200, json.dumps(payload))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            stats = c.server_info_connections()
        self.assertIsNotNone(stats.postgres)
        self.assertEqual(stats.postgres.acquired_conns, 5)
        self.assertIsNone(stats.nats)

    def test_sends_no_auth_header(self):
        # Public endpoint — SDK must NOT attach Authorization.
        routes = {"GET /api/v2/server-info/connections":
                  (200, json.dumps(_FULL_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.server_info_connections()
        req = srv.requests[0]
        self.assertEqual(req["auth"], "")
        self.assertEqual(req["method"], "GET")
        self.assertEqual(req["body"], "")

    def test_works_with_no_credentials(self):
        routes = {"GET /api/v2/server-info/connections":
                  (200, json.dumps(_FULL_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url)
            stats = c.server_info_connections()
        self.assertIsNotNone(stats.postgres)


class AsyncConnectionsTests(unittest.IsolatedAsyncioTestCase):

    async def test_async_returns_typed_response(self):
        routes = {"GET /api/v2/server-info/connections":
                  (200, json.dumps(_FULL_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                stats = await c.server_info_connections()
        self.assertIsInstance(stats, ConnectionStats)
        self.assertEqual(stats.postgres.max_conns, 20)
        self.assertEqual(stats.nats.status, "CONNECTED")

    async def test_async_handles_nulls(self):
        routes = {"GET /api/v2/server-info/connections":
                  (200, '{"postgres":null,"nats":null}')}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                stats = await c.server_info_connections()
        self.assertIsNone(stats.postgres)
        self.assertIsNone(stats.nats)


if __name__ == "__main__":
    unittest.main()
