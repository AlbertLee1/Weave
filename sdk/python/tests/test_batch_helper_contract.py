"""Round-94 SDK contract test — sibling of backend round 93.

The backend has 8 POST .../getByRidBatch endpoints (locked in
cmd/server/contract_batch_symmetry_test.go in round 93). This
test asserts the matching 8-of-8 helper methods exist on both
OntologiesAPI (sync) AND AsyncOntologiesAPI (async), and that
each one hits the URL the backend exposes with the expected
``{rids:[...]}`` body shape and returns the data array.

Codifies the round 84-90 SDK closure work as a regression guard:
a future PR that removes / renames any of the 16 methods
(8 sync + 8 async) fails this test with a clear message.

Mirrors round-93's two-test split:
  1. presence — all 16 method attributes exist as callables
  2. wire-shape — each method posts ``{rids: [...]}`` to the
     correct path and returns the ``data`` field, verified
     against an in-process _StubServer
"""
from __future__ import annotations

import inspect
import json
import unittest

from weave_client import Client, WeaveAsyncClient
from weave_client.async_client import AsyncOntologiesAPI
from weave_client.ontologies import OntologiesAPI

from tests.test_client import _StubServer


# (kind, helper-method-name, URL path segment). Order matches round
# 73→89 backend closure sequence.
_BATCH_KINDS = [
    ("ObjectType",   "get_object_types_by_rid_batch",          "objectTypes"),
    ("ActionType",   "get_action_types_by_rid_batch",          "actionTypes"),
    ("LinkType",     "get_link_types_by_rid_batch",            "linkTypes"),            # round 79+84+86
    ("InterfaceType", "get_interface_types_by_rid_batch",      "interfaceTypes"),       # round 81+84+86
    ("ValueType",    "get_value_types_by_rid_batch",           "valueTypes"),           # round 83+84+86
    ("SharedProperty", "get_shared_property_types_by_rid_batch", "sharedPropertyTypes"), # round 85+86
    ("TypeGroup",    "get_type_groups_by_rid_batch",           "typeGroups"),           # round 87+88
    ("QueryType",    "get_query_types_by_rid_batch",           "queryTypes"),           # round 89+90
]


class SyncBatchHelperContractTests(unittest.TestCase):

    def test_sync_ontologies_api_exposes_all_eight_batch_helpers(self):
        # Presence — round-93 sibling on the SDK side.
        missing = []
        for kind, method, _ in _BATCH_KINDS:
            if not callable(getattr(OntologiesAPI, method, None)):
                missing.append(f"{kind} (OntologiesAPI.{method})")
        if missing:
            self.fail(
                "8-of-8 sync batch helper symmetry broken. Missing methods:\n  "
                + "\n  ".join(missing)
                + "\n\nIf the removal was intentional, edit _BATCH_KINDS "
                  "in this test and document the Foundry-parity rationale "
                  "in the commit message."
            )

    def test_each_sync_helper_posts_rids_to_correct_path(self):
        # Wire-shape — every helper must hit the exact URL the
        # backend round-93 contract test guards.
        for kind, method, path_seg in _BATCH_KINDS:
            with self.subTest(kind=kind):
                resp = json.dumps({"data": [{"rid": "x-1", "apiName": kind}]})
                routes = {
                    f"POST /api/v2/ontologies/nw/{path_seg}/getByRidBatch": (200, resp)
                }
                with _StubServer(routes) as srv:
                    c = Client(srv.url, access_token="t")
                    fn = getattr(c.ontologies, method)
                    result = fn("nw", ["x-1"])
                    body = json.loads(srv.requests[0]["body"])
                self.assertEqual(body, {"rids": ["x-1"]},
                                 f"{method} did not POST canonical body")
                self.assertEqual(len(result), 1,
                                 f"{method} did not return data array")
                self.assertEqual(result[0]["rid"], "x-1")

    def test_each_sync_helper_signature_takes_ontology_and_rids(self):
        # Foundry-parity convention: every batch helper is
        # ``fn(ontology: str, rids: List[str])``. A future maintainer
        # who accidentally swaps argument order or drops the ontology
        # parameter would silently produce wrong-host requests; this
        # locks the signature.
        for _, method, _ in _BATCH_KINDS:
            with self.subTest(method=method):
                fn = getattr(OntologiesAPI, method)
                sig = inspect.signature(fn)
                params = list(sig.parameters)
                self.assertEqual(params[:3], ["self", "ontology", "rids"],
                                 f"{method} signature should start with "
                                 f"(self, ontology, rids); got {params}")


class AsyncBatchHelperContractTests(unittest.IsolatedAsyncioTestCase):

    def test_async_ontologies_api_exposes_all_eight_batch_helpers(self):
        missing = []
        for kind, method, _ in _BATCH_KINDS:
            attr = getattr(AsyncOntologiesAPI, method, None)
            if not callable(attr):
                missing.append(f"{kind} (AsyncOntologiesAPI.{method})")
                continue
            # Async-specific: must be a coroutine function.
            if not inspect.iscoroutinefunction(attr):
                missing.append(
                    f"{kind} (AsyncOntologiesAPI.{method}) is callable but "
                    "not a coroutine — async helpers must be `async def`"
                )
        if missing:
            self.fail(
                "8-of-8 async batch helper symmetry broken. Missing/wrong methods:\n  "
                + "\n  ".join(missing)
                + "\n\nIf the removal was intentional, edit _BATCH_KINDS "
                  "in this test and document the Foundry-parity rationale "
                  "in the commit message."
            )

    async def test_each_async_helper_posts_rids_to_correct_path(self):
        for kind, method, path_seg in _BATCH_KINDS:
            with self.subTest(kind=kind):
                resp = json.dumps({"data": [{"rid": "x-1", "apiName": kind}]})
                routes = {
                    f"POST /api/v2/ontologies/nw/{path_seg}/getByRidBatch": (200, resp)
                }
                with _StubServer(routes) as srv:
                    async with WeaveAsyncClient(srv.url, access_token="t") as c:
                        fn = getattr(c.ontologies, method)
                        result = await fn("nw", ["x-1"])
                        body = json.loads(srv.requests[0]["body"])
                self.assertEqual(body, {"rids": ["x-1"]},
                                 f"async {method} did not POST canonical body")
                self.assertEqual(len(result), 1,
                                 f"async {method} did not return data array")
                self.assertEqual(result[0]["rid"], "x-1")


if __name__ == "__main__":
    unittest.main()
