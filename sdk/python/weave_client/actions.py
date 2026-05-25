"""ActionsAPI - apply actions and inspect their resulting edits."""
from __future__ import annotations

from typing import TYPE_CHECKING, Any, Dict, List, Optional

from ._http import quote_path
from .types import (
    ActionCheckBatchResponse,
    ActionCheckResponse,
    ApplyActionResponse,
    BatchApplyActionResponse,
)


def _validate_check(payload: Any) -> ActionCheckResponse:
    if hasattr(ActionCheckResponse, "model_validate"):
        return ActionCheckResponse.model_validate(payload or {})
    return ActionCheckResponse(**(payload or {}))


def _validate_check_batch(payload: Any) -> ActionCheckBatchResponse:
    if hasattr(ActionCheckBatchResponse, "model_validate"):
        return ActionCheckBatchResponse.model_validate(payload or {})
    return ActionCheckBatchResponse(**(payload or {}))

if TYPE_CHECKING:
    from .client import Client


def _validate(payload: Any) -> ApplyActionResponse:
    if hasattr(ApplyActionResponse, "model_validate"):
        return ApplyActionResponse.model_validate(payload or {})
    return ApplyActionResponse(**(payload or {}))


def _validate_batch(payload: Any) -> BatchApplyActionResponse:
    if hasattr(BatchApplyActionResponse, "model_validate"):
        return BatchApplyActionResponse.model_validate(payload or {})
    return BatchApplyActionResponse(**(payload or {}))


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
        """Apply a single action and return the structured response.

        Foundry OSv2 shape: the action API name is carried in the URL, so
        the request body only contains ``parameters``. See
        ``palantir/foundry-platform-python`` ``action.py:58``.
        """
        body = {
            "parameters": parameters or {},
        }
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/actions/{quote_path(action_type)}/apply"
        )
        resp = self._client._request("POST", path, json_body=body)
        return _validate(resp)

    def apply_with_options(
        self,
        ontology: str,
        action_type: str,
        parameters: Dict[str, Any],
        *,
        mode: str = "VALIDATE_AND_EXECUTE",
        return_edits: str = "ALL",
    ) -> ApplyActionResponse:
        """Apply with Foundry OSv2 options (mode, returnEdits)."""
        body: Dict[str, Any] = {
            "parameters": parameters or {},
            "options": {"mode": mode, "returnEdits": return_edits},
        }
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/actions/{quote_path(action_type)}/apply"
        )
        resp = self._client._request("POST", path, json_body=body)
        return _validate(resp)

    def apply_batch(
        self,
        ontology: str,
        action_type: str,
        requests_list: List[Dict[str, Any]],
        *,
        return_edits: str = "ALL",
    ) -> BatchApplyActionResponse:
        """Apply a batch of actions."""
        body: Dict[str, Any] = {"requests": requests_list}
        if return_edits != "ALL":
            body["options"] = {"returnEdits": return_edits}
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/actions/{quote_path(action_type)}/applyBatch"
        )
        resp = self._client._request("POST", path, json_body=body)
        return _validate_batch(resp)

    def execute_query(
        self,
        ontology: str,
        query_api_name: str,
        parameters: Optional[Dict[str, Any]] = None,
    ) -> Dict[str, Any]:
        """Execute a named query."""
        body = {"parameters": parameters or {}}
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/queries/{quote_path(query_api_name)}/execute"
        )
        resp = self._client._request("POST", path, json_body=body)
        return resp or {}

    def check_batch(
        self,
        ontology: str,
        action_types: List[str],
    ) -> ActionCheckBatchResponse:
        """Bulk-probe N actions on one ontology (round 110).

        Mirrors round-109 backend POST /api/v2/me/checks/actionTypes.
        Sibling of round-108 c.objects.check_batch — together the
        two batch endpoints let the SPA resolve OT read/write matrix
        AND applicable-actions list in TWO round-trips on page load.

        Each result entry carries .found discriminator so callers
        distinguish "action removed from config" from "exists but no
        perm" — found=False entries always have can_apply=False
        regardless of caller perms. Empty action_types raises
        WeaveError (server-side 400) — wrapper stays thin.
        """
        body = {
            "ontologyApiName": ontology,
            "actionTypeApiNames": action_types,
        }
        resp = self._client._request(
            "POST", "/api/v2/me/checks/actionTypes", json_body=body)
        return _validate_check_batch(resp)

    def check(self, ontology: str, action_type: str) -> ActionCheckResponse:
        """Probe whether the caller can apply a named action (round 104).

        Mirrors round-103 backend GET /api/v2/ontologies/{ontology}/
        actions/{action}/check. Returns a typed ActionCheckResponse;
        always 200 with canApply boolean (never 403 — the probe is
        informational for SPA UI gating). 404 ActionTypeNotFound when
        the action does not exist (distinguishes "no" from "missing").
        """
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/actions/{quote_path(action_type)}/check"
        )
        resp = self._client._request("GET", path)
        return _validate_check(resp)
