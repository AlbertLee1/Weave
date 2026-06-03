"""ObjectSetBuilder — Pythonic composable ObjectSet construction.

PRD-V2 Gap-D1 round 43: the existing ``ObjectSetsAPI`` methods take
raw ``dict`` ObjectSet definitions, which gets verbose and error-
prone once you start composing ``filter`` over ``base`` over
``searchAround``. This module adds a chainable builder so callers
can write:

    from weave_client.builders import ObjectSetBuilder, count

    employees_in_eng = (
        ObjectSetBuilder.base("Employee")
        .filter({"type": "eq", "field": "deptId", "value": "eng"})
    )
    page = client.objectsets.load_objects(
        "northwind",
        employees_in_eng,                # Builder is accepted directly
        select=["employeeId", "name"],
    )

    counts = client.objectsets.aggregate(
        "northwind",
        employees_in_eng.search_around("manager"),
        aggregation=[count("perManager")],
        group_by=[{"field": "managerId", "type": "exact"}],
    )

The builder mirrors the eight ObjectSet variants the Go executor
recognises (``base``, ``static``, ``filter``, ``union``,
``intersect``, ``subtract``, ``searchAround``, ``reference``).
Operator overloading on property accessors (the Foundry-y
``Employee.age > 30`` shorthand) is deferred to a future round —
that needs typed schema codegen and is a much bigger change.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Dict, Iterable, List, Mapping, Optional, Sequence, Union


class ObjectSetBuilder:
    """Chainable builder for ObjectSet definitions.

    Instances are immutable: every chainable method returns a fresh
    builder so a base definition can be reused as the source of
    multiple downstream compositions without surprise mutation.
    """

    __slots__ = ("_definition",)

    def __init__(self, definition: Dict[str, Any]):
        # Defensive copy so callers can't mutate our state by holding
        # on to the dict they passed in.
        self._definition = dict(definition)

    # ------------------------------------------------------------------
    # Constructors — one per ObjectSet variant the executor knows.
    # ------------------------------------------------------------------

    @classmethod
    def base(cls, object_type: str) -> "ObjectSetBuilder":
        """Every object of ``object_type`` in the ontology."""
        return cls({"type": "base", "objectType": object_type})

    @classmethod
    def static(cls, object_type: str, primary_keys: Iterable[str]) -> "ObjectSetBuilder":
        """A fixed list of primary keys, no Bleve query."""
        return cls({
            "type": "static",
            "objectType": object_type,
            "primaryKeys": list(primary_keys),
        })

    @classmethod
    def reference(cls, reference: str) -> "ObjectSetBuilder":
        """Re-use a previously-created temporary ObjectSet by RID."""
        return cls({"type": "reference", "reference": reference})

    @classmethod
    def from_definition(cls, definition: Dict[str, Any]) -> "ObjectSetBuilder":
        """Wrap an already-built dict so it can be chained further.

        Useful when you've received an ObjectSet definition from the
        server (e.g. ``client.objectsets.get(...)``) and want to keep
        composing.
        """
        return cls(dict(definition))

    # ------------------------------------------------------------------
    # Chainable composition — every method returns a fresh builder.
    # ------------------------------------------------------------------

    def filter(self, where: Dict[str, Any]) -> "ObjectSetBuilder":
        """Apply a where-clause filter."""
        return ObjectSetBuilder({
            "type": "filter",
            "objectSet": self._definition,
            "where": where,
        })

    def search_around(self, link_type: str) -> "ObjectSetBuilder":
        """Follow ``link_type`` from this set to the linked target."""
        return ObjectSetBuilder({
            "type": "searchAround",
            "objectSet": self._definition,
            "link": link_type,
        })

    def search_around_path(self, path: Iterable[Dict[str, Any]]) -> "ObjectSetBuilder":
        """Multi-hop searchAround. Each path entry is ``{"link": "..."}``."""
        return ObjectSetBuilder({
            "type": "searchAround",
            "objectSet": self._definition,
            "path": list(path),
        })

    def union(self, *others: "ObjectSetBuilder") -> "ObjectSetBuilder":
        """Set union with one or more other ObjectSets."""
        return ObjectSetBuilder({
            "type": "union",
            "objectSets": [self._definition] + [o._definition for o in others],
        })

    def intersect(self, *others: "ObjectSetBuilder") -> "ObjectSetBuilder":
        """Set intersect with one or more other ObjectSets."""
        return ObjectSetBuilder({
            "type": "intersect",
            "objectSets": [self._definition] + [o._definition for o in others],
        })

    def subtract(self, *others: "ObjectSetBuilder") -> "ObjectSetBuilder":
        """Set difference: keep PKs in self that are NOT in any other."""
        return ObjectSetBuilder({
            "type": "subtract",
            "objectSets": [self._definition] + [o._definition for o in others],
        })

    # ------------------------------------------------------------------
    # Terminal accessors.
    # ------------------------------------------------------------------

    def build(self) -> Dict[str, Any]:
        """Return the ObjectSet definition as a plain ``dict``.

        Always returns a defensive copy so the caller can mutate
        freely without affecting the builder.
        """
        return dict(self._definition)

    # Duck-typed protocol the ObjectSetsAPI uses to accept either a
    # raw dict OR a Builder transparently. Same name as the method
    # other Foundry-style SDKs converge on so future codegen / type
    # stubs stay portable.
    def _to_object_set(self) -> Dict[str, Any]:
        return self.build()

    def __repr__(self) -> str:  # pragma: no cover - debug only
        return f"ObjectSetBuilder({self._definition!r})"


# ----------------------------------------------------------------------
# Aggregation helpers — tiny constructors so callers can write
# `count("total")` instead of remembering the dict shape.
# ----------------------------------------------------------------------

def count(name: str = "count") -> Dict[str, Any]:
    """``count`` aggregation spec. ``name`` labels the metric column."""
    return {"type": "count", "name": name}


def sum_(field: str, name: Optional[str] = None) -> Dict[str, Any]:
    """``sum`` aggregation over ``field``. Trailing underscore avoids
    shadowing the builtin ``sum``.
    """
    return {"type": "sum", "field": field, "name": name or f"sum_{field}"}


def avg(field: str, name: Optional[str] = None) -> Dict[str, Any]:
    """``avg`` aggregation over ``field``."""
    return {"type": "avg", "field": field, "name": name or f"avg_{field}"}


def min_(field: str, name: Optional[str] = None) -> Dict[str, Any]:
    """``min`` aggregation over ``field``. Trailing underscore avoids
    shadowing the builtin ``min``.
    """
    return {"type": "min", "field": field, "name": name or f"min_{field}"}


def max_(field: str, name: Optional[str] = None) -> Dict[str, Any]:
    """``max`` aggregation over ``field``. Trailing underscore avoids
    shadowing the builtin ``max``.
    """
    return {"type": "max", "field": field, "name": name or f"max_{field}"}


# ----------------------------------------------------------------------
# Helper used by ObjectSetsAPI to accept Builder OR raw dict.
# ----------------------------------------------------------------------

ObjectSetLike = Union[Dict[str, Any], ObjectSetBuilder]


def to_object_set(value: ObjectSetLike) -> Dict[str, Any]:
    """Coerce ``value`` to the plain-dict ObjectSet wire shape.

    Accepts:
      - ``ObjectSetBuilder`` — calls ``.build()``
      - ``dict`` — returned as-is (no defensive copy; callers
        upstream may already have prepared the dict)
      - anything implementing the duck-typed ``_to_object_set()``
        protocol (future builder variants, third-party wrappers)

    Raises ``TypeError`` for any other type so misuses surface
    immediately rather than corrupting the JSON body.
    """
    if isinstance(value, ObjectSetBuilder):
        return value.build()
    if hasattr(value, "_to_object_set"):
        return value._to_object_set()  # type: ignore[no-any-return]
    if isinstance(value, dict):
        return value
    raise TypeError(
        f"object_set must be ObjectSetBuilder or dict, got {type(value).__name__}"
    )


# ----------------------------------------------------------------------
# Round-51 additions: full aggregation surface to match Foundry.
#
# These helpers cover the metric and groupBy types the Go engine
# accepts (see pkg/oss/aggregation/engine.go AggregationSpec /
# GroupBySpec). Each returns a plain dict so the existing
# ObjectSetsAPI.aggregate() entry point keeps working without
# changes; callers just substitute these helpers for hand-rolled
# dicts.
# ----------------------------------------------------------------------

def approx_distinct(
    field: str,
    name: Optional[str] = None,
    precision: Optional[int] = None,
) -> Dict[str, Any]:
    """``approximateDistinct`` (HyperLogLog) over ``field``.

    ``precision`` is the per-spec HLL precision (4..18) overriding
    the request-level ``hllPrecision``. Default 14 (~0.81% error).
    """
    spec: Dict[str, Any] = {
        "type": "approximateDistinct",
        "field": field,
        "name": name or f"approxDistinct_{field}",
    }
    if precision is not None:
        spec["precision"] = precision
    return spec


def exact_distinct(field: str, name: Optional[str] = None) -> Dict[str, Any]:
    """``exactDistinct`` over ``field`` (no sketch — full enumeration)."""
    return {
        "type": "exactDistinct",
        "field": field,
        "name": name or f"exactDistinct_{field}",
    }


def stddev(field: str, name: Optional[str] = None) -> Dict[str, Any]:
    """``standardDeviation`` over a numeric ``field``."""
    return {
        "type": "standardDeviation",
        "field": field,
        "name": name or f"stddev_{field}",
    }


def variance(field: str, name: Optional[str] = None) -> Dict[str, Any]:
    """``variance`` over a numeric ``field``."""
    return {
        "type": "variance",
        "field": field,
        "name": name or f"variance_{field}",
    }


def collect_list(
    field: str,
    name: Optional[str] = None,
    max_items: Optional[int] = None,
) -> Dict[str, Any]:
    """``collectList`` of values for ``field``. ``max_items`` caps the
    list size (server default 100)."""
    spec: Dict[str, Any] = {
        "type": "collectList",
        "field": field,
        "name": name or f"collectList_{field}",
    }
    if max_items is not None:
        spec["maxItems"] = max_items
    return spec


def approx_percentile(
    field: str,
    percentile: float,
    name: Optional[str] = None,
    compression: Optional[float] = None,
) -> Dict[str, Any]:
    """``approximatePercentile`` (t-digest) for a single scalar
    percentile in [0, 100]. ``compression`` overrides the request-level
    t-digest compression (server default 100)."""
    # Default name uses pNN_field so the response column label is
    # interpretable in dashboards without callers thinking about it.
    spec: Dict[str, Any] = {
        "type": "approximatePercentile",
        "field": field,
        "name": name or f"p{int(percentile)}_{field}",
        "percentile": percentile,
    }
    if compression is not None:
        spec["compression"] = compression
    return spec


def approx_percentiles(
    field: str,
    percentiles: Sequence[float],
    name: Optional[str] = None,
    compression: Optional[float] = None,
) -> Dict[str, Any]:
    """Batch form of ``approx_percentile``: one t-digest pass produces
    a map of percentile → value. Server emits a single ``MetricValue``
    whose value is ``{"50": ..., "95": ..., ...}``."""
    spec: Dict[str, Any] = {
        "type": "approximatePercentile",
        "field": field,
        "name": name or f"percentiles_{field}",
        "percentiles": list(percentiles),
    }
    if compression is not None:
        spec["compression"] = compression
    return spec


# ----------------------------------------------------------------------
# GroupBy helpers.
# ----------------------------------------------------------------------

def exact_group(field: str, max_groups: Optional[int] = None) -> Dict[str, Any]:
    """``exact``-value groupBy. One bucket per distinct value of
    ``field``. ``max_groups`` caps the bucket count."""
    spec: Dict[str, Any] = {"type": "exact", "field": field}
    if max_groups is not None:
        spec["maxGroupCount"] = max_groups
    return spec


def fixed_width_group(
    field: str,
    width: float,
    max_groups: Optional[int] = None,
) -> Dict[str, Any]:
    """``fixedWidth`` numeric bucketing of ``width`` units per bucket."""
    spec: Dict[str, Any] = {"type": "fixedWidth", "field": field, "fixedWidth": width}
    if max_groups is not None:
        spec["maxGroupCount"] = max_groups
    return spec


def range_group(
    field: str,
    ranges: Sequence[Mapping[str, Any]],
) -> Dict[str, Any]:
    """``range`` bucketing. ``ranges`` is a list of ``{"start": x,
    "end": y}`` half-open buckets (server-side semantics).

    The Go engine accepts both ``"range"`` (singular) and ``"ranges"``
    (plural alias); the helper emits the canonical singular form.
    """
    return {"type": "range", "field": field, "ranges": list(ranges)}


def duration_group(
    field: str,
    duration: Optional[str] = None,
    unit: Optional[str] = None,
    value: Optional[int] = None,
) -> Dict[str, Any]:
    """``duration`` (time-bucketing) groupBy.

    Two mutually-exclusive forms:
      - ``duration="P1M"`` — ISO-8601 duration string (P1D / P1W /
        P1M / P3M (quarter) / P1Y / PT1H (hour)).
      - ``unit="DAYS", value=7`` — explicit ``{unit, value}`` form
        (server ``DurationValue``). ``unit`` is one of DAYS / WEEKS /
        MONTHS / YEARS / HOURS / MINUTES / SECONDS.
    """
    has_iso = duration is not None
    has_uv = unit is not None or value is not None
    if has_iso and has_uv:
        raise ValueError(
            "duration_group: pass either duration=... or unit=+value=, not both"
        )
    if not has_iso and not has_uv:
        raise ValueError(
            "duration_group: must specify duration=... or unit=+value="
        )
    spec: Dict[str, Any] = {"type": "duration", "field": field}
    if has_iso:
        spec["duration"] = duration
    else:
        if unit is None or value is None:
            raise ValueError("duration_group: unit and value must both be set")
        spec["value"] = {"unit": unit, "value": value}
    return spec


# ----------------------------------------------------------------------
# Typed response wrapper.
# ----------------------------------------------------------------------

@dataclass
class AggregationRow:
    """One row in an aggregation response.

    ``group`` is ``None`` for ungrouped (single-row) responses and a
    ``{dimension: value}`` map otherwise. ``metrics`` is a flat
    ``{name: value}`` map for O(1) lookup — the wire format is a list
    of ``{name, value}`` objects, which we flatten at parse time.
    ``sub_aggregations`` carries any nested per-bucket aggregations
    (e.g. for groupBy → subAggregation).
    """

    group: Optional[Dict[str, Any]]
    metrics: Dict[str, Any]
    sub_aggregations: Dict[str, "AggregationResponse"] = field(default_factory=dict)

    def metric(self, name: str, default: Any = None) -> Any:
        """Lookup a metric value by name. Returns ``default`` (``None``
        when omitted) for missing metrics so callers don't have to
        guard with ``in``."""
        return self.metrics.get(name, default)


@dataclass
class AggregationResponse:
    """Typed wrapper around the V2 aggregation response.

    Exposes ``.accuracy`` (the ACCURATE / APPROXIMATE verdict server
    stamps onto the response — see pkg/oss/aggregation/response.go),
    ``.excluded_items`` count, ``.data`` rows, and optional
    ``.sub_aggregations`` map.
    """

    accuracy: str
    excluded_items: int
    data: List[AggregationRow]
    sub_aggregations: Dict[str, "AggregationResponse"] = field(default_factory=dict)


def parse_aggregation_response(raw: Mapping[str, Any]) -> AggregationResponse:
    """Parse a raw aggregation response dict into an
    ``AggregationResponse``. The dict shape is the V2 wire format
    produced by ``pkg/oss/aggregation``.

    The helper is intentionally permissive: missing optional keys
    yield Python defaults (``accuracy=""``, ``excluded_items=0``,
    empty lists / maps) so it tolerates older server versions.
    """
    data_rows = [
        _parse_row(row) for row in raw.get("data", []) or []
    ]
    sub_aggs = {
        name: parse_aggregation_response(value)
        for name, value in (raw.get("subAggregations") or {}).items()
    }
    return AggregationResponse(
        accuracy=str(raw.get("accuracy") or ""),
        excluded_items=int(raw.get("excludedItems") or 0),
        data=data_rows,
        sub_aggregations=sub_aggs,
    )


def _parse_row(row: Mapping[str, Any]) -> AggregationRow:
    metrics = {m["name"]: m["value"] for m in row.get("metrics", []) or []}
    sub_aggs = {
        name: parse_aggregation_response(value)
        for name, value in (row.get("subAggregations") or {}).items()
    }
    return AggregationRow(
        group=dict(row["group"]) if row.get("group") is not None else None,
        metrics=metrics,
        sub_aggregations=sub_aggs,
    )


__all__ = [
    "ObjectSetBuilder",
    "ObjectSetLike",
    "to_object_set",
    "count",
    "sum_",
    "avg",
    "min_",
    "max_",
    "approx_distinct",
    "exact_distinct",
    "stddev",
    "variance",
    "collect_list",
    "approx_percentile",
    "approx_percentiles",
    "exact_group",
    "fixed_width_group",
    "range_group",
    "duration_group",
    "AggregationResponse",
    "AggregationRow",
    "parse_aggregation_response",
]
