# WebSocket Subscriptions

Weave exposes a WebSocket subscription endpoint for clients that want
bidirectional flow control, low-latency reconnect with cursor-based
replay, and integration with the Python `WeaveAsyncClient.objects`
surface. The WS endpoint and the SSE endpoint share the same NATS
broadcast hub and the same policy / `where`-clause filtering — pick WS
when you need cursor replay or want to stay inside the async SDK; pick
SSE when a saved-ObjectSet `where` filter is the right fan-in shape and
you want a pure HTTP transport (no `Upgrade:` requirement on proxies).

For the SSE counterpart see [`sse.md`](sse.md).

## Endpoint

```
GET /api/v2/ontologies/{ontologyApiName}/subscriptions/ws
```

The handler upgrades the HTTP request to WebSocket (`Upgrade:
websocket` + `Connection: Upgrade`). On success the server picks up the
incoming `subscribe` frame and starts streaming events.

### Query Parameters

| Name | Required | Default | Description |
|------|----------|---------|-------------|
| `since` | no | (none) | Resume from a known cursor. If omitted, the server starts at the latest position when the `subscribe` frame is processed. |
| `objectType` | no | (all) | Restrict the stream to a single ObjectType. Multiple may be supplied as a comma-separated list. |

The cursor is monotonically increasing across all ObjectTypes in the
ontology, so a resumed connection that filters by `objectType` still
catches up correctly against the global ordering.

### Response Headers

| Header | Value |
|--------|-------|
| `Sec-WebSocket-Accept` | per RFC 6455 |
| `X-Weave-Replay-Window-Limit` | server-side ring-buffer cap (events) |
| `X-Weave-Replay-Window-Ttl` | server-side ring-buffer TTL (seconds) |

A reconnecting client uses the limit + TTL to decide between "resume
from cursor" and "snapshot the world and restart".

## Frame Schema

The wire is a sequence of JSON frames. Three frame types travel
client → server, three travel server → client.

### Client → Server

```json
{ "type": "subscribe",
  "where": { ... optional Bleve-DSL filter ... },
  "objectTypes": ["Customer", "Order"]   // optional, narrows the stream
}
```

```json
{ "type": "ack", "cursor": 42 }
```

```json
{ "type": "unsubscribe" }
```

`ack` lets the client tell the server how far it has durably persisted
the stream so the broadcast hub can drop entries the client no longer
needs to replay on reconnect. `unsubscribe` is a polite shutdown — the
server closes the socket with `1000` after flushing pending events.

### Server → Client

```json
{ "type": "objectChanged",
  "cursor": 43,
  "ontologyRid": "ri.ontology.main.ontology.northwind",
  "objectTypeRid": "ri.ontology.main.objectType.customer",
  "primaryKey": "ALFKI",
  "eventType": "CREATE" | "MODIFY" | "DELETE",
  "wireObject": { ... v2 wire format ... } }
```

```json
{ "type": "heartbeat", "cursor": 43, "wallClock": "2026-05-29T00:21:00Z" }
```

```json
{ "type": "error",
  "code": "WEAVE_BACKPRESSURE_DROPPED",
  "message": "client failed to ack within the configured window",
  "cursor": 43 }
```

Heartbeat is sent on the configured keepalive interval (default 25 s)
regardless of event traffic. `error` frames carry the same
`errorCode` / `errorName` shape as the JSON-RPC and REST surfaces so a
single SDK error mapper covers all three.

## Cursor-Based Replay and Reconnection

The server maintains a per-connection cursor that monotonically
increases with every `objectChanged` frame. On reconnect the client
passes the last observed cursor through the `since=` query parameter.
The broadcast hub maintains a global ring buffer (default 5 minutes /
10 000 events) and serves the missed window before resuming the live
stream.

### Reconnection Flow

```
1. socket close → start exponential backoff (250 ms, 500, 1 s, 2 s, ...)
2. backoff elapsed → reopen with ?since=<last cursor>
3. server flushes ring-buffer entries above <last cursor> as
   objectChanged frames
4. server sends a single heartbeat frame to mark "caught up"
5. live stream resumes
```

If the requested cursor is below the buffer's tail (TTL or capacity
exceeded), the server closes with a typed `1011`
`WEAVE_REPLAY_WINDOW_EXHAUSTED` error so the client knows to snapshot
the world and restart from the live edge instead of silently dropping
events.

## Backpressure

Unlike SSE, WebSocket is bidirectional: the server requires the client
to `ack` cursors within a configured window. If the client falls
behind, the server first sends `error: WEAVE_BACKPRESSURE_WARNING` so
the client can drain its queue, and on a second offense closes the
socket with `1009` `WEAVE_BACKPRESSURE_DROPPED`. This is intentional —
silently buffering on the server would let one slow consumer take down
the broadcast hub for the rest of the fleet.

## Server-Side Filtering

The `subscribe` frame's optional `where` clause is composed into the
broadcast hub's per-connection filter and re-applied to every event
before delivery. Policy filtering (Gap-S1 / S2 / S3 — see
[`../security/policy-model.md`](../security/policy-model.md)) runs on
every event regardless: a user whose `policy_engine.Evaluate` query
excludes the row will never receive the event even if the row matches
the `where` clause.

## Per-User Connection Limits

The same per-user connection key used by SSE applies. Default cap is
configurable; the server returns `429 Too Many Connections` (with a
`Retry-After` header) when the user already has the maximum sockets
open.

## Architecture

The endpoint is wired by `pkg/subscriptions/`. The event pipeline is
shared with SSE:

```
Action.Apply
  └── EditBatch → NATS JetStream subject (per-ontology durable consumer)
        └── pkg/funnel/broadcast.go → broadcast hub
              ├── pkg/oss/subscribe_sse.go → SSE clients
              └── pkg/subscriptions/ws_handler.go → WebSocket clients
```

The ring buffer that backs `since=` replay lives in
`pkg/subscriptions/replay_buffer.go` and is exercised by
`pkg/subscriptions/replay_us418_test.go`.

## Client Implementation

### Python SDK (recommended)

`WeaveAsyncClient.objects.subscribe` opens the socket, handles
exponential-backoff reconnect with cursor preservation, and yields each
event as a typed `ChangeEvent`. See
[cookbook chapter 6](../cookbook/06-ws-subscription.md).

```python
import asyncio
from weave_client import WeaveAsyncClient

async def main():
    async with WeaveAsyncClient() as client:
        async for ev in client.objects.subscribe(
            ontology="northwind",
            object_types=["Customer"],
        ):
            print(ev.cursor, ev.event_type, ev.wire_object)

asyncio.run(main())
```

### Browser WebSocket

```js
const ws = new WebSocket(
  "wss://example.com/api/v2/ontologies/northwind/subscriptions/ws?since=42",
);

ws.addEventListener("open", () => {
  ws.send(JSON.stringify({
    type: "subscribe",
    objectTypes: ["Customer"],
  }));
});

ws.addEventListener("message", (frame) => {
  const msg = JSON.parse(frame.data);
  if (msg.type === "objectChanged") {
    // ... apply update, then ack
    ws.send(JSON.stringify({ type: "ack", cursor: msg.cursor }));
  }
});
```

The Python recipe abstracts the exponential backoff, cursor tracking,
and ack pacing; in the browser you wire those yourself or use a
companion library.

## Conditional Availability

The WebSocket endpoint requires NATS JetStream (configured via
`NATS_URL`) and the in-process broadcast hub. In degraded standalone
mode (no NATS), the endpoint returns `503 Service Unavailable` and the
SSE endpoint follows the same fallback. Both paths surface health
through `/health/ready`.
