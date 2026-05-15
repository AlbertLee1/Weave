"""ObjectsAPI - listing, retrieval, and search."""
from __future__ import annotations

from typing import TYPE_CHECKING, Any, Dict, Iterator, Optional

from ._http import build_query_string, quote_path
from .types import ObjectPage, WireObject

if TYPE_CHECKING:
    from .client import Client


def _validate_page(payload: Any) -> ObjectPage:
    if hasattr(ObjectPage, "model_validate"):
        return ObjectPage.model_validate(payload or {})
    return ObjectPage(**(payload or {}))


class ObjectsAPI:
    """Wraps ``/api/v2/ontologies/{ontology}/objects/...``."""

    def __init__(self, client: "Client"):
        self._client = client

    def list(
        self,
        ontology: str,
        object_type: str,
        *,
        page_size: int = 100,
        page_token: str = "",
        order_by: str = "",
    ) -> ObjectPage:
        """Fetch one page of objects."""
        params: Dict[str, Any] = {
            "pageSize": page_size if page_size > 0 else None,
            "pageToken": page_token,
            "orderBy": order_by,
        }
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}/objects/{quote_path(object_type)}"
            + build_query_string(params)
        )
        body = self._client._request("GET", path)
        return _validate_page(body)

    def iter_all(
        self,
        ontology: str,
        object_type: str,
        *,
        page_size: int = 100,
        order_by: str = "",
    ) -> Iterator[WireObject]:
        """Iterate every object in the type, walking ``nextPageToken``."""
        token = ""
        while True:
            page = self.list(
                ontology,
                object_type,
                page_size=page_size,
                page_token=token,
                order_by=order_by,
            )
            for obj in page.data:
                yield obj
            if not page.next_page_token:
                return
            token = page.next_page_token

    def get(
        self,
        ontology: str,
        object_type: str,
        primary_key: str,
        *,
        scenario_id: Optional[str] = None,
    ) -> WireObject:
        """Fetch a single object by its primary key.

        When ``scenario_id`` is set the request carries an ``X-Scenario-Id``
        header so the server folds the named Scenario's edits on top of the
        base view (VTX-004 / VTX-109).
        """
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}/objects/"
            f"{quote_path(object_type)}/{quote_path(primary_key)}"
        )
        extra_headers = {"X-Scenario-Id": scenario_id} if scenario_id else None
        body = self._client._request("GET", path, extra_headers=extra_headers)
        return body or {}

    def search(
        self,
        ontology: str,
        object_type: str,
        where: Dict[str, Any],
        *,
        select: "list[str]",
        page_size: Optional[int] = None,
        page_token: Optional[str] = None,
    ) -> ObjectPage:
        """POST a where-clause search request.

        Args:
            select: Required list of property apiNames to return.
        """
        body: Dict[str, Any] = {"where": where, "select": select}
        if page_size is not None:
            body["pageSize"] = page_size
        if page_token:
            body["pageToken"] = page_token
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}/objects/"
            f"{quote_path(object_type)}/search"
        )
        resp = self._client._request("POST", path, json_body=body)
        return _validate_page(resp)

    def count(
        self,
        ontology: str,
        object_type: str,
    ) -> int:
        """POST count request. Returns the count of objects."""
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}/objects/"
            f"{quote_path(object_type)}/count"
        )
        body = self._client._request("POST", path, json_body={})
        return (body or {}).get("count", 0)

    def list_linked_objects(
        self,
        ontology: str,
        object_type: str,
        primary_key: str,
        link_type: str,
        *,
        page_size: int = 100,
        page_token: str = "",
    ) -> ObjectPage:
        """List objects linked to a specific object via a link type."""
        params: Dict[str, Any] = {
            "pageSize": page_size if page_size > 0 else None,
            "pageToken": page_token,
        }
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}/objects/{quote_path(object_type)}"
            f"/{quote_path(primary_key)}/links/{quote_path(link_type)}"
            + build_query_string(params)
        )
        body = self._client._request("GET", path)
        return _validate_page(body)

    def get_linked_object(
        self,
        ontology: str,
        object_type: str,
        primary_key: str,
        link_type: str,
        linked_pk: str,
    ) -> WireObject:
        """Fetch a single linked object by primary key."""
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}/objects/{quote_path(object_type)}"
            f"/{quote_path(primary_key)}/links/{quote_path(link_type)}/{quote_path(linked_pk)}"
        )
        body = self._client._request("GET", path)
        return body or {}
