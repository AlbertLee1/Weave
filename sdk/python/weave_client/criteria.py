"""Submission-criteria builders for ActionType definitions.

Round 134 SDK mirror of round-133 backend pkg/actions/criteria.go.
Action authors building ActionType definitions through the admin
API previously had to hand-write the criteria dict wire shape;
this module ships named constructors so the dict comes from the
SDK instead of literals — typos in operator names fail at builder
time, not at the backend's 422 response.

Wire shape (mirrors pkg/actions/criteria.go exactly):

    always()                            -> {"type":"always"}
    parameter_match(p, op, v)           -> {"type":"parameterMatch", "value":{...}}
    parameter_compare(left, op, right)  -> {"type":"parameterCompare", "value":{...}}
    and_(c1, c2, ...)                   -> {"type":"group", "value":{"operator":"and", "criteria":[...]}}
    or_(c1, c2, ...)                    -> {"type":"group", "value":{"operator":"or",  "criteria":[...]}}
    not_(c)                             -> {"type":"group", "value":{"operator":"not", "criteria":[c]}}

Builders return plain dicts (no pydantic model) — admin endpoints
accept raw JSON, and the builder's only job is to prevent typos
and document the operator vocabulary.

The empty AND / empty OR cases are intentionally permitted so
callers can build criteria programmatically (e.g. AND-reducing a
list that happens to be empty) without special-casing zero.
"""
from __future__ import annotations

from typing import Any, Dict, List

Criterion = Dict[str, Any]

# Mirrors compareValues in pkg/actions/criteria.go. Kept in sync
# manually — if the backend gains a new operator, add it here and
# extend the BDD test.
_COMPARISON_OPERATORS = frozenset({"eq", "neq", "gt", "lt", "gte", "lte"})


def _validate_operator(op: str, ctx: str) -> None:
    if op not in _COMPARISON_OPERATORS:
        raise ValueError(
            f"{ctx}: unknown operator {op!r}; "
            f"expected one of {sorted(_COMPARISON_OPERATORS)}"
        )


def always() -> Criterion:
    """The always-passing criterion. Matches backend's empty
    short-circuit in EvaluateCriteria."""
    return {"type": "always"}


def parameter_match(parameter: str, operator: str, value: Any) -> Criterion:
    """Compare a single parameter against a literal value.

    Mirrors parameterMatchValue in pkg/actions/criteria.go.
    """
    if not parameter:
        raise ValueError("parameter_match: parameter name is required")
    _validate_operator(operator, "parameter_match")
    return {
        "type": "parameterMatch",
        "value": {
            "parameter": parameter,
            "operator": operator,
            "value": value,
        },
    }


def parameter_compare(
    left_parameter: str, operator: str, right_parameter: str
) -> Criterion:
    """Compare two parameters against each other.

    Mirrors parameterCompareValue in pkg/actions/criteria.go;
    closes the "endTime > startTime" use case without a DSL.
    """
    if not left_parameter:
        raise ValueError("parameter_compare: leftParameter is required")
    if not right_parameter:
        raise ValueError("parameter_compare: rightParameter is required")
    _validate_operator(operator, "parameter_compare")
    return {
        "type": "parameterCompare",
        "value": {
            "leftParameter": left_parameter,
            "operator": operator,
            "rightParameter": right_parameter,
        },
    }


def _group(operator: str, children: List[Criterion]) -> Criterion:
    return {
        "type": "group",
        "value": {"operator": operator, "criteria": children},
    }


def and_(*criteria: Criterion) -> Criterion:
    """AND composite — every child must pass. Zero children is
    vacuously true (matches backend semantics)."""
    return _group("and", list(criteria))


def or_(*criteria: Criterion) -> Criterion:
    """OR composite — at least one child must pass. Zero children
    is vacuously false (matches backend semantics)."""
    return _group("or", list(criteria))


def not_(criterion: Criterion) -> Criterion:
    """NOT composite — the single child must FAIL for the parent
    to pass. Backend rejects empty/multi-child NOT at evaluation."""
    return _group("not", [criterion])
