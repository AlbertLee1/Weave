package oss

import (
	"encoding/json"
	"fmt"
)

// WireObject formats an object for V2 API response.
// Properties are flattened at the top level per Palantir V2.
type WireObject struct {
	RID        string
	PrimaryKey interface{}
	APIName    string
	Properties map[string]interface{}
	// Highlights carries per-field snippet lists produced by a Bleve
	// highlighter (US-235). Keys are property apiNames; values are snippet
	// strings in which matched terms are wrapped with <mark>...</mark>. A
	// nil / empty map suppresses the `_highlights` key on the wire so
	// non-highlighted responses stay byte-identical to their pre-feature
	// shape.
	Highlights map[string][]string
}

// MarshalJSON produces the Palantir V2 flattened format where properties
// appear at the top level alongside __rid, __primaryKey, __apiName.
func (wo *WireObject) MarshalJSON() ([]byte, error) {
	m := make(map[string]interface{}, len(wo.Properties)+4)
	for k, v := range wo.Properties {
		m[k] = v
	}
	if wo.RID != "" {
		m["__rid"] = wo.RID
	}
	m["__primaryKey"] = wo.PrimaryKey
	m["__apiName"] = wo.APIName
	if len(wo.Highlights) > 0 {
		m["_highlights"] = wo.Highlights
	}
	return json.Marshal(m)
}

// UnmarshalJSON reverses the flattened Palantir V2 format: extracts __rid,
// __primaryKey, __apiName, _highlights from the top-level map and puts
// everything else into Properties.
func (wo *WireObject) UnmarshalJSON(data []byte) error {
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}

	if v, ok := m["__rid"]; ok {
		wo.RID, _ = v.(string)
		delete(m, "__rid")
	}
	if v, ok := m["__primaryKey"]; ok {
		wo.PrimaryKey = v
		delete(m, "__primaryKey")
	}
	if v, ok := m["__apiName"]; ok {
		wo.APIName, _ = v.(string)
		delete(m, "__apiName")
	}
	if v, ok := m["_highlights"]; ok {
		wo.Highlights = decodeHighlights(v)
		delete(m, "_highlights")
	}

	wo.Properties = m
	return nil
}

// decodeHighlights normalises an arbitrary JSON-decoded value back into the
// `map[string][]string` shape that MarshalJSON emits. Unknown shapes are
// silently dropped so round-tripping a WireObject never surfaces an error.
func decodeHighlights(raw interface{}) map[string][]string {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}
	out := make(map[string][]string, len(m))
	for k, v := range m {
		arr, ok := v.([]interface{})
		if !ok {
			continue
		}
		snippets := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				snippets = append(snippets, s)
			}
		}
		if len(snippets) > 0 {
			out[k] = snippets
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// FilterProperties returns a view of the WireObject whose Properties map
// only contains keys present in allowed. The contract matches
// security.Engine.AllowedProperties:
//
//   - allowed == nil   → no PROPERTY-scope policy attached, return the same
//     WireObject pointer unchanged so un-policied call sites stay zero-cost.
//   - allowed != nil   → explicit allow list. A fresh WireObject is returned
//     with a filtered Properties map; fields absent from allowed are omitted
//     (NOT nulled) so the wire JSON matches Foundry's column-level secrecy
//     behaviour. Reserved keys (__rid / __primaryKey / __apiName) are
//     preserved via WireObject.MarshalJSON regardless of the allow list.
//
// A zero-length (but non-nil) allowed slice is the explicit "restricted to
// nothing" state and strips every property field while keeping the
// reserved keys. The original WireObject is never mutated so callers can
// safely share read-only objects across goroutines.
func (wo *WireObject) FilterProperties(allowed []string) *WireObject {
	if wo == nil || allowed == nil {
		return wo
	}
	allowSet := make(map[string]struct{}, len(allowed))
	for _, k := range allowed {
		allowSet[k] = struct{}{}
	}
	filtered := make(map[string]interface{}, len(allowed))
	for k, v := range wo.Properties {
		if _, ok := allowSet[k]; ok {
			filtered[k] = v
		}
	}
	return &WireObject{
		RID:        wo.RID,
		PrimaryKey: wo.PrimaryKey,
		APIName:    wo.APIName,
		Properties: filtered,
		Highlights: wo.Highlights,
	}
}

// FormatObject creates a WireObject from raw index data.
func FormatObject(objectType string, primaryKey string, properties map[string]interface{}) *WireObject {
	return &WireObject{
		RID:        fmt.Sprintf("ri.phonograph2-objects.main.object.%s", primaryKey),
		PrimaryKey: primaryKey,
		APIName:    objectType,
		Properties: properties,
	}
}

// ObjectPage is a paginated list of objects.
type ObjectPage struct {
	Data          []*WireObject `json:"data"`
	NextPageToken string        `json:"nextPageToken,omitempty"`
	TotalCount    string        `json:"totalCount,omitempty"`
	// Facets carries per-field term counts when the caller requested
	// facets via `?facets=field1,field2` (US-236). Keys are property
	// apiNames; values are buckets sorted by descending count (Bleve's
	// native ordering). Nil / empty map suppresses the `facets` key on
	// the wire so non-faceted responses stay byte-identical to their
	// pre-feature shape.
	Facets map[string][]FacetBucket `json:"facets,omitempty"`
}

// FacetBucket is a single term + count bucket produced by a faceted search.
type FacetBucket struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}
