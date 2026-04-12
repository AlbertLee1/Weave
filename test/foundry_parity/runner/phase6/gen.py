#!/usr/bin/env python3
"""Phase 6 Gate parity fixture generator.

Emits a broad set of status-only parity fixtures that cover every endpoint
family touched by Phase 6 stories (US-037..US-041) against the Northwind
seed, so the foundry_parity runner has ≥80 executable samples asserting
that Weave's HTTP surface responds with the expected status codes under
realistic request shapes.

Body equality is intentionally NOT asserted here — the semantic matches
live in the Go integration tests and Playwright specs. The purpose of
this suite is breadth + smoke: if the ontology compiles, the server
boots, and ingest stays consistent, every fixture here should be 200.

Run from repo root:
    python3 test/foundry_parity/runner/phase6/gen.py

Re-running is idempotent: it overwrites the fixtures in place.
"""
from __future__ import annotations

import json
import os
import sys
from pathlib import Path

HERE = Path(__file__).parent
ROOT = HERE

ONTOLOGY = "northwind"
OBJECT_TYPES = ["customer", "order", "product"]
SAMPLE_PKS = {
    "customer": "ALFKI",
    "order": "10251",
    "product": "4",
}

fixtures: list[tuple[str, dict]] = []


def add(name: str, *, method: str, path: str, body: dict | None = None, status: int = 200, title: str = "") -> None:
    req: dict = {"method": method, "path": path}
    if body is not None:
        req["body"] = body
    fixture = {
        "name": name,
        "story": "US-042",
        "title": title or name,
        "request": req,
        "expected": {"status": status},
    }
    fixtures.append((name, fixture))


# -- root health + observability --
add("gate-health", method="GET", path="/health", title="GET /health smoke")

# -- OMS read surface --
add("gate-ontologies-list", method="GET", path="/api/v2/ontologies", title="List ontologies")
add("gate-ontology-get", method="GET", path=f"/api/v2/ontologies/{ONTOLOGY}", title="Get ontology by apiName")
add("gate-objecttypes-list", method="GET", path=f"/api/v2/ontologies/{ONTOLOGY}/objectTypes", title="List object types")
add("gate-actiontypes-list", method="GET", path=f"/api/v2/ontologies/{ONTOLOGY}/actionTypes", title="List action types")
add("gate-interfacetypes-list-preview", method="GET",
    path=f"/api/v2/ontologies/{ONTOLOGY}/interfaceTypes?preview=true",
    title="List interface types (preview flag)")

for ot in OBJECT_TYPES:
    add(f"gate-objecttype-get-{ot}", method="GET",
        path=f"/api/v2/ontologies/{ONTOLOGY}/objectTypes/{ot}",
        title=f"Get object type {ot}")
    add(f"gate-outgoing-linktypes-{ot}", method="GET",
        path=f"/api/v2/ontologies/{ONTOLOGY}/objectTypes/{ot}/outgoingLinkTypes",
        title=f"List outgoing link types for {ot}")

# -- OSS object list + get + count + search (GET + POST) --
for ot in OBJECT_TYPES:
    add(f"gate-list-{ot}", method="GET",
        path=f"/api/v2/ontologies/{ONTOLOGY}/objects/{ot}",
        title=f"List {ot} objects")
    add(f"gate-list-{ot}-page1", method="GET",
        path=f"/api/v2/ontologies/{ONTOLOGY}/objects/{ot}?pageSize=1",
        title=f"List {ot} with pageSize=1")
    add(f"gate-list-{ot}-page3", method="GET",
        path=f"/api/v2/ontologies/{ONTOLOGY}/objects/{ot}?pageSize=3",
        title=f"List {ot} with pageSize=3")
    add(f"gate-get-{ot}", method="GET",
        path=f"/api/v2/ontologies/{ONTOLOGY}/objects/{ot}/{SAMPLE_PKS[ot]}",
        title=f"Get a seeded {ot} by primary key")
    add(f"gate-count-{ot}", method="POST",
        path=f"/api/v2/ontologies/{ONTOLOGY}/objects/{ot}/count",
        body={}, title=f"Count {ot} objects")
    add(f"gate-search-{ot}", method="POST",
        path=f"/api/v2/ontologies/{ONTOLOGY}/objects/{ot}/search",
        body={"select": ["__primaryKey"], "pageSize": 2},
        title=f"Search {ot} with pageSize=2")
    add(f"gate-search-{ot}-page1", method="POST",
        path=f"/api/v2/ontologies/{ONTOLOGY}/objects/{ot}/search",
        body={"select": ["__primaryKey"], "pageSize": 1},
        title=f"Search {ot} with pageSize=1 (cursor window)")

# -- Aggregation surface covering US-039 multi-groupby semantics --
for ot in OBJECT_TYPES:
    add(f"gate-aggregate-count-{ot}", method="POST",
        path=f"/api/v2/ontologies/{ONTOLOGY}/objects/{ot}/aggregate",
        body={"aggregation": [{"type": "count", "name": "total"}]},
        title=f"Aggregate count for {ot}")

add("gate-aggregate-groupby-country", method="POST",
    path=f"/api/v2/ontologies/{ONTOLOGY}/objects/customer/aggregate",
    body={
        "aggregation": [{"type": "count", "name": "total"}],
        "groupBy": [{"type": "exact", "field": "country", "maxGroupCount": 50}],
    },
    title="Aggregate count grouped by customer.country")

add("gate-aggregate-groupby-order-shipCountry", method="POST",
    path=f"/api/v2/ontologies/{ONTOLOGY}/objects/order/aggregate",
    body={
        "aggregation": [{"type": "count", "name": "total"}],
        "groupBy": [{"type": "exact", "field": "shipCountry", "maxGroupCount": 50}],
    },
    title="Aggregate count grouped by order.shipCountry")

add("gate-aggregate-multi-groupby", method="POST",
    path=f"/api/v2/ontologies/{ONTOLOGY}/objects/order/aggregate",
    body={
        "aggregation": [{"type": "count", "name": "total"}],
        "groupBy": [
            {"type": "exact", "field": "shipCountry", "maxGroupCount": 20},
            {"type": "exact", "field": "customerID", "maxGroupCount": 20},
        ],
    },
    title="Aggregate count with 2-layer groupBy (US-039 shape)")

add("gate-aggregate-3-layer-groupby", method="POST",
    path=f"/api/v2/ontologies/{ONTOLOGY}/objects/order/aggregate",
    body={
        "aggregation": [{"type": "count", "name": "total"}],
        "groupBy": [
            {"type": "exact", "field": "shipCountry", "maxGroupCount": 10},
            {"type": "exact", "field": "customerID", "maxGroupCount": 10},
            {"type": "exact", "field": "orderID", "maxGroupCount": 10},
        ],
    },
    title="Aggregate count with 3-layer groupBy")

# -- loadObjectsOrInterfaces preview (US-041 interface paging) --
LOAD_PATH = f"/api/v2/ontologies/{ONTOLOGY}/objectSets/loadObjectsOrInterfaces?preview=true"
DEFAULT_SELECT = {
    "customer": ["customerID", "companyName", "country"],
    "order": ["orderID", "customerID", "shipCountry"],
    "product": ["productID", "productName"],
}

for ot in OBJECT_TYPES:
    add(f"gate-load-preview-{ot}", method="POST",
        path=LOAD_PATH,
        body={
            "objectSet": {"type": "base", "objectType": ot},
            "select": DEFAULT_SELECT[ot],
            "pageSize": 5,
        },
        title=f"loadObjectsOrInterfaces base {ot} pageSize=5 (preview)")

add("gate-load-preview-filter-customer-germany", method="POST",
    path=LOAD_PATH,
    body={
        "objectSet": {
            "type": "filter",
            "objectSet": {"type": "base", "objectType": "customer"},
            "where": {"type": "eq", "field": "country", "value": "germany"},
        },
        "select": ["customerID", "country"],
        "pageSize": 5,
    },
    title="loadObjectsOrInterfaces filter customer.country=germany")

add("gate-load-preview-filter-order-france", method="POST",
    path=LOAD_PATH,
    body={
        "objectSet": {
            "type": "filter",
            "objectSet": {"type": "base", "objectType": "order"},
            "where": {"type": "eq", "field": "shipCountry", "value": "france"},
        },
        "select": ["orderID", "shipCountry"],
        "pageSize": 5,
    },
    title="loadObjectsOrInterfaces filter order.shipCountry=france")

# -- ObjectSet composition: union / intersect / subtract shapes --
for variant in ["union", "intersect", "subtract"]:
    add(f"gate-load-preview-{variant}-customer", method="POST",
        path=LOAD_PATH,
        body={
            "objectSet": {
                "type": variant,
                "objectSets": [
                    {"type": "base", "objectType": "customer"},
                    {
                        "type": "filter",
                        "objectSet": {"type": "base", "objectType": "customer"},
                        "where": {"type": "eq", "field": "country", "value": "germany"},
                    },
                ],
            },
            "select": ["customerID"],
            "pageSize": 5,
        },
        title=f"loadObjectsOrInterfaces {variant} over customer")

# -- searchAround (US-040 withProperties depends on this resolver path) --
add("gate-searcharound-customer-orders", method="POST",
    path=LOAD_PATH,
    body={
        "objectSet": {
            "type": "searchAround",
            "objectSet": {"type": "base", "objectType": "customer"},
            "link": "customerOrders",
        },
        "select": ["orderID"],
        "pageSize": 5,
    },
    title="searchAround customer → order via customerOrders")

# -- withProperties derived metrics (US-040 shape) --
add("gate-withproperties-order-count", method="POST",
    path=LOAD_PATH,
    body={
        "objectSet": {
            "type": "withProperties",
            "objectSet": {"type": "base", "objectType": "customer"},
            "derivedProperties": [
                {
                    "name": "order_count",
                    "link": "customerOrders",
                    "metric": "count",
                }
            ],
        },
        "select": ["customerID", "order_count"],
        "pageSize": 5,
    },
    title="withProperties customer.order_count = count(customerOrders)")

# -- ObjectSets/aggregate (the multi-groupby compositional form) --
add("gate-objectset-aggregate-customer-country", method="POST",
    path=f"/api/v2/ontologies/{ONTOLOGY}/objectSets/aggregate",
    body={
        "objectSet": {"type": "base", "objectType": "customer"},
        "aggregation": [{"type": "count", "name": "total"}],
        "groupBy": [{"type": "exact", "field": "country", "maxGroupCount": 20}],
    },
    title="objectSets/aggregate customer groupBy country")

add("gate-objectset-aggregate-order-shipCountry", method="POST",
    path=f"/api/v2/ontologies/{ONTOLOGY}/objectSets/aggregate",
    body={
        "objectSet": {"type": "base", "objectType": "order"},
        "aggregation": [{"type": "count", "name": "total"}],
        "groupBy": [{"type": "exact", "field": "shipCountry", "maxGroupCount": 20}],
    },
    title="objectSets/aggregate order groupBy shipCountry")

# -- Search-around over filtered inner set (compositional parity sample) --
add("gate-load-preview-filter-then-search-around", method="POST",
    path=LOAD_PATH,
    body={
        "objectSet": {
            "type": "searchAround",
            "objectSet": {
                "type": "filter",
                "objectSet": {"type": "base", "objectType": "customer"},
                "where": {"type": "eq", "field": "country", "value": "germany"},
            },
            "link": "customerOrders",
        },
        "select": ["orderID"],
        "pageSize": 5,
    },
    title="searchAround(filter(customer.country=germany), customerOrders)")

# -- Filter variations over the existing where clause grammar --
for ot, field, value in [
    ("customer", "country", "germany"),
    ("customer", "country", "switzerland"),
    ("order", "shipCountry", "france"),
    ("order", "shipCountry", "switzerland"),
    ("order", "customerID", "ALFKI"),
]:
    add(f"gate-search-where-eq-{ot}-{field}-{value}", method="POST",
        path=f"/api/v2/ontologies/{ONTOLOGY}/objects/{ot}/search",
        body={
            "select": ["__primaryKey"],
            "pageSize": 5,
            "where": {"type": "eq", "field": field, "value": value},
        },
        title=f"search {ot} where {field}={value}")

# -- Negative / coverage samples: 404 for unknown PK, 400 for bad select --
add("gate-unknown-customer-404", method="GET",
    path=f"/api/v2/ontologies/{ONTOLOGY}/objects/customer/NOPE_NOT_A_CUSTOMER",
    status=404,
    title="GET unknown customer PK returns 404")

add("gate-search-missing-select-400", method="POST",
    path=f"/api/v2/ontologies/{ONTOLOGY}/objects/customer/search",
    body={"pageSize": 1},
    status=400,
    title="search without select returns 400")

add("gate-unknown-ontology-404", method="GET",
    path="/api/v2/ontologies/does-not-exist",
    status=404,
    title="GET unknown ontology returns 404")

# -- Per-type list pagination sweep (sizes 1..5) --
for ot in OBJECT_TYPES:
    for ps in (1, 2, 3, 4, 5):
        add(f"gate-list-{ot}-ps{ps}", method="GET",
            path=f"/api/v2/ontologies/{ONTOLOGY}/objects/{ot}?pageSize={ps}",
            title=f"List {ot} pageSize={ps}")

# -- Preview loadObjectsOrInterfaces at multiple page sizes --
for ot in OBJECT_TYPES:
    for ps in (1, 2):
        add(f"gate-load-preview-{ot}-ps{ps}", method="POST",
            path=LOAD_PATH,
            body={
                "objectSet": {"type": "base", "objectType": ot},
                "select": DEFAULT_SELECT[ot],
                "pageSize": ps,
            },
            title=f"loadObjectsOrInterfaces base {ot} pageSize={ps}")

# -- Round-trip each fixture to disk --
def main() -> int:
    existing = sorted(ROOT.glob("gate-*.json"))
    for p in existing:
        p.unlink()
    written = 0
    for name, body in fixtures:
        out = ROOT / f"{name}.json"
        out.write_text(json.dumps(body, indent=2, sort_keys=True) + "\n")
        written += 1
    print(f"wrote {written} fixtures under {ROOT.relative_to(Path.cwd())}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
