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
}

// MarshalJSON produces the Palantir V2 flattened format where properties
// appear at the top level alongside __rid, __primaryKey, __apiName.
func (wo *WireObject) MarshalJSON() ([]byte, error) {
	m := make(map[string]interface{}, len(wo.Properties)+3)
	for k, v := range wo.Properties {
		m[k] = v
	}
	if wo.RID != "" {
		m["__rid"] = wo.RID
	}
	m["__primaryKey"] = wo.PrimaryKey
	m["__apiName"] = wo.APIName
	return json.Marshal(m)
}

// UnmarshalJSON reverses the flattened Palantir V2 format: extracts __rid,
// __primaryKey, __apiName from the top-level map and puts everything else
// into Properties.
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

	wo.Properties = m
	return nil
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
}
