package aggregation

import (
	"fmt"
	"sort"
	"strings"
)

// resolvedMetricName mirrors the metric-naming logic in bleve_agg.go: an
// explicit Name wins, otherwise "<field>.<type>" (or just "<type>" for
// field-less metrics like count). Kept here so the ordering pass resolves the
// same name the response actually carries.
func resolvedMetricName(spec AggregationSpec) string {
	if spec.Name != "" {
		return spec.Name
	}
	if spec.Field != "" {
		return spec.Field + "." + spec.Type
	}
	return spec.Type
}

// normalizeDirection upper-cases and validates an aggregation metric ordering
// direction. Returns ("", nil) for an empty (no-ordering) direction.
func normalizeDirection(d string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(d)) {
	case "":
		return "", nil
	case "ASC":
		return "ASC", nil
	case "DESC":
		return "DESC", nil
	default:
		return "", fmt.Errorf("unknown metric direction %q (allowed: ASC / DESC)", d)
	}
}

// validateMetricDirections rejects any spec carrying an unrecognised direction
// so a typo surfaces as a 400 instead of silently disabling ordering.
func validateMetricDirections(specs []AggregationSpec) error {
	for i, s := range specs {
		if _, err := normalizeDirection(s.Direction); err != nil {
			return fmt.Errorf("aggregation[%d]: %w", i, err)
		}
	}
	return nil
}

// orderingMetric returns the (name, direction) of the first metric that
// declares a sort direction, or ("","") when none do. Palantir's aggregation
// grammar attaches the ordering direction to a single metric ("按聚合值排序",
// syntax ref L623); when several carry one the first wins.
func orderingMetric(specs []AggregationSpec) (string, string) {
	for _, s := range specs {
		dir, _ := normalizeDirection(s.Direction)
		if dir != "" {
			return resolvedMetricName(s), dir
		}
	}
	return "", ""
}

// applyMetricOrdering stable-sorts groupBy result rows by the first
// direction-bearing metric's value. Rows whose metric value is missing or
// non-numeric sort to the end so a partial result stays deterministic. A
// request with no directed metric (or fewer than two rows) returns data
// unchanged, preserving the engine's default facet ordering.
func applyMetricOrdering(data []AggregationRow, specs []AggregationSpec) []AggregationRow {
	name, dir := orderingMetric(specs)
	if name == "" || len(data) < 2 {
		return data
	}
	value := func(row AggregationRow) (float64, bool) {
		for _, m := range row.Metrics {
			if m.Name == name {
				return metricOrderValue(m.Value)
			}
		}
		return 0, false
	}
	sort.SliceStable(data, func(i, j int) bool {
		vi, oki := value(data[i])
		vj, okj := value(data[j])
		if oki != okj {
			return oki // present values sort ahead of missing ones
		}
		if !oki {
			return false // both missing: preserve existing order
		}
		if dir == "DESC" {
			return vi > vj
		}
		return vi < vj
	})
	return data
}

// metricOrderValue coerces a MetricValue.Value to float64 for ordering. count
// is uint64, sum/avg/min/max are float64; collectList / unparseable values are
// reported as non-numeric and sort to the end.
func metricOrderValue(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	default:
		return 0, false
	}
}
