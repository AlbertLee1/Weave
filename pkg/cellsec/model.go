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
	"fmt"
	"strings"
	"time"

	"github.com/liyang/weave/pkg/cellsec/celmask"
	"github.com/liyang/weave/pkg/masking"
)

// celmaskValidate is a package-private alias so the model file can call into
// the CEL evaluator without forcing every cellsec consumer to re-import the
// celmask subpackage.
var celmaskValidate = celmask.Validate

// Sentinel errors surfaced by Validate and Store implementations.
var (
	ErrObjectTypeRIDRequired = errors.New("objectTypeRID is required")
	ErrPrimaryKeyRequired    = errors.New("primaryKey is required")
	ErrPropertyRequired      = errors.New("propertyApiName is required")
	ErrMaskRuleRequired      = errors.New("maskRule or maskStrategy is required")
	ErrUnknownMaskRule       = errors.New("unknown maskRule")
	ErrUnknownMaskStrategy   = errors.New("unknown maskStrategy (want REDACT|HASH|NULL|PARTIAL)")
	ErrInvalidExpression     = errors.New("invalid expression")
	ErrNotFound              = errors.New("cell mask not found")
)

// CellMask is one row of the cell_masks table. Reuses masking.MaskRule and
// masking.AppliesTo so admins see the same rule vocabulary and identity
// semantics across column- and cell-level surfaces.
//
// US-376 adds two optional fields:
//
//   - Expression — a CEL predicate evaluated per row at serialisation time
//     against the {user, row} binding. When non-empty AND the program returns
//     true, the cell is masked. When empty, the engine falls back to the
//     legacy AppliesTo allow-list.
//   - MaskStrategy — the new uppercase REDACT|HASH|NULL|PARTIAL taxonomy.
//     When non-empty it wins over MaskRule. When empty the engine derives
//     the strategy from MaskRule via masking.StrategyFromRule.
type CellMask struct {
	RID             string               `json:"rid"`
	ObjectTypeRID   string               `json:"objectTypeRid"`
	PrimaryKey      string               `json:"primaryKey"`
	PropertyAPIName string               `json:"propertyApiName"`
	MaskRule        masking.MaskRule     `json:"maskRule,omitempty"`
	MaskStrategy    masking.MaskStrategy `json:"maskStrategy,omitempty"`
	Expression      string               `json:"expression,omitempty"`
	AppliesTo       masking.AppliesTo    `json:"appliesTo"`
	Description     string               `json:"description,omitempty"`
	CreatedBy       string               `json:"createdBy,omitempty"`
	CreatedAt       time.Time            `json:"createdAt,omitempty"`
	UpdatedAt       time.Time            `json:"updatedAt,omitempty"`
}

// EffectiveStrategy returns the MaskStrategy this mask applies when its
// predicate fires. Prefers the explicit MaskStrategy field; falls back to
// translating MaskRule. Returns the empty strategy when neither is set —
// callers should treat that as "no-op" (the validator rejects this case at
// the admin boundary).
func (m *CellMask) EffectiveStrategy() masking.MaskStrategy {
	if m == nil {
		return ""
	}
	if m.MaskStrategy != "" {
		return m.MaskStrategy
	}
	return masking.StrategyFromRule(m.MaskRule)
}

// Validate enforces required fields and rule-name canonicalisation. Keeps
// the legacy error-string shape while exposing the sentinel errors for
// errors.Is / errors.As dispatch in handlers.
//
// US-376 invariants: at least one of MaskRule or MaskStrategy must be set;
// when both are set MaskStrategy wins but MaskRule still must be a known
// rule (or empty). When Expression is set it must be valid CEL — the engine
// re-checks at Reload time but failing fast at Create avoids storing
// definitively broken masks.
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
	hasRule := strings.TrimSpace(string(m.MaskRule)) != ""
	hasStrategy := strings.TrimSpace(string(m.MaskStrategy)) != ""
	if !hasRule && !hasStrategy {
		return ErrMaskRuleRequired
	}
	if hasRule && !masking.IsKnownRule(m.MaskRule) {
		return ErrUnknownMaskRule
	}
	if hasStrategy {
		canon := masking.NormalizeStrategy(m.MaskStrategy)
		if canon == "" {
			return ErrUnknownMaskStrategy
		}
		m.MaskStrategy = canon
	}
	if expr := strings.TrimSpace(m.Expression); expr != "" {
		if err := celmaskValidate(expr); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidExpression, err)
		}
		m.Expression = expr
	}
	return nil
}

// CellMaskUpdate is the PATCH shape for mutable fields. All pointer-typed
// so "omit" (preserve) is distinguishable from "explicit value". US-376
// adds Expression and MaskStrategy as patchable fields; pass an empty string
// pointer to clear the field.
type CellMaskUpdate struct {
	MaskRule     *masking.MaskRule     `json:"maskRule,omitempty"`
	MaskStrategy *masking.MaskStrategy `json:"maskStrategy,omitempty"`
	Expression   *string               `json:"expression,omitempty"`
	AppliesTo    *masking.AppliesTo    `json:"appliesTo,omitempty"`
	Description  *string               `json:"description,omitempty"`
}
