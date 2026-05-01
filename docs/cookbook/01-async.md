# Chapter 1 — Async

`WeaveAsyncClient` mirrors the sync `Client` method-for-method on top of
`httpx.AsyncClient`. Reach for it whenever your call site is already inside
an event loop (FastAPI, an asyncio bot, a notebook with `await` enabled)
**or** when you want to fan many independent requests out concurrently.

## When async helps — and when it doesn't

Async is a concurrency primitive, not a speedup. A single async call to
`/api/v2/...` is no faster than a single sync call. The win shows up when
you have **many independent network round-trips** that can run in parallel:

| Workload | Use sync | Use async |
|---|---|---|
| Load one Customer, then update one field | yes | no benefit |
| Walk every Customer page sequentially | yes (already streams) | no benefit |
| Hydrate 50 ObjectTypes in one go | no — serial latency dominates | **yes — `asyncio.gather`** |
| Web framework handler that already `awaits` | no — blocks the loop | **yes** |

## Construction

```python
import asyncio
from weave_client import WeaveAsyncClient

async def main():
    async with WeaveAsyncClient(
        "http://localhost:9117",
        access_token="...",   # optional under AUTH_MODE=dev
    ) as client:
        page = await client.objects.list("northwind", "Customer", page_size=25)
        for row in page.data:
            print(row["__primaryKey"])

asyncio.run(main())
```

The `async with` form runs `aclose()` on exit so the underlying
`httpx.AsyncClient`'s connection pool is released. If you instead bind the
client to module scope, call `await client.aclose()` from your shutdown
hook.

## Concurrent fan-out

```python
import asyncio
from weave_client import WeaveAsyncClient

async def hydrate_object_types(client, ontology, names):
    coros = [
        client.ontologies.get_object_type_full_metadata(ontology, n)
        for n in names
    ]
    return dict(zip(names, await asyncio.gather(*coros)))
```

`asyncio.gather` schedules every coroutine on the same loop; httpx's
connection pool keeps reused TCP sockets warm so the practical limit is
the server's concurrency, not the SDK's. For very wide fan-outs (>100
concurrent requests) wrap the calls in an `asyncio.Semaphore` to keep the
pool from thrashing.

## Async iteration

`AsyncObjectsAPI.iter_all` returns an `async for`-able generator that
walks `nextPageToken` for you:

```python
async for row in client.objects.iter_all("northwind", "Customer", page_size=200):
    handle(row)
```

`AsyncFunctionsAPI.execute_stream` does the same shape for NDJSON
streaming Functions: each `{"item": ...}` line is yielded as it arrives,
and a terminal `{"error": ...}` line raises a typed `WeaveError`.

## Common pitfalls

- **Don't share clients across event loops.** `WeaveAsyncClient` binds a
  pool to the loop that constructs it. Re-instantiate per-task or per-test
  to avoid `RuntimeError: <httpcore connection> is bound to a different loop`.
- **Cancel scopes propagate.** If your enclosing task is cancelled, the
  in-flight HTTP request is cancelled too — you don't need to wire any
  shutdown plumbing.
- **Retries respect the loop.** `RetryPolicy` configured on the async
  transport uses `asyncio.sleep` for backoff, so the loop stays
  responsive while the SDK waits.

See [`01-async.py`](01-async.py) for a runnable end-to-end example.
