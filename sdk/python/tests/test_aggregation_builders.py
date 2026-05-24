"""BDD acceptance tests for the round-51 aggregation builder
extensions (PRD-V2 Gap Python-SDK Aggregation 10% → close).

Round 43 added the ObjectSetBuilder plus ``count``, ``sum_``, ``avg``,
``min_``, ``max_`` aggregation helpers. The remaining server-side
metric types (approximateDistinct, exactDistinct, standardDeviation,
variance, collectList, approximatePercentile) and the five Foundry
groupBy types (exact, fixedWidth, range, duration, ...) had no
Python-side helpers, so callers had to remember the wire shape.

Round 51 closes that by adding:
  - Metric helpers: approx_distinct, exact_distinct, stddev,
    variance, collect_list, approx_percentile, approx_percentiles
  - GroupBy helpers: exact_group, fixed_width_group, range_group,
    duration_group
  - AggregationResponse / AggregationRow typed wrappers so callers
    can inspect ``.accuracy`` / ``.data`` / ``.metrics`` instead of
    digging through nested dicts.

These tests assert the wire shapes the Go engine accepts (see
pkg/oss/aggregation/engine.go AggregationSpec / GroupBySpec) plus
the parsing of the response shape (pkg/oss/aggregation/response.go).
"""
from __future__ import annotations

import unittest

from weave_client.builders import (
    AggregationResponse,
    AggregationRow,
    approx_distinct,
    approx_percentile,
    approx_percentiles,
    collect_list,
    duration_group,
    exact_distinct,
    exact_group,
    fixed_width_group,
    parse_aggregation_response,
    range_group,
    stddev,
    variance,
)


class MetricHelperShapeTests(unittest.TestCase):
    """Each new metric helper must produce the exact wire shape the
    Go AggregationSpec unmarshaller accepts."""

    def test_approx_distinct_default_name(self):
        self.assertEqual(
            approx_distinct("customerId"),
            {"type": "approximateDistinct", "field": "customerId",
             "name": "approxDistinct_customerId"},
        )

    def test_approx_distinct_with_name_and_precision(self):
        self.assertEqual(
            approx_distinct("customerId", name="uniqueCustomers", precision=16),
            {"type": "approximateDistinct", "field": "customerId",
             "name": "uniqueCustomers", "precision": 16},
        )

    def test_exact_distinct_shape(self):
        self.assertEqual(
            exact_distinct("region"),
            {"type": "exactDistinct", "field": "region",
             "name": "exactDistinct_region"},
        )

    def test_stddev_shape(self):
        self.assertEqual(
            stddev("salary", name="salaryStdDev"),
            {"type": "standardDeviation", "field": "salary",
             "name": "salaryStdDev"},
        )

    def test_variance_shape(self):
        self.assertEqual(
            variance("salary"),
            {"type": "variance", "field": "salary",
             "name": "variance_salary"},
        )

    def test_collect_list_default(self):
        self.assertEqual(
            collect_list("region"),
            {"type": "collectList", "field": "region",
             "name": "collectList_region"},
        )

    def test_collect_list_with_max_items(self):
        self.assertEqual(
            collect_list("region", name="firstTen", max_items=10),
            {"type": "collectList", "field": "region",
             "name": "firstTen", "maxItems": 10},
        )

    def test_approx_percentile_scalar(self):
        # Server-side: percentile is a single float 0..100; result is scalar.
        self.assertEqual(
            approx_percentile("latencyMs", 95.0, name="p95"),
            {"type": "approximatePercentile", "field": "latencyMs",
             "name": "p95", "percentile": 95.0},
        )

    def test_approx_percentile_default_name_uses_percentile(self):
        # When name omitted the helper synthesises p{N}_field so the
        # default column label is interpretable in dashboards.
        self.assertEqual(
            approx_percentile("latencyMs", 50.0),
            {"type": "approximatePercentile", "field": "latencyMs",
             "name": "p50_latencyMs", "percentile": 50.0},
        )

    def test_approx_percentiles_batch(self):
        # Server-side: percentiles is a list of floats; result is a map.
        self.assertEqual(
            approx_percentiles("latencyMs", [50.0, 95.0, 99.0], name="latencyPs"),
            {"type": "approximatePercentile", "field": "latencyMs",
             "name": "latencyPs", "percentiles": [50.0, 95.0, 99.0]},
        )

    def test_approx_percentile_with_compression_override(self):
        self.assertEqual(
            approx_percentile("latencyMs", 99.0, compression=200.0),
            {"type": "approximatePercentile", "field": "latencyMs",
             "name": "p99_latencyMs", "percentile": 99.0,
             "compression": 200.0},
        )


class GroupByHelperShapeTests(unittest.TestCase):
    """Each new groupBy helper must produce the exact wire shape the
    Go GroupBySpec unmarshaller accepts."""

    def test_exact_group_minimum(self):
        self.assertEqual(
            exact_group("department"),
            {"type": "exact", "field": "department"},
        )

    def test_exact_group_with_max_groups(self):
        self.assertEqual(
            exact_group("department", max_groups=50),
            {"type": "exact", "field": "department", "maxGroupCount": 50},
        )

    def test_fixed_width_group_shape(self):
        self.assertEqual(
            fixed_width_group("age", width=10.0),
            {"type": "fixedWidth", "field": "age", "fixedWidth": 10.0},
        )

    def test_range_group_with_ranges_list(self):
        # Server accepts both "range" (singular alias) and "ranges" types;
        # builder normalises to the canonical "range" form.
        ranges = [
            {"start": 0, "end": 100},
            {"start": 100, "end": 500},
            {"start": 500, "end": 1000},
        ]
        self.assertEqual(
            range_group("price", ranges=ranges),
            {"type": "range", "field": "price", "ranges": ranges},
        )

    def test_duration_group_iso8601(self):
        # ISO-8601 duration string (P1D / P1W / P1M / P1Y).
        self.assertEqual(
            duration_group("orderDate", duration="P1M"),
            {"type": "duration", "field": "orderDate", "duration": "P1M"},
        )

    def test_duration_group_unit_value_form(self):
        # Alternate {unit, value} server form (engine.go DurationValue).
        self.assertEqual(
            duration_group("orderDate", unit="DAYS", value=7),
            {"type": "duration", "field": "orderDate",
             "value": {"unit": "DAYS", "value": 7}},
        )

    def test_duration_group_rejects_both_forms(self):
        with self.assertRaises(ValueError):
            duration_group("orderDate", duration="P1M", unit="DAYS", value=7)

    def test_duration_group_requires_one_form(self):
        with self.assertRaises(ValueError):
            duration_group("orderDate")


class AggregationResponseParsingTests(unittest.TestCase):
    """parse_aggregation_response() turns the raw server JSON into a
    typed wrapper so callers can inspect accuracy + data directly."""

    def test_parses_flat_response_with_accuracy(self):
        raw = {
            "excludedItems": 0,
            "accuracy": "APPROXIMATE",
            "data": [
                {
                    "group": {"department": "eng"},
                    "metrics": [
                        {"name": "count", "value": 42},
                        {"name": "avgSalary", "value": 125000.5},
                    ],
                },
                {
                    "group": {"department": "sales"},
                    "metrics": [
                        {"name": "count", "value": 17},
                        {"name": "avgSalary", "value": 95000.0},
                    ],
                },
            ],
        }
        resp = parse_aggregation_response(raw)
        self.assertIsInstance(resp, AggregationResponse)
        self.assertEqual(resp.accuracy, "APPROXIMATE")
        self.assertEqual(resp.excluded_items, 0)
        self.assertEqual(len(resp.data), 2)
        self.assertIsInstance(resp.data[0], AggregationRow)
        self.assertEqual(resp.data[0].group, {"department": "eng"})
        self.assertEqual(resp.data[0].metric("count"), 42)
        self.assertEqual(resp.data[0].metric("avgSalary"), 125000.5)

    def test_parses_response_without_groups(self):
        raw = {
            "excludedItems": 5,
            "accuracy": "ACCURATE",
            "data": [
                {"metrics": [{"name": "total", "value": 1000}]},
            ],
        }
        resp = parse_aggregation_response(raw)
        self.assertEqual(resp.accuracy, "ACCURATE")
        self.assertEqual(resp.excluded_items, 5)
        self.assertIsNone(resp.data[0].group)
        self.assertEqual(resp.data[0].metric("total"), 1000)

    def test_metric_lookup_missing_returns_default(self):
        row = AggregationRow(group=None, metrics={"count": 10})
        self.assertEqual(row.metric("count"), 10)
        self.assertIsNone(row.metric("nonExistent"))
        self.assertEqual(row.metric("nonExistent", default=0), 0)

    def test_parses_sub_aggregations(self):
        raw = {
            "excludedItems": 0,
            "accuracy": "ACCURATE",
            "data": [
                {
                    "group": {"department": "eng"},
                    "metrics": [{"name": "count", "value": 42}],
                    "subAggregations": {
                        "byLevel": {
                            "excludedItems": 0,
                            "accuracy": "ACCURATE",
                            "data": [
                                {"group": {"level": "L5"}, "metrics": [{"name": "count", "value": 12}]},
                                {"group": {"level": "L6"}, "metrics": [{"name": "count", "value": 30}]},
                            ],
                        },
                    },
                },
            ],
        }
        resp = parse_aggregation_response(raw)
        sub = resp.data[0].sub_aggregations["byLevel"]
        self.assertIsInstance(sub, AggregationResponse)
        self.assertEqual(len(sub.data), 2)
        self.assertEqual(sub.data[0].metric("count"), 12)


if __name__ == "__main__":
    unittest.main()
