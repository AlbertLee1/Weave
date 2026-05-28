# Chapter 7 — v2 SDK builders & typed errors

The v2 SDK ships fluent builders for the three nested wire shapes that
are most painful to hand-roll: ObjectSet definitions, aggregations, and
submission-criteria trees. This recipe glues them together against the
Northwind fixture and shows how to branch on the two new typed
exceptions (`WeaveValidationError`, `WeaveVersionedLookupError`).

Why bother with builders when the raw dict still works? Two reasons:

1. **Symmetry with Foundry** — the Foundry TS / Java SDKs are
   builder-shaped, so writing a Pythonic builder layer keeps your code
   readable when you port (or read) examples from the Foundry docs.
2. **Type-time safety on the criteria tree** — submission criteria is
   the part most likely to ship as broken JSON and only fail at admin
   save time. Builders lock the shape statically so the validation
   error surfaces in your test suite instead of staging.

## Prerequisites

Recipes 1 – 6 only depended on `weave-client` core. This one uses the
same core, so install in editable mode (see chapter 1) and you are
done:

```bash
pip install -e sdk/python
export WEAVE_BASE_URL=http://localhost:9117
export WEAVE_TOKEN=eyJhbGciOi...     # if AUTH_MODE=jwt
```

## ObjectSetBuilder — composable ObjectSet definitions

`ObjectSetBuilder` (Gap-D1) replaces hand-written ObjectSet dicts with
chained calls. The same builder feeds both
`POST .../objectSets/loadObjects` and `.../objectSets/createTemporary`.

```python
from weave_client import WeaveClient
from weave_client.objectsets import ObjectSetBuilder

client = WeaveClient()

definition = (
    ObjectSetBuilder(client)
    .base("Customer")
    .filter({"field": "country", "op": "eq", "value": "USA"})
    .with_properties(
        {"orderCount": {"link": "orders", "metric": "count"}}
    )
    .build()
)

page = client.objectsets.load(ontology="northwind", definition=definition)
print(f"got {len(page['data'])} rows, first derived: {page['data'][0].get('orderCount')}")
```

Chain steps: `base`, `static`, `reference`, `filter`, `search_around`,
`union`, `intersect`, `subtract`, `as_type`, `as_base_object_types`,
`interface_base`, `nearest_neighbors`, `with_properties`. The build
output is the same JSON the raw-dict path expects.

## AggregationAPI — builders for metrics + groupBy

The Aggregation surface (Gap-D2) exposes one builder per metric and one
per groupBy variant. The runtime `accuracy` flag is preserved in the
typed response so a query that exceeds the scan cap surfaces an
`APPROXIMATE` badge instead of silent truncation.

```python
agg = client.aggregations
resp = agg.run(
    ontology="northwind",
    object_type="Order",
    aggregations=[
        agg.count(name="total"),
        agg.sum("freight", name="freightSum"),
        agg.approximate_percentile("freight", percentile=90.0, name="p90"),
    ],
    group_by=[
        agg.exact("shipCountry"),
        agg.fixed_width("freight", width=100),
        agg.duration("orderDate", unit="DAYS", value=90),
    ],
)

print("accuracy:", resp.accuracy)
for row in resp.data:
    print(row.group, row.metrics)
```

## Criteria builders — type-safe submission criteria

`weave_client.criteria_builders` (Gap-A3) makes the cross-field
criteria tree a Pythonic expression instead of a nested dict.
`parameter_compare` is the meaningful addition: it supports `gt`,
`gte`, `lt`, `lte`, `eq`, `neq` against either a literal or the name of
another parameter.

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
        parameter_compare("age", "gt", "minAge"),       # age > minAge param
        parameter_compare("seniority", "gte", 10),      # seniority >= 10
    ),
    not_(parameter_match("region", "restricted")),
)
```

Feed `criteria` straight into your ActionType admin save call. Admin
save validates the tree structurally and returns a typed 400 — see
the next section.

## Typed errors — branch on contract, not on prose

Two new typed exceptions joined `WeaveError` in v2. Reach for them when
you want to branch on the specific contract rather than parsing the
prose of an error body:

```python
from weave_client import WeaveClient
from weave_client.errors import (
    WeaveError,
    WeaveValidationError,
    WeaveVersionedLookupError,
)

client = WeaveClient()

# 1) WeaveValidationError on bad criteria tree (Gap-A3 SDK136 / 400)
try:
    client.admin.action_types.create(
        ontology="northwind",
        action_type={"apiName": "broken", "submissionCriteria": {"type": "wat"}},
    )
except WeaveValidationError as exc:
    print(f"bad criteria field: {exc.parameters.get('field')}")

# 2) WeaveVersionedLookupError on @vN reads (Gap-T4 SDK118 / 501)
try:
    client.ontologies.get_object_type(
        ontology="northwind", object_type="Customer@v3"
    )
except WeaveVersionedLookupError:
    print("versioned read not supported yet — refresh against HEAD")
```

Both exceptions subclass `WeaveError`, so legacy `except WeaveError:`
blocks keep working. The companion script (`07-builders.py`) wires the
full flow end-to-end — clone, edit, and run.

## Where to from here

| Want to … | Read … |
|---|---|
| Drive subscriptions concurrently | [Chapter 1 — Async](01-async.md) |
| Apply many Actions atomically | [Chapter 3 — Batching](03-batching.md) |
| Stream ObjectSet changes over WS | [Chapter 6 — WS Subscription](06-ws-subscription.md) |
| See every v2 builder / API method in one place | [`docs/python-sdk.md` § v2 APIs](../python-sdk.md#v2-apis) |
