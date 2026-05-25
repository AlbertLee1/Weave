"""QueriesAPI — third axis of the per-resource check family
(round 114, mirror of round-113 backend).

Closes the trio: c.objects.check (r106), c.actions.check (r104),
c.queries.check (this module). SPA gates per-query-type
"Run Query" affordances by GETting check() instead of
round-tripping a real execute call.
"""
from __future__ import annotations

from typing import TYPE_CHECKING

from ._http import quote_path
from .types import QueryCheckResponse

if TYPE_CHECKING:
    from .client import Client


def _validate_query_check(payload):
    if hasattr(QueryCheckResponse, "model_validate"):
        return QueryCheckResponse.model_validate(payload or {})
    return QueryCheckResponse(**(payload or {}))


class QueriesAPI:
    """Wraps ``/api/v2/ontologies/{ontology}/queryTypes/.../check``."""

    def __init__(self, client: "Client"):
        self._client = client

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
