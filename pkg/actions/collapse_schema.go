package actions

import (
	"errors"
	"fmt"
	"sort"

	"github.com/liyang/weave/pkg/funnel"
)

// SchemaLookup resolves an ObjectType's declared property names so the
// post-collapse schema check (US-473) can verify that the merged Properties
// map of every CREATE / MODIFY edit only carries declared property names.
//
// Returning ok=false signals "ObjectType unknown to caller, skip validation"
// — the helper degrades gracefully, mirroring the pkg/actions executor's
// validateEditPropertyValues lenient pattern. Returning ok=true with an
// empty set means "ObjectType has zero declared properties" and any
// non-empty Properties on an edit for that OT is a schema violation.
type SchemaLookup interface {
	PropertyNames(objectType string) (set map[string]struct{}, ok bool)
}

// SchemaViolation pins one undeclared property write surfaced by
// CollapseEditsWithSchema. Multiple violations across the same object or
// across different objects are all reported on a single call so the caller
// can log the full failure set.
type SchemaViolation struct {
	ObjectType string
	PrimaryKey string
	Property   string
}

// ErrCollapseSchemaViolation is the sentinel returned by
// CollapseEditsWithSchema whenever one or more SchemaViolation entries land
// in the returned slice. Use errors.Is to dispatch on it.
var ErrCollapseSchemaViolation = errors.New("collapse: schema violation")

// CollapseEditsWithSchema runs the standard CollapseEdits collapse and then,
// for every surviving CREATE / MODIFY edit whose ObjectType is known to the
// supplied SchemaLookup, asserts that every property name in the merged
// Properties map is declared on the ObjectType. DELETE and LINK_* edits are
// not validated (they carry no Properties payload).
//
// Returns the collapsed edits even when violations are detected so the caller
// can log diagnostics or surface the rejected payload. err is nil when no
// violations are detected; otherwise it wraps ErrCollapseSchemaViolation
// with a human-readable summary.
//
// Passing a nil SchemaLookup degrades the helper to a pure CollapseEdits
// wrapper — matches the executor's "no omsRepo wired → skip schema check"
// boot pattern so degraded-mode tests keep working unchanged.
func CollapseEditsWithSchema(edits []funnel.Edit, schema SchemaLookup) ([]funnel.Edit, []SchemaViolation, error) {
	collapsed := CollapseEdits(edits)
	if schema == nil {
		return collapsed, nil, nil
	}

	violations := ValidateEditsAgainstSchema(collapsed, schema)
	if len(violations) == 0 {
		return collapsed, nil, nil
	}

	return collapsed, violations, fmt.Errorf("%w: %s", ErrCollapseSchemaViolation, summarizeViolations(violations))
}

// ValidateEditsAgainstSchema reports every undeclared-property write across
// the supplied edits. Used both by CollapseEditsWithSchema and by the
// executor's commit-phase schema enforcement (so the executor can call it
// directly on a pre-collapsed slice). Returns nil when there is no schema
// or no violations.
func ValidateEditsAgainstSchema(edits []funnel.Edit, schema SchemaLookup) []SchemaViolation {
	if schema == nil {
		return nil
	}
	var violations []SchemaViolation
	for _, edit := range edits {
		// LINK_* + DELETE edits never carry object properties.
		if edit.Type == funnel.EditTypeLinkCreate ||
			edit.Type == funnel.EditTypeLinkDelete ||
			edit.Type == funnel.EditTypeDelete {
			continue
		}
		if len(edit.Properties) == 0 {
			continue
		}
		declared, ok := schema.PropertyNames(edit.ObjectType)
		if !ok {
			// Unknown ObjectType — lenient skip so partial schemas
			// (degraded boot, missing OMS rows) don't blow up commit.
			continue
		}
		// Deterministic order: sort property names so violation slice
		// order is stable across runs (Go map iteration randomization
		// would otherwise produce flaky test assertions).
		names := make([]string, 0, len(edit.Properties))
		for k := range edit.Properties {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, name := range names {
			if _, allowed := declared[name]; allowed {
				continue
			}
			violations = append(violations, SchemaViolation{
				ObjectType: edit.ObjectType,
				PrimaryKey: edit.PrimaryKey,
				Property:   name,
			})
		}
	}
	return violations
}

func summarizeViolations(violations []SchemaViolation) string {
	if len(violations) == 0 {
		return ""
	}
	if len(violations) == 1 {
		v := violations[0]
		return fmt.Sprintf("%s/%s.%s", v.ObjectType, v.PrimaryKey, v.Property)
	}
	v := violations[0]
	return fmt.Sprintf("%s/%s.%s (+%d more)", v.ObjectType, v.PrimaryKey, v.Property, len(violations)-1)
}

// MapSchemaLookup is a convenience SchemaLookup backed by a plain
// map[objectType]map[propertyName]struct{}. The executor builds one of
// these from omsRepo at commit time; tests and CLI tools can use it
// directly without depending on a live OMS repo.
type MapSchemaLookup map[string]map[string]struct{}

// PropertyNames satisfies SchemaLookup.
func (m MapSchemaLookup) PropertyNames(objectType string) (map[string]struct{}, bool) {
	props, ok := m[objectType]
	return props, ok
}
