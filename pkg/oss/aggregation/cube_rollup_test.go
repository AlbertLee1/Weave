package aggregation

import (
	"reflect"
	"sort"
	"testing"
)

// TestExpandGroupByCombinations_Cube verifies that a 2-dim cube emits exactly
// 4 subsets in full-first, empty-last order and that a 3-dim cube emits 8.
func TestExpandGroupByCombinations_Cube(t *testing.T) {
	combos := ExpandGroupByCombinations(2, true, false)
	// 2^2 = 4 subsets.
	if len(combos) != 4 {
		t.Fatalf("cube(2): expected 4 combos, got %d: %v", len(combos), combos)
	}
	// Full set first.
	if !reflect.DeepEqual(combos[0], []int{0, 1}) {
		t.Errorf("cube(2)[0] = %v, want [0 1]", combos[0])
	}
	// Grand total (empty) last.
	if len(combos[len(combos)-1]) != 0 {
		t.Errorf("cube(2) last = %v, want empty subset", combos[len(combos)-1])
	}
	// Every subset appears exactly once.
	seen := map[string]bool{}
	for _, sub := range combos {
		key := subsetKey(sub)
		if seen[key] {
			t.Errorf("duplicate subset %v", sub)
		}
		seen[key] = true
	}

	// 2^3 = 8 subsets for 3 dims.
	combos3 := ExpandGroupByCombinations(3, true, false)
	if len(combos3) != 8 {
		t.Fatalf("cube(3): expected 8 combos, got %d", len(combos3))
	}
}

// TestExpandGroupByCombinations_Rollup verifies that rollup on N dims emits
// the hierarchical chain of N+1 subsets from full → empty.
func TestExpandGroupByCombinations_Rollup(t *testing.T) {
	combos := ExpandGroupByCombinations(3, false, true)
	// N+1 = 4 subsets.
	expected := [][]int{{0, 1, 2}, {0, 1}, {0}, nil}
	if len(combos) != len(expected) {
		t.Fatalf("rollup(3): expected %d combos, got %d: %v", len(expected), len(combos), combos)
	}
	for i, sub := range combos {
		got := sub
		want := expected[i]
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("rollup(3)[%d] = %v, want %v", i, got, want)
		}
	}
}

// TestExpandGroupByCombinations_CubeWinsOverRollup verifies that Cube takes
// precedence when both flags are set (full 2^N combos).
func TestExpandGroupByCombinations_CubeWinsOverRollup(t *testing.T) {
	combos := ExpandGroupByCombinations(3, true, true)
	if len(combos) != 8 {
		t.Errorf("cube+rollup (cube wins): expected 8 combos, got %d", len(combos))
	}
}

// TestExpandGroupByCombinations_Neither verifies that when both flags are
// false, a single full-index subset is returned.
func TestExpandGroupByCombinations_Neither(t *testing.T) {
	combos := ExpandGroupByCombinations(2, false, false)
	if len(combos) != 1 {
		t.Fatalf("neither: expected 1 combo, got %d", len(combos))
	}
	if !reflect.DeepEqual(combos[0], []int{0, 1}) {
		t.Errorf("neither[0] = %v, want [0 1]", combos[0])
	}
}

// TestAggregate_Cube_TwoDim verifies that a 2-dim cube on the department×active
// cross-tab returns 4 groupings worth of rows: (dept×active) + (dept) + (active)
// + grand total.
func TestAggregate_Cube_TwoDim(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count"}},
		GroupBy: []GroupBySpec{
			{Type: "exact", Field: "department"},
			{Type: "exact", Field: "active"},
		},
		Cube: true,
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}

	// Expected row counts per subset (setupAggIndex has 3 departments × 2
	// active states: eng×T=2, sales×T=1, sales×F=1, hr×T=1 ⇒ 4 leaf rows):
	//   subset {dept, active}: 4 rows
	//   subset {dept}:         3 rows (one per department)
	//   subset {active}:       2 rows (T/F)
	//   subset {}:             1 row (grand total, 5 docs)
	// Total: 4 + 3 + 2 + 1 = 10.
	if len(resp.Data) != 10 {
		t.Fatalf("expected 10 rows (4+3+2+1), got %d: %+v", len(resp.Data), resp.Data)
	}

	// The grand total row has an empty Group; count must equal total docs.
	var grandTotalRows int
	for _, row := range resp.Data {
		if len(row.Group) == 0 {
			grandTotalRows++
			c, _ := findMetric(row.Metrics, "count")
			if got, want := c.(uint64), uint64(5); got != want {
				t.Errorf("grand total count = %d, want %d", got, want)
			}
		}
	}
	if grandTotalRows != 1 {
		t.Errorf("expected exactly 1 grand-total row, got %d", grandTotalRows)
	}

	// Sum of counts in the {dept, active} subset (rows with both keys) must equal
	// total docs; same for the two 1-dim subsets and the grand total.
	sumsBySubset := map[string]uint64{}
	for _, row := range resp.Data {
		hasDept := row.Group["department"] != nil
		hasActive := row.Group["active"] != nil
		var key string
		switch {
		case hasDept && hasActive:
			key = "dept+active"
		case hasDept:
			key = "dept"
		case hasActive:
			key = "active"
		default:
			key = "grand"
		}
		c, _ := findMetric(row.Metrics, "count")
		sumsBySubset[key] += c.(uint64)
	}
	for subset, total := range sumsBySubset {
		if total != 5 {
			t.Errorf("sum(%s) = %d, want 5", subset, total)
		}
	}
}

// TestAggregate_Rollup_TwoDim verifies that rollup on department×active emits
// only 3 groupings (hierarchical chain): (dept×active) + (dept) + grand total —
// NOT the (active-only) subset that cube would include.
func TestAggregate_Rollup_TwoDim(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count"}},
		GroupBy: []GroupBySpec{
			{Type: "exact", Field: "department"},
			{Type: "exact", Field: "active"},
		},
		Rollup: true,
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}

	// Expected rows: 4 (leaf) + 3 (dept only) + 1 (grand total) = 8.
	if len(resp.Data) != 8 {
		t.Fatalf("rollup: expected 8 rows (4+3+1), got %d: %+v", len(resp.Data), resp.Data)
	}

	// Ensure no active-only row (would have active set but no department).
	for _, row := range resp.Data {
		if row.Group["department"] == nil && row.Group["active"] != nil {
			t.Errorf("rollup MUST NOT emit active-only rows; got %+v", row.Group)
		}
	}
	// One grand total row.
	var grandTotals int
	for _, row := range resp.Data {
		if len(row.Group) == 0 {
			grandTotals++
			c, _ := findMetric(row.Metrics, "count")
			if got := c.(uint64); got != 5 {
				t.Errorf("grand total count = %d, want 5", got)
			}
		}
	}
	if grandTotals != 1 {
		t.Errorf("expected 1 grand total row, got %d", grandTotals)
	}
}

// TestAggregate_Cube_Sum verifies that sum metric totals for dept-only rollups
// equal the per-department sum of salaries.
func TestAggregate_Cube_Sum(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "sum", Field: "salary", Name: "salarySum"}},
		GroupBy: []GroupBySpec{
			{Type: "exact", Field: "department"},
			{Type: "exact", Field: "active"},
		},
		Cube: true,
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}

	// Per-department totals (from setupAggIndex):
	//   engineering: 100k + 120k = 220k
	//   sales:       80k  + 90k  = 170k
	//   hr:          75k
	// Grand total: 465k
	wantByDept := map[string]float64{
		"engineering": 220000,
		"sales":       170000,
		"hr":          75000,
	}
	for _, row := range resp.Data {
		// Dept-only row: dept present, active absent.
		if d, ok := row.Group["department"].(string); ok && row.Group["active"] == nil {
			if want, found := wantByDept[d]; found {
				got, _ := findMetric(row.Metrics, "salarySum")
				if math := got.(float64); math != want {
					t.Errorf("dept-only sum[%s] = %v, want %v", d, math, want)
				}
			}
		}
		// Grand total row: both keys absent.
		if len(row.Group) == 0 {
			got, _ := findMetric(row.Metrics, "salarySum")
			if math := got.(float64); math != 465000 {
				t.Errorf("grand total salary sum = %v, want 465000", math)
			}
		}
	}
}

// TestAggregate_Cube_HavingAppliesPostExpansion verifies that Having filters
// the concatenated row list after cube expansion (a having on count >= 2 must
// drop the single-doc leaf and any grouping whose entire scope is <2).
func TestAggregate_Cube_HavingAppliesPostExpansion(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count"}},
		GroupBy: []GroupBySpec{
			{Type: "exact", Field: "department"},
		},
		Cube: true,
		Having: []HavingClause{
			{Metric: "count", Op: "gte", Value: 2},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}

	// After the filter, only the engineering row (count=2), the sales row
	// (count=2), and the grand total (count=5) remain. HR (count=1) is
	// filtered.
	for _, row := range resp.Data {
		c, _ := findMetric(row.Metrics, "count")
		if c.(uint64) < 2 {
			t.Errorf("row with count<2 survived Having: %+v", row)
		}
	}
	// Verify at least one non-empty Group survives (so we know Having didn't
	// collapse everything to grand total only).
	var hasDept int
	for _, row := range resp.Data {
		if _, ok := row.Group["department"]; ok {
			hasDept++
		}
	}
	if hasDept < 1 {
		t.Errorf("expected at least one department-keyed row to survive, got 0")
	}
}

// TestAggregate_CubeOnEmptyGroupBy is a no-op path: cube=true without any
// groupBy must still work and produce a single grand-total row (same as the
// simple path).
func TestAggregate_CubeOnEmptyGroupBy(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count"}},
		Cube:         true,
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("cube-without-groupBy: expected 1 row, got %d", len(resp.Data))
	}
	c, _ := findMetric(resp.Data[0].Metrics, "count")
	if got := c.(uint64); got != 5 {
		t.Errorf("grand total count = %d, want 5", got)
	}
}

// subsetKey serializes a sorted subset for map equality checks.
func subsetKey(sub []int) string {
	sorted := append([]int(nil), sub...)
	sort.Ints(sorted)
	key := ""
	for _, v := range sorted {
		key += "," + itoa(v)
	}
	return key
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
