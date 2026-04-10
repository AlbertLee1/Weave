"""Tests for the ActionsAPI namespace.

The Python SDK mirrors the Foundry OSv2 action apply shape, where the
action API name is carried in the URL rather than the request body.
Endpoints follow:

    POST /api/v2/ontologies/{ontology}/actions/{action}/apply

Response envelope is SyncApplyActionResponseV2:
    { operationId?, validation?, edits?: ActionResults }
"""
from __future__ import annotations

import json
import unittest

from weave_client import Client

from tests.test_client import _StubServer


class ActionsAPITests(unittest.TestCase):
    def test_apply_posts_request_body(self):
        body = json.dumps({
            "operationId": "op-123",
            "edits": {
                "type": "edits",
                "addedObjectCount": 1,
                "modifiedObjectCount": 0,
                "deletedObjectCount": 0,
                "addedLinksCount": 0,
                "deletedLinksCount": 0,
            },
        })
        routes = {"POST /api/v2/ontologies/nw/actions/createCustomer/apply": (200, body)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.actions.apply("nw", "createCustomer", {"name": "X"})
            req_body = srv.requests[0]["body"]
        self.assertEqual(result.operation_id, "op-123")
        self.assertIsNotNone(result.edits)
        self.assertEqual(result.edits.added_object_count, 1)
        self.assertEqual(result.edits.type, "edits")
        sent = json.loads(req_body)
        # Body must NOT contain actionType — the action lives in the URL.
        self.assertNotIn("actionType", sent)
        self.assertEqual(sent["parameters"], {"name": "X"})

    def test_apply_propagates_404_as_not_found(self):
        from weave_client.exceptions import WeaveNotFoundError

        body = '{"errorCode":"NOT_FOUND","errorName":"ActionTypeNotFound","errorInstanceId":"x","parameters":{}}'
        routes = {"POST /api/v2/ontologies/nw/actions/missingAction/apply": (404, body)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            with self.assertRaises(WeaveNotFoundError):
                c.actions.apply("nw", "missingAction", {})

    def test_apply_returns_no_edits_when_response_missing_field(self):
        body = '{"operationId":"op-empty"}'
        routes = {"POST /api/v2/ontologies/nw/actions/noop/apply": (200, body)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.actions.apply("nw", "noop", {})
        self.assertEqual(result.operation_id, "op-empty")
        self.assertIsNone(result.edits)

    def test_apply_serializes_complex_parameters(self):
        body = json.dumps({"operationId": "op-c"})
        routes = {"POST /api/v2/ontologies/nw/actions/complexAction/apply": (200, body)}
        with _StubServer(routes) as srv:
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
        body = json.dumps({"operationId": "op-auth"})
        routes = {"POST /api/v2/ontologies/nw/actions/doIt/apply": (200, body)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="my-token")
            c.actions.apply("nw", "doIt", {})
        self.assertEqual(srv.requests[0]["auth"], "Bearer my-token")

    def test_apply_url_encodes_action_name(self):
        """Action names with spaces or slashes must be percent-encoded."""
        body = json.dumps({"operationId": "op-enc"})
        routes = {"POST /api/v2/ontologies/nw/actions/weird%20action/apply": (200, body)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.actions.apply("nw", "weird action", {})
        # The stub server matches on exact path; if the SDK didn't encode
        # the space the request would 404 and raise.


if __name__ == "__main__":
    unittest.main()
