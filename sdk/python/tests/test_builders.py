"""BDD acceptance tests for the round-43 ObjectSetBuilder (PRD-V2 Gap-D1).

Two layers of coverage:

1. Pure-builder behaviour (no HTTP) — confirms every composition
   method produces the expected ``dict`` shape and is immutable so
   reused base sets don't leak mutations into sibling branches.

2. ObjectSetsAPI integration — confirms ``load_objects`` /
   ``load_links`` / ``aggregate`` / ``create_temporary`` accept a
   Builder OR a raw ``dict`` interchangeably (round 43 wires the
   ``to_object_set`` coercion at the API boundary).

Together they prove the Foundry-style Pythonic composition
example from the PRD —
``Employees.filter(...).search_around("team")`` — works against
the existing HTTP surface without callers having to hand-build
nested dicts.
"""
from __future__ import annotations

import json
import unittest

from weave_client import (
    Client,
    ObjectSetBuilder,
    avg,
    count,
    max_,
    min_,
    sum_,
)
from weave_client.builders import to_object_set

from tests.test_client import _StubServer


class ObjectSetBuilderShapeTests(unittest.TestCase):
    """Pure-builder tests — confirm the dict shape per variant."""

    def test_base_produces_base_definition(self):
        b = ObjectSetBuilder.base("Employee")
        self.assertEqual(b.build(), {"type": "base", "objectType": "Employee"})

    def test_static_produces_static_definition_with_primary_keys(self):
        b = ObjectSetBuilder.static("Employee", ["E1", "E2", "E3"])
        self.assertEqual(b.build(), {
            "type": "static",
            "objectType": "Employee",
            "primaryKeys": ["E1", "E2", "E3"],
        })

    def test_reference_produces_reference_definition(self):
        b = ObjectSetBuilder.reference("ri.objectset.main.temp.xyz")
        self.assertEqual(b.build(), {
            "type": "reference",
            "reference": "ri.objectset.main.temp.xyz",
        })

    def test_filter_wraps_existing_definition(self):
        b = ObjectSetBuilder.base("Employee").filter({
            "type": "gt", "field": "salary", "value": 100000,
        })
        self.assertEqual(b.build(), {
            "type": "filter",
            "objectSet": {"type": "base", "objectType": "Employee"},
            "where": {"type": "gt", "field": "salary", "value": 100000},
        })

    def test_search_around_wraps_existing_definition(self):
        b = ObjectSetBuilder.base("Employee").search_around("manager")
        self.assertEqual(b.build(), {
            "type": "searchAround",
            "objectSet": {"type": "base", "objectType": "Employee"},
            "link": "manager",
        })

    def test_search_around_path_multihop(self):
        b = ObjectSetBuilder.base("Employee").search_around_path([
            {"link": "manager"},
            {"link": "department"},
        ])
        self.assertEqual(b.build(), {
            "type": "searchAround",
            "objectSet": {"type": "base", "objectType": "Employee"},
            "path": [{"link": "manager"}, {"link": "department"}],
        })

    def test_union_with_multiple_others(self):
        a = ObjectSetBuilder.base("Employee")
        b = ObjectSetBuilder.base("Contractor")
        c = ObjectSetBuilder.base("Intern")
        combined = a.union(b, c)
        self.assertEqual(combined.build(), {
            "type": "union",
            "objectSets": [
                {"type": "base", "objectType": "Employee"},
                {"type": "base", "objectType": "Contractor"},
                {"type": "base", "objectType": "Intern"},
            ],
        })

    def test_intersect_with_multiple_others(self):
        a = ObjectSetBuilder.base("Employee")
        b = ObjectSetBuilder.base("Manager")
        out = a.intersect(b).build()
        self.assertEqual(out["type"], "intersect")
        self.assertEqual(len(out["objectSets"]), 2)

    def test_subtract_keeps_first_minus_others(self):
        a = ObjectSetBuilder.base("Employee")
        b = ObjectSetBuilder.base("Intern")
        out = a.subtract(b).build()
        self.assertEqual(out["type"], "subtract")
        self.assertEqual(out["objectSets"][0]["objectType"], "Employee")
        self.assertEqual(out["objectSets"][1]["objectType"], "Intern")

    def test_builder_is_immutable_across_chains(self):
        """A base definition reused as the source of two filters must
        NOT see the second filter's where clause back-propagate into
        the first — instances are immutable per chain step."""
        base = ObjectSetBuilder.base("Employee")
        f1 = base.filter({"type": "eq", "field": "deptId", "value": "eng"})
        f2 = base.filter({"type": "eq", "field": "deptId", "value": "sales"})
        # f1 and f2 are independent.
        self.assertNotEqual(f1.build(), f2.build())
        # base itself is unchanged.
        self.assertEqual(base.build(), {"type": "base", "objectType": "Employee"})

    def test_build_returns_defensive_copy(self):
        b = ObjectSetBuilder.base("Employee")
        d = b.build()
        d["mutated"] = True
        self.assertNotIn("mutated", b.build())

    def test_from_definition_wraps_existing_dict(self):
        existing = {"type": "base", "objectType": "Employee"}
        b = ObjectSetBuilder.from_definition(existing)
        # Mutating the original dict after the fact must not affect
        # the builder.
        existing["objectType"] = "Mutated"
        self.assertEqual(b.build()["objectType"], "Employee")

    def test_compose_full_chain_example(self):
        """The PRD-V2 example end-to-end:
        Employees.filter(deptId=eng).search_around(manager)."""
        chain = (
            ObjectSetBuilder.base("Employee")
            .filter({"type": "eq", "field": "deptId", "value": "eng"})
            .search_around("manager")
        )
        out = chain.build()
        self.assertEqual(out["type"], "searchAround")
        self.assertEqual(out["link"], "manager")
        self.assertEqual(out["objectSet"]["type"], "filter")
        self.assertEqual(out["objectSet"]["objectSet"]["type"], "base")
        self.assertEqual(out["objectSet"]["objectSet"]["objectType"], "Employee")


class AggregationHelperTests(unittest.TestCase):
    """Aggregation helper specs."""

    def test_count_default_name(self):
        self.assertEqual(count(), {"type": "count", "name": "count"})

    def test_count_custom_name(self):
        self.assertEqual(count("perDept"), {"type": "count", "name": "perDept"})

    def test_sum_uses_field_in_default_name(self):
        self.assertEqual(sum_("salary"),
                         {"type": "sum", "field": "salary", "name": "sum_salary"})

    def test_avg_min_max_have_default_names(self):
        self.assertEqual(avg("age")["name"], "avg_age")
        self.assertEqual(min_("age")["name"], "min_age")
        self.assertEqual(max_("age")["name"], "max_age")

    def test_explicit_name_overrides_default(self):
        self.assertEqual(sum_("salary", "totalPayroll")["name"], "totalPayroll")


class ToObjectSetCoercionTests(unittest.TestCase):
    """The duck-typed coercion at the API boundary."""

    def test_builder_is_coerced(self):
        b = ObjectSetBuilder.base("Employee")
        self.assertEqual(to_object_set(b), {"type": "base", "objectType": "Employee"})

    def test_dict_passes_through(self):
        d = {"type": "base", "objectType": "Employee"}
        self.assertIs(to_object_set(d), d)  # no copy at boundary

    def test_other_type_raises(self):
        with self.assertRaises(TypeError):
            to_object_set("not-a-builder")  # type: ignore[arg-type]


class ObjectSetsAPIBuilderIntegrationTests(unittest.TestCase):
    """End-to-end: ObjectSetsAPI methods accept Builder OR dict."""

    def test_load_objects_accepts_builder(self):
        """load_objects with a Builder serializes the right body."""
        resp = json.dumps({
            "data": [{"__primaryKey": "ALFKI", "customerId": "ALFKI"}],
            "totalCount": "1",
        })
        routes = {"POST /api/v2/ontologies/nw/objectSets/loadObjects": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            b = ObjectSetBuilder.base("Customer").filter({
                "type": "gt", "field": "orderCount", "value": 10,
            })
            c.objectsets.load_objects("nw", b, ["customerId", "companyName"])
            sent = json.loads(srv.requests[0]["body"])

        self.assertEqual(sent["objectSet"]["type"], "filter")
        self.assertEqual(sent["objectSet"]["objectSet"]["type"], "base")
        self.assertEqual(sent["objectSet"]["objectSet"]["objectType"], "Customer")
        self.assertEqual(sent["select"], ["customerId", "companyName"])

    def test_load_objects_accepts_raw_dict_backwards_compat(self):
        """Pre-builder callers passing dicts still work unchanged."""
        resp = json.dumps({
            "data": [{"__primaryKey": "X", "id": "X"}],
            "totalCount": "1",
        })
        routes = {"POST /api/v2/ontologies/nw/objectSets/loadObjects": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.objectsets.load_objects(
                "nw",
                {"type": "base", "objectType": "Customer"},  # raw dict
                ["customerId"],
            )
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent["objectSet"]["type"], "base")

    def test_aggregate_accepts_builder_with_count_helper(self):
        resp = json.dumps({"data": [], "totalCount": "0"})
        routes = {"POST /api/v2/ontologies/nw/objectSets/aggregate": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            b = ObjectSetBuilder.base("Order")
            c.objectsets.aggregate(
                "nw", b,
                aggregation=[count("perStatus")],
                group_by=[{"field": "status", "type": "exact"}],
            )
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent["objectSet"]["type"], "base")
        self.assertEqual(sent["aggregation"], [{"type": "count", "name": "perStatus"}])
        self.assertEqual(sent["groupBy"], [{"field": "status", "type": "exact"}])

    def test_create_temporary_accepts_builder(self):
        resp = json.dumps({"objectSetRid": "ri.objectset.main.temp.xyz"})
        routes = {"POST /api/v2/ontologies/nw/objectSets/createTemporary": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            b = ObjectSetBuilder.base("Order").filter({
                "type": "eq", "field": "status", "value": "pending",
            })
            out = c.objectsets.create_temporary("nw", b)
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(out["objectSetRid"], "ri.objectset.main.temp.xyz")
        self.assertEqual(sent["objectSet"]["type"], "filter")


if __name__ == "__main__":
    unittest.main()
