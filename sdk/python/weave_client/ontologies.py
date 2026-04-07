"""OntologiesAPI - read access to ontology metadata."""
from __future__ import annotations

from typing import TYPE_CHECKING, List

from ._http import quote_path
from .types import ObjectType, Ontology

if TYPE_CHECKING:
    from .client import Client


def _validate(model_cls, payload):
    if hasattr(model_cls, "model_validate"):
        return model_cls.model_validate(payload)
    return model_cls(**payload)


class OntologiesAPI:
    """Read access to ``/api/v2/ontologies/...``."""

    def __init__(self, client: "Client"):
        self._client = client

    def list(self) -> List[Ontology]:
        """Return every ontology the caller can see."""
        body = self._client._request("GET", "/api/v2/ontologies") or {}
        items = body.get("data", []) if isinstance(body, dict) else []
        return [_validate(Ontology, item) for item in items]

    def get(self, api_name: str) -> Ontology:
        """Fetch a single ontology by API name (or RID)."""
        body = self._client._request("GET", f"/api/v2/ontologies/{quote_path(api_name)}")
        return _validate(Ontology, body)

    def list_object_types(self, ontology: str) -> List[ObjectType]:
        """List object types in an ontology."""
        body = self._client._request(
            "GET", f"/api/v2/ontologies/{quote_path(ontology)}/objectTypes"
        ) or {}
        items = body.get("data", []) if isinstance(body, dict) else []
        return [_validate(ObjectType, item) for item in items]

    def get_object_type(self, ontology: str, object_type: str) -> ObjectType:
        """Fetch a single object type wire payload."""
        body = self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/objectTypes/{quote_path(object_type)}",
        )
        return _validate(ObjectType, body)
