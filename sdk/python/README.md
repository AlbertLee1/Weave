# weave-client

Python SDK for the [Weave](../../README.md) ontology engine.

A hand-written, typed wrapper around the Weave REST API covering both the
v1 core (auth, ontology metadata, object retrieval / search, action apply)
and the v2 Deep Parity surface that landed in Phase 6 – 8:

- **`ObjectSetBuilder`** — chainable composable ObjectSets (no more raw dict)
- **`AggregationAPI`** — builders for every metric + groupBy variant with
  typed responses (`accuracy=ACCURATE|APPROXIMATE`)
- **`TimeSeriesAPI`** — seven per-property TimeSeries endpoints, including
  the `transform` path that pushes `resample` down to the server
- **`AttachmentsAPI`** — global + per-property attachment upload / read
- **`criteria_builders`** — submission-criteria DSL (`parameter_compare`
  cross-field comparison + `and_` / `or_` / `not_`)
- **`WeaveAsyncClient`** — full async mirror of every namespace
- Typed exceptions for branching on contract instead of prose:
  `WeaveValidationError` (400 `InvalidParameter:submissionCriteria`),
  `WeaveVersionedLookupError` (501 `VersionedLookupNotSupported`),
  `WeaveAuthError` (401/403, plus session management endpoints)

See [`docs/python-sdk.md` § v2 APIs](../../docs/python-sdk.md#v2-apis) for
the full reference and [`docs/cookbook/07-builders.md`](../../docs/cookbook/07-builders.md)
for a runnable end-to-end example covering all of the above.

## Install

The package targets Python 3.9+. From a clone of the repository:

```bash
cd sdk/python
pip install -e .
# or, with the test extras:
pip install -e .[test]
```

The runtime dependencies are `httpx>=0.25` and `pydantic>=2.0`. The SDK will
also fall back to the stdlib `urllib.request` transport if `httpx` is not
installed, which is convenient for tests and constrained environments.

## Quickstart

```python
from weave_client import Client

# 1. Construct a client. Use either an access token (JWT from /api/auth/login)
#    or an API key (starts with "wvk_").
weave = Client("http://localhost:9117", access_token="eyJhbGciOi…")

# 2. Browse ontologies.
for ontology in weave.ontologies.list():
    print(ontology.api_name, ontology.display_name)

# 3. List objects in an ontology.
page = weave.objects.list("northwind", "Customer", page_size=25)
for customer in page.data:
    print(customer["__primaryKey"], customer.get("companyName"))

# 4. Iterate every object in a type, transparently following nextPageToken.
for customer in weave.objects.iter_all("northwind", "Customer", page_size=100):
    ...

# 5. Get a single object by primary key.
alfki = weave.objects.get("northwind", "Customer", "ALFKI")

# 6. Search.
hits = weave.objects.search("northwind", "Customer", {
    "type": "eq",
    "field": "country",
    "value": "Germany",
}, select=["customerId", "companyName", "country"])

# 7. Apply an action.
result = weave.actions.apply("northwind", "createCustomer", {
    "customerId": "WEAVE",
    "companyName": "Weave Co",
})
print(result.action_rid, len(result.edits))
```

## Authentication

There are three ways to authenticate:

```python
# 1. Pass an access token directly.
Client("http://localhost:9117", access_token="…")

# 2. Pass an API key (starts with "wvk_").
Client("http://localhost:9117", api_key="wvk_…")

# 3. Login interactively. This populates the client's access_token in place.
weave = Client("http://localhost:9117")
weave.login("admin@example.com", "password")
weave.ontologies.list()  # now authenticated
```

## Async client (US-355)

For event-loop-driven applications, `WeaveAsyncClient` mirrors the sync
`Client` API method-for-method on top of `httpx.AsyncClient`. Every method
is awaitable and the client doubles as an `async with` context manager:

```python
import asyncio
from weave_client import WeaveAsyncClient

async def main():
    async with WeaveAsyncClient("http://localhost:9117", access_token="…") as c:
        # 1. Browse ontologies.
        for ontology in await c.ontologies.list():
            print(ontology.api_name)

        # 2. Page through every customer.
        async for customer in c.objects.iter_all("northwind", "Customer"):
            print(customer["__primaryKey"])

        # 3. Apply an action.
        result = await c.actions.apply("northwind", "createCustomer", {
            "customerId": "WEAVE",
            "companyName": "Weave Co",
        })

        # 4. Stream a function's NDJSON output.
        it = await c.functions.execute_stream("northwind", "topProducts", {"limit": 100})
        async for item in it:
            print(item)

asyncio.run(main())
```

Errors raised by the async client are the same `WeaveError` /
`WeaveAuthError` / `WeaveNotFoundError` hierarchy as the sync client.

## Retry policy (US-358)

Pass `retry=RetryPolicy(...)` to either `Client` or `WeaveAsyncClient` to
opt into automatic retries with exponential backoff and full jitter:

```python
from weave_client import Client, RetryPolicy

weave = Client(
    "http://localhost:9117",
    retry=RetryPolicy(max_attempts=5, base_delay=0.2, max_delay=4.0),
)
```

Retries fire only on idempotent methods (`GET`, `HEAD`, `OPTIONS`, `PUT`,
`DELETE`) and on transient status codes (`408`, `425`, `429`, `500`, `502`,
`503`, `504`) plus transport-level errors. `POST` and `PATCH` never retry —
they may have already taken effect on the server. The server's `Retry-After`
header (delta-seconds or HTTP-date) overrides the computed backoff. Set
`max_attempts=1` to disable retries; the constructor default (no `retry=`
argument) also leaves retries off.

## Errors

Non-2xx responses raise typed exceptions:

| Status        | Exception                                |
|---------------|------------------------------------------|
| 401, 403      | `weave_client.WeaveAuthError`            |
| 404           | `weave_client.WeaveNotFoundError`        |
| anything else | `weave_client.WeaveError`                |

All exceptions carry the structured `errorCode`, `errorName`,
`errorInstanceId`, and `parameters` from the server when available.

## Tests

```bash
cd sdk/python
PYTHONPATH=. python -m unittest discover -s tests -p 'test_*.py' -v
# or, with pytest after `pip install -e .[test]`:
pytest
```
