"""BDD acceptance tests for the round-72 NotificationsAPI Python wrapper.

The Go server has 4 /api/v2/notifications endpoints (List + round-66
unread-count + MarkAllRead + MarkRead). This wrapper makes them
ergonomic for Python callers without hand-building URLs.
"""
from __future__ import annotations

import json
import unittest

from weave_client import Client, Notification

from tests.test_client import _StubServer


def _route(payload, status=200):
    return (status, json.dumps(payload))


class NotificationsAPITests(unittest.TestCase):

    def test_list_returns_typed_notifications(self):
        payload = {
            "data": [
                {
                    "id": "n1", "userId": "u-dev", "title": "Welcome",
                    "body": "Hi there", "type": "system", "read": False,
                    "createdAt": "2026-05-25T00:00:00Z",
                },
                {
                    "id": "n2", "userId": "u-dev", "title": "Mention",
                    "body": "@dev was tagged", "type": "mention", "read": True,
                    "createdAt": "2026-05-25T00:05:00Z",
                    "link": "/object/x",
                },
            ],
        }
        routes = {"GET /api/v2/notifications": _route(payload)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            items = c.notifications.list()
        self.assertEqual(len(items), 2)
        self.assertIsInstance(items[0], Notification)
        self.assertEqual(items[0].id, "n1")
        self.assertEqual(items[0].title, "Welcome")
        self.assertEqual(items[0].type, "system")
        self.assertFalse(items[0].read)
        self.assertEqual(items[1].link, "/object/x")
        self.assertTrue(items[1].read)
        # Plain list, no query params on the default call.
        sent = srv.requests[0]
        self.assertNotIn("?", sent["path"])

    def test_list_unread_only_appends_query_param(self):
        payload = {"data": []}
        routes = {"GET /api/v2/notifications": _route(payload)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.notifications.list(unread_only=True)
        sent = srv.requests[0]
        self.assertIn("unread=true", sent["path"])

    def test_list_with_type_filter_appends_csv_query(self):
        payload = {"data": []}
        routes = {"GET /api/v2/notifications": _route(payload)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.notifications.list(types=["mention", "watch"])
        sent = srv.requests[0]
        # Server reads ?type=… via r.URL.Query()["type"] — multi-param
        # OR csv both work; wrapper uses multi-param for portability.
        self.assertEqual(sent["path"].count("type="), 2)
        self.assertIn("type=mention", sent["path"])
        self.assertIn("type=watch", sent["path"])

    def test_list_empty_data_returns_empty_list_not_none(self):
        # Server may emit data=null on degraded paths; wrapper must
        # normalise to [] so SDK iteration stays nil-safe.
        routes = {"GET /api/v2/notifications": _route({"data": None})}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            items = c.notifications.list()
        self.assertEqual(items, [])

    def test_unread_count_returns_int(self):
        routes = {"GET /api/v2/notifications/unread-count": _route({"count": 7})}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            n = c.notifications.unread_count()
        self.assertEqual(n, 7)
        self.assertIsInstance(n, int)

    def test_unread_count_handles_missing_field_safely(self):
        # Defensive: a future server bug emitting empty body shouldn't
        # explode SDK callers — return 0 and let them handle it.
        routes = {"GET /api/v2/notifications/unread-count": _route({})}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            n = c.notifications.unread_count()
        self.assertEqual(n, 0)

    def test_mark_read_posts_to_per_row_path(self):
        # 204 No Content — the server's MarkNotificationRead returns
        # 204 on success; wrapper returns None.
        routes = {"POST /api/v2/notifications/n42/read": (204, "")}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.notifications.mark_read("n42")
        self.assertIsNone(result)
        sent = srv.requests[0]
        self.assertEqual(sent["method"], "POST")
        self.assertEqual(sent["path"], "/api/v2/notifications/n42/read")

    def test_mark_all_read_returns_updated_count(self):
        routes = {"POST /api/v2/notifications/read-all": _route({"updated": 12})}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            updated = c.notifications.mark_all_read()
        self.assertEqual(updated, 12)
        sent = srv.requests[0]
        self.assertNotIn("type=", sent["path"])

    def test_mark_all_read_with_type_filter(self):
        routes = {"POST /api/v2/notifications/read-all": _route({"updated": 3})}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            updated = c.notifications.mark_all_read(types=["mention"])
        self.assertEqual(updated, 3)
        sent = srv.requests[0]
        self.assertIn("type=mention", sent["path"])

    def test_path_quote_handles_special_chars_in_id(self):
        # Notification IDs are server-assigned (RFC4122 in practice) but
        # the wrapper must still URL-quote in case a future server uses
        # opaque tokens with /:?# in them.
        routes = {}  # any request → 404 from stub
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            try:
                c.notifications.mark_read("n/42?foo")
            except Exception:
                pass
            sent = srv.requests[0]
            self.assertIn("n%2F42%3Ffoo", sent["path"])


if __name__ == "__main__":
    unittest.main()
