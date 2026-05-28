# SDK Cookbook

Practical recipes for the Weave Python SDK (`weave-client`). Each chapter
ships a runnable companion script under the same directory so you can copy,
edit, and execute against a live `make dev` server.

| # | Chapter | Script | Pattern |
|---|---------|--------|---------|
| 1 | [Async](01-async.md) | [`01-async.py`](01-async.py) | Concurrent reads via `WeaveAsyncClient` and `asyncio.gather` |
| 2 | [Retry](02-retry.md) | [`02-retry.py`](02-retry.py) | Tuning `RetryPolicy` for flaky upstreams and 429 storms |
| 3 | [Batching](03-batching.md) | [`03-batching.py`](03-batching.py) | Bulk Action application with `apply_batch` and chunking |
| 4 | [Subscription](04-subscription.md) | [`04-subscription.py`](04-subscription.py) | SSE consumer with `lastEventId` resume and exponential backoff |
| 5 | [RAG](05-rag.md) | [`05-rag.py`](05-rag.py) | Retrieval-augmented generation grounded in ontology objects |
| 6 | [WS Subscription](06-ws-subscription.md) | [`06-ws-subscription.py`](06-ws-subscription.py) | `WeaveAsyncClient.objects.subscribe` with cursor + replay (US-418) |
| 7 | [Builders & typed errors](07-builders.md) | [`07-builders.py`](07-builders.py) | `ObjectSetBuilder` / `AggregationAPI` / `criteria_builders` + branching on `WeaveValidationError` / `WeaveVersionedLookupError` (Gap-D1+D2+A3+T4) |

## Conventions

Every script honours the same two environment variables so you can point
recipes at any deployment without editing code:

```bash
export WEAVE_BASE_URL=http://localhost:9117   # default
export WEAVE_TOKEN=eyJhbGciOi...              # optional under AUTH_MODE=dev
python3 docs/cookbook/01-async.py
```

The recipes assume a populated ontology (`make dev` + load Northwind or
Chinook fixtures). Each script prints a friendly hint when the server has
no ontologies yet rather than crashing.

Recipes are intentionally dependency-light: the only third-party imports
are those already pulled in by `weave-client` itself (`httpx`, `pydantic`).
