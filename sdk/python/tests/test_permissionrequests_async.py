"""BDD acceptance tests for the round-82 AsyncPermissionRequestsAPI mirror."""
from __future__ import annotations

import json
import unittest

from weave_client import (
    PermissionRequest,
    PermissionRequestList,
    STATUS_APPROVED,
    STATUS_PENDING,
    STATUS_REJECTED,
    WeaveAsyncClient,
)

from tests.test_client import _StubServer


def _route(payload, status=200):
    return (status, json.dumps(payload))


def _pr_payload(**overrides):
    base = {
        "id": "pr-1",
        "targetRid": "ri.objects.main.Customer.42",
        "requestedBy": "u-requester",
        "reason": "need it",
        "status": "PENDING",
        "decidedBy": "",
        "decisionNote": "",
        "createdAt": "2026-05-25T00:00:00Z",
        "updatedAt": "2026-05-25T00:00:00Z",
    }
    base.update(overrides)
    return base


class AsyncPermissionRequestsAPITests(unittest.IsolatedAsyncioTestCase):

    async def test_create_returns_typed_request(self):
        routes = {"POST /api/v2/permission-requests":
                  (201, json.dumps(_pr_payload(reason="please")))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                pr = await c.permissionrequests.create(
                    target_rid="ri.objects.main.Customer.42",
                    reason="please",
                )
        self.assertIsInstance(pr, PermissionRequest)
        self.assertEqual(pr.status, STATUS_PENDING)
        body = json.loads(srv.requests[0]["body"])
        self.assertEqual(body["targetRid"], "ri.objects.main.Customer.42")
        self.assertEqual(body["reason"], "please")

    async def test_list_returns_typed_envelope(self):
        payload = {
            "requests": [_pr_payload(id="pr-1"), _pr_payload(id="pr-2", status="APPROVED")],
            "total": 5, "limit": 50, "offset": 0,
        }
        routes = {"GET /api/v2/permission-requests": _route(payload)}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                result = await c.permissionrequests.list()
        self.assertIsInstance(result, PermissionRequestList)
        self.assertEqual(len(result.requests), 2)
        self.assertEqual(result.total, 5)

    async def test_list_with_filters_appends_query(self):
        routes = {"GET /api/v2/permission-requests": _route(
            {"requests": [], "total": 0, "limit": 25, "offset": 50})}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                await c.permissionrequests.list(
                    status=STATUS_PENDING,
                    requested_by="u-alice",
                    limit=25,
                    offset=50,
                )
        path = srv.requests[0]["path"]
        self.assertIn("status=PENDING", path)
        self.assertIn("requestedBy=u-alice", path)
        self.assertIn("limit=25", path)
        self.assertIn("offset=50", path)

    async def test_get_returns_typed_request(self):
        routes = {"GET /api/v2/permission-requests/pr-42":
                  _route(_pr_payload(id="pr-42"))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                pr = await c.permissionrequests.get("pr-42")
        self.assertEqual(pr.id, "pr-42")

    async def test_approve_with_note(self):
        routes = {"POST /api/v2/permission-requests/pr-1/approve":
                  _route(_pr_payload(status="APPROVED", decidedBy="u-admin", decisionNote="ok"))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                pr = await c.permissionrequests.approve("pr-1", note="ok")
        self.assertEqual(pr.status, STATUS_APPROVED)
        body = json.loads(srv.requests[0]["body"])
        self.assertEqual(body["note"], "ok")

    async def test_approve_without_note_omits_body(self):
        routes = {"POST /api/v2/permission-requests/pr-1/approve":
                  _route(_pr_payload(status="APPROVED", decidedBy="u-admin"))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                await c.permissionrequests.approve("pr-1")
        body = srv.requests[0]["body"]
        if body:
            parsed = json.loads(body)
            self.assertNotIn("note", parsed)

    async def test_reject_with_note(self):
        routes = {"POST /api/v2/permission-requests/pr-1/reject":
                  _route(_pr_payload(status="REJECTED", decidedBy="u-admin", decisionNote="nope"))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                pr = await c.permissionrequests.reject("pr-1", note="nope")
        self.assertEqual(pr.status, STATUS_REJECTED)
        body = json.loads(srv.requests[0]["body"])
        self.assertEqual(body["note"], "nope")

    async def test_cancel_returns_none_on_204(self):
        routes = {"DELETE /api/v2/permission-requests/pr-1": (204, "")}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                result = await c.permissionrequests.cancel("pr-1")
        self.assertIsNone(result)

    async def test_lifecycle_create_list_approve(self):
        # End-to-end async lifecycle on a single client connection:
        # create, then list pending, then approve. Asserts method
        # ordering — same pattern as round-74 notifications BDD.
        routes = {
            "POST /api/v2/permission-requests": (201, json.dumps(_pr_payload(id="pr-life"))),
            "GET /api/v2/permission-requests": _route({
                "requests": [_pr_payload(id="pr-life")],
                "total": 1, "limit": 50, "offset": 0,
            }),
            "POST /api/v2/permission-requests/pr-life/approve": _route(
                _pr_payload(id="pr-life", status="APPROVED", decidedBy="u-admin"),
            ),
        }
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                pr = await c.permissionrequests.create(
                    target_rid="ri.objects.main.Customer.42",
                )
                self.assertEqual(pr.id, "pr-life")
                pending = await c.permissionrequests.list(status=STATUS_PENDING)
                self.assertEqual(len(pending.requests), 1)
                decided = await c.permissionrequests.approve(pending.requests[0].id, note="ok")
                self.assertEqual(decided.status, STATUS_APPROVED)
        methods = [(r["method"], r["path"].split("?")[0]) for r in srv.requests]
        self.assertEqual(methods, [
            ("POST", "/api/v2/permission-requests"),
            ("GET", "/api/v2/permission-requests"),
            ("POST", "/api/v2/permission-requests/pr-life/approve"),
        ])


if __name__ == "__main__":
    unittest.main()
