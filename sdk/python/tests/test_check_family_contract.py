"""Round-112 SDK contract test mirroring round-111 backend.

Asserts each round-95-109 backend check-family endpoint has its
matching c.{namespace}.{method} surface on BOTH sync and async
clients. Sibling of round-94's batch helper contract; the recipe
is: enumerate every (sync_attr, async_attr, url, method) and prove
the wrapper produces the right wire shape.

Coverage map — matches cmd/server/contract_check_family_test.go:
  r95+96   c.ontologies.get_me(ontology)      GET /api/v2/ontologies/{ont}/me
  r97+98   c.permissions.check(...)           POST /api/v2/me/permissions/check
  r99+100  c.ontologies.list_me()             GET /api/v2/me/ontologies
  r101+102 c.sessions.revoke_others()         POST /api/auth/sessions/revoke-others
  r101+102 c.sessions.list()                  GET /api/auth/sessions
  r101+102 c.sessions.revoke(id)              DELETE /api/auth/sessions/{id}
  r103+104 c.actions.check(ont, action)       GET /api/v2/ontologies/{ont}/actions/{at}/check
  r105+106 c.objects.check(ont, ot)           GET /api/v2/ontologies/{ont}/objectTypes/{ot}/check
  r107+108 c.objects.check_batch(...)         POST /api/v2/me/checks/objectTypes
  r109+110 c.actions.check_batch(...)         POST /api/v2/me/checks/actionTypes

A future PR that removes/renames any of the 10 sync methods OR
10 async methods OR changes URL shapes fails this test with a
clear diagnostic.
"""
from __future__ import annotations

import inspect
import json
import unittest

from weave_client import (
    Client,
    WeaveAsyncClient,
)
from weave_client.async_client import (
    AsyncActionsAPI,
    AsyncObjectsAPI,
    AsyncOntologiesAPI,
    AsyncPermissionsAPI,
    AsyncQueriesAPI,
    AsyncSessionsAPI,
)
from weave_client.actions import ActionsAPI
from weave_client.objects import ObjectsAPI
from weave_client.ontologies import OntologiesAPI
from weave_client.permissions import PermissionsAPI
from weave_client.queries import QueriesAPI
from weave_client.sessions import SessionsAPI

from tests.test_client import _StubServer


# (namespace_attr, method_name) tuples — used by the presence tests.
_SDK_SURFACE = [
    ("ontologies", "get_me", OntologiesAPI, AsyncOntologiesAPI),
    ("ontologies", "list_me", OntologiesAPI, AsyncOntologiesAPI),
    ("permissions", "check", PermissionsAPI, AsyncPermissionsAPI),
    ("sessions", "list", SessionsAPI, AsyncSessionsAPI),
    ("sessions", "revoke", SessionsAPI, AsyncSessionsAPI),
    ("sessions", "revoke_others", SessionsAPI, AsyncSessionsAPI),
    ("actions", "check", ActionsAPI, AsyncActionsAPI),
    ("actions", "check_batch", ActionsAPI, AsyncActionsAPI),
    ("objects", "check", ObjectsAPI, AsyncObjectsAPI),
    ("objects", "check_batch", ObjectsAPI, AsyncObjectsAPI),
    ("queries", "check", QueriesAPI, AsyncQueriesAPI),
]


class SyncSurfaceContractTests(unittest.TestCase):

    def test_sync_check_family_methods_exist(self):
        # Round-111 sibling on the SDK side. Each backend endpoint
        # added in rounds 95-109 has a c.{ns}.{method} wrapper landed
        # in the matching SDK round (96-110).
        missing = []
        for ns_attr, method_name, sync_cls, _async_cls in _SDK_SURFACE:
            if not callable(getattr(sync_cls, method_name, None)):
                missing.append(f"{sync_cls.__name__}.{method_name}  (c.{ns_attr}.{method_name})")
        if missing:
            self.fail(
                "Check-family sync surface contract broken. Missing methods:\n  "
                + "\n  ".join(missing)
                + "\n\nIf removal was intentional, edit _SDK_SURFACE "
                  "in this test and document the Foundry-parity "
                  "rationale in the commit message."
            )

    def test_sync_namespaces_wired_on_client(self):
        # The class attributes existing doesn't prove `c.{ns}` works
        # — Client.__init__ has to instantiate them. Stub a dummy
        # client and check each namespace attribute resolves.
        # Use a no-op transport because we don't call any method.
        c = Client("http://example.test", access_token="t")
        for ns_attr, _method, sync_cls, _async in _SDK_SURFACE:
            attr = getattr(c, ns_attr, None)
            if attr is None:
                self.fail(f"Client.{ns_attr} not wired in __init__")
            if not isinstance(attr, sync_cls):
                self.fail(f"Client.{ns_attr} is {type(attr).__name__}, want {sync_cls.__name__}")


class AsyncSurfaceContractTests(unittest.IsolatedAsyncioTestCase):

    def test_async_check_family_methods_are_coroutines(self):
        # Async sibling — every wrapper must be `async def`, not just
        # callable, so caller `await` works without TypeError.
        missing = []
        for ns_attr, method_name, _sync_cls, async_cls in _SDK_SURFACE:
            attr = getattr(async_cls, method_name, None)
            if not callable(attr):
                missing.append(f"{async_cls.__name__}.{method_name}  (missing entirely)")
                continue
            if not inspect.iscoroutinefunction(attr):
                missing.append(f"{async_cls.__name__}.{method_name}  (not async def)")
        if missing:
            self.fail(
                "Check-family async surface contract broken. Missing/wrong methods:\n  "
                + "\n  ".join(missing)
                + "\n\nIf removal was intentional, edit _SDK_SURFACE "
                  "in this test and document the Foundry-parity "
                  "rationale in the commit message."
            )

    async def test_async_namespaces_wired_on_client(self):
        async with WeaveAsyncClient("http://example.test", access_token="t") as c:
            for ns_attr, _method, _sync, async_cls in _SDK_SURFACE:
                attr = getattr(c, ns_attr, None)
                if attr is None:
                    self.fail(f"WeaveAsyncClient.{ns_attr} not wired in __init__")
                if not isinstance(attr, async_cls):
                    self.fail(
                        f"WeaveAsyncClient.{ns_attr} is {type(attr).__name__}, "
                        f"want {async_cls.__name__}")


# (sync method name → expected (HTTP method, URL after substitution, body-match)).
# These exercise the wrapper-to-wire mapping the round-95-109 BDD tests
# already covered individually; the contract test repeats them as a
# single matrix so a single-cell URL drift becomes one test failure
# instead of N scattered surface mismatches.
_WIRE_CASES = [
    # GET endpoints
    ("ontologies.get_me", "GET", "/api/v2/ontologies/nw/me", ("nw",), None),
    ("ontologies.list_me", "GET", "/api/v2/me/ontologies", (), None),
    ("sessions.list", "GET", "/api/auth/sessions", (), None),
    ("actions.check", "GET",
     "/api/v2/ontologies/nw/actions/createCustomer/check",
     ("nw", "createCustomer"), None),
    ("objects.check", "GET",
     "/api/v2/ontologies/nw/objectTypes/Customer/check",
     ("nw", "Customer"), None),
    ("queries.check", "GET",
     "/api/v2/ontologies/nw/queryTypes/topCustomers/check",
     ("nw", "topCustomers"), None),
    # DELETE endpoint
    ("sessions.revoke", "DELETE", "/api/auth/sessions/s1", ("s1",), None),
    # POST endpoints — body shape contract
    ("permissions.check", "POST", "/api/v2/me/permissions/check",
     (["objectType.read"],),
     {"permissions": ["objectType.read"]}),
    ("sessions.revoke_others", "POST", "/api/auth/sessions/revoke-others",
     (), None),
    ("objects.check_batch", "POST", "/api/v2/me/checks/objectTypes",
     ("nw", ["Customer"]),
     {"ontologyApiName": "nw", "objectTypeApiNames": ["Customer"]}),
    ("actions.check_batch", "POST", "/api/v2/me/checks/actionTypes",
     ("nw", ["createCustomer"]),
     {"ontologyApiName": "nw", "actionTypeApiNames": ["createCustomer"]}),
]


class WireShapeContractTests(unittest.TestCase):

    def _stub_payload(self, sync_dotted: str) -> str:
        """Per-method canned 200 body so the wrapper's response
        parser doesn't error out on empty bodies. The contract test
        cares about REQUEST shape, not response parsing — but we
        still need to send something well-formed."""
        if sync_dotted.endswith(".revoke"):
            return ""  # 204 No Content
        if sync_dotted in ("ontologies.get_me",):
            return json.dumps({
                "ontologyRid": "ri.x", "ontologyApiName": "nw",
                "role": "", "permissions": [], "markings": [],
            })
        if sync_dotted == "ontologies.list_me":
            return json.dumps({"ontologies": []})
        if sync_dotted == "sessions.list":
            return json.dumps({"sessions": []})
        if sync_dotted == "sessions.revoke_others":
            return json.dumps({"revoked": 0, "currentSessionId": ""})
        if sync_dotted == "permissions.check":
            return json.dumps({"granted": [], "denied": []})
        if sync_dotted == "actions.check":
            return json.dumps({
                "ontologyApiName": "nw", "actionApiName": "createCustomer",
                "actionRid": "ri.at", "canApply": True,
            })
        if sync_dotted == "objects.check":
            return json.dumps({
                "ontologyApiName": "nw", "objectTypeApiName": "Customer",
                "objectTypeRid": "ri.ot", "canRead": True, "canWrite": True,
            })
        if sync_dotted == "queries.check":
            return json.dumps({
                "ontologyApiName": "nw", "queryTypeApiName": "topCustomers",
                "queryTypeRid": "ri.qt", "canExecute": True,
            })
        if sync_dotted == "objects.check_batch":
            return json.dumps({"ontologyApiName": "nw", "results": []})
        if sync_dotted == "actions.check_batch":
            return json.dumps({"ontologyApiName": "nw", "results": []})
        return "{}"

    def test_wire_shape_for_each_sync_method(self):
        for dotted, http_method, expected_path, args, expected_body in _WIRE_CASES:
            with self.subTest(method=dotted):
                ns, method_name = dotted.split(".")
                status = 204 if dotted.endswith(".revoke") else 200
                routes = {
                    f"{http_method} {expected_path}": (status, self._stub_payload(dotted)),
                }
                with _StubServer(routes) as srv:
                    c = Client(srv.url, access_token="t")
                    fn = getattr(getattr(c, ns), method_name)
                    fn(*args)
                # Path the wrapper actually hit must match expected.
                req = srv.requests[0]
                self.assertEqual(req["method"], http_method,
                                 f"{dotted} method drift")
                self.assertEqual(req["path"], expected_path,
                                 f"{dotted} path drift")
                if expected_body is not None:
                    sent = json.loads(req["body"])
                    self.assertEqual(sent, expected_body,
                                     f"{dotted} body shape drift")


if __name__ == "__main__":
    unittest.main()
