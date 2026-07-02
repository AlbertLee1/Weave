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
                "modifiedObjectsCount": 0,
                "deletedObjectsCount": 0,
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


class ApplyWithOptionsTests(unittest.TestCase):
    def test_apply_with_options_sends_options_block(self):
        body = json.dumps({
            "operationId": "op-opts",
            "edits": {
                "type": "edits",
                "addedObjectCount": 2,
                "modifiedObjectsCount": 0,
                "deletedObjectsCount": 0,
                "addedLinksCount": 0,
                "deletedLinksCount": 0,
            },
        })
        routes = {"POST /api/v2/ontologies/nw/actions/createCustomer/apply": (200, body)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.actions.apply_with_options(
                "nw", "createCustomer", {"name": "X"},
                mode="VALIDATE_ONLY",
                return_edits="NONE",
            )
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent["parameters"], {"name": "X"})
        self.assertEqual(sent["options"]["mode"], "VALIDATE_ONLY")
        self.assertEqual(sent["options"]["returnEdits"], "NONE")
        self.assertEqual(result.operation_id, "op-opts")
        self.assertEqual(result.edits.added_object_count, 2)

    def test_apply_with_options_default_values(self):
        body = json.dumps({"operationId": "op-def"})
        routes = {"POST /api/v2/ontologies/nw/actions/doIt/apply": (200, body)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.actions.apply_with_options("nw", "doIt", {})
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent["options"]["mode"], "VALIDATE_AND_EXECUTE")
        self.assertEqual(sent["options"]["returnEdits"], "ALL")


class ApplyBatchTests(unittest.TestCase):
    def test_apply_batch_posts_requests_list(self):
        body = json.dumps({
            "edits": {
                "type": "edits",
                "addedObjectCount": 3,
                "modifiedObjectsCount": 0,
                "deletedObjectsCount": 0,
                "addedLinksCount": 0,
                "deletedLinksCount": 0,
            },
        })
        routes = {"POST /api/v2/ontologies/nw/actions/createCustomer/applyBatch": (200, body)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            reqs = [
                {"parameters": {"name": "A"}},
                {"parameters": {"name": "B"}},
                {"parameters": {"name": "C"}},
            ]
            result = c.actions.apply_batch("nw", "createCustomer", reqs)
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(len(sent["requests"]), 3)
        self.assertNotIn("options", sent)  # default return_edits="ALL" omits options
        self.assertIsNotNone(result.edits)
        self.assertEqual(result.edits.added_object_count, 3)

    def test_apply_batch_with_return_edits_option(self):
        body = json.dumps({})
        routes = {"POST /api/v2/ontologies/nw/actions/doIt/applyBatch": (200, body)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.actions.apply_batch("nw", "doIt", [{"parameters": {}}], return_edits="NONE")
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent["options"]["returnEdits"], "NONE")

    def test_apply_batch_empty_response(self):
        routes = {"POST /api/v2/ontologies/nw/actions/doIt/applyBatch": (200, "{}")}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.actions.apply_batch("nw", "doIt", [])
        self.assertIsNone(result.edits)


class ExecuteQueryTests(unittest.TestCase):
    def test_execute_query_posts_parameters(self):
        resp = json.dumps({"value": [{"customerId": "ALFKI"}]})
        routes = {"POST /api/v2/ontologies/nw/queries/topCustomers/execute": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.actions.execute_query("nw", "topCustomers", {"limit": 10})
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent["parameters"], {"limit": 10})
        self.assertEqual(result["value"][0]["customerId"], "ALFKI")

    def test_execute_query_with_no_parameters(self):
        resp = json.dumps({"value": 42})
        routes = {"POST /api/v2/ontologies/nw/queries/countAll/execute": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.actions.execute_query("nw", "countAll")
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent["parameters"], {})
        self.assertEqual(result["value"], 42)

    def test_execute_query_returns_empty_dict_on_null(self):
        routes = {"POST /api/v2/ontologies/nw/queries/noResult/execute": (200, "")}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.actions.execute_query("nw", "noResult")
        self.assertEqual(result, {})


if __name__ == "__main__":
    unittest.main()
