package oms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// ColumnLineageEdge records one (src_dataset, src_column) → (dst_property)
// derivation derived from a datasource binding's column_mapping. The edge
// graph is refreshed in lockstep with the parent binding: a binding write
// replaces the entire set of edges keyed by binding_rid, and a binding
// delete clears them. This is the column-level analogue of LineageEdge —
// LineageEdge tracks where an *object* came from at row granularity, this
// tracks where a *property* came from at column granularity.
type ColumnLineageEdge struct {
	ID                 int64     `json:"id"`
	BindingRID         string    `json:"bindingRid"`
	SrcDatasetRID      string    `json:"srcDatasetRid"`
	SrcColumn          string    `json:"srcColumn"`
	DstObjectTypeRID   string    `json:"dstObjectTypeRid"`
	DstPropertyRID     string    `json:"dstPropertyRid"`
	DstPropertyAPIName string    `json:"dstPropertyApiName"`
	Timestamp          time.Time `json:"timestamp"`
}

// ColumnLineageStore is the narrow interface the binding write path uses
// to persist derived column edges and the lineage handler uses for the
// property-level read paths. The store is OPTIONAL — degraded-mode
// bootstraps (no PG) leave it nil; binding handlers fall back to a no-op
// for derivation and the read endpoints surface 404
// ColumnLineageNotConfigured. Defined here (not on Repository) for the
// same reason as LineageStore — degraded-mode test routers should not
// have to cascade-stub this on top of every Repository mock.
type ColumnLineageStore interface {
	// ReplaceColumnLineageForBinding atomically clears every edge owned by
	// bindingRID and re-inserts the supplied set. The supplied edges'
	// BindingRID field is ignored — the store stamps it from the explicit
	// bindingRID parameter so callers cannot accidentally mix owners. Each
	// inserted row's ID + Timestamp are back-filled. A nil/empty edges
	// slice is valid: the call simply clears the prior set (use
	// DeleteColumnLineageForBinding for clarity when that is the intent).
	ReplaceColumnLineageForBinding(ctx context.Context, bindingRID string, edges []ColumnLineageEdge) error
	// DeleteColumnLineageForBinding removes every edge owned by
	// bindingRID. A binding that never had derived edges is a no-op (no
	// error). Returns the number of rows deleted so callers can detect
	// whether the binding actually had any edges.
	DeleteColumnLineageForBinding(ctx context.Context, bindingRID string) (int64, error)
	// ListUpstreamColumnLineageForProperty returns every edge whose
	// dst_property_rid matches. Newest-first by ts (then id as tiebreaker
	// so rows that landed in the same TIMESTAMPTZ tick still emerge in a
	// stable order). limit <= 0 falls back to a sensible default.
	ListUpstreamColumnLineageForProperty(ctx context.Context, propertyRID string, limit int) ([]ColumnLineageEdge, error)
	// ListDownstreamColumnLineageForDatasetColumn is the reverse-impact
	// path — given an upstream (dataset, column) pair, list every
	// downstream property that derives from it. Used by the
	// "what breaks if I remove this column?" admin tooling.
	ListDownstreamColumnLineageForDatasetColumn(ctx context.Context, datasetRID, column string, limit int) ([]ColumnLineageEdge, error)
}

// ErrEmptyDatasetRID is surfaced when DeriveColumnLineageEdges is called
// on a binding whose DatasetRID is unset — a mapping with no upstream
// dataset cannot produce edges and almost always indicates a programming
// bug at the call site.
var ErrEmptyDatasetRID = errors.New("oms: datasource binding has empty dataset_rid")

// DeriveColumnLineageEdges parses a binding's column_mapping JSON and
// emits one ColumnLineageEdge per (propertyApiName → datasetColumn) pair
// where the property exists in the supplied properties slice. Properties
// referenced in the mapping that do not exist in `properties` are
// silently skipped — the mapping is allowed to drift ahead of (or
// behind) the property schema. Unmapped properties (present in
// `properties` but not in the mapping) are likewise skipped — only
// explicitly mapped columns produce edges.
//
// Two mapping shapes are accepted:
//
//  1. Flat object: {"propertyApiName": "dataset_column", ...} — the
//     canonical Foundry-style shape.
//  2. Array form:  [{"property": "...", "column": "..."}, ...] — a
//     spread-form alternative; either "property"/"column" or "p"/"c"
//     short keys are recognised.
//
// Output edges are sorted lexicographically by (dst_property_api_name,
// src_column) so the slice has a deterministic ordering — useful for
// idempotent-replace round-tripping in tests and for stable diff output.
func DeriveColumnLineageEdges(binding *DatasourceBinding, properties []Property) ([]ColumnLineageEdge, error) {
	if binding == nil {
		return nil, nil
	}
	if binding.DatasetRID == "" {
		return nil, ErrEmptyDatasetRID
	}
	mapping, err := parseColumnMapping(binding.ColumnMapping)
	if err != nil {
		return nil, fmt.Errorf("oms: invalid column_mapping for binding %s: %w", binding.RID, err)
	}
	if len(mapping) == 0 {
		return nil, nil
	}

	byAPIName := make(map[string]*Property, len(properties))
	for i := range properties {
		p := &properties[i]
		if p.APIName == "" {
			continue
		}
		byAPIName[p.APIName] = p
	}

	out := make([]ColumnLineageEdge, 0, len(mapping))
	for apiName, column := range mapping {
		if column == "" {
			continue
		}
		prop, ok := byAPIName[apiName]
		if !ok {
			continue
		}
		out = append(out, ColumnLineageEdge{
			BindingRID:         binding.RID,
			SrcDatasetRID:      binding.DatasetRID,
			SrcColumn:          column,
			DstObjectTypeRID:   binding.ObjectTypeRID,
			DstPropertyRID:     prop.RID,
			DstPropertyAPIName: prop.APIName,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DstPropertyAPIName != out[j].DstPropertyAPIName {
			return out[i].DstPropertyAPIName < out[j].DstPropertyAPIName
		}
		return out[i].SrcColumn < out[j].SrcColumn
	})
	return out, nil
}

// parseColumnMapping accepts either the flat object form or the array
// form and returns a flat propertyApiName → column map. An empty
// json.RawMessage or `{}` / `[]` returns an empty map without error.
// Mixing both shapes is rejected; an unrecognised payload yields an
// error so callers can surface a clean validation failure.
func parseColumnMapping(raw json.RawMessage) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	// Try the flat object form first — it's the dominant Foundry shape.
	var asObject map[string]json.RawMessage
	if err := json.Unmarshal(raw, &asObject); err == nil {
		// Detect the array-form-wrapped-in-object case (highly unusual but
		// guarded against): keys with non-string values are treated as
		// "not the flat shape" so we fall through to the array path.
		out := make(map[string]string, len(asObject))
		flat := true
		for k, v := range asObject {
			var col string
			if err := json.Unmarshal(v, &col); err != nil {
				flat = false
				break
			}
			out[k] = col
		}
		if flat {
			return out, nil
		}
	}

	// Fall back to the array form.
	var asArray []map[string]string
	if err := json.Unmarshal(raw, &asArray); err == nil {
		out := make(map[string]string, len(asArray))
		for _, entry := range asArray {
			prop := entry["property"]
			if prop == "" {
				prop = entry["p"]
			}
			col := entry["column"]
			if col == "" {
				col = entry["c"]
			}
			if prop == "" || col == "" {
				continue
			}
			out[prop] = col
		}
		return out, nil
	}

	return nil, fmt.Errorf("oms: column_mapping must be a flat object {prop: column} or an array of {property, column}; got %s", string(raw))
}
