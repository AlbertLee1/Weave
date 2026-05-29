"""Round-102 SDK BDD — sessions namespace, mirror of round-101 +
the existing US-254 session endpoints (which round 101 also
documented in OpenAPI).

Contract under test:
- ``c.sessions.list() -> List[Session]``
- ``c.sessions.revoke(session_id) -> None`` (204)
- ``c.sessions.revoke_others() -> RevokeOthersResponse``
- Same surface on AsyncSessionsAPI via WeaveAsyncClient.

The wire format uses snake_case for created_at/last_seen/user_agent
(SessionView keeps Go's json:"..." tag spellings unchanged); the
SDK Session model accepts both via Pydantic field aliases.
"""
from __future__ import annotations

import json
import unittest

from weave_client import (
    Client,
    RevokeOthersResponse,
    Session,
    WeaveAsyncClient,
)

from tests.test_client import _StubServer


_SESSION_PAYLOAD = {
    "sessions": [
        {
            "id": "s1",
            "ip": "1.1.1.1",
            "user_agent": "Mozilla",
            "created_at": "2026-05-25T10:00:00Z",
            "last_seen": "2026-05-25T11:30:00Z",
            "current": True,
        },
        {
            "id": "s2",
            "ip": "2.2.2.2",
            "user_agent": "curl",
            "created_at": "2026-05-24T08:00:00Z",
            "last_seen": "2026-05-24T08:05:00Z",
            "current": False,
        },
    ]
}


class SyncSessionsTests(unittest.TestCase):

    def test_list_returns_typed_sessions(self):
        routes = {"GET /api/auth/sessions": (200, json.dumps(_SESSION_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            sessions = c.sessions.list()
        self.assertEqual(len(sessions), 2)
        self.assertIsInstance(sessions[0], Session)
        self.assertEqual(sessions[0].id, "s1")
        self.assertEqual(sessions[0].ip, "1.1.1.1")
        self.assertEqual(sessions[0].user_agent, "Mozilla")
        self.assertTrue(sessions[0].current)
        self.assertFalse(sessions[1].current)

    def test_list_handles_empty_response(self):
        routes = {"GET /api/auth/sessions": (200, '{"sessions":[]}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            sessions = c.sessions.list()
        self.assertEqual(sessions, [])

    def test_revoke_sends_delete_and_returns_none(self):
        routes = {"DELETE /api/auth/sessions/s1": (204, "")}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.sessions.revoke("s1")
        self.assertIsNone(result)
        self.assertEqual(srv.requests[0]["method"], "DELETE")
        self.assertEqual(srv.requests[0]["path"], "/api/auth/sessions/s1")

    def test_revoke_url_quotes_session_id(self):
        # Defense in depth — even though session IDs are server-generated
        # opaque tokens, the wrapper must url-quote any slashes that a
        # caller might pass via wrong-type defensive coding.
        routes = {"DELETE /api/auth/sessions/sess%2Ffoo": (204, "")}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.sessions.revoke("sess/foo")
        self.assertIn("sess%2Ffoo", srv.requests[0]["path"])

    def test_revoke_others_returns_typed_response(self):
        payload = {"revoked": 3, "currentSessionId": "s1"}
        routes = {"POST /api/auth/sessions/revoke-others":
                  (200, json.dumps(payload))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            resp = c.sessions.revoke_others()
        self.assertIsInstance(resp, RevokeOthersResponse)
        self.assertEqual(resp.revoked, 3)
        self.assertEqual(resp.current_session_id, "s1")
        # Body must be empty/absent — the endpoint takes no input.
        self.assertEqual(srv.requests[0]["body"], "")

    def test_revoke_others_handles_no_anchor(self):
        # API-key auth contract: server returns currentSessionId="" and
        # revoked count of all caller sessions.
        payload = {"revoked": 7, "currentSessionId": ""}
        routes = {"POST /api/auth/sessions/revoke-others":
                  (200, json.dumps(payload))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            resp = c.sessions.revoke_others()
        self.assertEqual(resp.revoked, 7)
        self.assertEqual(resp.current_session_id, "")


class AsyncSessionsTests(unittest.IsolatedAsyncioTestCase):

    async def test_async_list_returns_typed_sessions(self):
        routes = {"GET /api/auth/sessions": (200, json.dumps(_SESSION_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                sessions = await c.sessions.list()
        self.assertEqual(len(sessions), 2)
        self.assertIsInstance(sessions[0], Session)
        self.assertEqual(sessions[0].id, "s1")

    async def test_async_revoke(self):
        routes = {"DELETE /api/auth/sessions/s1": (204, "")}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                result = await c.sessions.revoke("s1")
        self.assertIsNone(result)
        self.assertEqual(srv.requests[0]["method"], "DELETE")

    async def test_async_revoke_others(self):
        payload = {"revoked": 2, "currentSessionId": "s1"}
        routes = {"POST /api/auth/sessions/revoke-others":
                  (200, json.dumps(payload))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                resp = await c.sessions.revoke_others()
        self.assertEqual(resp.revoked, 2)
        self.assertEqual(resp.current_session_id, "s1")


if __name__ == "__main__":
    unittest.main()
