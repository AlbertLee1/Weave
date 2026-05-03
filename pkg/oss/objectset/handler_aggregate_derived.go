package objectset

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search"
	"github.com/liyang/weave/pkg/oss/aggregation"
)

// aggregationNeedsDerivedPath returns true when at least one metric in req
// references a field name that appears in any row of derivedValues. A metric
// whose Field matches a derived property cannot be computed from the base
// Bleve index alone — the values live in Result.DerivedValues, attached by
// executeWithProperties.
func aggregationNeedsDerivedPath(aggs []aggregation.AggregationSpec, derivedValues map[string]map[string]interface{}) bool {
	if len(derivedValues) == 0 {
		return false
	}
	derivedFields := collectDerivedFieldNames(derivedValues)
	if len(derivedFields) == 0 {
		return false
	}
	for _, spec := range aggs {
		if spec.Field != "" && derivedFields[spec.Field] {
			return true
		}
	}
	return false
}

// collectDerivedFieldNames flattens the per-PK derived value map into the
// set of all derived property names that appeared in at least one row.
func collectDerivedFieldNames(derivedValues map[string]map[string]interface{}) map[string]bool {
	out := map[string]bool{}
	for _, row := range derivedValues {
		for name := range row {
			out[name] = true
		}
	}
	return out
}

// aggregateWithDerived computes aggregations when at least one metric
// references a withProperties-derived field. Only groupBy.type = "exact" is
// supported in this path — derived values live entirely in memory, not in
// any Bleve field, so faceting groupBy modes (fixedWidth / ranges / duration
// / topValues) cannot apply.
//
// The function fetches base-field values (groupBy fields plus any non-derived
// metric fields) for every primary key in the ObjectSet in a single Bleve
// lookup, then groups rows and computes metrics entirely in Go. Rows with no
// derived value for a derived metric field are skipped for that metric, so
// avg/min/max over sparse derived maps still return finite values whenever at
// least one row contributes.
func (h *Handler) aggregateWithDerived(ctx context.Context, result *Result, req *AggregateObjectSetRequest) (*aggregation.AggregationResponse, error) {
	startTime := time.Now()
	for _, gb := range req.GroupBy {
		if gb.Type != "" && gb.Type != "exact" {
			return nil, fmt.Errorf("derived-field aggregation only supports exact groupBy, got %q", gb.Type)
		}
	}
	if err := aggregation.ValidateHaving(req.Having); err != nil {
		return nil, err
	}

	// US-382: pre-filter excludedItems against the resolved PK set BEFORE
	// any base-field fetch or metric pass. Mirrors the engine path's
	// "intersection count" semantics so a duplicated or out-of-scope PK
	// contributes zero to response.excludedItems.
	pks, excludedCount := applyDerivedExcludedItems(result.PrimaryKeys, req.ExcludedItems)

	derivedFields := collectDerivedFieldNames(result.DerivedValues)

	// Base fields we still need Bleve for: every groupBy field plus any
	// metric field that is NOT itself a derived property.
	baseFieldSet := map[string]bool{}
	for _, gb := range req.GroupBy {
		if gb.Field != "" {
			baseFieldSet[gb.Field] = true
		}
	}
	for _, spec := range req.Aggregation {
		if spec.Field == "" {
			continue
		}
		if !derivedFields[spec.Field] {
			baseFieldSet[spec.Field] = true
		}
	}
	baseFields := make([]string, 0, len(baseFieldSet))
	for f := range baseFieldSet {
		baseFields = append(baseFields, f)
	}

	// Fetch base fields for every PK in one shot.
	hitsByID := map[string]*search.DocumentMatch{}
	if len(baseFields) > 0 && len(pks) > 0 {
		searchReq := bleve.NewSearchRequest(bleve.NewDocIDQuery(pks))
		searchReq.Size = len(pks)
		searchReq.Fields = baseFields
		idxKey := scopedIndexKey(ctx, h.indexMgr, result.ObjectType)
		res, err := h.indexMgr.Search(idxKey, searchReq)
		if err != nil {
			return nil, fmt.Errorf("fetch derived groupBy fields: %w", err)
		}
		for _, hit := range res.Hits {
			hitsByID[hit.ID] = hit
		}
	}

	rows := make([]derivedAggRow, 0, len(pks))
	for _, pk := range pks {
		row := derivedAggRow{pk: pk, base: map[string]interface{}{}}
		if hit, ok := hitsByID[pk]; ok {
			for _, f := range baseFields {
				row.base[f] = flattenBleveField(hit.Fields[f])
			}
		}
		row.derived = result.DerivedValues[pk]
		rows = append(rows, row)
	}

	var data []aggregation.AggregationRow
	if req.Cube || req.Rollup {
		data = expandDerivedCubeOrRollup(rows, req.GroupBy, req.Aggregation, req.Cube, req.Rollup)
	} else {
		data = groupDerivedRows(rows, 0, req.GroupBy, req.Aggregation)
	}
	if len(req.Having) > 0 {
		data = aggregation.ApplyHaving(data, req.Having)
	}
	resp := &aggregation.AggregationResponse{
		Data:          data,
		Accuracy:      "ACCURATE",
		ExcludedItems: excludedCount,
	}
	resp.ComputeUsage = &aggregation.ComputeUsage{
		ScannedRows: int64(len(rows)),
		DurationMs:  time.Since(startTime).Milliseconds(),
		Accuracy:    resp.Accuracy,
	}
	return resp, nil
}

// applyDerivedExcludedItems mirrors engine.applyExcludedItems for the
// in-memory derived-field aggregation path: resolve the pks set against the
// caller-supplied exclusion list, deduplicate input, and report the count of
// PKs that were actually present in the ObjectSet (so duplicates and
// out-of-scope ids don't inflate response.excludedItems).
func applyDerivedExcludedItems(pks []string, excluded []string) ([]string, int) {
	if len(excluded) == 0 || len(pks) == 0 {
		return pks, 0
	}
	excludeSet := make(map[string]struct{}, len(excluded))
	for _, pk := range excluded {
		if pk == "" {
			continue
		}
		excludeSet[pk] = struct{}{}
	}
	if len(excludeSet) == 0 {
		return pks, 0
	}
	out := make([]string, 0, len(pks))
	excludedCount := 0
	for _, pk := range pks {
		if _, hit := excludeSet[pk]; hit {
			excludedCount++
			continue
		}
		out = append(out, pk)
	}
	return out, excludedCount
}

// expandDerivedCubeOrRollup mirrors the engine's Cube/Rollup path for the
// derived in-memory rows. Each subset of groupBys produces its own slice of
// AggregationRows via groupDerivedRows; concatenation order (full set first,
// grand total last for cube; hierarchical chain for rollup) matches the engine
// path. Non-grouped dimensions are absent from each row's Group map.
func expandDerivedCubeOrRollup(rows []derivedAggRow, gbs []aggregation.GroupBySpec, specs []aggregation.AggregationSpec, cube, rollup bool) []aggregation.AggregationRow {
	combos := aggregation.ExpandGroupByCombinations(len(gbs), cube, rollup)
	var out []aggregation.AggregationRow
	for _, subset := range combos {
		if len(subset) == 0 {
			out = append(out, aggregation.AggregationRow{
				Metrics: computeDerivedMetrics(rows, specs),
			})
			continue
		}
		subsetGBs := make([]aggregation.GroupBySpec, len(subset))
		for i, idx := range subset {
			subsetGBs[i] = gbs[idx]
		}
		out = append(out, groupDerivedRows(rows, 0, subsetGBs, specs)...)
	}
	return out
}

// derivedAggRow carries everything needed to emit a single row through a
// derived-aware aggregation: its primary key, the subset of base-index field
// values that participate in groupBy / base metrics, and the per-PK derived
// value map computed upstream by executeWithProperties.
type derivedAggRow struct {
	pk      string
	base    map[string]interface{}
	derived map[string]interface{}
}

// flattenBleveField collapses Bleve's multi-value field payload. Bleve
// surfaces singular fields as their raw value but multi-value (array) fields
// as []interface{}; downstream groupBy keying expects a scalar, so we take
// the first element.
func flattenBleveField(v interface{}) interface{} {
	switch vv := v.(type) {
	case []interface{}:
		if len(vv) == 0 {
			return nil
		}
		return vv[0]
	default:
		return vv
	}
}

// groupDerivedRows mirrors engine.recursiveGroupBy but operates on the
// in-memory slice built from Result.DerivedValues + Bleve base fields. It is
// the recursive core of the derived-aware aggregation path.
func groupDerivedRows(rows []derivedAggRow, depth int, gbs []aggregation.GroupBySpec, aggs []aggregation.AggregationSpec) []aggregation.AggregationRow {
	if depth >= len(gbs) {
		return []aggregation.AggregationRow{{Metrics: computeDerivedMetrics(rows, aggs)}}
	}
	gb := gbs[depth]
	buckets := map[string][]derivedAggRow{}
	bucketValues := map[string]interface{}{}
	for _, r := range rows {
		v := r.base[gb.Field]
		key := fmt.Sprint(v)
		if v == nil {
			key = "\x00nil"
		}
		buckets[key] = append(buckets[key], r)
		bucketValues[key] = v
	}
	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		vi, vj := bucketValues[keys[i]], bucketValues[keys[j]]
		iNil := vi == nil
		jNil := vj == nil
		if iNil && jNil {
			return false
		}
		if iNil {
			return false
		}
		if jNil {
			return true
		}
		return fmt.Sprint(vi) < fmt.Sprint(vj)
	})

	out := make([]aggregation.AggregationRow, 0, len(keys))
	for _, k := range keys {
		subRows := groupDerivedRows(buckets[k], depth+1, gbs, aggs)
		for _, sub := range subRows {
			merged := map[string]interface{}{gb.Field: bucketValues[k]}
			for kk, vv := range sub.Group {
				merged[kk] = vv
			}
			out = append(out, aggregation.AggregationRow{Group: merged, Metrics: sub.Metrics})
		}
	}
	return out
}

// computeDerivedMetrics runs per-spec numeric aggregation over an in-memory
// slice of rows. Supported metric types are count / min / max / sum / avg;
// any other spec type emits a nil value so callers get an explicit signal the
// metric was not computed rather than a silent zero.
func computeDerivedMetrics(rows []derivedAggRow, specs []aggregation.AggregationSpec) []aggregation.MetricValue {
	out := make([]aggregation.MetricValue, 0, len(specs))
	for _, spec := range specs {
		name := spec.Name
		if name == "" {
			name = spec.Type
			if spec.Field != "" {
				name = spec.Field + "." + spec.Type
			}
		}
		switch spec.Type {
		case "count":
			out = append(out, aggregation.MetricValue{Name: name, Value: uint64(len(rows))})
		case "min", "max", "sum", "avg":
			values := make([]float64, 0, len(rows))
			for _, r := range rows {
				v, ok := readRowNumber(r, spec.Field)
				if !ok {
					continue
				}
				values = append(values, v)
			}
			if len(values) == 0 {
				out = append(out, aggregation.MetricValue{Name: name, Value: nil})
				continue
			}
			switch spec.Type {
			case "min":
				m := values[0]
				for _, v := range values[1:] {
					if v < m {
						m = v
					}
				}
				out = append(out, aggregation.MetricValue{Name: name, Value: m})
			case "max":
				m := values[0]
				for _, v := range values[1:] {
					if v > m {
						m = v
					}
				}
				out = append(out, aggregation.MetricValue{Name: name, Value: m})
			case "sum":
				s := 0.0
				for _, v := range values {
					s += v
				}
				out = append(out, aggregation.MetricValue{Name: name, Value: s})
			case "avg":
				s := 0.0
				for _, v := range values {
					s += v
				}
				out = append(out, aggregation.MetricValue{Name: name, Value: s / float64(len(values))})
			}
		default:
			out = append(out, aggregation.MetricValue{Name: name, Value: nil})
		}
	}
	return out
}

// readRowNumber extracts a numeric value for `field` from a derivedAggRow,
// checking the per-PK derived map first (so a withProperties-attached value
// wins even if the base index happens to carry a shadow field of the same
// name) and then falling back to the Bleve-sourced base field values.
func readRowNumber(row derivedAggRow, field string) (float64, bool) {
	if field == "" {
		return 0, false
	}
	if v, ok := row.derived[field]; ok {
		if f, ok := coerceDerivedNumber(v); ok {
			return f, true
		}
	}
	if v, ok := row.base[field]; ok {
		if f, ok := coerceDerivedNumber(v); ok {
			return f, true
		}
	}
	return 0, false
}

// coerceDerivedNumber converts the interface{} values that can end up in a
// DerivedValues map (int/int64/uint/float32/float64) or a Bleve hit Field
// (usually float64) into a canonical float64 for metric math.
func coerceDerivedNumber(v interface{}) (float64, bool) {
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
