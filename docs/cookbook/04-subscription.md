# Chapter 4 — Subscription (SSE)

Weave streams object-edit events over Server-Sent Events. Open a TCP
connection to `/api/v2/ontologies/{ontology}/objectSets/{rid}/subscribe`,
keep `lastEventId` so reconnects can resume from the right point in the
ring buffer, and use a `where` clause inside the saved ObjectSet to
filter the firehose down to what you care about.

## Why SSE and not WebSockets?

Edits flow one direction (server → client), each message is JSON, and
the browser `EventSource` API is a stable two-line wrapper. WebSockets
would buy bidirectional framing the SDK doesn't need and lose the
built-in reconnect / replay semantics SSE gives us for free. See
[`docs/subscriptions/sse.md`](../subscriptions/sse.md) for the full wire
contract; this chapter is the consumer-side recipe.

## Prerequisites

1. **NATS running.** Subscriptions require JetStream — `make docker-up`
   starts it locally. A server without NATS responds with
   `SSESubscribeNotConfigured` (HTTP 500).
2. **A saved ObjectSet RID.** Subscriptions attach to a previously
   `createTemporary`'d ObjectSet so the server has a stable filter to
   apply per-event:

   ```python
   resp = client.objectsets.create_temporary(
       "northwind",
       {
           "type": "filter",
           "objectSet": {"type": "base", "objectType": "Order"},
           "where": {"type": "eq", "field": "country", "value": "Germany"},
       },
   )
   object_set_rid = resp["objectSetRid"]
   ```

   The server filters every edit through this ObjectSet's `where` clause
   before delivery — events that don't match are silently dropped, so
   the client sees only what it asked for.

## Minimal consumer

```python
import httpx, json

url = f"{base_url}/api/v2/ontologies/{ontology}/objectSets/{rid}/subscribe"
headers = {"Authorization": f"Bearer {token}"} if token else {}
last_event_id: str | None = None

with httpx.stream("GET", url, headers=headers, timeout=None) as resp:
    resp.raise_for_status()
    event_id = ""
    data_lines: list[str] = []
    for raw in resp.iter_lines():
        if raw == "":  # blank line terminates an event frame
            if data_lines:
                last_event_id = event_id or last_event_id
                payload = json.loads("\n".join(data_lines))
                handle(payload)
            event_id = ""
            data_lines = []
            continue
        if raw.startswith(":"):       # heartbeat / comment — ignore
            continue
        if raw.startswith("id: "):
            event_id = raw[4:]
        elif raw.startswith("data: "):
            data_lines.append(raw[6:])
```

Each frame is a JSON object with two fields:

- `eventType`: `"ADDED_OR_UPDATED"` or `"DELETED"`
- `object`: the affected object in `WireObject` shape
  (`__primaryKey`, `__apiName`, plus all selected properties)

## Reconnection with `lastEventId`

The browser `EventSource` reconnects automatically; for a Python
consumer you have to do it yourself. Track `last_event_id`, and on
disconnect re-attach the same RID with the `lastEventId` query param so
the server replays anything in the ring buffer (capacity ≈ 1024 events,
24h durable in JetStream):

```python
def url_with_resume(base: str, ont: str, rid: str, last_id: str | None) -> str:
    qs = f"?lastEventId={last_id}" if last_id else ""
    return f"{base}/api/v2/ontologies/{ont}/objectSets/{rid}/subscribe{qs}"
```

## Backoff strategy

Between reconnects, sleep with exponential backoff capped at 30s and
**reset on successful `onopen`**:

| Attempt | Delay |
|---|---|
| 1 | 1s |
| 2 | 2s |
| 3 | 4s |
| 4 | 8s |
| 5 | 16s |
| 6+ | 30s (cap) |

```python
delay = 1.0
while True:
    try:
        consume_one_session(...)        # blocks until disconnect
        delay = 1.0                     # successful session — reset
    except (httpx.HTTPError, ConnectionError):
        time.sleep(delay)
        delay = min(delay * 2, 30.0)
```

## Heartbeats

The server emits `:ping` SSE comments every 30 seconds so idle proxies
don't kill the connection. Comment lines start with `:` and the loop
above already discards them.

## Per-user connection cap

Default cap: 10 concurrent SSE sessions per user. Exceeding it returns
`SSEConnectionLimitExceeded` (HTTP 429). Don't open one stream per
ObjectSet — multiplex by widening the saved ObjectSet's filter and
demuxing client-side instead.

## Common pitfalls

- **`Last-Event-ID` header is dropped through some proxies.** The query
  param fallback (`?lastEventId=N`) survives every proxy we've tested.
- **`EventSource` in the browser cannot send custom headers.** That's
  why the server accepts the query param; pass the auth token in a
  cookie or as a query arg, not in `Authorization`.
- **Blocking on `iter_lines` defeats `httpx.AsyncClient`.** For async
  consumers use `aiter_lines()` inside an async-with stream block.
- **Don't poll instead of subscribing.** A 1-second poll on an
  ObjectType with frequent edits hammers the server and still misses
  intermediate edits; SSE delivers ordered, durable events for free.

See [`04-subscription.py`](04-subscription.py) for a runnable consumer
with reconnect-and-resume.
