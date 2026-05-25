"""QueriesAPI — third axis of the per-resource check family
(round 114, mirror of round-113 backend).

Closes the trio: c.objects.check (r106), c.actions.check (r104),
c.queries.check (this module). SPA gates per-query-type
"Run Query" affordances by GETting check() instead of
round-tripping a real execute call.
"""
from __future__ import annotations

from typing import TYPE_CHECKING

from typing import List

from ._http import quote_path
from .types import QueryCheckBatchResponse, QueryCheckResponse

if TYPE_CHECKING:
    from .client import Client


def _validate_query_check(payload):
    if hasattr(QueryCheckResponse, "model_validate"):
        return QueryCheckResponse.model_validate(payload or {})
    return QueryCheckResponse(**(payload or {}))


def _validate_query_check_batch(payload):
    if hasattr(QueryCheckBatchResponse, "model_validate"):
        return QueryCheckBatchResponse.model_validate(payload or {})
    return QueryCheckBatchResponse(**(payload or {}))


class QueriesAPI:
    """Wraps ``/api/v2/ontologies/{ontology}/queryTypes/.../check``."""

    def __init__(self, client: "Client"):
        self._client = client

    def check_batch(
        self,
        ontology: str,
        query_types: List[str],
    ) -> QueryCheckBatchResponse:
        """Bulk-probe N queries on one ontology (round 116).

        Mirrors round-115 backend POST /api/v2/me/checks/queryTypes.
        Completes the SDK three-axis bulk parity — with
        c.objects.check_batch + c.actions.check_batch a freshly-
        loaded SPA page resolves its full per-resource gating in
        THREE round-trips.

        Each result entry carries .found discriminator. found=False
        entries always have can_execute=False regardless of caller
        perms. Empty query_types raises WeaveError (server 400).
        """
        body = {
            "ontologyApiName": ontology,
            "queryTypeApiNames": query_types,
        }
        resp = self._client._request(
            "POST", "/api/v2/me/checks/queryTypes", json_body=body)
        return _validate_query_check_batch(resp)

    def check(self, ontology: str, query_type: str) -> QueryCheckResponse:
        """Probe whether the caller can execute a named query (round 114).

        Mirrors round-113 backend GET /api/v2/ontologies/{ontology}/
        queryTypes/{qt}/check. Returns typed response with
        can_execute boolean; 200 even when caller lacks perm (probe
        is informational, never 403). 404 QueryTypeNotFound when
        query doesn't exist — distinguishes "no" from "missing".
        """
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/queryTypes/{quote_path(query_type)}/check"
        )
        resp = self._client._request("GET", path)
        return _validate_query_check(resp)
