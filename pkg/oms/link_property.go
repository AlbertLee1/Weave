package oms

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// LinkProperty is the OMS-side declaration of a typed property on a
// MANY_TO_MANY link's edges (US-210). The metadata — apiName, baseType,
// optional constraints — lives in the link_properties table; the actual
// values for each edge continue to ride on the link_edges.edge_properties
// JSONB column that migration 000006 introduced.
//
// Shape intentionally mirrors Property so downstream code (validators,
// constraint enforcement, sdkgen) can treat it the same.
type LinkProperty struct {
	RID         string          `json:"rid"`
	LinkTypeRID string          `json:"linkTypeRid"`
	APIName     string          `json:"apiName"`
	DisplayName string          `json:"displayName,omitempty"`
	Description string          `json:"description,omitempty"`
	BaseType    string          `json:"baseType"`
	TypeConfig  json.RawMessage `json:"typeConfig,omitempty"`
	IsArray     bool            `json:"isArray"`
	IsNullable  bool            `json:"isNullable"`
	CreatedAt   time.Time       `json:"-"`
}

// DataTypeJSON returns the Palantir V2 dataType JSON representation, matching
// Property.DataTypeJSON so a single UI/type-resolver can render both.
func (lp *LinkProperty) DataTypeJSON() map[string]interface{} {
	if lp.IsArray {
		return map[string]interface{}{
			"type":    "array",
			"subType": map[string]interface{}{"type": lp.BaseType},
		}
	}
	dt := map[string]interface{}{"type": lp.BaseType}
	if len(lp.TypeConfig) > 0 && string(lp.TypeConfig) != "{}" {
		var extra map[string]interface{}
		if json.Unmarshal(lp.TypeConfig, &extra) == nil {
			for k, v := range extra {
				dt[k] = v
			}
		}
	}
	return dt
}

// Validate enforces the minimum shape for a persistable LinkProperty. Run by
// the repo writes and by admin handlers before mutation so failures surface
// at definition time rather than at edge-write time.
func (lp *LinkProperty) Validate() error {
	if lp.RID == "" {
		return fmt.Errorf("link property requires rid")
	}
	if lp.LinkTypeRID == "" {
		return fmt.Errorf("link property requires linkTypeRid")
	}
	if lp.APIName == "" {
		return fmt.Errorf("link property requires apiName")
	}
	if lp.BaseType == "" {
		return fmt.Errorf("link property requires baseType")
	}
	return nil
}

// LinkPropertyStore is the narrow read/write surface the admin handlers and
// downstream enrichers depend on. It lives outside oms.Repository so the many
// mock repos scattered through the test tree do not have to grow stub methods
// for every row type added after v1 (same pattern US-202 / US-203 established
// for ComputedPropertyStore and MediaAssetStore).
type LinkPropertyStore interface {
	CreateLinkProperty(ctx context.Context, lp *LinkProperty) error
	GetLinkProperty(ctx context.Context, rid string) (*LinkProperty, error)
	ListLinkProperties(ctx context.Context, linkTypeRID string) ([]LinkProperty, error)
	UpdateLinkProperty(ctx context.Context, lp *LinkProperty) error
	DeleteLinkProperty(ctx context.Context, rid string) error
}
