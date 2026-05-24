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

from typing import Any, Dict, Iterable, List, Optional, Union


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


__all__ = [
    "ObjectSetBuilder",
    "ObjectSetLike",
    "to_object_set",
    "count",
    "sum_",
    "avg",
    "min_",
    "max_",
]
