"""PermissionRequestsAPI — Python wrapper for the /api/v2/
permission-requests surface (PRD-V2 上层体验, round 80).

The Go server exposes six endpoints:

    POST   /api/v2/permission-requests              (Create)
    GET    /api/v2/permission-requests              (List, paginated)
    GET    /api/v2/permission-requests/{id}         (Get)
    POST   /api/v2/permission-requests/{id}/approve (Approve)
    POST   /api/v2/permission-requests/{id}/reject  (Reject)
    DELETE /api/v2/permission-requests/{id}         (Cancel — round 63)

Quickstart::

    from weave_client import Client, STATUS_PENDING, STATUS_APPROVED

    client = Client("http://localhost:9117", access_token="…")

    pr = client.permissionrequests.create(
        target_rid="ri.objects.main.Customer.42",
        reason="rendering this customer in tomorrow's report",
    )

    # Approver flow:
    pending = client.permissionrequests.list(status=STATUS_PENDING)
    for r in pending.requests:
        client.permissionrequests.approve(r.id, note="approved by oncall")

    # Requester flow — withdraw a pending ask:
    client.permissionrequests.cancel(pr.id)
"""
from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime
from typing import TYPE_CHECKING, Any, List, Mapping, Optional

from ._http import build_query_string, quote_path

if TYPE_CHECKING:
    from .client import Client


# Wire-format status constants. Match pkg/permissionrequests/model.go
# constants byte-for-byte — round 80 BDD pins this alignment.
STATUS_PENDING = "PENDING"
STATUS_APPROVED = "APPROVED"
STATUS_REJECTED = "REJECTED"
STATUS_CANCELLED = "CANCELLED"


@dataclass
class PermissionRequest:
    """Wire shape of one permission-request row. Mirrors
    pkg/permissionrequests.Request field-for-field."""

    id: str
    target_rid: str
    requested_by: str
    reason: str
    status: str
    decided_by: str = ""
    decision_note: str = ""
    created_at: Optional[datetime] = None
    updated_at: Optional[datetime] = None
    decided_at: Optional[datetime] = None


@dataclass
class PermissionRequestList:
    """Paginated list response wire shape."""

    requests: List[PermissionRequest] = field(default_factory=list)
    total: int = 0
    limit: int = 0
    offset: int = 0


def _parse_dt(v: Any) -> Optional[datetime]:
    if not v:
        return None
    try:
        return datetime.fromisoformat(str(v).replace("Z", "+00:00"))
    except ValueError:
        return None


def _parse_request(raw: Mapping[str, Any]) -> PermissionRequest:
    return PermissionRequest(
        id=str(raw.get("id") or ""),
        target_rid=str(raw.get("targetRid") or ""),
        requested_by=str(raw.get("requestedBy") or ""),
        reason=str(raw.get("reason") or ""),
        status=str(raw.get("status") or ""),
        decided_by=str(raw.get("decidedBy") or ""),
        decision_note=str(raw.get("decisionNote") or ""),
        created_at=_parse_dt(raw.get("createdAt")),
        updated_at=_parse_dt(raw.get("updatedAt")),
        decided_at=_parse_dt(raw.get("decidedAt")),
    )


class PermissionRequestsAPI:
    """Wrapper for the /api/v2/permission-requests surface."""

    def __init__(self, client: "Client") -> None:
        self._client = client

    def create(self, target_rid: str, reason: str = "") -> PermissionRequest:
        """POST /api/v2/permission-requests — file a new request.

        `reason` is optional (server accepts empty string per the
        4-KiB-max length CHECK; user doesn't have to justify).
        """
        body = {"targetRid": target_rid, "reason": reason}
        resp = self._client._request("POST", "/api/v2/permission-requests", json_body=body)
        return _parse_request(resp or {})

    def list(
        self,
        status: Optional[str] = None,
        requested_by: Optional[str] = None,
        target_rid: Optional[str] = None,
        limit: Optional[int] = None,
        offset: Optional[int] = None,
    ) -> PermissionRequestList:
        """GET /api/v2/permission-requests — paginated list.

        Filters compose: status / requestedBy / targetRid each
        narrow the result set. limit + offset drive pagination;
        server clamps to [1, MaxPageLimit=200] and defaults to 50.
        """
        params: dict = {}
        if status is not None:
            params["status"] = status
        if requested_by is not None:
            params["requestedBy"] = requested_by
        if target_rid is not None:
            params["targetRid"] = target_rid
        if limit is not None:
            params["limit"] = str(limit)
        if offset is not None:
            params["offset"] = str(offset)
        path = "/api/v2/permission-requests" + build_query_string(params)
        resp = self._client._request("GET", path)
        envelope = resp or {}
        rows = envelope.get("requests") or []
        return PermissionRequestList(
            requests=[_parse_request(r) for r in rows],
            total=int(envelope.get("total") or 0),
            limit=int(envelope.get("limit") or 0),
            offset=int(envelope.get("offset") or 0),
        )

    def get(self, request_id: str) -> PermissionRequest:
        """GET /api/v2/permission-requests/{id}."""
        path = "/api/v2/permission-requests/" + quote_path(request_id)
        resp = self._client._request("GET", path)
        return _parse_request(resp or {})

    def approve(self, request_id: str, note: str = "") -> PermissionRequest:
        """POST /api/v2/permission-requests/{id}/approve — approver
        path. Note is optional; when omitted no body is sent so the
        server's readOptionalJSON accepts that path.
        """
        return self._decide(request_id, "approve", note)

    def reject(self, request_id: str, note: str = "") -> PermissionRequest:
        """POST /api/v2/permission-requests/{id}/reject — approver
        path. Same optional-note semantics as approve."""
        return self._decide(request_id, "reject", note)

    def cancel(self, request_id: str) -> None:
        """DELETE /api/v2/permission-requests/{id} — requester
        cancels their own pending request (round 63). Returns 204
        on success; 403 when caller is not the original requester;
        409 when row is already in a terminal state.
        """
        path = "/api/v2/permission-requests/" + quote_path(request_id)
        self._client._request("DELETE", path)
        return None

    def _decide(self, request_id: str, verb: str, note: str) -> PermissionRequest:
        path = "/api/v2/permission-requests/" + quote_path(request_id) + "/" + verb
        body = {"note": note} if note else None
        resp = self._client._request("POST", path, json_body=body)
        return _parse_request(resp or {})
