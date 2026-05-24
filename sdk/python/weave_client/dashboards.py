"""DashboardsAPI — Python wrapper for the /api/v2/dashboards
surface (PRD-V2 上层体验 row, round 76).

The Go server exposes six endpoints:

    GET    /api/v2/dashboards                        (List)
    POST   /api/v2/dashboards                        (Create)
    GET    /api/v2/dashboards/{id}                   (Get)
    PUT    /api/v2/dashboards/{id}                   (Update)
    DELETE /api/v2/dashboards/{id}                   (Delete)
    POST   /api/v2/dashboards/{id}/duplicate         (round 62)

This module wraps them so Python callers don't have to hand-build
URLs or remember the partial-update DTO semantics — Update fields
that are None mean "preserve the existing value" rather than
"clear it", matching the server's pointer-field convention.

Quickstart::

    from weave_client import Client

    client = Client("http://localhost:9117", access_token="…")

    d = client.dashboards.create(
        "Sales", definition={"widgets": [{"id": "w1", "type": "chart"}]},
    )

    # Toggle to public without touching name/definition:
    client.dashboards.update(d.id, is_public=True)

    # Clone it — server appends " (copy)" suffix (round 62):
    copy = client.dashboards.duplicate(d.id)
"""
from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime
from typing import TYPE_CHECKING, Any, Dict, List, Mapping, Optional

from ._http import quote_path

if TYPE_CHECKING:
    from .client import Client


@dataclass
class Dashboard:
    """Wire shape of a persisted dashboard row.

    Mirrors pkg/dashboards.Dashboard field-for-field. `definition`
    stays a plain dict (Mapping) because the schema is intentionally
    opaque — the server treats it as JSONB and lets the SPA evolve
    the on-the-wire shape without schema changes.
    """

    id: str
    name: str
    created_by: str
    is_public: bool
    definition: Dict[str, Any] = field(default_factory=dict)
    created_at: Optional[datetime] = None
    updated_at: Optional[datetime] = None


def _parse_dashboard(raw: Mapping[str, Any]) -> Dashboard:
    def _ts(v: Any) -> Optional[datetime]:
        if not v:
            return None
        try:
            return datetime.fromisoformat(str(v).replace("Z", "+00:00"))
        except ValueError:
            return None
    return Dashboard(
        id=str(raw.get("id") or ""),
        name=str(raw.get("name") or ""),
        created_by=str(raw.get("createdBy") or ""),
        is_public=bool(raw.get("isPublic")),
        definition=dict(raw.get("definition") or {}),
        created_at=_ts(raw.get("createdAt")),
        updated_at=_ts(raw.get("updatedAt")),
    )


class DashboardsAPI:
    """Wrapper for the /api/v2/dashboards CRUD + duplicate surface."""

    def __init__(self, client: "Client") -> None:
        self._client = client

    def list(self) -> List[Dashboard]:
        """GET /api/v2/dashboards — every dashboard the caller owns."""
        resp = self._client._request("GET", "/api/v2/dashboards")
        rows = (resp or {}).get("dashboards") or []
        return [_parse_dashboard(d) for d in rows]

    def create(
        self,
        name: str,
        definition: Optional[Dict[str, Any]] = None,
        is_public: bool = False,
    ) -> Dashboard:
        """POST /api/v2/dashboards — create a new dashboard."""
        body: Dict[str, Any] = {"name": name}
        if definition is not None:
            body["definition"] = definition
        if is_public:
            body["isPublic"] = True
        resp = self._client._request("POST", "/api/v2/dashboards", json_body=body)
        return _parse_dashboard(resp or {})

    def get(self, dashboard_id: str) -> Dashboard:
        """GET /api/v2/dashboards/{id} — owner OR public."""
        path = "/api/v2/dashboards/" + quote_path(dashboard_id)
        resp = self._client._request("GET", path)
        return _parse_dashboard(resp or {})

    def update(
        self,
        dashboard_id: str,
        name: Optional[str] = None,
        definition: Optional[Dict[str, Any]] = None,
        is_public: Optional[bool] = None,
    ) -> Dashboard:
        """PUT /api/v2/dashboards/{id} — partial update.

        Fields set to None preserve the existing value (matches the
        server's pointer-field DTO convention). Only fields the
        caller explicitly supplied are encoded in the request body.
        """
        body: Dict[str, Any] = {}
        if name is not None:
            body["name"] = name
        if definition is not None:
            body["definition"] = definition
        if is_public is not None:
            body["isPublic"] = is_public
        path = "/api/v2/dashboards/" + quote_path(dashboard_id)
        resp = self._client._request("PUT", path, json_body=body)
        return _parse_dashboard(resp or {})

    def delete(self, dashboard_id: str) -> None:
        """DELETE /api/v2/dashboards/{id} — owner only."""
        path = "/api/v2/dashboards/" + quote_path(dashboard_id)
        self._client._request("DELETE", path)
        return None

    def duplicate(self, dashboard_id: str) -> Dashboard:
        """POST /api/v2/dashboards/{id}/duplicate — round-62 clone.

        Server auto-suffixes the name with " (copy)" / " (copy 2)" /
        ... so repeat clicks don't 409. The duplicate is always
        owned by the caller with IsPublic reset to false (Foundry
        privacy-on-clone semantic).
        """
        path = "/api/v2/dashboards/" + quote_path(dashboard_id) + "/duplicate"
        resp = self._client._request("POST", path)
        return _parse_dashboard(resp or {})
