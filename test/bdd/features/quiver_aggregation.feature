Feature: Quiver multi-level aggregation over time-series points
  As a dashboard analyst
  I want the aggregate endpoint to compute time-bucketed sums, exact
  percentiles, and distinct-cardinality counts over a seeded set of
  time-series points
  So that the Quiver workbench can render trend, percentile, and
  uniqueness panels off the same OSS aggregate route.

  Background:
    Given a fresh weave database with migrations applied
    And the quiver ontology "bdd_quiver" is seeded with one metric_point object type and six time-series rows

  Scenario: Sum-by-1h — group by 1-hour duration buckets over the timestamp field
    When the analyst aggregates "bdd_quiver" "metric_point" with sum on "value" bucketed by 1 hour on "timestamp"
    Then the aggregate HTTP status code is 200
    And the aggregate response has 3 buckets
    And the aggregate bucket "2026-05-13T00:00:00Z" sum metric "value.sum" equals 30
    And the aggregate bucket "2026-05-13T01:00:00Z" sum metric "value.sum" equals 70
    And the aggregate bucket "2026-05-13T02:00:00Z" sum metric "value.sum" equals 110

  Scenario: Percentile — exact p50 / p95 / p99 of the value field
    When the analyst aggregates "bdd_quiver" "metric_point" exact percentile on "value" at "50,95,99"
    Then the aggregate HTTP status code is 200
    And the aggregate response has 1 row
    And the aggregate row 0 percentile metric "value.approximatePercentile" at "50" equals 30
    And the aggregate row 0 percentile metric "value.approximatePercentile" at "95" equals 60
    And the aggregate row 0 percentile metric "value.approximatePercentile" at "99" equals 60

  Scenario: Cardinality — exact distinct count of the host field
    When the analyst aggregates "bdd_quiver" "metric_point" exact distinct on "host"
    Then the aggregate HTTP status code is 200
    And the aggregate response has 1 row
    And the aggregate row 0 metric "host.approximateDistinct" equals 3
