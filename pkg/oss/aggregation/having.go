package aggregation

import "fmt"

// havingOps is the closed set of comparison operators supported by a
// HavingClause. Using a small closed enum keeps the HTTP surface stable and
// avoids accidentally accepting SQL-ish aliases that would later need
// deprecation.
var havingOps = map[string]struct{}{
	"eq":  {},
	"ne":  {},
	"gt":  {},
	"gte": {},
	"lt":  {},
	"lte": {},
}

// ValidateHaving rejects obvious authoring errors before execution: every
// clause must name a metric and use a known op. Numeric coercion of the
// metric value happens at application time — a missing metric on a row is
// treated as a failed comparison, not a validation error, so partially
// computed responses still filter cleanly.
func ValidateHaving(clauses []HavingClause) error {
	for i, c := range clauses {
		if c.Metric == "" {
			return fmt.Errorf("having[%d]: metric is required", i)
		}
		if _, ok := havingOps[c.Op]; !ok {
			return fmt.Errorf("having[%d]: unsupported op %q (supported: eq, ne, gt, gte, lt, lte)", i, c.Op)
		}
	}
	return nil
}

// ApplyHaving filters rows by evaluating every clause against each row's
// metrics. A row passes only when EVERY clause evaluates true (AND
// semantics); any metric that is absent or non-numeric fails the clause.
func ApplyHaving(rows []AggregationRow, clauses []HavingClause) []AggregationRow {
	if len(clauses) == 0 || len(rows) == 0 {
		return rows
	}
	out := make([]AggregationRow, 0, len(rows))
	for _, row := range rows {
		if rowMatchesHaving(row, clauses) {
			out = append(out, row)
		}
	}
	return out
}

func rowMatchesHaving(row AggregationRow, clauses []HavingClause) bool {
	for _, c := range clauses {
		v, ok := lookupMetric(row.Metrics, c.Metric)
		if !ok {
			return false
		}
		num, ok := coerceHavingNumber(v)
		if !ok {
			return false
		}
		if !compareHaving(num, c.Op, c.Value) {
			return false
		}
	}
	return true
}

func lookupMetric(metrics []MetricValue, name string) (interface{}, bool) {
	for _, m := range metrics {
		if m.Name == name {
			return m.Value, true
		}
	}
	return nil, false
}

// coerceHavingNumber accepts the numeric types that MetricValue.Value can
// carry (uint64 from count / exactDistinct / approximateDistinct, float64 from
// numeric aggregations) plus the handful of int flavours returned by the
// derived-aggregation path.
func coerceHavingNumber(v interface{}) (float64, bool) {
	switch vv := v.(type) {
	case float64:
		return vv, true
	case float32:
		return float64(vv), true
	case int:
		return float64(vv), true
	case int8:
		return float64(vv), true
	case int16:
		return float64(vv), true
	case int32:
		return float64(vv), true
	case int64:
		return float64(vv), true
	case uint:
		return float64(vv), true
	case uint8:
		return float64(vv), true
	case uint16:
		return float64(vv), true
	case uint32:
		return float64(vv), true
	case uint64:
		return float64(vv), true
	default:
		return 0, false
	}
}

func compareHaving(lhs float64, op string, rhs float64) bool {
	switch op {
	case "eq":
		return lhs == rhs
	case "ne":
		return lhs != rhs
	case "gt":
		return lhs > rhs
	case "gte":
		return lhs >= rhs
	case "lt":
		return lhs < rhs
	case "lte":
		return lhs <= rhs
	default:
		return false
	}
}
