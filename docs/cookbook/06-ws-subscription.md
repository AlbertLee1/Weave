# Chapter 6 — WebSocket subscription with cursor + replay (US-418)

The Python SDK ships a first-class `subscribe()` method on
`WeaveAsyncClient.objects` that opens a WebSocket against
`/api/v2/ontologies/{ontology}/subscriptions/ws`, sends a `subscribe`
frame, and yields each `objectChanged` event as a typed
`ChangeEvent`. Auto-reconnect resumes from the most recent cursor via
`?since=<n>` so a brief disconnect silently replays the missed window
(US-380; default 5 minute / 10 000 event sliding buffer).

This is the canonical pattern for Python consumers. The SSE recipe in
[chapter 4](04-subscription.md) is still relevant when a saved
ObjectSet's `where` filter is the right fan-in shape, but the
WebSocket path is leaner for "give me every change to this
ObjectType, optionally filtered by a `where` clause" and integrates
with the rest of the async SDK surface.

## Prerequisites

Install the optional `ws` extra:

```bash
pip install 'weave-client[ws]'
# or for editable installs:
pip install -e '.[ws]'
```

The base SDK keeps `websockets` out of `dependencies` so HTTP-only
consumers don't pay the install cost. `subscribe()` lazy-imports
`websockets` on first use; calling it without the package installed
raises a `RuntimeError` with a clear `pip install` hint.

## Minimal consumer

```python
import asyncio
from weave_client import WeaveAsyncClient

async def main():
    async with WeaveAsyncClient("http://localhost:9117", access_token="…") as c:
        async with c.objects.subscribe("northwind", "Customer") as sub:
            async for evt in sub:
                print(evt.state, evt.object.get("__primaryKey"), "cursor=", evt.cursor)

asyncio.run(main())
```

The `async with` form closes the underlying transport deterministically
when the iterator exits (or if the caller raises). For long-running
consumers you can keep the subscription open indefinitely:

```python
sub = c.objects.subscribe("northwind", "Customer")
async for evt in sub:
    handle(evt)  # never returns
```

## Filtering with `where` and `select`

```python
sub = c.objects.subscribe(
    "northwind",
    "Order",
    where={"type": "eq", "field": "country", "value": "Germany"},
    select=["orderID", "customerID", "freight"],
)
```

The `where` clause is the same Weave WhereClause the synchronous
`objects.search` accepts. `select` projects the per-event payload to
the listed properties.

## Cursor + replay

Every `ChangeEvent.cursor` is a strictly monotonic id assigned by the
server's replay log. The SDK tracks the highest seen cursor on the
subscription instance (`sub.cursor`) and supplies it as
`?since=<cursor>` on every reconnect:

```
ws://host/api/v2/ontologies/northwind/subscriptions/ws?token=…&since=42
```

If the cursor falls outside the server's replay window (default 5
minutes / 10 000 events), the server emits a connection-level
`onOutOfDate` frame which the SDK surfaces as a `WeaveOutOfDate`
exception. Callers should refresh full state before re-subscribing:

```python
from weave_client import WeaveOutOfDate

while True:
    try:
        async with c.objects.subscribe("nw", "Customer") as sub:
            async for evt in sub:
                handle(evt)
    except WeaveOutOfDate:
        # Cursor predates the live window — reseed from a full list call.
        await reseed_local_cache(c)
        continue
```

## Auto-reconnect

`auto_reconnect=True` (the default) wraps the entire session in an
exponential-backoff loop:

| Attempt | Delay |
|---|---|
| 1 | 1 s |
| 2 | 2 s |
| 3 | 4 s |
| 4 | 8 s |
| 5 | 16 s |
| 6+ | 30 s (cap) |

The backoff resets on every successful event so a healthy connection
that flaps occasionally never escalates the delay. Subscription-level
`onOutOfDate` (the per-subscription send buffer overflowed) triggers
the same reconnect path so the resume cursor replays the dropped
window without explicit caller handling.

Set `auto_reconnect=False` if you want to handle disconnects yourself —
the iterator then re-raises the underlying exception:

```python
sub = c.objects.subscribe("nw", "Customer", auto_reconnect=False)
async for evt in sub:
    ...
# falls through with ConnectionError on disconnect
```

## Closing cleanly

The async context manager is the recommended exit path. From inside
the loop body, `await sub.aclose()` followed by `break` is also
clean:

```python
async for evt in sub:
    if should_stop(evt):
        await sub.aclose()
        break
```

## Common pitfalls

- **`?token=` is mandatory in `AUTH_MODE=token`.** The server's
  WebSocket handler reads the bearer from `?token=` (browsers can't
  send `Authorization` headers on a WebSocket upgrade). The SDK does
  this automatically when `WeaveAsyncClient(..., access_token="…")`
  is set; passing the token via `Authorization` headers does NOT
  work.
- **One subscription per ObjectType per connection.** The Hub's
  `MaxSubscriptionsPerConnection` cap is 10. For multi-ObjectType
  feeds, open one Subscription per type — they each get their own
  WebSocket and reconnect independently.
- **`where` clauses that always evaluate false produce silence.** If
  no events arrive, double-check the filter against
  `client.objects.search(...)` first — the same clause applies in
  both places.

See [`06-ws-subscription.py`](06-ws-subscription.py) for a runnable
consumer with reconnect-and-resume.
