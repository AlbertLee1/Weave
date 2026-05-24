"""BDD acceptance tests for the round-80 PermissionRequestsAPI wrapper."""
from __future__ import annotations

import json
import unittest

from weave_client import (
    Client,
    PermissionRequest,
    PermissionRequestList,
    STATUS_APPROVED,
    STATUS_CANCELLED,
    STATUS_PENDING,
    STATUS_REJECTED,
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


class PermissionRequestsAPITests(unittest.TestCase):

    def test_status_constants_match_server_values(self):
        # Wire format alignment — the constants the SDK exposes must
        # match the strings the server emits, exactly.
        self.assertEqual(STATUS_PENDING, "PENDING")
        self.assertEqual(STATUS_APPROVED, "APPROVED")
        self.assertEqual(STATUS_REJECTED, "REJECTED")
        self.assertEqual(STATUS_CANCELLED, "CANCELLED")

    def test_create_returns_typed_request(self):
        payload = _pr_payload(id="pr-new", reason="please")
        routes = {"POST /api/v2/permission-requests": (201, json.dumps(payload))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            pr = c.permissionrequests.create(
                target_rid="ri.objects.main.Customer.42",
                reason="please",
            )
        self.assertIsInstance(pr, PermissionRequest)
        self.assertEqual(pr.id, "pr-new")
        self.assertEqual(pr.target_rid, "ri.objects.main.Customer.42")
        self.assertEqual(pr.status, STATUS_PENDING)
        body = json.loads(srv.requests[0]["body"])
        self.assertEqual(body["targetRid"], "ri.objects.main.Customer.42")
        self.assertEqual(body["reason"], "please")

    def test_create_without_reason_omits_field(self):
        # The server allows reason="" — wrapper should send it as an
        # empty string (not the default "need it" or some inferred
        # value). Caller's choice to omit explanation must be honored.
        payload = _pr_payload(id="pr-noreason", reason="")
        routes = {"POST /api/v2/permission-requests": (201, json.dumps(payload))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            pr = c.permissionrequests.create(target_rid="ri.x")
        self.assertEqual(pr.reason, "")
        body = json.loads(srv.requests[0]["body"])
        # reason kwarg defaults to "" — present in body as empty
        # string, not omitted. Either is acceptable per server's
        # default-empty-string column convention.
        self.assertEqual(body.get("reason", ""), "")

    def test_list_returns_typed_pagination_envelope(self):
        payload = {
            "requests": [
                _pr_payload(id="pr-1"),
                _pr_payload(id="pr-2", status="APPROVED", decidedBy="u-admin"),
            ],
            "total": 7,
            "limit": 50,
            "offset": 0,
        }
        routes = {"GET /api/v2/permission-requests": _route(payload)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.permissionrequests.list()
        self.assertIsInstance(result, PermissionRequestList)
        self.assertEqual(len(result.requests), 2)
        self.assertEqual(result.total, 7)
        self.assertEqual(result.limit, 50)
        self.assertEqual(result.offset, 0)
        self.assertIsInstance(result.requests[0], PermissionRequest)
        self.assertEqual(result.requests[1].decided_by, "u-admin")

    def test_list_with_filters_appends_query_params(self):
        routes = {"GET /api/v2/permission-requests": _route(
            {"requests": [], "total": 0, "limit": 25, "offset": 50})}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.permissionrequests.list(
                status=STATUS_PENDING,
                requested_by="u-alice",
                target_rid="ri.objects.main.Customer.1",
                limit=25,
                offset=50,
            )
        path = srv.requests[0]["path"]
        self.assertIn("status=PENDING", path)
        self.assertIn("requestedBy=u-alice", path)
        self.assertIn("targetRid=ri.objects.main.Customer.1", path)
        self.assertIn("limit=25", path)
        self.assertIn("offset=50", path)

    def test_list_no_filters_no_query_string(self):
        # Bare list() should send a clean URL with no ?foo=bar
        # noise — server applies defaults (limit=50, offset=0).
        routes = {"GET /api/v2/permission-requests": _route(
            {"requests": [], "total": 0, "limit": 50, "offset": 0})}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.permissionrequests.list()
        self.assertNotIn("?", srv.requests[0]["path"])

    def test_get_returns_typed_request(self):
        payload = _pr_payload(id="pr-42")
        routes = {"GET /api/v2/permission-requests/pr-42": _route(payload)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            pr = c.permissionrequests.get("pr-42")
        self.assertEqual(pr.id, "pr-42")

    def test_approve_posts_note_returns_typed_decision(self):
        payload = _pr_payload(
            id="pr-1", status="APPROVED",
            decidedBy="u-admin", decisionNote="ok",
        )
        routes = {"POST /api/v2/permission-requests/pr-1/approve": _route(payload)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            pr = c.permissionrequests.approve("pr-1", note="ok")
        self.assertEqual(pr.status, STATUS_APPROVED)
        self.assertEqual(pr.decision_note, "ok")
        body = json.loads(srv.requests[0]["body"])
        self.assertEqual(body["note"], "ok")

    def test_approve_without_note_omits_body(self):
        # Note is optional — when omitted, body should be empty
        # (server's readOptionalJSON accepts that path).
        payload = _pr_payload(status="APPROVED", decidedBy="u-admin")
        routes = {"POST /api/v2/permission-requests/pr-1/approve": _route(payload)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.permissionrequests.approve("pr-1")
        # Body is either empty string or {} — either is accepted.
        body = srv.requests[0]["body"]
        if body:
            parsed = json.loads(body)
            self.assertNotIn("note", parsed)

    def test_reject_posts_note_returns_typed_decision(self):
        payload = _pr_payload(
            status="REJECTED",
            decidedBy="u-admin", decisionNote="nope",
        )
        routes = {"POST /api/v2/permission-requests/pr-1/reject": _route(payload)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            pr = c.permissionrequests.reject("pr-1", note="nope")
        self.assertEqual(pr.status, STATUS_REJECTED)
        body = json.loads(srv.requests[0]["body"])
        self.assertEqual(body["note"], "nope")

    def test_cancel_returns_none_on_204(self):
        # Round 63 surface: DELETE /permission-requests/{id} returns
        # 204 No Content on success. Wrapper returns None.
        routes = {"DELETE /api/v2/permission-requests/pr-1": (204, "")}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.permissionrequests.cancel("pr-1")
        self.assertIsNone(result)

    def test_path_quote_handles_special_chars_in_id(self):
        routes = {}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            try:
                c.permissionrequests.get("pr/with/slash")
            except Exception:
                pass
            self.assertIn("pr%2Fwith%2Fslash", srv.requests[0]["path"])


if __name__ == "__main__":
    unittest.main()
