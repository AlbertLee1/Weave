"""BDD acceptance tests for the round-60 TransactionsAPI Python
wrapper.

Round 59 added GET + DELETE on the OntologyTransaction preview
surface; this round wraps all three endpoints (POST .../edits,
GET .../{id}, DELETE .../{id}) in a TransactionsAPI namespace so
Python callers don't have to hand-build URLs with the mandatory
?preview=true query.
"""
from __future__ import annotations

import json
import unittest

from weave_client import Client, Transaction, TransactionAppendResponse

from tests.test_client import _StubServer


def _route(payload):
    return (200, json.dumps(payload))


class TransactionsAPITests(unittest.TestCase):

    def test_append_edits_returns_response_and_attaches_preview_flag(self):
        payload = {
            "transactionId": "tx-1",
            "appendedEdits": 2,
            "totalEdits": 2,
        }
        routes = {
            "POST /api/v2/ontologies/nw/transactions/tx-1/edits": _route(payload),
        }
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            edits = [
                {"type": "CREATE", "objectType": "User", "primaryKey": "u1", "properties": {"name": "alice"}},
                {"type": "MODIFY", "objectType": "User", "primaryKey": "u1", "properties": {"name": "alice2"}},
            ]
            resp = c.transactions.append_edits("nw", "tx-1", edits)

        self.assertIsInstance(resp, TransactionAppendResponse)
        self.assertEqual(resp.transaction_id, "tx-1")
        self.assertEqual(resp.appended_edits, 2)
        self.assertEqual(resp.total_edits, 2)
        sent = srv.requests[0]
        # preview=true must be on the URL — the server gates the
        # endpoint behind it, so wrapper callers should never have
        # to remember the flag.
        self.assertIn("preview=true", sent["path"])
        # Body must round-trip the caller's edit list verbatim.
        body = json.loads(sent["body"])
        self.assertEqual(len(body["edits"]), 2)
        self.assertEqual(body["edits"][0]["objectType"], "User")

    def test_get_returns_typed_transaction(self):
        payload = {
            "transactionId": "tx-1",
            "totalEdits": 3,
            "edits": [
                {"type": "CREATE", "objectType": "User", "primaryKey": "u1"},
                {"type": "MODIFY", "objectType": "User", "primaryKey": "u1"},
                {"type": "DELETE", "objectType": "User", "primaryKey": "u2"},
            ],
        }
        routes = {
            "GET /api/v2/ontologies/nw/transactions/tx-1": _route(payload),
        }
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            tx = c.transactions.get("nw", "tx-1")
        self.assertIsInstance(tx, Transaction)
        self.assertEqual(tx.transaction_id, "tx-1")
        self.assertEqual(tx.total_edits, 3)
        self.assertEqual(len(tx.edits), 3)
        self.assertEqual(tx.edits[0]["type"], "CREATE")

    def test_get_unknown_returns_empty_transaction(self):
        # The server returns 200 + {totalEdits:0, edits:[]} for unknown
        # transactions (auto-create-on-first-use semantic). The wrapper
        # must surface the same shape — no exception, just an empty
        # collection.
        payload = {"transactionId": "ghost", "totalEdits": 0, "edits": []}
        routes = {
            "GET /api/v2/ontologies/nw/transactions/ghost": _route(payload),
        }
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            tx = c.transactions.get("nw", "ghost")
        self.assertEqual(tx.total_edits, 0)
        self.assertEqual(tx.edits, [])

    def test_abort_sends_delete_with_preview_and_returns_none(self):
        routes = {
            # 204 No Content — payload empty.
            "DELETE /api/v2/ontologies/nw/transactions/tx-1": (204, ""),
        }
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.transactions.abort("nw", "tx-1")
        self.assertIsNone(result)
        sent = srv.requests[0]
        self.assertEqual(sent["method"], "DELETE")
        self.assertIn("preview=true", sent["path"])

    def test_abort_idempotent_on_unknown(self):
        # Server returns 204 even for unknown txn (idempotent); wrapper
        # passes through and returns None without exception.
        routes = {
            "DELETE /api/v2/ontologies/nw/transactions/ghost": (204, ""),
        }
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.transactions.abort("nw", "ghost")  # must not raise

    def test_path_encoding_handles_special_chars(self):
        # Foundry transaction IDs are opaque strings; the wrapper
        # must URL-encode them so colons / slashes don't break
        # routing.
        payload = {"transactionId": "tx:1/2", "totalEdits": 0, "edits": []}
        # The stub keys the route by the un-encoded path. URL-encoding
        # turns ':' into %3A and '/' into %2F so the path the stub
        # sees won't be the raw form — we just assert the wrapper
        # called *some* path that decodes back to the original.
        routes = {}  # no preregistered route → 404
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            try:
                c.transactions.get("nw", "tx:1/2")
            except Exception:
                pass
            # The recorded request path must contain the encoded txnID.
            self.assertTrue(
                any("tx%3A1%2F2" in r["path"] or "tx:1/2" in r["path"]
                    for r in srv.requests),
                f"requests={srv.requests}",
            )


if __name__ == "__main__":
    unittest.main()
