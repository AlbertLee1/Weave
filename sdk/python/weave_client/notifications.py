"""NotificationsAPI — Python wrapper for the /api/v2/notifications
surface (PRD-V2 上层体验 row, round 72).

The Go server exposes four endpoints:

    GET    /api/v2/notifications                       (List)
    GET    /api/v2/notifications/unread-count          (round 66 badge)
    POST   /api/v2/notifications/read-all              (MarkAllRead)
    POST   /api/v2/notifications/{notificationId}/read (MarkRead)

This module wraps them so Python callers don't have to hand-build
URLs or remember the wire shape of the unread-count badge.

Quickstart::

    from weave_client import Client

    client = Client("http://localhost:9117", access_token="…")

    badge = client.notifications.unread_count()
    if badge > 0:
        for n in client.notifications.list(unread_only=True):
            print(n.title, n.body)
            client.notifications.mark_read(n.id)

    # Bulk variant — scope by tag if the UI shows separate tabs:
    cleared = client.notifications.mark_all_read(types=["mention"])
"""
from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from typing import TYPE_CHECKING, Any, List, Mapping, Optional, Sequence

from ._http import build_query_string, quote_path

if TYPE_CHECKING:
    from .client import Client


@dataclass
class Notification:
    """Wire shape of one notification row.

    Mirrors pkg/oms.Notification field-for-field. createdAt is parsed
    to datetime where the server emits a valid RFC3339 string;
    falls back to None on parse failure so SDK iteration never panics
    on a malformed payload from a future server version.
    """

    id: str
    user_id: str
    title: str
    body: str
    type: str
    read: bool
    created_at: Optional[datetime] = None
    link: str = ""


def _parse_notification(raw: Mapping[str, Any]) -> Notification:
    created_at: Optional[datetime] = None
    if ca := raw.get("createdAt"):
        try:
            created_at = datetime.fromisoformat(str(ca).replace("Z", "+00:00"))
        except ValueError:
            created_at = None
    return Notification(
        id=str(raw.get("id") or ""),
        user_id=str(raw.get("userId") or ""),
        title=str(raw.get("title") or ""),
        body=str(raw.get("body") or ""),
        type=str(raw.get("type") or ""),
        read=bool(raw.get("read")),
        link=str(raw.get("link") or ""),
        created_at=created_at,
    )


class NotificationsAPI:
    """Wrapper for the /api/v2/notifications surface."""

    def __init__(self, client: "Client") -> None:
        self._client = client

    def list(
        self,
        unread_only: bool = False,
        types: Optional[Sequence[str]] = None,
    ) -> List[Notification]:
        """GET /api/v2/notifications — the caller's notifications.

        Args:
            unread_only: when True, appends ?unread=true so the server
                filters at the SQL layer.
            types: optional list of type tags (mention / watch /
                approval / system / …). Repeated as multiple ?type=
                params so the server's r.URL.Query()["type"] returns
                them in order.
        """
        params: dict = {}
        if unread_only:
            params["unread"] = "true"
        path = "/api/v2/notifications" + build_query_string(params)
        # Multi-value type filter is appended manually because
        # build_query_string returns a single value per key.
        if types:
            sep = "&" if params else "?"
            extras = sep.join(
                "type=" + _url_quote_value(t) for t in types
            )
            path = path + ("&" if "?" in path else "?") + extras
        resp = self._client._request("GET", path)
        data = (resp or {}).get("data") or []
        return [_parse_notification(n) for n in data]

    def unread_count(self) -> int:
        """GET /api/v2/notifications/unread-count — lightweight
        badge poll added in round 66. Response carries `count` only;
        wrapper returns the int with a defensive 0 fallback so a
        malformed body never raises on the navbar polling path.
        """
        resp = self._client._request("GET", "/api/v2/notifications/unread-count")
        try:
            return int((resp or {}).get("count") or 0)
        except (TypeError, ValueError):
            return 0

    def mark_read(self, notification_id: str) -> None:
        """POST /api/v2/notifications/{id}/read — marks one row read.
        Idempotent on the server; wrapper returns None on 204."""
        path = "/api/v2/notifications/" + quote_path(notification_id) + "/read"
        self._client._request("POST", path)
        return None

    def mark_all_read(self, types: Optional[Sequence[str]] = None) -> int:
        """POST /api/v2/notifications/read-all — bulk variant scoped
        optionally by type tag. Returns the server-reported `updated`
        count so the SPA can show "12 cleared" toasts."""
        path = "/api/v2/notifications/read-all"
        if types:
            extras = "&".join("type=" + _url_quote_value(t) for t in types)
            path = path + "?" + extras
        resp = self._client._request("POST", path)
        try:
            return int((resp or {}).get("updated") or 0)
        except (TypeError, ValueError):
            return 0


def _url_quote_value(s: str) -> str:
    """Quote a query-string value. Uses urllib so multi-byte
    notification type tags ('提及' / 'mention') survive transport."""
    from urllib.parse import quote
    return quote(s, safe="")
