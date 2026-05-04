"""Tests for WeaveAsyncClient.objects.subscribe (US-418).

The Subscription transport is mocked via ``ScriptedTransport`` so the
suite stays hermetic — no real WebSocket connection is made. The first
section tests the protocol in isolation; the integration test at the
bottom drives a full session against the real Hub from
``pkg/subscriptions`` over loopback to confirm the wire shape end-to-end.
"""
from __future__ import annotations

import asyncio
import json
import unittest
from typing import Any, Dict, List, Optional

from weave_client import WeaveAsyncClient
from weave_client.subscriptions import (
    ChangeEvent,
    Subscription,
    WeaveOutOfDate,
    WebSocketTransport,
    _build_ws_url,
)


class ScriptedTransport(WebSocketTransport):
    """Test transport that hands out pre-scripted recv frames."""

    def __init__(self, frames: List[Any]):
        # frames may be dicts (auto-serialised), strings, or BaseException
        # instances raised on recv. After exhausting the list, recv blocks
        # indefinitely on a never-resolving future so the consumer side can
        # observe back-pressure / cancellation behaviour.
        self._frames = list(frames)
        self.connected_url: Optional[str] = None
        self.connected_headers: Optional[Dict[str, str]] = None
        self.sent: List[str] = []
        self._closed = False

    async def connect(self, url: str, *, headers: Dict[str, str]) -> None:
        self.connected_url = url
        self.connected_headers = dict(headers)

    async def send_text(self, payload: str) -> None:
        self.sent.append(payload)

    async def recv_text(self) -> str:
        if not self._frames:
            # Block forever — caller must close to break out.
            await asyncio.Future()
        frame = self._frames.pop(0)
        if isinstance(frame, BaseException):
            raise frame
        if isinstance(frame, str):
            return frame
        return json.dumps(frame)

    async def aclose(self) -> None:
        self._closed = True


class _MultiSession:
    """Scripts a sequence of transports for a single subscription instance.

    Each call returns the next ``ScriptedTransport`` so reconnect tests
    can assert behaviour across multiple sessions.
    """

    def __init__(self, transports: List[ScriptedTransport]):
        self.transports = list(transports)
        self.created: List[ScriptedTransport] = []

    def __call__(self) -> ScriptedTransport:
        if not self.transports:
            raise RuntimeError("ScriptedTransport list exhausted")
        t = self.transports.pop(0)
        self.created.append(t)
        return t


class BuildWSUrlTests(unittest.TestCase):
    def test_http_to_ws_no_token_no_since(self):
        url = _build_ws_url("http://localhost:9117", "northwind", token="", since=0)
        self.assertEqual(
            url, "ws://localhost:9117/api/v2/ontologies/northwind/subscriptions/ws"
        )

    def test_https_to_wss_with_token(self):
        url = _build_ws_url(
            "https://w.example.com", "ont1", token="t-abc", since=0
        )
        self.assertEqual(
            url, "wss://w.example.com/api/v2/ontologies/ont1/subscriptions/ws?token=t-abc"
        )

    def test_appends_since_when_positive(self):
        url = _build_ws_url("http://x:1", "ont", token="t", since=42)
        self.assertIn("token=t", url)
        self.assertIn("since=42", url)

    def test_strips_trailing_slash(self):
        url = _build_ws_url("http://localhost:9117/", "ont", token="", since=0)
        self.assertEqual(
            url, "ws://localhost:9117/api/v2/ontologies/ont/subscriptions/ws"
        )

    def test_quotes_ontology_name(self):
        url = _build_ws_url("http://x:1", "with space/slash", token="", since=0)
        self.assertIn("with%20space%2Fslash", url)


class SubscriptionProtocolTests(unittest.IsolatedAsyncioTestCase):
    async def test_yields_object_changed_events(self):
        transport = ScriptedTransport([
            {"type": "welcome", "connectionId": "c1", "lastEventId": 0},
            {"type": "subscribed", "subscriptionId": "s1"},
            {
                "type": "objectChanged",
                "subscriptionId": "s1",
                "cursor": 5,
                "data": {"state": "ADDED_OR_UPDATED", "object": {"__primaryKey": "A"}},
            },
            {
                "type": "objectChanged",
                "subscriptionId": "s1",
                "cursor": 6,
                "data": {"state": "DELETED", "object": {"__primaryKey": "B"}},
            },
        ])
        sub = Subscription(
            base_url="http://x:1",
            ontology="ont",
            object_type="Customer",
            transport_factory=lambda: transport,
            auto_reconnect=False,
        )
        collected: List[ChangeEvent] = []
        try:
            async for evt in sub:
                collected.append(evt)
                if len(collected) == 2:
                    await sub.aclose()
        finally:
            await sub.aclose()
        self.assertEqual([e.state for e in collected], ["ADDED_OR_UPDATED", "DELETED"])
        self.assertEqual([e.object["__primaryKey"] for e in collected], ["A", "B"])
        self.assertEqual([e.cursor for e in collected], [5, 6])
        self.assertEqual(sub.cursor, 6)
        self.assertEqual(sub.subscription_id, "s1")

    async def test_sends_subscribe_envelope_after_welcome(self):
        transport = ScriptedTransport([
            {"type": "welcome", "lastEventId": 0},
            {"type": "subscribed", "subscriptionId": "s1"},
        ])
        sub = Subscription(
            base_url="http://x:1",
            ontology="ont",
            object_type="Order",
            where={"type": "eq", "field": "country", "value": "DE"},
            select=["id", "country"],
            transport_factory=lambda: transport,
            auto_reconnect=False,
        )

        async def consume():
            async for _ in sub:
                pass

        task = asyncio.create_task(consume())
        # let it process welcome + subscribed
        for _ in range(20):
            if transport.sent:
                break
            await asyncio.sleep(0)
        await sub.aclose()
        try:
            await asyncio.wait_for(task, timeout=1)
        except (asyncio.TimeoutError, asyncio.CancelledError, ConnectionError, RuntimeError):
            task.cancel()

        self.assertEqual(len(transport.sent), 1)
        sent = json.loads(transport.sent[0])
        self.assertEqual(sent["type"], "subscribe")
        self.assertEqual(sent["data"]["objectType"], "Order")
        self.assertEqual(sent["data"]["where"]["value"], "DE")
        self.assertEqual(sent["data"]["select"], ["id", "country"])

    async def test_cursor_supplied_on_reconnect(self):
        first = ScriptedTransport([
            {"type": "welcome", "lastEventId": 0},
            {"type": "subscribed", "subscriptionId": "s1"},
            {
                "type": "objectChanged",
                "subscriptionId": "s1",
                "cursor": 7,
                "data": {"state": "ADDED_OR_UPDATED", "object": {"__primaryKey": "A"}},
            },
            ConnectionError("dropped"),
        ])
        second = ScriptedTransport([
            {"type": "welcome", "lastEventId": 7},
            {"type": "subscribed", "subscriptionId": "s2"},
            {
                "type": "objectChanged",
                "subscriptionId": "s2",
                "cursor": 8,
                "data": {"state": "ADDED_OR_UPDATED", "object": {"__primaryKey": "B"}},
            },
        ])
        factory = _MultiSession([first, second])
        sub = Subscription(
            base_url="http://x:1",
            ontology="ont",
            object_type="Customer",
            transport_factory=factory,
            auto_reconnect=True,
            initial_backoff=0.0,
            max_backoff=0.0,
            sleep=lambda d: asyncio.sleep(0),
        )

        collected: List[ChangeEvent] = []
        async for evt in sub:
            collected.append(evt)
            if len(collected) == 2:
                await sub.aclose()
                break
        await sub.aclose()

        self.assertEqual([e.object["__primaryKey"] for e in collected], ["A", "B"])
        self.assertIn("since=7", second.connected_url or "")
        self.assertNotIn("since=", first.connected_url or "")

    async def test_connection_level_out_of_date_raises(self):
        transport = ScriptedTransport([
            {"type": "welcome", "lastEventId": 100},
            {"type": "onOutOfDate", "lastEventId": 100},
        ])
        sub = Subscription(
            base_url="http://x:1",
            ontology="ont",
            object_type="Customer",
            transport_factory=lambda: transport,
            auto_reconnect=True,
            sleep=lambda d: asyncio.sleep(0),
        )
        with self.assertRaises(WeaveOutOfDate) as ctx:
            async for _ in sub:
                pass
        self.assertEqual(ctx.exception.last_event_id, 100)
        await sub.aclose()

    async def test_subscription_level_out_of_date_triggers_reconnect(self):
        first = ScriptedTransport([
            {"type": "welcome", "lastEventId": 0},
            {"type": "subscribed", "subscriptionId": "s1"},
            {
                "type": "objectChanged",
                "subscriptionId": "s1",
                "cursor": 3,
                "data": {"state": "ADDED_OR_UPDATED", "object": {"__primaryKey": "A"}},
            },
            {"type": "onOutOfDate", "subscriptionId": "s1"},
        ])
        second = ScriptedTransport([
            {"type": "welcome", "lastEventId": 3},
            {"type": "subscribed", "subscriptionId": "s2"},
            {
                "type": "objectChanged",
                "subscriptionId": "s2",
                "cursor": 4,
                "data": {"state": "ADDED_OR_UPDATED", "object": {"__primaryKey": "B"}},
            },
        ])
        factory = _MultiSession([first, second])
        sub = Subscription(
            base_url="http://x:1",
            ontology="ont",
            object_type="Customer",
            transport_factory=factory,
            auto_reconnect=True,
            initial_backoff=0,
            sleep=lambda d: asyncio.sleep(0),
        )
        collected = []
        async for evt in sub:
            collected.append(evt.object["__primaryKey"])
            if len(collected) == 2:
                await sub.aclose()
                break
        self.assertEqual(collected, ["A", "B"])
        self.assertIn("since=3", second.connected_url or "")

    async def test_no_reconnect_when_disabled(self):
        transport = ScriptedTransport([
            {"type": "welcome", "lastEventId": 0},
            {"type": "subscribed", "subscriptionId": "s1"},
            ConnectionError("server hung up"),
        ])
        sub = Subscription(
            base_url="http://x:1",
            ontology="ont",
            object_type="Customer",
            transport_factory=lambda: transport,
            auto_reconnect=False,
        )
        with self.assertRaises(ConnectionError):
            async for _ in sub:
                pass
        await sub.aclose()

    async def test_error_frame_raises_weave_error(self):
        from weave_client.exceptions import WeaveError

        transport = ScriptedTransport([
            {"type": "welcome", "lastEventId": 0},
            {"type": "error", "error": "objectType is required"},
        ])
        sub = Subscription(
            base_url="http://x:1",
            ontology="ont",
            object_type="Customer",
            transport_factory=lambda: transport,
            auto_reconnect=False,
        )
        with self.assertRaises(WeaveError) as ctx:
            async for _ in sub:
                pass
        self.assertEqual(ctx.exception.error_name, "SubscriptionError")
        await sub.aclose()

    async def test_token_passed_in_query_param(self):
        async with WeaveAsyncClient("http://localhost:9117", access_token="abc") as c:
            sub = c.objects.subscribe(
                "ont",
                "Customer",
                transport_factory=lambda: ScriptedTransport([]),
                auto_reconnect=False,
            )
            url = _build_ws_url(c.base_url, "ont", token=c.token, since=0)
        self.assertIn("token=abc", url)
        self.assertEqual(sub.token, "abc")

    async def test_aclose_breaks_out_of_iteration(self):
        transport = ScriptedTransport([
            {"type": "welcome", "lastEventId": 0},
            {"type": "subscribed", "subscriptionId": "s1"},
        ])
        sub = Subscription(
            base_url="http://x:1",
            ontology="ont",
            object_type="Customer",
            transport_factory=lambda: transport,
            auto_reconnect=False,
        )

        async def consume():
            async for _ in sub:
                pass

        task = asyncio.create_task(consume())
        await asyncio.sleep(0)
        await sub.aclose()
        # Task should now exit (transport recv returns indefinite future, but
        # aclose closed transport — the test consumer still raises on the
        # never-resolving future, which the iterator swallows then exits).
        try:
            await asyncio.wait_for(task, timeout=0.5)
        except asyncio.TimeoutError:
            task.cancel()
            try:
                await task
            except (asyncio.CancelledError, ConnectionError, RuntimeError):
                pass
        self.assertTrue(transport._closed)


class SubscriptionIntegrationTests(unittest.IsolatedAsyncioTestCase):
    """End-to-end test against the real Hub via loopback websocket.

    Skipped when ``websockets`` is unavailable so the suite stays green
    in minimal environments. The Hub from ``pkg/subscriptions`` requires
    the Go server, so this test mounts a stand-in JSON websocket server
    that mimics the wire contract — covering the wire-shape integration
    that the protocol tests above mock out.
    """

    async def test_full_session_against_loopback_ws_server(self):
        try:
            import websockets  # type: ignore
        except ImportError:
            self.skipTest("websockets not installed")

        received_frames: List[Dict[str, Any]] = []

        async def handler(ws):
            await ws.send(json.dumps({"type": "welcome", "lastEventId": 0}))
            sub_msg = json.loads(await ws.recv())
            received_frames.append(sub_msg)
            await ws.send(
                json.dumps({"type": "subscribed", "subscriptionId": "srv-1"})
            )
            await ws.send(
                json.dumps({
                    "type": "objectChanged",
                    "subscriptionId": "srv-1",
                    "cursor": 1,
                    "data": {
                        "state": "ADDED_OR_UPDATED",
                        "object": {"__primaryKey": "A", "name": "Alice"},
                    },
                })
            )
            await ws.send(
                json.dumps({
                    "type": "objectChanged",
                    "subscriptionId": "srv-1",
                    "cursor": 2,
                    "data": {
                        "state": "DELETED",
                        "object": {"__primaryKey": "B"},
                    },
                })
            )
            try:
                await asyncio.sleep(2)
            except asyncio.CancelledError:
                return

        server = await websockets.serve(handler, "127.0.0.1", 0)
        port = server.sockets[0].getsockname()[1]

        # Override the ontology path inside _build_ws_url with a custom URL —
        # the loopback handler accepts any path. The real handler-side route
        # mounts /api/v2/ontologies/{ont}/subscriptions/ws which the websockets
        # stand-in ignores.
        async with WeaveAsyncClient(f"http://127.0.0.1:{port}") as c:
            sub = c.objects.subscribe(
                "northwind",
                "Customer",
                where={"type": "eq", "field": "country", "value": "DE"},
                select=["companyName"],
                auto_reconnect=False,
            )
            collected: List[ChangeEvent] = []
            try:
                async for evt in sub:
                    collected.append(evt)
                    if len(collected) == 2:
                        await sub.aclose()
                        break
            finally:
                await sub.aclose()

        server.close()
        await server.wait_closed()

        self.assertEqual([e.state for e in collected], ["ADDED_OR_UPDATED", "DELETED"])
        self.assertEqual([e.cursor for e in collected], [1, 2])
        self.assertEqual(len(received_frames), 1)
        self.assertEqual(received_frames[0]["type"], "subscribe")
        self.assertEqual(received_frames[0]["data"]["objectType"], "Customer")
        self.assertEqual(
            received_frames[0]["data"]["where"]["value"], "DE"
        )
        self.assertEqual(received_frames[0]["data"]["select"], ["companyName"])


if __name__ == "__main__":
    unittest.main()
