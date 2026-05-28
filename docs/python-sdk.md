# Python SDK

The `weave-client` package is a thin, typed Python wrapper around the
Weave REST API. It covers the MVP surface (auth, ontology metadata, object
retrieval, search, action apply) and is hand-written rather than auto-generated
from the OpenAPI spec, so it has no Java/openapi-generator dependency.

The full SDK source lives at [`sdk/python/`](../sdk/python/).

## Installation

The SDK targets Python 3.9 or newer.

```bash
# From a clone of the repository (editable install)
cd sdk/python
pip install -e .

# With the test extras
pip install -e .[test]
```

Runtime dependencies: `httpx>=0.25`, `pydantic>=2.0`. The SDK can also fall
back to `urllib.request` when `httpx` is not installed — convenient for tests,
CI without network egress, or constrained runtimes.

## Quickstart

```python
from weave_client import Client

weave = Client("http://localhost:9117", access_token="eyJhbGciOi…")

# Ontologies and types
for ont in weave.ontologies.list():
    print(ont.api_name, ont.display_name)

types = weave.ontologies.list_object_types("northwind")
customer = weave.ontologies.get_object_type("northwind", "Customer")

# Object retrieval
page = weave.objects.list("northwind", "Customer", page_size=25)
print(len(page.data), page.total_count, page.next_page_token)

for obj in weave.objects.iter_all("northwind", "Customer", page_size=100):
    print(obj["__primaryKey"])

alfki = weave.objects.get("northwind", "Customer", "ALFKI")

# Where-clause search
hits = weave.objects.search("northwind", "Customer", {
    "type": "eq", "field": "country", "value": "Germany",
})

# Action execution
result = weave.actions.apply("northwind", "createCustomer", {
    "customerId": "WEAVE", "companyName": "Weave Co",
})
print(result.action_rid, len(result.edits))
```

## API reference

### Client

```python
Client(
    base_url: str,
    *,
    access_token: str | None = None,
    api_key:      str | None = None,
    timeout:      float = 30.0,
)
```

| Property        | Type             | Notes                                         |
|-----------------|------------------|-----------------------------------------------|
| `base_url`      | `str`            | trailing slash stripped automatically         |
| `token`         | `str` (read-only)| `access_token` if set, else `api_key`         |
| `ontologies`    | `OntologiesAPI`  | metadata namespace                            |
| `objects`       | `ObjectsAPI`     | object retrieval and search                   |
| `actions`       | `ActionsAPI`     | action execution                              |
| `login(email, password)`  | `LoginResponse` | refreshes `access_token` in-place |
| `logout(refresh_token="")`| `None`          | clears the in-memory token        |

### OntologiesAPI

| Method | Endpoint | Returns |
|---|---|---|
| `list()` | `GET /api/v2/ontologies` | `List[Ontology]` |
| `get(api_name)` | `GET /api/v2/ontologies/{name}` | `Ontology` |
| `list_object_types(ontology)` | `GET /api/v2/ontologies/{name}/objectTypes` | `List[ObjectType]` |
| `get_object_type(ontology, object_type)` | `GET .../objectTypes/{type}` | `ObjectType` |

### ObjectsAPI

| Method | Endpoint | Returns |
|---|---|---|
| `list(ontology, object_type, page_size=100, page_token="", order_by="")` | `GET .../objects/{type}` | `ObjectPage` |
| `iter_all(ontology, object_type, page_size=100, order_by="")` | follows `nextPageToken` | `Iterator[WireObject]` |
| `get(ontology, object_type, primary_key)` | `GET .../objects/{type}/{pk}` | `WireObject` (`dict`) |
| `search(ontology, object_type, where, page_size=None, page_token=None)` | `POST .../objects/{type}/search` | `ObjectPage` |

`WireObject` is a plain `dict[str, Any]` with the V2 wire format
(properties flattened, plus `__rid`, `__primaryKey`, `__apiName`).

### ActionsAPI

| Method | Endpoint | Returns |
|---|---|---|
| `apply(ontology, action_type, parameters)` | `POST .../actions/{action}/apply` | `ApplyActionResponse` |

### Errors

| Exception | Status codes | Notes |
|---|---|---|
| `WeaveError` | any non-2xx | base class |
| `WeaveAuthError` | 401, 403 | missing/invalid credentials |
| `WeaveNotFoundError` | 404 | resource not found |
| `WeaveValidationError` | 400 `InvalidParameter:submissionCriteria` | typed save-time criteria validation error; carries the offending `field` path so callers can highlight the input (Gap-A3 SDK136 / commit c0bb215) |
| `WeaveVersionedLookupError` | 501 `VersionedLookupNotSupported` | typed `@vN` read rejection on the 8 ontology Get endpoints (Gap-T4 SDK118 / commit 265cffd) |

All exceptions carry `status_code`, `error_code`, `error_name`,
`error_instance_id`, `parameters`, and `raw_body`. The two typed exceptions
above subclass `WeaveError` so existing `except WeaveError:` blocks keep
working; reach for the typed class when you want to branch on the specific
contract.

## v2 APIs

The v1 sections above (`OntologiesAPI` / `ObjectsAPI` / `ActionsAPI`) stay
on the box. v2 adds the high-level builders, three Foundry-parity API
namespaces, the criteria-builder DSL, and a fully mirrored async client.

### ObjectSetBuilder

Composable ObjectSet construction (Gap-D1, commit a042fa5). Chainable
methods produce a definition dict suitable for `POST .../objectSets/load`
or `.../objectSets/createTemporary` without hand-writing the JSON.

```python
from weave_client import WeaveClient
from weave_client.objectsets import ObjectSetBuilder

client = WeaveClient("http://localhost:9117", token="...")

definition = (
    ObjectSetBuilder(client)
    .base("Employee")
    .filter({"field": "age", "op": "gt", "value": 30})
    .search_around("team")
    .with_properties({"reportsCount": {"link": "reports", "metric": "count"}})
    .build()
)

rows = client.objectsets.load(ontology="northwind", definition=definition)
```

Supported chain steps: `base`, `static`, `reference`, `filter`,
`search_around`, `union`, `intersect`, `subtract`, `as_type`,
`as_base_object_types`, `interface_base`, `nearest_neighbors`,
`with_properties`. See `sdk/python/tests/test_objectsets.py` +
`test_builders.py` for end-to-end examples.

### AggregationAPI

Foundry-parity aggregation (Gap-D2, commit 863a19e). Builders for every
metric and groupBy variant, plus typed response parsing.

```python
resp = client.aggregations.run(
    ontology="northwind",
    object_type="Order",
    aggregations=[
        client.aggregations.count(name="total"),
        client.aggregations.sum("freight", name="freightSum"),
        client.aggregations.approx_percentile("freight", percentile=90.0, name="p90"),
    ],
    group_by=[
        client.aggregations.exact("shipCountry"),
        client.aggregations.fixed_width("freight", width=100),
        client.aggregations.duration("orderDate", unit="DAYS", value=90),
    ],
)
print(resp.accuracy)  # "ACCURATE" | "APPROXIMATE"
for row in resp.data:
    print(row.group, row.metrics)
```

Supported metrics: `count`, `sum`, `avg`, `min`, `max`,
`approximate_distinct`, `exact_distinct`, `standard_deviation`, `variance`,
`collect_list`, `approximate_percentile`. Supported groupBys: `exact`,
`fixed_width`, `range`, `duration`. `parse_aggregation_response` splits
out `accuracy`, `data`, and any sub-aggregations.

### TimeSeriesAPI

Per-property TimeSeries (Gap-D2 partial, commit 751d9dc). Wraps the seven
TimeSeries endpoints on the ontology object:

```python
points = client.timeseries.points(
    ontology="northwind",
    object_type="Sensor",
    primary_key="sensor-42",
    property="temperatureC",
    after="2026-01-01T00:00:00Z",
    before="2026-02-01T00:00:00Z",
)

first = client.timeseries.first_point(ontology=..., object_type=..., primary_key=..., property=...)
last = client.timeseries.last_point(...)
window = client.timeseries.window(..., aggregation="avg", bucket="P1D")
```

Methods: `first_point`, `last_point`, `points`, `stream_points`, `window`,
`transform` (chained transforms incl. `resample`), and a typed
`Point` / `WindowResult` model. The transform path pushes `resample` down
to `pkg/timeseries/downsample.go::DownsamplePoints` server-side.

### AttachmentsAPI

Attachment upload / read (Gap-D2 close-out, commit 66a675d). Mirrors the
four global + four per-property endpoints:

```python
# Global attachment by RID
blob = client.attachments.get(rid="ri.attachment.main.attachment.abc123")

# Per-property attachment on an object
client.attachments.upload_property(
    ontology="northwind",
    object_type="Employee",
    primary_key="ALFKI",
    property="photo",
    content=open("alice.png", "rb").read(),
    content_type="image/png",
)
```

### Criteria builders

Type-safe submission-criteria DSL for ActionType validation rules
(Gap-A3, commit c7725c1). Replaces hand-writing the nested dict structure.

```python
from weave_client.criteria_builders import (
    always,
    parameter_match,
    parameter_compare,
    and_,
    or_,
    not_,
)

criteria = and_(
    parameter_match("status", "active"),
    or_(
        parameter_compare("age", "gt", "minAge"),       # age > minAge
        parameter_compare("seniority", "gte", 10),       # seniority >= 10
    ),
    not_(parameter_match("region", "restricted")),
)

action_type = {
    "apiName": "promoteEmployee",
    "submissionCriteria": criteria,
    "parameters": [...],
}
```

Builders: `always` (no-op truthy), `parameter_match` (equality vs literal
or other parameter), `parameter_compare` (cross-field `gt` / `gte` / `lt`
/ `lte` / `eq` / `neq`), `and_` / `or_` / `not_` (composite groups). The
server-side admin save step structurally validates the tree and returns
`WeaveValidationError` on bad input — there is no silent acceptance path.

### TransactionsAPI (preview)

Preview-mode OntologyTransaction wrapper (commits 609e815 + 4175be4).

```python
tx = client.transactions.append_edits(
    ontology="northwind",
    transaction_id="tx-abc",
    edits=[...],
    preview=True,
)

state = client.transactions.get(ontology="northwind", transaction_id="tx-abc", preview=True)
client.transactions.abort(ontology="northwind", transaction_id="tx-abc", preview=True)
```

`?preview=true` is appended automatically; `abort` is idempotent.

### Async client mirror

`WeaveAsyncClient` mirrors every namespace above with `await`-able methods.
The class names and signatures are identical to the sync surfaces — the
only difference is that they return coroutines:

```python
import asyncio
from weave_client import WeaveAsyncClient

async def main():
    async with WeaveAsyncClient("http://localhost:9117", token="...") as client:
        rows = await client.objectsets.load(ontology="northwind", definition=...)
        resp = await client.aggregations.run(...)
        await client.transactions.append_edits(...)

asyncio.run(main())
```

The async client uses `httpx.AsyncClient` under the hood and reuses the
same dataclass models so consumers can swap surfaces without rewriting
response handling.

## Testing

The default test suite uses Python stdlib `unittest` with a tiny in-process
HTTP stub server, so it works without `pip install` or network access:

```bash
cd sdk/python
PYTHONPATH=. python -m unittest discover -s tests -p 'test_*.py' -v
```

After installing the test extras you can also use pytest:

```bash
pip install -e .[test]
pytest
```

The repository root provides a `make python-test` shortcut that runs the
former (no extra deps required).
