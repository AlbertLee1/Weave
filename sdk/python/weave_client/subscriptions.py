"""WebSocket subscription support for WeaveAsyncClient (US-418).

Connects to the per-ontology WebSocket endpoint
``/api/v2/ontologies/{ontology}/subscriptions/ws`` and exposes the live
``objectChanged`` stream as an async iterator. Cursor + replay (US-380)
is wired in: every ``objectChanged`` envelope's ``cursor`` is tracked
and supplied as ``?since=<cursor>`` on reconnect so a brief disconnect
silently replays the missed window. A connection-level ``onOutOfDate``
(cursor outside the server's 5-minute / 10000-event window) raises
:class:`WeaveOutOfDate` so the caller can refresh full state before
re-subscribing.

Usage::

    import asyncio
    from weave_client import WeaveAsyncClient

    async def main():
        async with WeaveAsyncClient("http://localhost:9117", access_token="…") as c:
            async with c.objects.subscribe("northwind", "Customer") as sub:
                async for evt in sub:
                    print(evt.state, evt.object.get("__primaryKey"))

    asyncio.run(main())

The default transport uses the ``websockets`` library (lazy-imported so
SDK consumers that never call subscribe don't need it). Tests inject a
custom transport via ``transport_factory=`` to script message
sequences without a real WebSocket.
"""
from __future__ import annotations

import asyncio
import json
from dataclasses import dataclass, field
from typing import Any, AsyncIterator, Awaitable, Callable, Dict, List, Optional
from urllib.parse import urlencode, urlsplit

from ._http import quote_path
from .exceptions import WeaveError


@dataclass(frozen=True)
class ChangeEvent:
    """A single object change event from a Weave subscription.

    ``cursor`` is the monotonic event id stamped by the server's replay
    log (US-380). Persist the most recent value if you need to resume
    from a specific point in a future Subscription instance.
    """

    state: str
    object: Dict[str, Any]
    cursor: int = 0
    subscription_id: str = ""


class WeaveOutOfDate(WeaveError):
    """Raised when the server signals connection-level ``onOutOfDate``.

    The saved cursor has fallen outside the server's replay window;
    callers should refresh full state before re-subscribing.
    """

    def __init__(self, last_event_id: int = 0):
        super().__init__(
            status_code=0,
            error_code="WEAVE_SUBSCRIPTION_OUT_OF_DATE",
            error_name="OutOfDate",
            parameters={"lastEventId": last_event_id},
        )
        self.last_event_id = last_event_id


class WebSocketTransport:
    """Pluggable WebSocket transport.

    Subclass and pass a factory via :class:`Subscription.transport_factory`
    to script message sequences in tests. The default
    :class:`WebsocketsTransport` uses the ``websockets`` library.
    """

    async def connect(self, url: str, *, headers: Dict[str, str]) -> None:
        raise NotImplementedError

    async def send_text(self, payload: str) -> None:
        raise NotImplementedError

    async def recv_text(self) -> str:
        raise NotImplementedError

    async def aclose(self) -> None:
        raise NotImplementedError


class WebsocketsTransport(WebSocketTransport):
    """Default transport built on the ``websockets`` library."""

    def __init__(self) -> None:
        self._ws: Any = None

    async def connect(self, url: str, *, headers: Dict[str, str]) -> None:
        try:
            import websockets  # type: ignore
        except ImportError as e:  # pragma: no cover - exercised when dep is absent
            raise RuntimeError(
                "the 'websockets' package is required for subscribe(); "
                "install with: pip install websockets"
            ) from e
        header_pairs = list(headers.items())
        try:
            self._ws = await websockets.connect(url, additional_headers=header_pairs)
        except TypeError:
            # websockets <12 uses ``extra_headers`` instead of ``additional_headers``.
            self._ws = await websockets.connect(url, extra_headers=header_pairs)

    async def send_text(self, payload: str) -> None:
        await self._ws.send(payload)

    async def recv_text(self) -> str:
        msg = await self._ws.recv()
        if isinstance(msg, (bytes, bytearray)):
            return msg.decode("utf-8")
        return msg

    async def aclose(self) -> None:
        if self._ws is not None:
            try:
                await self._ws.close()
            finally:
                self._ws = None


def _build_ws_url(
    base_url: str, ontology: str, *, token: str, since: int
) -> str:
    sp = urlsplit(base_url.rstrip("/"))
    scheme = "wss" if sp.scheme == "https" else "ws"
    netloc = sp.netloc or sp.path
    path = f"/api/v2/ontologies/{quote_path(ontology)}/subscriptions/ws"
    params: Dict[str, str] = {}
    if token:
        params["token"] = token
    if since > 0:
        params["since"] = str(since)
    qs = ("?" + urlencode(params)) if params else ""
    return f"{scheme}://{netloc}{path}{qs}"


@dataclass
class Subscription:
    """Async-iterable object-change subscription with auto-reconnect.

    Acquire via :meth:`AsyncObjectsAPI.subscribe`. The instance is also
    an async context manager so the underlying transport closes
    deterministically on exit::

        async with c.objects.subscribe("nw", "Customer") as sub:
            async for evt in sub:
                ...
    """

    base_url: str
    ontology: str
    object_type: str
    where: Optional[Dict[str, Any]] = None
    select: Optional[List[str]] = None
    token: str = ""
    transport_factory: Optional[Callable[[], WebSocketTransport]] = None
    auto_reconnect: bool = True
    initial_backoff: float = 1.0
    max_backoff: float = 30.0
    backoff_factor: float = 2.0
    sleep: Optional[Callable[[float], Awaitable[None]]] = None

    _cursor: int = field(default=0, init=False, repr=False)
    _last_event_id: int = field(default=0, init=False, repr=False)
    _subscription_id: str = field(default="", init=False, repr=False)
    _transport: Optional[WebSocketTransport] = field(default=None, init=False, repr=False)
    _closed: bool = field(default=False, init=False, repr=False)

    @property
    def cursor(self) -> int:
        """Most recent event cursor observed (0 before the first event)."""
        return self._cursor

    @property
    def subscription_id(self) -> str:
        """Server-assigned subscription id from the most recent ``subscribed`` frame."""
        return self._subscription_id

    async def __aenter__(self) -> "Subscription":
        return self

    async def __aexit__(self, *exc_info: Any) -> None:
        await self.aclose()

    def __aiter__(self) -> AsyncIterator[ChangeEvent]:
        return self._iter()

    async def aclose(self) -> None:
        """Stop the subscription and close the underlying transport."""
        self._closed = True
        await self._close_transport()

    async def _close_transport(self) -> None:
        t = self._transport
        self._transport = None
        if t is not None:
            try:
                await t.aclose()
            except Exception:  # pragma: no cover - best effort
                pass

    def _make_transport(self) -> WebSocketTransport:
        factory = self.transport_factory or WebsocketsTransport
        return factory()

    async def _iter(self) -> AsyncIterator[ChangeEvent]:
        backoff = self.initial_backoff
        sleep = self.sleep or asyncio.sleep
        while not self._closed:
            reconnect = False
            try:
                async for evt in self._session():
                    backoff = self.initial_backoff
                    yield evt
                # session ended naturally — only reconnect when allowed.
                reconnect = self.auto_reconnect
            except WeaveOutOfDate:
                raise
            except asyncio.CancelledError:
                raise
            except Exception:
                # Caller-initiated close manifests as a recv-side failure on
                # the closed transport — swallow it silently rather than
                # bubbling a confusing "transport not connected" or socket
                # error past aclose().
                if self._closed:
                    return
                if not self.auto_reconnect:
                    raise
                reconnect = True
            finally:
                await self._close_transport()

            if self._closed or not reconnect:
                return
            await sleep(backoff)
            backoff = min(backoff * self.backoff_factor, self.max_backoff)

    async def _session(self) -> AsyncIterator[ChangeEvent]:
        transport = self._make_transport()
        self._transport = transport
        url = _build_ws_url(
            self.base_url, self.ontology, token=self.token, since=self._cursor
        )
        await transport.connect(url, headers={})

        first = await self._recv_envelope()
        if _is_connection_out_of_date(first):
            raise WeaveOutOfDate(int(first.get("lastEventId", 0)))
        if first.get("type") != "welcome":
            raise WeaveError(
                status_code=0,
                error_code="WEAVE_UNEXPECTED_FRAME",
                error_name="UnexpectedFrame",
                parameters={"type": first.get("type", "")},
                raw_body=json.dumps(first),
            )
        self._last_event_id = int(first.get("lastEventId", 0))

        body: Dict[str, Any] = {"objectType": self.object_type}
        if self.where is not None:
            body["where"] = self.where
        if self.select:
            body["select"] = self.select
        await transport.send_text(json.dumps({"type": "subscribe", "data": body}))

        while True:
            env = await self._recv_envelope()
            etype = env.get("type")
            if _is_connection_out_of_date(env):
                raise WeaveOutOfDate(int(env.get("lastEventId", 0)))
            if etype == "error":
                raise WeaveError(
                    status_code=0,
                    error_code="WEAVE_SUBSCRIPTION_ERROR",
                    error_name="SubscriptionError",
                    parameters={"message": env.get("error", "")},
                    raw_body=json.dumps(env),
                )
            if etype == "subscribed":
                self._subscription_id = env.get("subscriptionId", "")
                break
            # else: ignore unrelated frames

        while True:
            env = await self._recv_envelope()
            etype = env.get("type")
            if etype == "objectChanged":
                cursor = int(env.get("cursor", 0))
                if cursor > self._cursor:
                    self._cursor = cursor
                data = env.get("data") or {}
                yield ChangeEvent(
                    state=str(data.get("state", "")),
                    object=dict(data.get("object") or {}),
                    cursor=cursor,
                    subscription_id=env.get("subscriptionId", ""),
                )
                continue
            if _is_connection_out_of_date(env):
                raise WeaveOutOfDate(int(env.get("lastEventId", 0)))
            if etype == "onOutOfDate":
                # Subscription-level out-of-date: server's send buffer
                # overflowed. Resync via reconnect (the resume cursor
                # replays the missed window).
                raise ConnectionError("subscription out of date — resync")
            if etype == "error":
                raise WeaveError(
                    status_code=0,
                    error_code="WEAVE_SUBSCRIPTION_ERROR",
                    error_name="SubscriptionError",
                    parameters={"message": env.get("error", "")},
                    raw_body=json.dumps(env),
                )
            # else: ignore other frames (heartbeats, unsubscribed echoes…)

    async def _recv_envelope(self) -> Dict[str, Any]:
        if self._transport is None:
            raise RuntimeError("subscription transport is not connected")
        text = await self._transport.recv_text()
        try:
            obj = json.loads(text)
        except ValueError as e:
            raise WeaveError(
                status_code=0,
                error_code="WEAVE_INVALID_FRAME",
                error_name="InvalidFrame",
                parameters={"raw": text[:200]},
                raw_body=text,
            ) from e
        return obj if isinstance(obj, dict) else {}


def _is_connection_out_of_date(env: Dict[str, Any]) -> bool:
    """Connection-level onOutOfDate has type=onOutOfDate AND no subscriptionId."""
    return env.get("type") == "onOutOfDate" and not env.get("subscriptionId")


__all__ = [
    "ChangeEvent",
    "Subscription",
    "WeaveOutOfDate",
    "WebSocketTransport",
    "WebsocketsTransport",
]
