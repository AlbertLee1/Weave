"""ActionsAPI - apply actions and inspect their resulting edits."""
from __future__ import annotations

from typing import TYPE_CHECKING, Any, Dict

from ._http import quote_path
from .types import ApplyActionResponse

if TYPE_CHECKING:
    from .client import Client


def _validate(payload: Any) -> ApplyActionResponse:
    if hasattr(ApplyActionResponse, "model_validate"):
        return ApplyActionResponse.model_validate(payload or {})
    return ApplyActionResponse(**(payload or {}))


class ActionsAPI:
    """Wraps ``/api/v2/ontologies/{ontology}/actions/...``."""

    def __init__(self, client: "Client"):
        self._client = client

    def apply(
        self,
        ontology: str,
        action_type: str,
        parameters: Dict[str, Any],
    ) -> ApplyActionResponse:
        """Apply a single action and return the structured response."""
        body = {
            "actionType": action_type,
            "parameters": parameters or {},
        }
        path = f"/api/v2/ontologies/{quote_path(ontology)}/actions/apply"
        resp = self._client._request("POST", path, json_body=body)
        return _validate(resp)
