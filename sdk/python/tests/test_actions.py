"""Tests for the ActionsAPI namespace."""
from __future__ import annotations

import json
import unittest

from weave_client import Client

from tests.test_client import _StubServer


class ActionsAPITests(unittest.TestCase):
    def test_apply_posts_request_body(self):
        body = '{"actionRid":"ri.act.123","edits":[{"type":"CREATE","objectType":"Customer"}],"batchId":"b1","offset":7}'
        with _StubServer({"POST /api/v2/ontologies/nw/actions/apply": (200, body)}) as srv:
            c = Client(srv.url, access_token="t")
            result = c.actions.apply("nw", "createCustomer", {"name": "X"})
            req_body = srv.requests[0]["body"]
        self.assertEqual(result.action_rid, "ri.act.123")
        self.assertEqual(result.batch_id, "b1")
        self.assertEqual(result.offset, 7)
        self.assertEqual(len(result.edits), 1)
        sent = json.loads(req_body)
        self.assertEqual(sent["actionType"], "createCustomer")
        self.assertEqual(sent["parameters"], {"name": "X"})

    def test_apply_propagates_404_as_not_found(self):
        from weave_client.exceptions import WeaveNotFoundError

        body = '{"errorCode":"NOT_FOUND","errorName":"ActionTypeNotFound","errorInstanceId":"x","parameters":{}}'
        with _StubServer({"POST /api/v2/ontologies/nw/actions/apply": (404, body)}) as srv:
            c = Client(srv.url, access_token="t")
            with self.assertRaises(WeaveNotFoundError):
                c.actions.apply("nw", "missingAction", {})

    def test_apply_returns_no_edits_when_response_missing_field(self):
        body = '{"actionRid":"ri.act.empty"}'
        with _StubServer({"POST /api/v2/ontologies/nw/actions/apply": (200, body)}) as srv:
            c = Client(srv.url, access_token="t")
            result = c.actions.apply("nw", "noop", {})
        self.assertEqual(result.action_rid, "ri.act.empty")
        self.assertEqual(result.edits, [])

    def test_apply_serializes_complex_parameters(self):
        body = '{"actionRid":"r","edits":[]}'
        with _StubServer({"POST /api/v2/ontologies/nw/actions/apply": (200, body)}) as srv:
            c = Client(srv.url, access_token="t")
            c.actions.apply("nw", "complexAction", {
                "items": [1, 2, 3],
                "meta": {"x": True, "y": None},
            })
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent["parameters"]["items"], [1, 2, 3])
        self.assertEqual(sent["parameters"]["meta"]["x"], True)
        self.assertIsNone(sent["parameters"]["meta"]["y"])

    def test_apply_includes_bearer_when_token_set(self):
        body = '{"actionRid":"r","edits":[]}'
        with _StubServer({"POST /api/v2/ontologies/nw/actions/apply": (200, body)}) as srv:
            c = Client(srv.url, access_token="my-token")
            c.actions.apply("nw", "doIt", {})
        self.assertEqual(srv.requests[0]["auth"], "Bearer my-token")


if __name__ == "__main__":
    unittest.main()
