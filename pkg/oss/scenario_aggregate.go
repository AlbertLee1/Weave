package oss

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/liyang/weave/pkg/oss/aggregation"
	"github.com/liyang/weave/pkg/oss/where"
	"github.com/liyang/weave/pkg/scenarios"
)

// AggregateWithOverlay folds scenario edits onto a base object set and runs
// an in-memory aggregation. This is the scenario-aware fallback for the
// /aggregate endpoint: when X-Scenario-Id is set the handler hydrates base
// rows via the Service layer, applies edits via FoldObject (per-key), then
// computes COUNT / SUM / AVG with optional single-level GROUP BY (exact).
//
// Trade-off: full in-memory pass — O(base + edits) — bypasses the Bleve
// index entirely. Phase 1 PoC. For large object sets the perf hit is
// acknowledged in the PRD Open Questions section. Unsupported in this PoC:
// fixedWidth / range / topValues groupBys, sub-aggregations, having, cube,
// rollup, approximate*, accuracy modes, exclude lists. Use the Bleve path
// (no X-Scenario-Id header) for those.
func AggregateWithOverlay(base []*WireObject, edits []scenarios.ScenarioEdit, req *aggregation.AggregationRequest) (*aggregation.AggregationResponse, error) {
	resp, _, err := AggregateWithOverlayAndConflicts(base, edits, req)
	return resp, err
}

// AggregateWithOverlayAndConflicts is the US-481 superset of
// AggregateWithOverlay. It additionally returns the per-object fold
// conflicts surfaced while replaying edits over the base object set. The
// conflicts list is the union across every base row plus any synthesised
// createObject row — callers can pivot per ConflictType when emitting audit.
func AggregateWithOverlayAndConflicts(base []*WireObject, edits []scenarios.ScenarioEdit, req *aggregation.AggregationRequest) (*aggregation.AggregationResponse, []scenarios.ScenarioConflict, error) {
	if req == nil {
		return nil, nil, fmt.Errorf("nil aggregation request")
	}
	if err := where.ValidateMatchClauseSupported(req.Where); err != nil {
		return nil, nil, fmt.Errorf("scenario overlay aggregation where: %w", err)
	}
	overlaid, conflicts := applyOverlayToObjectSetWithConflicts(base, edits, req.ObjectType)
	if req.Where != nil {
		overlaid = filterOverlayRowsByWhere(overlaid, req.Where)
	}

	// Single-level groupBy only. No groupBy → one synthetic bucket.
	type bucket struct {
		key  map[string]any
		rows []*WireObject
	}
	var buckets []*bucket
	switch len(req.GroupBy) {
	case 0:
		buckets = []*bucket{{key: nil, rows: overlaid}}
	case 1:
		field := req.GroupBy[0].Field
		if req.GroupBy[0].Type != "" && req.GroupBy[0].Type != "exact" {
			return nil, conflicts, fmt.Errorf("scenario overlay aggregation: only exact groupBy is supported (got %q)", req.GroupBy[0].Type)
		}
		byKey := map[string]*bucket{}
		var order []string
		for _, obj := range overlaid {
			v := obj.Properties[field]
			k := fmt.Sprintf("%v", v)
			b, ok := byKey[k]
			if !ok {
				b = &bucket{key: map[string]any{field: v}}
				byKey[k] = b
				order = append(order, k)
			}
			b.rows = append(b.rows, obj)
		}
		sort.Strings(order)
		for _, k := range order {
			buckets = append(buckets, byKey[k])
		}
	default:
		return nil, conflicts, fmt.Errorf("scenario overlay aggregation: multi-level groupBy not supported in PoC (got %d levels)", len(req.GroupBy))
	}

	rows := make([]aggregation.AggregationRow, 0, len(buckets))
	for _, b := range buckets {
		metrics, err := computeOverlayMetrics(b.rows, req.Aggregations)
		if err != nil {
			return nil, conflicts, err
		}
		rows = append(rows, aggregation.AggregationRow{
			Group:   b.key,
			Metrics: metrics,
		})
	}
	return &aggregation.AggregationResponse{
		Data:     rows,
		Accuracy: aggregation.AccuracyRequireAccurate,
	}, conflicts, nil
}

// applyOverlayToObjectSet folds edits over a set of base objects for a single
// objectType. Returns a fresh slice. Semantics:
//   - base rows whose object_id is touched by edits get FoldObject applied
//   - base rows untouched pass through unchanged
//   - createObject edits whose object_id is not in base are synthesized as
//     new WireObjects (BDD #3)
//   - deleteObject edits filter the matching base row out (BDD #2)
//
// Edits for other objectTypes are ignored at this scope.
func applyOverlayToObjectSet(base []*WireObject, edits []scenarios.ScenarioEdit, objectType string) []*WireObject {
	out, _ := applyOverlayToObjectSetWithConflicts(base, edits, objectType)
	return out
}

// applyOverlayToObjectSetWithConflicts is the US-481 superset that returns
// the union of per-row fold conflicts alongside the overlaid object set.
// Conflicts from synthesised createObject rows (no base counterpart) are
// captured too — e.g. two createObject edits for the same brand-new id surface
// a duplicate_create conflict here exactly the same as on a base row.
func applyOverlayToObjectSetWithConflicts(base []*WireObject, edits []scenarios.ScenarioEdit, objectType string) ([]*WireObject, []scenarios.ScenarioConflict) {
	editsByID := map[string][]scenarios.ScenarioEdit{}
	createdIDs := map[string]bool{}
	for _, e := range edits {
		if objectType != "" && e.ObjectType != "" && e.ObjectType != objectType {
			continue
		}
		editsByID[e.ObjectID] = append(editsByID[e.ObjectID], e)
		if e.Op == "createObject" {
			createdIDs[e.ObjectID] = true
		}
	}

	out := make([]*WireObject, 0, len(base))
	var allConflicts []scenarios.ScenarioConflict
	seen := map[string]bool{}
	for _, obj := range base {
		id := fmt.Sprintf("%v", obj.PrimaryKey)
		seen[id] = true
		objEdits := editsByID[id]
		if len(objEdits) == 0 {
			out = append(out, obj)
			continue
		}
		ov := (&ScenarioOverlay{Edits: objEdits})
		overlaid, deleted, conflicts := ov.applyToObjectWithConflicts(obj)
		allConflicts = append(allConflicts, conflicts...)
		if deleted {
			continue
		}
		out = append(out, overlaid)
	}
	// Synthesize created objects that were not in base. Iterate in a sorted
	// key order so the conflict slice is deterministic across runs. Fold
	// directly with base=nil — passing a stub WireObject through
	// applyToObjectWithConflicts would let the fold treat the stub as a
	// live base and mis-flag the first createObject as a duplicate.
	createdIDOrder := make([]string, 0, len(createdIDs))
	for id := range createdIDs {
		if seen[id] {
			continue
		}
		createdIDOrder = append(createdIDOrder, id)
	}
	sort.Strings(createdIDOrder)
	for _, id := range createdIDOrder {
		target := scenarios.ObjectKey{ObjectType: objectType, ObjectID: id}
		view, deleted, conflicts := scenarios.FoldObjectWithConflicts(target, nil, editsByID[id])
		allConflicts = append(allConflicts, conflicts...)
		if deleted || view == nil {
			continue
		}
		template := &WireObject{APIName: objectType, PrimaryKey: id}
		out = append(out, viewToWireObject(view, template))
	}
	return out, allConflicts
}

func filterOverlayRowsByWhere(rows []*WireObject, clause *where.WhereClause) []*WireObject {
	if clause == nil {
		return rows
	}
	out := make([]*WireObject, 0, len(rows))
	for _, row := range rows {
		props := row.Properties
		if props == nil {
			props = map[string]interface{}{}
		}
		if where.MatchClause(clause, props) {
			out = append(out, row)
		}
	}
	return out
}

func computeOverlayMetrics(rows []*WireObject, specs []aggregation.AggregationSpec) ([]aggregation.MetricValue, error) {
	metrics := make([]aggregation.MetricValue, 0, len(specs))
	for _, spec := range specs {
		name := spec.Name
		if name == "" {
			if spec.Field != "" {
				name = spec.Type + "." + spec.Field
			} else {
				name = spec.Type
			}
		}
		switch spec.Type {
		case "count":
			metrics = append(metrics, aggregation.MetricValue{Name: name, Value: float64(len(rows))})
		case "sum":
			sum := 0.0
			for _, r := range rows {
				v, ok := numericProperty(r, spec.Field)
				if !ok {
					continue
				}
				sum += v
			}
			metrics = append(metrics, aggregation.MetricValue{Name: name, Value: sum})
		case "avg":
			sum, n := 0.0, 0
			for _, r := range rows {
				v, ok := numericProperty(r, spec.Field)
				if !ok {
					continue
				}
				sum += v
				n++
			}
			var v interface{}
			if n == 0 {
				v = nil
			} else {
				v = sum / float64(n)
			}
			metrics = append(metrics, aggregation.MetricValue{Name: name, Value: v})
		case "min", "max":
			var cur float64
			hasAny := false
			for _, r := range rows {
				v, ok := numericProperty(r, spec.Field)
				if !ok {
					continue
				}
				if !hasAny {
					cur = v
					hasAny = true
					continue
				}
				if spec.Type == "min" && v < cur {
					cur = v
				}
				if spec.Type == "max" && v > cur {
					cur = v
				}
			}
			var v interface{}
			if hasAny {
				v = cur
			} else {
				v = nil
			}
			metrics = append(metrics, aggregation.MetricValue{Name: name, Value: v})
		default:
			return nil, fmt.Errorf("scenario overlay aggregation: metric type %q not supported in PoC", spec.Type)
		}
	}
	return metrics, nil
}

func numericProperty(obj *WireObject, field string) (float64, bool) {
	if obj == nil || field == "" {
		return 0, false
	}
	v, ok := obj.Properties[field]
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}
