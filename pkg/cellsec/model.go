// Package cellsec implements US-258 cell-level security: per-(ObjectType,
// primary_key, property) mask transforms applied at response-serialisation
// time to hide a single cell's value from callers outside an allow list.
//
// This is the "one specific object instance + one specific property"
// extension of US-257's column-level masking. A CellMask carries the
// ObjectType RID, the instance's primary key, the property apiName, a
// MaskRule (reused from pkg/masking), and an AppliesTo scope
// (roles/groups/users). The AppliesTo set identifies the callers ALLOWED
// to see the clear value; every other authenticated caller receives the
// masked value. Admins (PermUserManage) bypass all cell masks, matching
// column-mask semantics.
package cellsec

import (
	"errors"
	"strings"
	"time"

	"github.com/liyang/weave/pkg/masking"
)

// Sentinel errors surfaced by Validate and Store implementations.
var (
	ErrObjectTypeRIDRequired = errors.New("objectTypeRID is required")
	ErrPrimaryKeyRequired    = errors.New("primaryKey is required")
	ErrPropertyRequired      = errors.New("propertyApiName is required")
	ErrMaskRuleRequired      = errors.New("maskRule is required")
	ErrUnknownMaskRule       = errors.New("unknown maskRule")
	ErrNotFound              = errors.New("cell mask not found")
)

// CellMask is one row of the cell_masks table. Reuses masking.MaskRule and
// masking.AppliesTo so admins see the same rule vocabulary and identity
// semantics across column- and cell-level surfaces.
type CellMask struct {
	RID             string            `json:"rid"`
	ObjectTypeRID   string            `json:"objectTypeRid"`
	PrimaryKey      string            `json:"primaryKey"`
	PropertyAPIName string            `json:"propertyApiName"`
	MaskRule        masking.MaskRule  `json:"maskRule"`
	AppliesTo       masking.AppliesTo `json:"appliesTo"`
	Description     string            `json:"description,omitempty"`
	CreatedBy       string            `json:"createdBy,omitempty"`
	CreatedAt       time.Time         `json:"createdAt,omitempty"`
	UpdatedAt       time.Time         `json:"updatedAt,omitempty"`
}

// Validate enforces required fields and rule-name canonicalisation. Keeps
// the legacy error-string shape while exposing the sentinel errors for
// errors.Is / errors.As dispatch in handlers.
func (m *CellMask) Validate() error {
	if m == nil {
		return ErrObjectTypeRIDRequired
	}
	if strings.TrimSpace(m.ObjectTypeRID) == "" {
		return ErrObjectTypeRIDRequired
	}
	if strings.TrimSpace(m.PrimaryKey) == "" {
		return ErrPrimaryKeyRequired
	}
	if strings.TrimSpace(m.PropertyAPIName) == "" {
		return ErrPropertyRequired
	}
	if strings.TrimSpace(string(m.MaskRule)) == "" {
		return ErrMaskRuleRequired
	}
	if !masking.IsKnownRule(m.MaskRule) {
		return ErrUnknownMaskRule
	}
	return nil
}

// CellMaskUpdate is the PATCH shape for mutable fields. All pointer-typed
// so "omit" (preserve) is distinguishable from "explicit value".
type CellMaskUpdate struct {
	MaskRule    *masking.MaskRule  `json:"maskRule,omitempty"`
	AppliesTo   *masking.AppliesTo `json:"appliesTo,omitempty"`
	Description *string            `json:"description,omitempty"`
}
