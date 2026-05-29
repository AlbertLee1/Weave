"""BDD acceptance tests for the round-61 AsyncTransactionsAPI mirror.

Round 60 added the sync TransactionsAPI; this round mirrors it
on WeaveAsyncClient so async callers don't have to drop down to
raw httpx and hand-build the ?preview=true query.
"""
from __future__ import annotations

import json
import unittest

from weave_client import (
    Transaction,
    TransactionAppendResponse,
    WeaveAsyncClient,
)

from tests.test_client import _StubServer


def _route(payload):
    return (200, json.dumps(payload))


class AsyncTransactionsAPITests(unittest.IsolatedAsyncioTestCase):

    async def test_append_edits_returns_response_and_attaches_preview_flag(self):
        payload = {
            "transactionId": "tx-1",
            "appendedEdits": 2,
            "totalEdits": 2,
        }
        routes = {
            "POST /api/v2/ontologies/nw/transactions/tx-1/edits": _route(payload),
        }
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                edits = [
                    {"type": "CREATE", "objectType": "User", "primaryKey": "u1",
                     "properties": {"name": "alice"}},
                    {"type": "MODIFY", "objectType": "User", "primaryKey": "u1",
                     "properties": {"name": "alice2"}},
                ]
                resp = await c.transactions.append_edits("nw", "tx-1", edits)

        self.assertIsInstance(resp, TransactionAppendResponse)
        self.assertEqual(resp.transaction_id, "tx-1")
        self.assertEqual(resp.appended_edits, 2)
        self.assertEqual(resp.total_edits, 2)
        sent = srv.requests[0]
        self.assertIn("preview=true", sent["path"])
        body = json.loads(sent["body"])
        self.assertEqual(len(body["edits"]), 2)

    async def test_get_returns_typed_transaction(self):
        payload = {
            "transactionId": "tx-1",
            "totalEdits": 3,
            "edits": [
                {"type": "CREATE", "objectType": "User", "primaryKey": "u1"},
                {"type": "MODIFY", "objectType": "User", "primaryKey": "u1"},
                {"type": "DELETE", "objectType": "User", "primaryKey": "u2"},
            ],
        }
        routes = {"GET /api/v2/ontologies/nw/transactions/tx-1": _route(payload)}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                tx = await c.transactions.get("nw", "tx-1")
        self.assertIsInstance(tx, Transaction)
        self.assertEqual(tx.transaction_id, "tx-1")
        self.assertEqual(tx.total_edits, 3)
        self.assertEqual(len(tx.edits), 3)
        self.assertEqual(tx.edits[2]["primaryKey"], "u2")

    async def test_get_unknown_returns_empty_transaction(self):
        payload = {"transactionId": "ghost", "totalEdits": 0, "edits": []}
        routes = {"GET /api/v2/ontologies/nw/transactions/ghost": _route(payload)}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                tx = await c.transactions.get("nw", "ghost")
        self.assertEqual(tx.total_edits, 0)
        self.assertEqual(tx.edits, [])

    async def test_abort_sends_delete_with_preview_and_returns_none(self):
        routes = {
            "DELETE /api/v2/ontologies/nw/transactions/tx-1": (204, ""),
        }
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                result = await c.transactions.abort("nw", "tx-1")
        self.assertIsNone(result)
        sent = srv.requests[0]
        self.assertEqual(sent["method"], "DELETE")
        self.assertIn("preview=true", sent["path"])

    async def test_abort_idempotent_on_unknown(self):
        routes = {"DELETE /api/v2/ontologies/nw/transactions/ghost": (204, "")}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                await c.transactions.abort("nw", "ghost")  # must not raise

    async def test_lifecycle_append_get_abort(self):
        # End-to-end async lifecycle on a single client connection:
        # append, read back, abort. Each call uses one route from
        # the stub so we can also assert request ordering / count.
        append_payload = {"transactionId": "tx-life", "appendedEdits": 1, "totalEdits": 1}
        get_payload = {
            "transactionId": "tx-life",
            "totalEdits": 1,
            "edits": [{"type": "CREATE", "objectType": "User", "primaryKey": "u1"}],
        }
        routes = {
            "POST /api/v2/ontologies/nw/transactions/tx-life/edits": _route(append_payload),
            "GET /api/v2/ontologies/nw/transactions/tx-life": _route(get_payload),
            "DELETE /api/v2/ontologies/nw/transactions/tx-life": (204, ""),
        }
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                appended = await c.transactions.append_edits(
                    "nw", "tx-life",
                    [{"type": "CREATE", "objectType": "User", "primaryKey": "u1"}],
                )
                self.assertEqual(appended.total_edits, 1)
                tx = await c.transactions.get("nw", "tx-life")
                self.assertEqual(len(tx.edits), 1)
                await c.transactions.abort("nw", "tx-life")

        # Order matters — POST then GET then DELETE.
        methods = [r["method"] for r in srv.requests]
        self.assertEqual(methods, ["POST", "GET", "DELETE"])


if __name__ == "__main__":
    unittest.main()
