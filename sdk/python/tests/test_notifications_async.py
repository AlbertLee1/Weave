"""BDD acceptance tests for the round-74 AsyncNotificationsAPI mirror.

Round 72 added the sync NotificationsAPI; this round mirrors it on
WeaveAsyncClient so async navbar / inbox polling stays ergonomic.
Reuses Notification dataclass via lazy import (round-61 pattern).
"""
from __future__ import annotations

import json
import unittest

from weave_client import Notification, WeaveAsyncClient

from tests.test_client import _StubServer


def _route(payload, status=200):
    return (status, json.dumps(payload))


class AsyncNotificationsAPITests(unittest.IsolatedAsyncioTestCase):

    async def test_list_returns_typed_notifications(self):
        payload = {
            "data": [
                {
                    "id": "n1", "userId": "u-dev", "title": "Welcome",
                    "body": "Hi", "type": "system", "read": False,
                    "createdAt": "2026-05-25T00:00:00Z",
                },
            ],
        }
        routes = {"GET /api/v2/notifications": _route(payload)}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                items = await c.notifications.list()
        self.assertEqual(len(items), 1)
        self.assertIsInstance(items[0], Notification)
        self.assertEqual(items[0].id, "n1")
        self.assertFalse(items[0].read)

    async def test_list_unread_only_appends_query(self):
        routes = {"GET /api/v2/notifications": _route({"data": []})}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                await c.notifications.list(unread_only=True)
        self.assertIn("unread=true", srv.requests[0]["path"])

    async def test_list_with_type_filter_emits_repeated_params(self):
        routes = {"GET /api/v2/notifications": _route({"data": []})}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                await c.notifications.list(types=["mention", "watch"])
        path = srv.requests[0]["path"]
        self.assertEqual(path.count("type="), 2)
        self.assertIn("type=mention", path)
        self.assertIn("type=watch", path)

    async def test_unread_count_returns_int(self):
        routes = {"GET /api/v2/notifications/unread-count": _route({"count": 7})}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                n = await c.notifications.unread_count()
        self.assertEqual(n, 7)
        self.assertIsInstance(n, int)

    async def test_unread_count_defensive_zero_on_missing_field(self):
        routes = {"GET /api/v2/notifications/unread-count": _route({})}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                n = await c.notifications.unread_count()
        self.assertEqual(n, 0)

    async def test_mark_read_posts_per_row(self):
        routes = {"POST /api/v2/notifications/n42/read": (204, "")}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                result = await c.notifications.mark_read("n42")
        self.assertIsNone(result)
        self.assertEqual(srv.requests[0]["path"], "/api/v2/notifications/n42/read")

    async def test_mark_all_read_returns_updated_count(self):
        routes = {"POST /api/v2/notifications/read-all": _route({"updated": 12})}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                updated = await c.notifications.mark_all_read()
        self.assertEqual(updated, 12)

    async def test_mark_all_read_with_type_filter(self):
        routes = {"POST /api/v2/notifications/read-all": _route({"updated": 3})}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                updated = await c.notifications.mark_all_read(types=["mention"])
        self.assertEqual(updated, 3)
        self.assertIn("type=mention", srv.requests[0]["path"])

    async def test_lifecycle_count_list_then_mark(self):
        # End-to-end async lifecycle on a single client connection:
        # poll count, fetch list, mark one read. Each call uses one
        # route from the stub so we can also assert request ordering.
        routes = {
            "GET /api/v2/notifications/unread-count": _route({"count": 1}),
            "GET /api/v2/notifications": _route({"data": [
                {"id": "n1", "userId": "u-dev", "title": "T", "body": "B",
                 "type": "mention", "read": False, "createdAt": "2026-05-25T00:00:00Z"},
            ]}),
            "POST /api/v2/notifications/n1/read": (204, ""),
        }
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                count = await c.notifications.unread_count()
                self.assertEqual(count, 1)
                items = await c.notifications.list(unread_only=True)
                self.assertEqual(len(items), 1)
                await c.notifications.mark_read(items[0].id)
        # Order matters — count, list, mark.
        methods = [(r["method"], r["path"].split("?")[0]) for r in srv.requests]
        self.assertEqual(methods, [
            ("GET", "/api/v2/notifications/unread-count"),
            ("GET", "/api/v2/notifications"),
            ("POST", "/api/v2/notifications/n1/read"),
        ])


if __name__ == "__main__":
    unittest.main()
