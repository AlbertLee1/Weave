"""SessionsAPI — caller's active session inventory + revocation
(round 102, mirror of US-254 + round-101 backend).

The Foundry-parity sessions surface lets the SPA show the user's
"active devices" list, revoke a specific one, or "log out other
devices" in bulk. Sibling of c.permissionrequests / c.permissions
in the post-symmetry-exhaustion Foundry-parity expansion (rounds
91+).
"""
from __future__ import annotations

from typing import TYPE_CHECKING, List

from ._http import quote_path
from .types import RevokeOthersResponse, Session

if TYPE_CHECKING:
    from .client import Client


def _validate(model_cls, payload):
    if hasattr(model_cls, "model_validate"):
        return model_cls.model_validate(payload)
    return model_cls(**payload)


class SessionsAPI:
    """Read/revoke access to ``/api/auth/sessions/...``."""

    def __init__(self, client: "Client"):
        self._client = client

    def list(self) -> List[Session]:
        """List the caller's active sessions (sorted last_seen desc).

        Returns an empty list when the caller has no active sessions.
        The ``current`` flag marks the row bound to the caller's
        current JWT (empty for API-key auth — there's no anchor).
        """
        body = self._client._request("GET", "/api/auth/sessions") or {}
        items = body.get("sessions", []) if isinstance(body, dict) else []
        return [_validate(Session, item) for item in items]

    def revoke(self, session_id: str) -> None:
        """Revoke a single session the caller owns. 204 on success."""
        self._client._request(
            "DELETE", f"/api/auth/sessions/{quote_path(session_id)}")
        return None

    def revoke_others(self) -> RevokeOthersResponse:
        """Bulk-revoke every session except the current one (round 101).

        Returns the revoked count + the preserved session ID.
        API-key callers (no session anchor) trigger ALL-sessions
        revocation and currentSessionId is empty in the response.
        """
        resp = self._client._request(
            "POST", "/api/auth/sessions/revoke-others")
        return _validate(RevokeOthersResponse, resp or {})
