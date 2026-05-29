"""BDD coverage for the SDK submission-criteria builders.

Round 134 SDK mirror of round-133 backend
(pkg/actions/criteria.go group composite). Action authors building
ActionType definitions through the admin API previously had to
hand-write the criteria dict wire shape — easy to mistype operator
names, easy to forget that the top-level array is AND-only, easy
to nest groups incorrectly.

This module ships a thin builder so the dict comes from named
constructors instead of literals. Round 133 added the "group"
operator on the backend; round 134 makes it ergonomic on the SDK.

Wire shape (mirrors pkg/actions/criteria.go exactly):

  always()                                  -> {"type":"always"}
  parameter_match(p, op, v)                 -> {"type":"parameterMatch", "value":{...}}
  parameter_compare(left, op, right)        -> {"type":"parameterCompare", "value":{...}}
  and_(c1, c2, ...)                         -> {"type":"group", "value":{"operator":"and", "criteria":[...]}}
  or_(c1, c2, ...)                          -> {"type":"group", "value":{"operator":"or",  "criteria":[...]}}
  not_(c)                                   -> {"type":"group", "value":{"operator":"not", "criteria":[c]}}

The builders return plain dicts (no pydantic model), since the
admin endpoints accept raw JSON and the builder's only job is to
prevent typos and document the operator vocabulary.
"""
from __future__ import annotations

import pytest

from weave_client import criteria as c


class TestAlwaysBuilder:
    def test_always_returns_typed_dict(self):
        """Given the always builder, when invoked, then it
        returns {'type':'always'} matching the backend's empty
        short-circuit."""
        assert c.always() == {"type": "always"}


class TestParameterMatchBuilder:
    def test_parameter_match_with_eq(self):
        """Given a parameter_match with eq operator, the dict
        carries parameter / operator / value at the value layer
        exactly as the backend expects."""
        got = c.parameter_match("status", "eq", "active")
        assert got == {
            "type": "parameterMatch",
            "value": {"parameter": "status", "operator": "eq", "value": "active"},
        }

    def test_parameter_match_with_numeric_gt(self):
        """Numeric values are preserved (not stringified) so the
        backend's compareValues helper takes the numeric path."""
        got = c.parameter_match("priority", "gt", 5)
        assert got["value"]["value"] == 5
        assert got["value"]["operator"] == "gt"

    def test_parameter_match_rejects_unknown_operator(self):
        """Unknown operators are rejected at builder time rather
        than waiting for the backend to 422. Keeps round-trips off
        the wire when the author made a typo."""
        with pytest.raises(ValueError) as excinfo:
            c.parameter_match("status", "xor", "active")
        assert "xor" in str(excinfo.value).lower()

    def test_parameter_match_empty_parameter_rejected(self):
        with pytest.raises(ValueError):
            c.parameter_match("", "eq", "active")


class TestParameterCompareBuilder:
    def test_parameter_compare_emits_left_right(self):
        """parameter_compare emits leftParameter/rightParameter
        matching pkg/actions/criteria.go parameterCompareValue."""
        got = c.parameter_compare("endTime", "gt", "startTime")
        assert got == {
            "type": "parameterCompare",
            "value": {
                "leftParameter": "endTime",
                "operator": "gt",
                "rightParameter": "startTime",
            },
        }

    def test_parameter_compare_rejects_empty_left(self):
        with pytest.raises(ValueError):
            c.parameter_compare("", "gt", "startTime")

    def test_parameter_compare_rejects_empty_right(self):
        with pytest.raises(ValueError):
            c.parameter_compare("endTime", "gt", "")

    def test_parameter_compare_rejects_unknown_operator(self):
        with pytest.raises(ValueError):
            c.parameter_compare("endTime", "xor", "startTime")


class TestAndOrNotGroupBuilders:
    def test_and_wraps_children_in_group(self):
        """and_ emits a group with operator='and' and the children
        in the criteria array — matches the wire shape consumed by
        evaluateGroupCriteria in pkg/actions/criteria.go."""
        child1 = c.parameter_match("status", "eq", "active")
        child2 = c.parameter_match("priority", "gt", 0)
        got = c.and_(child1, child2)
        assert got == {
            "type": "group",
            "value": {
                "operator": "and",
                "criteria": [child1, child2],
            },
        }

    def test_or_emits_or_operator(self):
        child1 = c.parameter_match("status", "eq", "draft")
        child2 = c.parameter_match("status", "eq", "active")
        got = c.or_(child1, child2)
        assert got["value"]["operator"] == "or"
        assert got["value"]["criteria"] == [child1, child2]

    def test_not_wraps_single_child(self):
        child = c.parameter_match("status", "eq", "archived")
        got = c.not_(child)
        assert got == {
            "type": "group",
            "value": {"operator": "not", "criteria": [child]},
        }

    def test_and_accepts_zero_children_for_vacuous_truth(self):
        """Backend treats empty AND as vacuously true (composability
        with programmatic construction). Builder must allow it."""
        got = c.and_()
        assert got["value"]["criteria"] == []
        assert got["value"]["operator"] == "and"

    def test_or_accepts_zero_children_for_vacuous_falsity(self):
        """Backend treats empty OR as vacuously false. Builder must
        allow it — caller may be constructing programmatically."""
        got = c.or_()
        assert got["value"]["criteria"] == []
        assert got["value"]["operator"] == "or"

    def test_nested_groups_produce_nested_wire_shape(self):
        """Given AND[OR[a, b], c], when built, then the wire shape
        nests the inner group inside the outer criteria array."""
        a = c.parameter_match("a", "eq", "x")
        b = c.parameter_match("b", "eq", "y")
        cc = c.parameter_match("c", "eq", "z")

        inner = c.or_(a, b)
        outer = c.and_(inner, cc)

        assert outer["value"]["operator"] == "and"
        assert outer["value"]["criteria"][0]["value"]["operator"] == "or"
        assert outer["value"]["criteria"][0]["value"]["criteria"] == [a, b]
        assert outer["value"]["criteria"][1] == cc


class TestBuilderInteroperability:
    def test_builders_round_trip_through_json(self):
        """Builder output must be json.dumps-able as-is. Locks
        in that the dict carries only JSON-native types so the
        author can paste it straight into an admin payload."""
        import json

        composite = c.and_(
            c.parameter_match("status", "eq", "active"),
            c.or_(
                c.parameter_compare("endTime", "gt", "startTime"),
                c.not_(c.parameter_match("kind", "eq", "internal")),
            ),
        )
        # Must serialize without TypeError.
        payload = json.dumps(composite)
        # Round-trip preserves shape.
        assert json.loads(payload) == composite
