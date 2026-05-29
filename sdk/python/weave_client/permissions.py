"""PermissionsAPI — Foundry-parity fine-grained permission probes
(round 98, mirrors round-97 backend POST /api/v2/me/permissions/check).

The SPA uses this for batch-gating dynamic UI ("for each row, can
the user run THIS action?") without N round-trips to /api/v2/me +
client-side filtering. Foundry-parity sibling of the role-based
gating exposed via Client.ontologies.get_me() (per-ontology) and
the global /api/v2/me endpoint.
"""
from __future__ import annotations

from typing import TYPE_CHECKING, Any, Dict, List, Optional

from .types import PermissionsCheckResponse

if TYPE_CHECKING:
    from .client import Client


def _validate(model_cls, payload):
    if hasattr(model_cls, "model_validate"):
        return model_cls.model_validate(payload)
    return model_cls(**payload)


class PermissionsAPI:
    """Read access to ``/api/v2/me/permissions/...``."""

    def __init__(self, client: "Client"):
        self._client = client

    def check(
        self,
        permissions: List[str],
        ontology: Optional[str] = None,
    ) -> PermissionsCheckResponse:
        """Probe a batch of permissions; return granted/denied partition.

        ``permissions`` must be a non-empty list. When ``ontology`` is
        provided, the check narrows to (global roles ∪ that ontology's
        scoped role) instead of summing every per-ontology role. When
        ``ontology`` is None the field is omitted from the wire body
        so the server's global-check branch fires (vs an empty string
        which would resolve to "ontology not found" against a real
        resolver).
        """
        body: Dict[str, Any] = {"permissions": permissions}
        if ontology is not None:
            body["ontology"] = ontology
        resp = self._client._request(
            "POST", "/api/v2/me/permissions/check", json_body=body)
        return _validate(PermissionsCheckResponse, resp or {})
