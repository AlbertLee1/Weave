"""ObjectSetsAPI - composable ObjectSet operations."""
from __future__ import annotations

from typing import TYPE_CHECKING, Any, Dict, List, Optional

from ._http import quote_path
from .types import ObjectPage

if TYPE_CHECKING:
    from .client import Client


def _validate_page(payload: Any) -> ObjectPage:
    if hasattr(ObjectPage, "model_validate"):
        return ObjectPage.model_validate(payload or {})
    return ObjectPage(**(payload or {}))


class ObjectSetsAPI:
    """Wraps ``/api/v2/ontologies/{ontology}/objectSets/...``."""

    def __init__(self, client: "Client"):
        self._client = client

    def load_objects(
        self,
        ontology: str,
        object_set: Dict[str, Any],
        select: List[str],
        *,
        page_size: Optional[int] = None,
        page_token: Optional[str] = None,
    ) -> ObjectPage:
        """Load objects matching an ObjectSet definition."""
        body: Dict[str, Any] = {"objectSet": object_set, "select": select}
        if page_size is not None:
            body["pageSize"] = page_size
        if page_token:
            body["pageToken"] = page_token
        path = f"/api/v2/ontologies/{quote_path(ontology)}/objectSets/loadObjects"
        resp = self._client._request("POST", path, json_body=body)
        return _validate_page(resp)

    def load_links(
        self,
        ontology: str,
        object_set: Dict[str, Any],
        link_type: str,
        select: List[str],
        *,
        page_size: Optional[int] = None,
        page_token: Optional[str] = None,
    ) -> ObjectPage:
        """Load linked objects from an ObjectSet definition."""
        body: Dict[str, Any] = {
            "objectSet": object_set,
            "linkType": link_type,
            "select": select,
        }
        if page_size is not None:
            body["pageSize"] = page_size
        if page_token:
            body["pageToken"] = page_token
        path = f"/api/v2/ontologies/{quote_path(ontology)}/objectSets/loadLinks"
        resp = self._client._request("POST", path, json_body=body)
        return _validate_page(resp)

    def aggregate(
        self,
        ontology: str,
        object_set: Dict[str, Any],
        aggregation: List[Dict[str, Any]],
        group_by: Optional[List[Dict[str, Any]]] = None,
    ) -> Dict[str, Any]:
        """Run an aggregation over an ObjectSet."""
        body: Dict[str, Any] = {
            "objectSet": object_set,
            "aggregation": aggregation,
        }
        if group_by:
            body["groupBy"] = group_by
        path = f"/api/v2/ontologies/{quote_path(ontology)}/objectSets/aggregate"
        resp = self._client._request("POST", path, json_body=body)
        return resp or {}

    def create_temporary(
        self,
        ontology: str,
        object_set: Dict[str, Any],
    ) -> Dict[str, Any]:
        """Create a temporary ObjectSet for later re-use."""
        body = {"objectSet": object_set}
        path = f"/api/v2/ontologies/{quote_path(ontology)}/objectSets/createTemporary"
        resp = self._client._request("POST", path, json_body=body)
        return resp or {}

    def get(
        self,
        ontology: str,
        object_set_rid: str,
    ) -> Dict[str, Any]:
        """Fetch a previously-created ObjectSet by its RID."""
        path = f"/api/v2/ontologies/{quote_path(ontology)}/objectSets/{quote_path(object_set_rid)}"
        resp = self._client._request("GET", path)
        return resp or {}
