"""BDD acceptance tests for the round-74 AsyncReactionsAPI mirror.

Round 71 added the sync ReactionsAPI; this round mirrors it on
WeaveAsyncClient so async callers (FastAPI handlers, asyncio bots)
get the same ergonomic wrapper without dropping down to raw httpx.
Reuses Reaction/ReactionSummary/EmojiCount dataclasses from the
sync module via lazy import (round-61 pattern).
"""
from __future__ import annotations

import json
import unittest

from weave_client import EmojiCount, Reaction, ReactionSummary, WeaveAsyncClient

from tests.test_client import _StubServer


def _route(payload, status=200):
    return (status, json.dumps(payload))


class AsyncReactionsAPITests(unittest.IsolatedAsyncioTestCase):

    async def test_aggregate_returns_typed_summary(self):
        payload = {
            "targetRid": "ri.objects.main.Customer.1",
            "emojis": [
                {"emoji": "👍", "count": 5, "mine": True},
                {"emoji": "🔥", "count": 2, "mine": False},
            ],
        }
        routes = {"GET /api/v2/reactions": _route(payload)}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                summary = await c.reactions.aggregate("ri.objects.main.Customer.1")
        self.assertIsInstance(summary, ReactionSummary)
        self.assertEqual(summary.target_rid, "ri.objects.main.Customer.1")
        self.assertEqual(len(summary.emojis), 2)
        self.assertIsInstance(summary.emojis[0], EmojiCount)
        self.assertTrue(summary.emojis[0].mine)
        sent = srv.requests[0]
        self.assertIn("targetRid=ri.objects.main.Customer.1", sent["path"])

    async def test_create_returns_typed_reaction(self):
        payload = {
            "id": "r-1", "userId": "u-1",
            "targetRid": "ri.objects.main.Customer.1",
            "emoji": "👍",
            "createdAt": "2026-05-25T00:00:00Z",
        }
        routes = {"POST /api/v2/reactions": (201, json.dumps(payload))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                r = await c.reactions.create("ri.objects.main.Customer.1", "👍")
        self.assertIsInstance(r, Reaction)
        self.assertEqual(r.id, "r-1")
        self.assertEqual(r.emoji, "👍")

    async def test_delete_returns_none(self):
        routes = {"DELETE /api/v2/reactions": (204, "")}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                result = await c.reactions.delete("ri.objects.main.Customer.1", "👍")
        self.assertIsNone(result)
        sent = srv.requests[0]
        self.assertEqual(sent["method"], "DELETE")

    async def test_aggregate_batch_preserves_input_order(self):
        payload = {
            "summaries": [
                {"targetRid": "ri.a", "emojis": [{"emoji": "👍", "count": 1, "mine": False}]},
                {"targetRid": "ri.b", "emojis": []},
                {"targetRid": "ri.c", "emojis": [{"emoji": "🎉", "count": 3, "mine": True}]},
            ],
        }
        routes = {"POST /api/v2/reactions/batch": _route(payload)}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                summaries = await c.reactions.aggregate_batch(["ri.a", "ri.b", "ri.c"])
        self.assertEqual([s.target_rid for s in summaries], ["ri.a", "ri.b", "ri.c"])

    async def test_aggregate_batch_empty_input_short_circuits(self):
        with _StubServer({}) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                summaries = await c.reactions.aggregate_batch([])
        self.assertEqual(summaries, [])
        self.assertEqual(len(srv.requests), 0,
                         "async wrapper must short-circuit empty input without HTTP — same perf invariant as round 71")


if __name__ == "__main__":
    unittest.main()
