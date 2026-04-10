"""OntologiesAPI - read access to ontology metadata."""
from __future__ import annotations

from typing import TYPE_CHECKING, Any, Dict, List

from ._http import quote_path
from .types import (
    ActionType,
    InterfaceType,
    ObjectType,
    Ontology,
    QueryType,
    ValueType,
)

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

    # ---- ontology CRUD ------------------------------------------------------

    def list(self) -> List[Ontology]:
        """Return every ontology the caller can see."""
        body = self._client._request("GET", "/api/v2/ontologies") or {}
        items = body.get("data", []) if isinstance(body, dict) else []
        return [_validate(Ontology, item) for item in items]

    def get(self, api_name: str) -> Ontology:
        """Fetch a single ontology by API name (or RID)."""
        body = self._client._request("GET", f"/api/v2/ontologies/{quote_path(api_name)}")
        return _validate(Ontology, body)

    def load_metadata(self, ontology: str, subsets: Dict[str, bool]) -> Dict[str, Any]:
        """POST selective ontology metadata load."""
        body = self._client._request(
            "POST",
            f"/api/v2/ontologies/{quote_path(ontology)}/metadata",
            json_body=subsets,
        )
        return body or {}

    def get_full_metadata(self, ontology: str) -> Dict[str, Any]:
        """GET ontology full metadata (requires ?preview=true)."""
        body = self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/fullMetadata?preview=true",
        )
        return body or {}

    # ---- object types -------------------------------------------------------

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

    def get_object_type_full_metadata(self, ontology: str, object_type: str) -> Dict[str, Any]:
        """GET full metadata for a single object type (preview)."""
        body = self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/objectTypes/{quote_path(object_type)}/fullMetadata?preview=true",
        )
        return body or {}

    def get_object_types_by_rid_batch(self, ontology: str, rids: List[str]) -> List[Dict[str, Any]]:
        """POST batch-fetch object types by their RIDs."""
        body = self._client._request(
            "POST",
            f"/api/v2/ontologies/{quote_path(ontology)}/objectTypes/getByRidBatch",
            json_body={"rids": rids},
        )
        return (body or {}).get("data", [])

    # ---- action types -------------------------------------------------------

    def list_action_types(self, ontology: str) -> List[ActionType]:
        """List all action types in an ontology."""
        body = self._client._request(
            "GET", f"/api/v2/ontologies/{quote_path(ontology)}/actionTypes"
        ) or {}
        items = body.get("data", []) if isinstance(body, dict) else []
        return [_validate(ActionType, item) for item in items]

    def get_action_type(self, ontology: str, action_type: str) -> ActionType:
        """Fetch a single action type by API name."""
        body = self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/actionTypes/{quote_path(action_type)}",
        )
        return _validate(ActionType, body)

    def get_action_type_by_rid(self, ontology: str, rid: str) -> ActionType:
        """Fetch a single action type by its RID."""
        body = self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/actionTypes/byRid/{quote_path(rid)}",
        )
        return _validate(ActionType, body)

    def get_action_types_by_rid_batch(self, ontology: str, rids: List[str]) -> List[Dict[str, Any]]:
        """POST batch-fetch action types by their RIDs."""
        body = self._client._request(
            "POST",
            f"/api/v2/ontologies/{quote_path(ontology)}/actionTypes/getByRidBatch",
            json_body={"rids": rids},
        )
        return (body or {}).get("data", [])

    def get_action_type_full_metadata(self, ontology: str, action_type: str) -> Dict[str, Any]:
        """GET full metadata for a single action type (preview)."""
        body = self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/actionTypes/{quote_path(action_type)}/fullMetadata?preview=true",
        )
        return body or {}

    def list_action_types_full_metadata(self, ontology: str) -> List[Dict[str, Any]]:
        """GET full metadata for all action types (preview)."""
        body = self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/actionTypesFullMetadata?preview=true",
        ) or {}
        return body.get("data", []) if isinstance(body, dict) else []

    # ---- interface types ----------------------------------------------------

    def list_interface_types(self, ontology: str) -> List[InterfaceType]:
        """List all interface types in an ontology (preview)."""
        body = self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/interfaceTypes?preview=true",
        ) or {}
        items = body.get("data", []) if isinstance(body, dict) else []
        return [_validate(InterfaceType, item) for item in items]

    def get_interface_type(self, ontology: str, interface_type: str) -> InterfaceType:
        """Fetch a single interface type by API name."""
        body = self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/interfaceTypes/{quote_path(interface_type)}",
        )
        return _validate(InterfaceType, body)

    # ---- value types --------------------------------------------------------

    def list_value_types(self, ontology: str) -> List[ValueType]:
        """List all value types in an ontology (preview)."""
        body = self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/valueTypes?preview=true",
        ) or {}
        items = body.get("data", []) if isinstance(body, dict) else []
        return [_validate(ValueType, item) for item in items]

    def get_value_type(self, ontology: str, value_type: str) -> ValueType:
        """Fetch a single value type by API name."""
        body = self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/valueTypes/{quote_path(value_type)}",
        )
        return _validate(ValueType, body)

    # ---- query types --------------------------------------------------------

    def list_query_types(self, ontology: str) -> List[QueryType]:
        """List all query types in an ontology."""
        body = self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/queryTypes",
        ) or {}
        items = body.get("data", []) if isinstance(body, dict) else []
        return [_validate(QueryType, item) for item in items]

    def get_query_type(self, ontology: str, query_type: str) -> QueryType:
        """Fetch a single query type by API name."""
        body = self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/queryTypes/{quote_path(query_type)}",
        )
        return _validate(QueryType, body)
