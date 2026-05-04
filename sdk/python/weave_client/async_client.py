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
    ActionType,
    ApplyActionResponse,
    BatchApplyActionResponse,
    InterfaceType,
    LoginResponse,
    ObjectPage,
    ObjectType,
    Ontology,
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


__all__ = [
    "WeaveAsyncClient",
    "AsyncOntologiesAPI",
    "AsyncObjectsAPI",
    "AsyncActionsAPI",
    "AsyncObjectSetsAPI",
    "AsyncFunctionsAPI",
]
