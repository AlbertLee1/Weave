"""BDD acceptance tests for the round-71 ReactionsAPI Python wrapper.

The Go server has /api/v2/reactions surface — Aggregate / Create /
Delete / AggregateBatch (round 67). This module wraps them so
Python callers don't have to hand-build URLs with the right
query params (?targetRid=… on the single endpoints) or remember
the wire shape of the batch envelope.
"""
from __future__ import annotations

import json
import unittest

from weave_client import Client, EmojiCount, Reaction, ReactionSummary

from tests.test_client import _StubServer


def _route(payload, status=200):
    return (status, json.dumps(payload))


class ReactionsAPITests(unittest.TestCase):

    def test_aggregate_returns_typed_summary(self):
        payload = {
            "targetRid": "ri.objects.main.Customer.1",
            "emojis": [
                {"emoji": "👍", "count": 5, "mine": True},
                {"emoji": "🔥", "count": 2, "mine": False},
            ],
        }
        routes = {"GET /api/v2/reactions": _route(payload)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            summary = c.reactions.aggregate("ri.objects.main.Customer.1")

        self.assertIsInstance(summary, ReactionSummary)
        self.assertEqual(summary.target_rid, "ri.objects.main.Customer.1")
        self.assertEqual(len(summary.emojis), 2)
        self.assertIsInstance(summary.emojis[0], EmojiCount)
        self.assertEqual(summary.emojis[0].emoji, "👍")
        self.assertEqual(summary.emojis[0].count, 5)
        self.assertTrue(summary.emojis[0].mine)
        # GET should pass targetRid as a query param.
        sent = srv.requests[0]
        self.assertIn("targetRid=ri.objects.main.Customer.1", sent["path"])

    def test_aggregate_empty_emojis_returns_empty_list(self):
        # Server emits {emojis: []} for targets with no reactions —
        # wrapper must surface non-nil empty list so callers iterate
        # without nil-check.
        payload = {"targetRid": "ri.objects.main.Customer.99", "emojis": []}
        routes = {"GET /api/v2/reactions": _route(payload)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            summary = c.reactions.aggregate("ri.objects.main.Customer.99")
        self.assertEqual(summary.emojis, [])

    def test_create_returns_typed_reaction(self):
        payload = {
            "id": "r-1",
            "userId": "u-1",
            "targetRid": "ri.objects.main.Customer.1",
            "emoji": "👍",
            "createdAt": "2026-05-25T00:00:00Z",
        }
        routes = {"POST /api/v2/reactions": (201, json.dumps(payload))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            reaction = c.reactions.create("ri.objects.main.Customer.1", "👍")

        self.assertIsInstance(reaction, Reaction)
        self.assertEqual(reaction.id, "r-1")
        self.assertEqual(reaction.user_id, "u-1")
        self.assertEqual(reaction.emoji, "👍")
        # Body must carry targetRid + emoji.
        body = json.loads(srv.requests[0]["body"])
        self.assertEqual(body["targetRid"], "ri.objects.main.Customer.1")
        self.assertEqual(body["emoji"], "👍")

    def test_delete_returns_none_and_passes_query(self):
        routes = {"DELETE /api/v2/reactions": (204, "")}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.reactions.delete("ri.objects.main.Customer.1", "👍")
        self.assertIsNone(result)
        sent = srv.requests[0]
        self.assertEqual(sent["method"], "DELETE")
        # targetRid + emoji on the URL query — server reads them
        # from r.URL.Query().Get(…) on the same path as Aggregate.
        self.assertIn("targetRid=ri.objects.main.Customer.1", sent["path"])
        # 👍 is URL-encoded to %F0%9F%91%8D — accept either form.
        self.assertTrue(
            "emoji=%F0%9F%91%8D" in sent["path"] or "emoji=👍" in sent["path"],
            f"emoji missing or wrong shape in path: {sent['path']}",
        )

    def test_aggregate_batch_round_trips_summaries_in_input_order(self):
        payload = {
            "summaries": [
                {"targetRid": "ri.a", "emojis": [{"emoji": "👍", "count": 1, "mine": False}]},
                {"targetRid": "ri.b", "emojis": []},
                {"targetRid": "ri.c", "emojis": [{"emoji": "🎉", "count": 3, "mine": True}]},
            ],
        }
        routes = {"POST /api/v2/reactions/batch": _route(payload)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            summaries = c.reactions.aggregate_batch(["ri.a", "ri.b", "ri.c"])

        self.assertEqual(len(summaries), 3)
        self.assertIsInstance(summaries[0], ReactionSummary)
        # Order preserved — summaries[i] matches targetRids[i].
        self.assertEqual(summaries[0].target_rid, "ri.a")
        self.assertEqual(summaries[1].target_rid, "ri.b")
        self.assertEqual(summaries[2].target_rid, "ri.c")
        # Empty middle target has non-nil empty emojis.
        self.assertEqual(summaries[1].emojis, [])
        # POST body carries the targetRids array.
        body = json.loads(srv.requests[0]["body"])
        self.assertEqual(body["targetRids"], ["ri.a", "ri.b", "ri.c"])

    def test_aggregate_batch_empty_input_short_circuits_without_http(self):
        # Empty input → return empty list without hitting the
        # server (saves a round-trip on the no-row Foundry list
        # render path).
        routes = {}  # any GET/POST would 404 from the stub
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            summaries = c.reactions.aggregate_batch([])
        self.assertEqual(summaries, [])
        self.assertEqual(len(srv.requests), 0,
                         "wrapper should short-circuit empty input without HTTP")


if __name__ == "__main__":
    unittest.main()
