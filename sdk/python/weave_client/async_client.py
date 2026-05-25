"""WeaveAsyncClient — async sibling of :class:`weave_client.Client` (US-355).

Mirrors the sync surface method-for-method using ``httpx.AsyncClient`` so
applications running inside an event loop (FastAPI, asyncio bots, async
ETL) can talk to Weave without blocking. The five namespaces — ontologies,
objects, actions, objectsets, functions — are rebound as ``Async*`` classes
that ``await`` every transport call.

Usage::

    import asyncio
    from weave_client import WeaveAsyncClient

    async def main():
        async with WeaveAsyncClient("http://localhost:9117", access_token="…") as c:
            page = await c.objects.list("northwind", "Customer", page_size=25)
            for row in page.data:
                print(row["__primaryKey"])

    asyncio.run(main())

Streaming endpoints expose ``async for`` iterators; see
:meth:`AsyncFunctionsAPI.execute_stream`.
"""
from __future__ import annotations

import json
from typing import Any, AsyncIterator, Dict, List, Optional

from ._async_http import AsyncTransport
from ._http import HTTPResponse, build_query_string, quote_path
from ._retry import RetryPolicy
from .exceptions import WeaveAuthError, WeaveError, WeaveNotFoundError
from .subscriptions import Subscription, WebSocketTransport
from .types import (
    ActionCheckResponse,
    ActionType,
    ApplyActionResponse,
    BatchApplyActionResponse,
    InterfaceType,
    ObjectCheckBatchResponse,
    ObjectCheckResponse,
    LoginResponse,
    MeOntologiesEntry,
    ObjectPage,
    ObjectType,
    Ontology,
    OntologyMe,
    QueryType,
    ValueType,
    WireObject,
)


def _validate(model_cls, payload):
    if hasattr(model_cls, "model_validate"):
        return model_cls.model_validate(payload)
    return model_cls(**payload)


def _validate_page(payload: Any) -> ObjectPage:
    return _validate(ObjectPage, payload or {})


def _validate_apply(payload: Any) -> ApplyActionResponse:
    return _validate(ApplyActionResponse, payload or {})


def _validate_apply_batch(payload: Any) -> BatchApplyActionResponse:
    return _validate(BatchApplyActionResponse, payload or {})


class WeaveAsyncClient:
    """Async configured Weave client.

    Constructor parameters mirror :class:`weave_client.Client`. The
    ``transport`` keyword accepts a custom :class:`AsyncTransport` to
    swap in mock backends in tests.
    """

    def __init__(
        self,
        base_url: str,
        *,
        access_token: Optional[str] = None,
        api_key: Optional[str] = None,
        timeout: float = 30.0,
        transport: Optional[AsyncTransport] = None,
        retry: Optional[RetryPolicy] = None,
    ):
        self.base_url = base_url.rstrip("/")
        self.access_token = access_token
        self.api_key = api_key
        if transport is None:
            transport = AsyncTransport(timeout=timeout, retry=retry)
        elif retry is not None:
            transport.retry = retry
        self._transport = transport

        self.ontologies = AsyncOntologiesAPI(self)
        self.objects = AsyncObjectsAPI(self)
        self.actions = AsyncActionsAPI(self)
        self.objectsets = AsyncObjectSetsAPI(self)
        self.functions = AsyncFunctionsAPI(self)
        self.transactions = AsyncTransactionsAPI(self)
        self.reactions = AsyncReactionsAPI(self)
        self.notifications = AsyncNotificationsAPI(self)
        self.dashboards = AsyncDashboardsAPI(self)
        self.permissionrequests = AsyncPermissionRequestsAPI(self)
        self.permissions = AsyncPermissionsAPI(self)
        self.sessions = AsyncSessionsAPI(self)

    @property
    def token(self) -> str:
        if self.access_token:
            return self.access_token
        if self.api_key:
            return self.api_key
        return ""

    async def aclose(self) -> None:
        await self._transport.aclose()

    async def __aenter__(self) -> "WeaveAsyncClient":
        return self

    async def __aexit__(self, *exc):
        await self.aclose()

    # ---- internal helpers --------------------------------------------------

    def _headers(self, *, anonymous: bool = False) -> dict:
        h: dict = {}
        if not anonymous and self.token:
            h["Authorization"] = f"Bearer {self.token}"
        return h

    async def _request(
        self,
        method: str,
        path: str,
        *,
        json_body: Any = None,
        anonymous: bool = False,
    ) -> Any:
        url = self.base_url + path
        resp = await self._transport.request(
            method,
            url,
            headers=self._headers(anonymous=anonymous),
            json_body=json_body,
        )
        return _handle(resp)

    # ---- top-level convenience --------------------------------------------

    async def login(self, email: str, password: str) -> LoginResponse:
        """Exchange credentials for an access/refresh pair.

        On success the new ``access_token`` is automatically attached to
        this client so subsequent calls are authenticated.
        """
        body = await self._request(
            "POST",
            "/api/auth/login",
            json_body={"email": email, "password": password},
            anonymous=True,
        )
        resp = _validate(LoginResponse, body)
        self.access_token = resp.access_token
        return resp

    async def logout(self, refresh_token: str = "") -> None:
        """Revoke the supplied (or last-issued) refresh token. Idempotent."""
        body = {"refresh_token": refresh_token} if refresh_token else None
        await self._request("POST", "/api/auth/logout", json_body=body, anonymous=True)
        self.access_token = None


def _handle(resp: HTTPResponse) -> Any:
    """Translate an HTTPResponse into either a Python value or a typed exception.

    Identical contract to :meth:`weave_client.Client._handle` so async
    callers see the same exception hierarchy.
    """
    if 200 <= resp.status_code < 300:
        if not resp.text:
            return None
        try:
            return resp.json()
        except ValueError as e:  # pragma: no cover - bad server response
            raise WeaveError(resp.status_code, raw_body=resp.text) from e

    envelope: dict = {}
    try:
        parsed = resp.json()
        if isinstance(parsed, dict):
            envelope = parsed
    except ValueError:
        pass

    kwargs = dict(
        status_code=resp.status_code,
        error_code=envelope.get("errorCode", "") or "",
        error_name=envelope.get("errorName", "") or "",
        error_instance_id=envelope.get("errorInstanceId", "") or "",
        parameters=envelope.get("parameters") or {},
        raw_body=resp.text,
    )
    if resp.status_code in (401, 403):
        raise WeaveAuthError(**kwargs)
    if resp.status_code == 404:
        raise WeaveNotFoundError(**kwargs)
    raise WeaveError(**kwargs)


# --- API namespaces -----------------------------------------------------------


class AsyncOntologiesAPI:
    """Async sibling of :class:`weave_client.ontologies.OntologiesAPI`."""

    def __init__(self, client: "WeaveAsyncClient"):
        self._client = client

    async def list(self) -> List[Ontology]:
        body = await self._client._request("GET", "/api/v2/ontologies") or {}
        items = body.get("data", []) if isinstance(body, dict) else []
        return [_validate(Ontology, item) for item in items]

    async def get(self, api_name: str) -> Ontology:
        body = await self._client._request("GET", f"/api/v2/ontologies/{quote_path(api_name)}")
        return _validate(Ontology, body)

    async def load_metadata(self, ontology: str, subsets: Dict[str, bool]) -> Dict[str, Any]:
        body = await self._client._request(
            "POST",
            f"/api/v2/ontologies/{quote_path(ontology)}/metadata",
            json_body=subsets,
        )
        return body or {}

    async def get_full_metadata(self, ontology: str) -> Dict[str, Any]:
        body = await self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/fullMetadata?preview=true",
        )
        return body or {}

    async def list_object_types(self, ontology: str) -> List[ObjectType]:
        body = await self._client._request(
            "GET", f"/api/v2/ontologies/{quote_path(ontology)}/objectTypes"
        ) or {}
        items = body.get("data", []) if isinstance(body, dict) else []
        return [_validate(ObjectType, item) for item in items]

    async def get_object_type(self, ontology: str, object_type: str) -> ObjectType:
        body = await self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/objectTypes/{quote_path(object_type)}",
        )
        return _validate(ObjectType, body)

    async def get_object_type_full_metadata(
        self, ontology: str, object_type: str
    ) -> Dict[str, Any]:
        body = await self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/objectTypes/{quote_path(object_type)}/fullMetadata?preview=true",
        )
        return body or {}

    async def get_object_types_by_rid_batch(
        self, ontology: str, rids: List[str]
    ) -> List[Dict[str, Any]]:
        body = await self._client._request(
            "POST",
            f"/api/v2/ontologies/{quote_path(ontology)}/objectTypes/getByRidBatch",
            json_body={"rids": rids},
        )
        return (body or {}).get("data", [])

    async def list_action_types(self, ontology: str) -> List[ActionType]:
        body = await self._client._request(
            "GET", f"/api/v2/ontologies/{quote_path(ontology)}/actionTypes"
        ) or {}
        items = body.get("data", []) if isinstance(body, dict) else []
        return [_validate(ActionType, item) for item in items]

    async def get_action_type(self, ontology: str, action_type: str) -> ActionType:
        body = await self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/actionTypes/{quote_path(action_type)}",
        )
        return _validate(ActionType, body)

    async def get_action_type_by_rid(self, ontology: str, rid: str) -> ActionType:
        body = await self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/actionTypes/byRid/{quote_path(rid)}",
        )
        return _validate(ActionType, body)

    async def get_action_types_by_rid_batch(
        self, ontology: str, rids: List[str]
    ) -> List[Dict[str, Any]]:
        body = await self._client._request(
            "POST",
            f"/api/v2/ontologies/{quote_path(ontology)}/actionTypes/getByRidBatch",
            json_body={"rids": rids},
        )
        return (body or {}).get("data", [])

    async def get_link_types_by_rid_batch(
        self, ontology: str, rids: List[str]
    ) -> List[Dict[str, Any]]:
        body = await self._client._request(
            "POST",
            f"/api/v2/ontologies/{quote_path(ontology)}/linkTypes/getByRidBatch",
            json_body={"rids": rids},
        )
        return (body or {}).get("data", [])

    async def get_interface_types_by_rid_batch(
        self, ontology: str, rids: List[str]
    ) -> List[Dict[str, Any]]:
        body = await self._client._request(
            "POST",
            f"/api/v2/ontologies/{quote_path(ontology)}/interfaceTypes/getByRidBatch",
            json_body={"rids": rids},
        )
        return (body or {}).get("data", [])

    async def get_value_types_by_rid_batch(
        self, ontology: str, rids: List[str]
    ) -> List[Dict[str, Any]]:
        body = await self._client._request(
            "POST",
            f"/api/v2/ontologies/{quote_path(ontology)}/valueTypes/getByRidBatch",
            json_body={"rids": rids},
        )
        return (body or {}).get("data", [])

    async def get_shared_property_types_by_rid_batch(
        self, ontology: str, rids: List[str]
    ) -> List[Dict[str, Any]]:
        body = await self._client._request(
            "POST",
            f"/api/v2/ontologies/{quote_path(ontology)}/sharedPropertyTypes/getByRidBatch",
            json_body={"rids": rids},
        )
        return (body or {}).get("data", [])

    async def get_type_groups_by_rid_batch(
        self, ontology: str, rids: List[str]
    ) -> List[Dict[str, Any]]:
        body = await self._client._request(
            "POST",
            f"/api/v2/ontologies/{quote_path(ontology)}/typeGroups/getByRidBatch",
            json_body={"rids": rids},
        )
        return (body or {}).get("data", [])

    async def get_query_types_by_rid_batch(
        self, ontology: str, rids: List[str]
    ) -> List[Dict[str, Any]]:
        body = await self._client._request(
            "POST",
            f"/api/v2/ontologies/{quote_path(ontology)}/queryTypes/getByRidBatch",
            json_body={"rids": rids},
        )
        return (body or {}).get("data", [])

    async def get_action_type_full_metadata(
        self, ontology: str, action_type: str
    ) -> Dict[str, Any]:
        body = await self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/actionTypes/{quote_path(action_type)}/fullMetadata?preview=true",
        )
        return body or {}

    async def list_action_types_full_metadata(self, ontology: str) -> List[Dict[str, Any]]:
        body = await self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/actionTypesFullMetadata?preview=true",
        ) or {}
        return body.get("data", []) if isinstance(body, dict) else []

    async def list_interface_types(self, ontology: str) -> List[InterfaceType]:
        body = await self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/interfaceTypes?preview=true",
        ) or {}
        items = body.get("data", []) if isinstance(body, dict) else []
        return [_validate(InterfaceType, item) for item in items]

    async def get_interface_type(self, ontology: str, interface_type: str) -> InterfaceType:
        body = await self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/interfaceTypes/{quote_path(interface_type)}",
        )
        return _validate(InterfaceType, body)

    async def list_value_types(self, ontology: str) -> List[ValueType]:
        body = await self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/valueTypes?preview=true",
        ) or {}
        items = body.get("data", []) if isinstance(body, dict) else []
        return [_validate(ValueType, item) for item in items]

    async def get_value_type(self, ontology: str, value_type: str) -> ValueType:
        body = await self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/valueTypes/{quote_path(value_type)}",
        )
        return _validate(ValueType, body)

    async def list_query_types(self, ontology: str) -> List[QueryType]:
        body = await self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/queryTypes",
        ) or {}
        items = body.get("data", []) if isinstance(body, dict) else []
        return [_validate(QueryType, item) for item in items]

    async def get_query_type(self, ontology: str, query_type: str) -> QueryType:
        body = await self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/queryTypes/{quote_path(query_type)}",
        )
        return _validate(QueryType, body)

    async def get_me(self, ontology: str) -> OntologyMe:
        """Async mirror of OntologiesAPI.get_me — round-96."""
        body = await self._client._request(
            "GET",
            f"/api/v2/ontologies/{quote_path(ontology)}/me",
        )
        return _validate(OntologyMe, body or {})

    async def list_me(self) -> List[MeOntologiesEntry]:
        """Async mirror of OntologiesAPI.list_me — round-100."""
        body = await self._client._request("GET", "/api/v2/me/ontologies") or {}
        items = body.get("ontologies", []) if isinstance(body, dict) else []
        return [_validate(MeOntologiesEntry, item) for item in items]


class AsyncObjectsAPI:
    """Async sibling of :class:`weave_client.objects.ObjectsAPI`."""

    def __init__(self, client: "WeaveAsyncClient"):
        self._client = client

    async def list(
        self,
        ontology: str,
        object_type: str,
        *,
        page_size: int = 100,
        page_token: str = "",
        order_by: str = "",
    ) -> ObjectPage:
        params: Dict[str, Any] = {
            "pageSize": page_size if page_size > 0 else None,
            "pageToken": page_token,
            "orderBy": order_by,
        }
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}/objects/{quote_path(object_type)}"
            + build_query_string(params)
        )
        body = await self._client._request("GET", path)
        return _validate_page(body)

    async def iter_all(
        self,
        ontology: str,
        object_type: str,
        *,
        page_size: int = 100,
        order_by: str = "",
    ) -> AsyncIterator[WireObject]:
        """Async-iterate every object in the type, walking ``nextPageToken``.

        Returned as an ``async for``-able generator::

            async for row in client.objects.iter_all("nw", "Customer"):
                ...
        """
        token = ""
        while True:
            page = await self.list(
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

    async def get(
        self,
        ontology: str,
        object_type: str,
        primary_key: str,
    ) -> WireObject:
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}/objects/"
            f"{quote_path(object_type)}/{quote_path(primary_key)}"
        )
        body = await self._client._request("GET", path)
        return body or {}

    async def search(
        self,
        ontology: str,
        object_type: str,
        where: Dict[str, Any],
        *,
        select: List[str],
        page_size: Optional[int] = None,
        page_token: Optional[str] = None,
    ) -> ObjectPage:
        body: Dict[str, Any] = {"where": where, "select": select}
        if page_size is not None:
            body["pageSize"] = page_size
        if page_token:
            body["pageToken"] = page_token
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}/objects/"
            f"{quote_path(object_type)}/search"
        )
        resp = await self._client._request("POST", path, json_body=body)
        return _validate_page(resp)

    async def count(self, ontology: str, object_type: str) -> int:
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}/objects/"
            f"{quote_path(object_type)}/count"
        )
        body = await self._client._request("POST", path, json_body={})
        return (body or {}).get("count", 0)

    async def list_linked_objects(
        self,
        ontology: str,
        object_type: str,
        primary_key: str,
        link_type: str,
        *,
        page_size: int = 100,
        page_token: str = "",
    ) -> ObjectPage:
        params: Dict[str, Any] = {
            "pageSize": page_size if page_size > 0 else None,
            "pageToken": page_token,
        }
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}/objects/{quote_path(object_type)}"
            f"/{quote_path(primary_key)}/links/{quote_path(link_type)}"
            + build_query_string(params)
        )
        body = await self._client._request("GET", path)
        return _validate_page(body)

    async def get_linked_object(
        self,
        ontology: str,
        object_type: str,
        primary_key: str,
        link_type: str,
        linked_pk: str,
    ) -> WireObject:
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}/objects/{quote_path(object_type)}"
            f"/{quote_path(primary_key)}/links/{quote_path(link_type)}/{quote_path(linked_pk)}"
        )
        body = await self._client._request("GET", path)
        return body or {}

    async def check_batch(
        self,
        ontology: str,
        object_types: list,
    ) -> ObjectCheckBatchResponse:
        """Async mirror of ObjectsAPI.check_batch — round 108."""
        body = {
            "ontologyApiName": ontology,
            "objectTypeApiNames": object_types,
        }
        resp = await self._client._request(
            "POST", "/api/v2/me/checks/objectTypes", json_body=body)
        return _validate(ObjectCheckBatchResponse, resp or {})

    async def check(self, ontology: str, object_type: str) -> ObjectCheckResponse:
        """Async mirror of ObjectsAPI.check — round 106."""
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/objectTypes/{quote_path(object_type)}/check"
        )
        resp = await self._client._request("GET", path)
        return _validate(ObjectCheckResponse, resp or {})

    def subscribe(
        self,
        ontology: str,
        object_type: str,
        *,
        where: Optional[Dict[str, Any]] = None,
        select: Optional[List[str]] = None,
        auto_reconnect: bool = True,
        transport_factory: Optional[Any] = None,
    ) -> Subscription:
        """Open a WebSocket subscription on the given ObjectType (US-418).

        Yields :class:`weave_client.subscriptions.ChangeEvent` instances
        for each ``ADDED_OR_UPDATED`` / ``DELETED`` event the server
        delivers. Auto-reconnect resumes from the most recent cursor
        via ``?since=<cursor>`` so a brief disconnect silently replays
        the missed window. A connection-level ``onOutOfDate`` (cursor
        outside the server's 5-minute / 10000-event replay window)
        raises :class:`WeaveOutOfDate` so the caller can refresh full
        state before re-subscribing.

        ``where`` is a Weave WhereClause; ``select`` projects the
        subset of properties to deliver. ``transport_factory`` is
        injected by tests to script message sequences without a real
        WebSocket; production callers should leave it None to use
        :class:`WebsocketsTransport`.
        """
        return Subscription(
            base_url=self._client.base_url,
            ontology=ontology,
            object_type=object_type,
            where=where,
            select=select,
            token=self._client.token,
            transport_factory=transport_factory,
            auto_reconnect=auto_reconnect,
        )


class AsyncActionsAPI:
    """Async sibling of :class:`weave_client.actions.ActionsAPI`."""

    def __init__(self, client: "WeaveAsyncClient"):
        self._client = client

    async def apply(
        self,
        ontology: str,
        action_type: str,
        parameters: Dict[str, Any],
    ) -> ApplyActionResponse:
        body = {"parameters": parameters or {}}
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/actions/{quote_path(action_type)}/apply"
        )
        resp = await self._client._request("POST", path, json_body=body)
        return _validate_apply(resp)

    async def apply_with_options(
        self,
        ontology: str,
        action_type: str,
        parameters: Dict[str, Any],
        *,
        mode: str = "VALIDATE_AND_EXECUTE",
        return_edits: str = "ALL",
    ) -> ApplyActionResponse:
        body: Dict[str, Any] = {
            "parameters": parameters or {},
            "options": {"mode": mode, "returnEdits": return_edits},
        }
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/actions/{quote_path(action_type)}/apply"
        )
        resp = await self._client._request("POST", path, json_body=body)
        return _validate_apply(resp)

    async def apply_batch(
        self,
        ontology: str,
        action_type: str,
        requests_list: List[Dict[str, Any]],
        *,
        return_edits: str = "ALL",
    ) -> BatchApplyActionResponse:
        body: Dict[str, Any] = {"requests": requests_list}
        if return_edits != "ALL":
            body["options"] = {"returnEdits": return_edits}
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/actions/{quote_path(action_type)}/applyBatch"
        )
        resp = await self._client._request("POST", path, json_body=body)
        return _validate_apply_batch(resp)

    async def execute_query(
        self,
        ontology: str,
        query_api_name: str,
        parameters: Optional[Dict[str, Any]] = None,
    ) -> Dict[str, Any]:
        body = {"parameters": parameters or {}}
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/queries/{quote_path(query_api_name)}/execute"
        )
        resp = await self._client._request("POST", path, json_body=body)
        return resp or {}

    async def check(self, ontology: str, action_type: str) -> ActionCheckResponse:
        """Async mirror of ActionsAPI.check — round 104."""
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/actions/{quote_path(action_type)}/check"
        )
        resp = await self._client._request("GET", path)
        return _validate(ActionCheckResponse, resp or {})


class AsyncObjectSetsAPI:
    """Async sibling of :class:`weave_client.objectsets.ObjectSetsAPI`."""

    def __init__(self, client: "WeaveAsyncClient"):
        self._client = client

    async def load_objects(
        self,
        ontology: str,
        object_set: Dict[str, Any],
        select: List[str],
        *,
        page_size: Optional[int] = None,
        page_token: Optional[str] = None,
    ) -> ObjectPage:
        body: Dict[str, Any] = {"objectSet": object_set, "select": select}
        if page_size is not None:
            body["pageSize"] = page_size
        if page_token:
            body["pageToken"] = page_token
        path = f"/api/v2/ontologies/{quote_path(ontology)}/objectSets/loadObjects"
        resp = await self._client._request("POST", path, json_body=body)
        return _validate_page(resp)

    async def load_links(
        self,
        ontology: str,
        object_set: Dict[str, Any],
        link_type: str,
        select: List[str],
        *,
        page_size: Optional[int] = None,
        page_token: Optional[str] = None,
    ) -> ObjectPage:
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
        resp = await self._client._request("POST", path, json_body=body)
        return _validate_page(resp)

    async def aggregate(
        self,
        ontology: str,
        object_set: Dict[str, Any],
        aggregation: List[Dict[str, Any]],
        group_by: Optional[List[Dict[str, Any]]] = None,
    ) -> Dict[str, Any]:
        body: Dict[str, Any] = {
            "objectSet": object_set,
            "aggregation": aggregation,
        }
        if group_by:
            body["groupBy"] = group_by
        path = f"/api/v2/ontologies/{quote_path(ontology)}/objectSets/aggregate"
        resp = await self._client._request("POST", path, json_body=body)
        return resp or {}

    async def create_temporary(
        self,
        ontology: str,
        object_set: Dict[str, Any],
    ) -> Dict[str, Any]:
        body = {"objectSet": object_set}
        path = f"/api/v2/ontologies/{quote_path(ontology)}/objectSets/createTemporary"
        resp = await self._client._request("POST", path, json_body=body)
        return resp or {}

    async def get(
        self,
        ontology: str,
        object_set_rid: str,
    ) -> Dict[str, Any]:
        path = f"/api/v2/ontologies/{quote_path(ontology)}/objectSets/{quote_path(object_set_rid)}"
        resp = await self._client._request("GET", path)
        return resp or {}


class AsyncFunctionsAPI:
    """Async sibling of :class:`weave_client.functions.FunctionsAPI`."""

    def __init__(self, client: "WeaveAsyncClient"):
        self._client = client

    async def execute(
        self,
        ontology: str,
        function_ref: str,
        parameters: Optional[Dict[str, Any]] = None,
    ) -> Any:
        body = {"parameters": parameters or {}}
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/functions/{quote_path(function_ref)}/execute"
        )
        resp = await self._client._request("POST", path, json_body=body)
        if not isinstance(resp, dict):
            return resp
        return resp.get("result")

    async def execute_stream(
        self,
        ontology: str,
        function_ref: str,
        parameters: Optional[Dict[str, Any]] = None,
    ) -> AsyncIterator[Any]:
        """Execute with ``?stream=1`` and yield each NDJSON item asynchronously.

        Pre-execution failures (validation, 404, quota, no-executor) raise
        a typed :class:`WeaveError` immediately — BEFORE the caller's
        ``async for`` loop runs — by re-raising eagerly inside this method
        before returning the iterator. A terminal in-band ``{"error": ...}``
        line raises during iteration with ``error_name`` set to the
        server-supplied ``code``.
        """
        body = {"parameters": parameters or {}}
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/functions/{quote_path(function_ref)}/execute?stream=1"
        )
        url = self._client.base_url + path
        status, _, lines = await self._client._transport.stream_lines(
            "POST",
            url,
            headers=self._client._headers(),
            json_body=body,
        )
        if not (200 <= status < 300):
            collected: List[str] = []
            async for chunk in lines:
                collected.append(chunk)
            payload = "\n".join(collected)
            envelope: Dict[str, Any] = {}
            try:
                parsed = json.loads(payload) if payload else {}
                if isinstance(parsed, dict):
                    envelope = parsed
            except ValueError:
                pass
            raise WeaveError(
                status_code=status,
                error_code=envelope.get("errorCode", "") or "",
                error_name=envelope.get("errorName", "") or "",
                error_instance_id=envelope.get("errorInstanceId", "") or "",
                parameters=envelope.get("parameters") or {},
                raw_body=payload,
            )

        return _consume_ndjson_async(status, lines)


async def _consume_ndjson_async(
    status: int, lines: AsyncIterator[str]
) -> AsyncIterator[Any]:
    """Yield items / raise on terminal error from an async NDJSON line stream."""
    async for line in lines:
        line = line.strip()
        if not line:
            continue
        try:
            obj = json.loads(line)
        except ValueError as e:
            raise WeaveError(
                status_code=status,
                error_code="STREAM_DECODE",
                error_name="InvalidStreamLine",
                error_instance_id="",
                parameters={"line": line},
                raw_body=str(e),
            ) from e
        if not isinstance(obj, dict):
            yield obj
            continue
        if "error" in obj:
            err = obj["error"] if isinstance(obj["error"], dict) else {}
            raise WeaveError(
                status_code=status,
                error_code=str(err.get("code", "FunctionExecutionFailed")),
                error_name=str(err.get("code", "FunctionExecutionFailed")),
                error_instance_id="",
                parameters=err,
                raw_body=line,
            )
        if "item" in obj:
            yield obj["item"]
            continue
        yield obj


class AsyncTransactionsAPI:
    """Async mirror of TransactionsAPI (round 61, PRD-V2 Transaction
    preview). Wraps the three OntologyTransaction preview endpoints
    (POST .../edits, GET .../{id}, DELETE .../{id}) and attaches the
    mandatory ?preview=true flag automatically so async callers don't
    have to remember it. Returns the same dataclasses as the sync
    sibling so application code can swap clients without touching
    response handling.
    """

    def __init__(self, client: "WeaveAsyncClient") -> None:
        self._client = client

    async def append_edits(
        self,
        ontology: str,
        transaction_id: str,
        edits: List[Dict[str, Any]],
    ) -> "TransactionAppendResponse":
        from .transactions import TransactionAppendResponse
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/transactions/{quote_path(transaction_id)}/edits?preview=true"
        )
        resp = await self._client._request("POST", path, json_body={"edits": edits})
        return TransactionAppendResponse(
            transaction_id=str(resp.get("transactionId") or transaction_id),
            appended_edits=int(resp.get("appendedEdits") or 0),
            total_edits=int(resp.get("totalEdits") or 0),
        )

    async def get(self, ontology: str, transaction_id: str) -> "Transaction":
        from .transactions import Transaction
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/transactions/{quote_path(transaction_id)}?preview=true"
        )
        resp = await self._client._request("GET", path)
        edits = resp.get("edits") or []
        return Transaction(
            transaction_id=str(resp.get("transactionId") or transaction_id),
            total_edits=int(resp.get("totalEdits") or 0),
            edits=list(edits),
        )

    async def abort(self, ontology: str, transaction_id: str) -> None:
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/transactions/{quote_path(transaction_id)}?preview=true"
        )
        await self._client._request("DELETE", path)
        return None


class AsyncReactionsAPI:
    """Async mirror of ReactionsAPI (round 74). Wraps the four
    /api/v2/reactions endpoints (Aggregate / Create / Delete /
    AggregateBatch from round 67). Returns the same Reaction /
    ReactionSummary / EmojiCount dataclasses as the sync sibling so
    application code can swap clients without touching response
    handling.
    """

    def __init__(self, client: "WeaveAsyncClient") -> None:
        self._client = client

    async def aggregate(self, target_rid: str) -> "ReactionSummary":
        from .reactions import _parse_summary  # noqa: WPS433  lazy to avoid cycle
        path = "/api/v2/reactions" + build_query_string({"targetRid": target_rid})
        resp = await self._client._request("GET", path)
        return _parse_summary(resp or {})

    async def create(self, target_rid: str, emoji: str) -> "Reaction":
        from .reactions import _parse_reaction
        resp = await self._client._request(
            "POST", "/api/v2/reactions",
            json_body={"targetRid": target_rid, "emoji": emoji},
        )
        return _parse_reaction(resp or {})

    async def delete(self, target_rid: str, emoji: str) -> None:
        path = "/api/v2/reactions" + build_query_string({
            "targetRid": target_rid,
            "emoji": emoji,
        })
        await self._client._request("DELETE", path)
        return None

    async def aggregate_batch(self, target_rids: List[str]) -> List["ReactionSummary"]:
        if not target_rids:
            return []
        from .reactions import _parse_summary
        resp = await self._client._request(
            "POST", "/api/v2/reactions/batch",
            json_body={"targetRids": list(target_rids)},
        )
        raw = (resp or {}).get("summaries") or []
        return [_parse_summary(s) for s in raw]


class AsyncNotificationsAPI:
    """Async mirror of NotificationsAPI (round 74). Wraps the four
    /api/v2/notifications endpoints (List + round-66 unread-count
    badge + MarkAllRead + MarkRead). Reuses Notification dataclass
    from the sync module via lazy import.
    """

    def __init__(self, client: "WeaveAsyncClient") -> None:
        self._client = client

    async def list(
        self,
        unread_only: bool = False,
        types: Optional[List[str]] = None,
    ) -> List["Notification"]:
        from .notifications import _parse_notification, _url_quote_value
        params: dict = {}
        if unread_only:
            params["unread"] = "true"
        path = "/api/v2/notifications" + build_query_string(params)
        if types:
            extras = "&".join("type=" + _url_quote_value(t) for t in types)
            path = path + ("&" if "?" in path else "?") + extras
        resp = await self._client._request("GET", path)
        data = (resp or {}).get("data") or []
        return [_parse_notification(n) for n in data]

    async def unread_count(self) -> int:
        resp = await self._client._request("GET", "/api/v2/notifications/unread-count")
        try:
            return int((resp or {}).get("count") or 0)
        except (TypeError, ValueError):
            return 0

    async def mark_read(self, notification_id: str) -> None:
        path = "/api/v2/notifications/" + quote_path(notification_id) + "/read"
        await self._client._request("POST", path)
        return None

    async def mark_all_read(self, types: Optional[List[str]] = None) -> int:
        from .notifications import _url_quote_value
        path = "/api/v2/notifications/read-all"
        if types:
            extras = "&".join("type=" + _url_quote_value(t) for t in types)
            path = path + "?" + extras
        resp = await self._client._request("POST", path)
        try:
            return int((resp or {}).get("updated") or 0)
        except (TypeError, ValueError):
            return 0


class AsyncDashboardsAPI:
    """Async mirror of DashboardsAPI (round 78). Wraps the six
    dashboards endpoints (CRUD + round-62 Duplicate). Reuses
    Dashboard dataclass from the sync module via lazy import.
    Same PARTIAL UPDATE SEMANTIC: fields set to None preserve
    the existing server value rather than clearing it.
    """

    def __init__(self, client: "WeaveAsyncClient") -> None:
        self._client = client

    async def list(self) -> List["Dashboard"]:
        from .dashboards import _parse_dashboard
        resp = await self._client._request("GET", "/api/v2/dashboards")
        rows = (resp or {}).get("dashboards") or []
        return [_parse_dashboard(d) for d in rows]

    async def create(
        self,
        name: str,
        definition: Optional[Dict[str, Any]] = None,
        is_public: bool = False,
    ) -> "Dashboard":
        from .dashboards import _parse_dashboard
        body: Dict[str, Any] = {"name": name}
        if definition is not None:
            body["definition"] = definition
        if is_public:
            body["isPublic"] = True
        resp = await self._client._request("POST", "/api/v2/dashboards", json_body=body)
        return _parse_dashboard(resp or {})

    async def get(self, dashboard_id: str) -> "Dashboard":
        from .dashboards import _parse_dashboard
        path = "/api/v2/dashboards/" + quote_path(dashboard_id)
        resp = await self._client._request("GET", path)
        return _parse_dashboard(resp or {})

    async def update(
        self,
        dashboard_id: str,
        name: Optional[str] = None,
        definition: Optional[Dict[str, Any]] = None,
        is_public: Optional[bool] = None,
    ) -> "Dashboard":
        from .dashboards import _parse_dashboard
        body: Dict[str, Any] = {}
        if name is not None:
            body["name"] = name
        if definition is not None:
            body["definition"] = definition
        if is_public is not None:
            body["isPublic"] = is_public
        path = "/api/v2/dashboards/" + quote_path(dashboard_id)
        resp = await self._client._request("PUT", path, json_body=body)
        return _parse_dashboard(resp or {})

    async def delete(self, dashboard_id: str) -> None:
        path = "/api/v2/dashboards/" + quote_path(dashboard_id)
        await self._client._request("DELETE", path)
        return None

    async def duplicate(self, dashboard_id: str) -> "Dashboard":
        from .dashboards import _parse_dashboard
        path = "/api/v2/dashboards/" + quote_path(dashboard_id) + "/duplicate"
        resp = await self._client._request("POST", path)
        return _parse_dashboard(resp or {})


class AsyncPermissionRequestsAPI:
    """Async mirror of PermissionRequestsAPI (round 82). Wraps the
    six permission-requests endpoints (5 from VTX-339 + round-63
    Cancel). Reuses PermissionRequest + PermissionRequestList
    dataclasses from the sync module via lazy import.

    Same approve/reject _decide helper that omits body when note is
    empty — server's readOptionalJSON accepts that path.
    """

    def __init__(self, client: "WeaveAsyncClient") -> None:
        self._client = client

    async def create(self, target_rid: str, reason: str = "") -> "PermissionRequest":
        from .permissionrequests import _parse_request
        body = {"targetRid": target_rid, "reason": reason}
        resp = await self._client._request("POST", "/api/v2/permission-requests", json_body=body)
        return _parse_request(resp or {})

    async def list(
        self,
        status: Optional[str] = None,
        requested_by: Optional[str] = None,
        target_rid: Optional[str] = None,
        limit: Optional[int] = None,
        offset: Optional[int] = None,
    ) -> "PermissionRequestList":
        from .permissionrequests import PermissionRequestList, _parse_request
        params: dict = {}
        if status is not None:
            params["status"] = status
        if requested_by is not None:
            params["requestedBy"] = requested_by
        if target_rid is not None:
            params["targetRid"] = target_rid
        if limit is not None:
            params["limit"] = str(limit)
        if offset is not None:
            params["offset"] = str(offset)
        path = "/api/v2/permission-requests" + build_query_string(params)
        resp = await self._client._request("GET", path)
        envelope = resp or {}
        rows = envelope.get("requests") or []
        return PermissionRequestList(
            requests=[_parse_request(r) for r in rows],
            total=int(envelope.get("total") or 0),
            limit=int(envelope.get("limit") or 0),
            offset=int(envelope.get("offset") or 0),
        )

    async def get(self, request_id: str) -> "PermissionRequest":
        from .permissionrequests import _parse_request
        path = "/api/v2/permission-requests/" + quote_path(request_id)
        resp = await self._client._request("GET", path)
        return _parse_request(resp or {})

    async def approve(self, request_id: str, note: str = "") -> "PermissionRequest":
        return await self._decide(request_id, "approve", note)

    async def reject(self, request_id: str, note: str = "") -> "PermissionRequest":
        return await self._decide(request_id, "reject", note)

    async def cancel(self, request_id: str) -> None:
        path = "/api/v2/permission-requests/" + quote_path(request_id)
        await self._client._request("DELETE", path)
        return None

    async def _decide(self, request_id: str, verb: str, note: str) -> "PermissionRequest":
        from .permissionrequests import _parse_request
        path = "/api/v2/permission-requests/" + quote_path(request_id) + "/" + verb
        body = {"note": note} if note else None
        resp = await self._client._request("POST", path, json_body=body)
        return _parse_request(resp or {})


class AsyncSessionsAPI:
    """Async mirror of SessionsAPI (round 102). Wraps the
    US-254 list + delete + round-101 revoke-others surface."""

    def __init__(self, client: "WeaveAsyncClient") -> None:
        self._client = client

    async def list(self) -> "List[Session]":
        from .types import Session
        body = await self._client._request("GET", "/api/auth/sessions") or {}
        items = body.get("sessions", []) if isinstance(body, dict) else []
        return [_validate(Session, item) for item in items]

    async def revoke(self, session_id: str) -> None:
        await self._client._request(
            "DELETE", f"/api/auth/sessions/{quote_path(session_id)}")
        return None

    async def revoke_others(self) -> "RevokeOthersResponse":
        from .types import RevokeOthersResponse
        resp = await self._client._request(
            "POST", "/api/auth/sessions/revoke-others")
        return _validate(RevokeOthersResponse, resp or {})


class AsyncPermissionsAPI:
    """Async mirror of PermissionsAPI (round 98). Wraps the
    round-97 POST /api/v2/me/permissions/check probe so the async
    client has full parity with the sync surface."""

    def __init__(self, client: "WeaveAsyncClient") -> None:
        self._client = client

    async def check(
        self,
        permissions: List[str],
        ontology: Optional[str] = None,
    ) -> "PermissionsCheckResponse":
        from .types import PermissionsCheckResponse
        body: Dict[str, Any] = {"permissions": permissions}
        if ontology is not None:
            body["ontology"] = ontology
        resp = await self._client._request(
            "POST", "/api/v2/me/permissions/check", json_body=body)
        return _validate(PermissionsCheckResponse, resp or {})


__all__ = [
    "WeaveAsyncClient",
    "AsyncOntologiesAPI",
    "AsyncObjectsAPI",
    "AsyncActionsAPI",
    "AsyncObjectSetsAPI",
    "AsyncFunctionsAPI",
    "AsyncTransactionsAPI",
    "AsyncReactionsAPI",
    "AsyncNotificationsAPI",
    "AsyncDashboardsAPI",
    "AsyncPermissionRequestsAPI",
    "AsyncPermissionsAPI",
    "AsyncSessionsAPI",
]
