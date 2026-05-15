package functionactions

import (
	"fmt"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// MappingError is the typed error MapFunctionOutputToScenarioEdits emits
// when a binding's OutputMapping cannot be satisfied at invoke time —
// either the Function's output is missing the named field, the
// invocation parameters are missing the named PK parameter, or the
// PK parameter resolves to a non-string value the scenario_edits row
// could not carry verbatim. Surfacing a typed error lets the routing
// layer map field-level failures into structured 4xx responses rather
// than scraping error strings.
type MappingError struct {
	Field  string
	Reason string
}

// Error renders MappingError as a human-readable message. The reason is
// the load-bearing half; the field name is appended when present so log
// lines and toasts can point the operator at the offending key.
func (e *MappingError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("functionactions: %s", e.Reason)
	}
	return fmt.Sprintf("functionactions: %s (%s)", e.Reason, e.Field)
}

// ValidateOutputMappings runs the registration-time shape check on a
// binding's OutputMappings. Returns the first *MappingError it
// encounters; an empty slice is valid (the registration is allowed to
// land a binding with no output-side effects, e.g. a Function whose only
// effect is a side-effect we do not yet model).
//
// Validation is structural: it does not look at the bound Function's
// signature. Cross-checking each mapping's OutputField against the
// Function's declared return shape is deferred to a later story so the
// registration surface stays decoupled from the Function-resolution
// machinery.
func ValidateOutputMappings(mappings []OutputMapping) error {
	for i, m := range mappings {
		if m.OutputField == "" {
			return &MappingError{Field: indexLabel(i, "outputField"), Reason: "outputField is required"}
		}
		if m.ObjectType == "" {
			return &MappingError{Field: indexLabel(i, "objectType"), Reason: "objectType is required"}
		}
		if m.PrimaryKeyParameter == "" {
			return &MappingError{Field: indexLabel(i, "primaryKeyParameter"), Reason: "primaryKeyParameter is required"}
		}
		if m.Property == "" {
			return &MappingError{Field: indexLabel(i, "property"), Reason: "property is required"}
		}
	}
	return nil
}

func indexLabel(i int, field string) string {
	return fmt.Sprintf("outputMappings[%d].%s", i, field)
}

// ResolveActionMode decides whether the ActionType should be invoked as
// a function-backed Action (calling the bound Function and mapping its
// output to scenario edits) or as a standard Action (running the
// existing OMS ActionExecutor pipeline with the Vertex layer's
// scenario_edits writer wired in instead of the main writer).
//
// The decision rule is conservative: a row that opted into is_function_backed
// but forgot to populate function_rid falls back to ActionModeStandard
// rather than blowing up at invoke time. A nil ActionType resolves to
// ActionModeStandard so callers that fail to look up the row still get a
// deterministic routing decision before the missing-row check.
func ResolveActionMode(at *oms.ActionType) ActionMode {
	if at != nil && at.IsFunctionBacked && at.FunctionRID != "" {
		return ActionModeFunctionBacked
	}
	return ActionModeStandard
}

// MapFunctionOutputToScenarioEdits walks the binding's OutputMappings and
// produces one modifyProperty scenario edit per mapping. Each edit
// targets the object whose PK is looked up from `parameters` under
// PrimaryKeyParameter; the new value is pulled from `output` under
// OutputField. Field-level misses return a *MappingError so the routing
// layer can attach structured 4xx detail.
//
// The mapping list itself is validated up front — same shape check
// ValidateOutputMappings runs at registration time — so an invocation
// against an under-specified binding fails the same way registration
// would, rather than emitting a partial edit batch.
func MapFunctionOutputToScenarioEdits(
	output map[string]interface{},
	parameters map[string]interface{},
	mappings []OutputMapping,
) ([]ScenarioEdit, error) {
	if err := ValidateOutputMappings(mappings); err != nil {
		return nil, err
	}
	edits := make([]ScenarioEdit, 0, len(mappings))
	for _, m := range mappings {
		val, ok := output[m.OutputField]
		if !ok {
			return nil, &MappingError{Field: m.OutputField, Reason: "missing in function output"}
		}
		pkVal, ok := parameters[m.PrimaryKeyParameter]
		if !ok {
			return nil, &MappingError{Field: m.PrimaryKeyParameter, Reason: "missing in invocation parameters"}
		}
		pk, ok := pkVal.(string)
		if !ok {
			return nil, &MappingError{Field: m.PrimaryKeyParameter, Reason: "primaryKeyParameter value must be a string"}
		}
		if pk == "" {
			return nil, &MappingError{Field: m.PrimaryKeyParameter, Reason: "primaryKeyParameter value must be non-empty"}
		}
		edits = append(edits, ScenarioEdit{
			Op:         OpModifyProperty,
			ObjectType: m.ObjectType,
			ObjectID:   pk,
			Property:   m.Property,
			NewValue:   val,
		})
	}
	return edits, nil
}

// FunnelEditsToScenarioEdits is the bridge the Vertex routing layer uses
// when an ActionType resolves to ActionModeStandard: the OMS
// ActionExecutor still produces funnel.Edit values, but instead of
// publishing them to the main ontology Vertex converts them to
// scenario_edits rows. Link edits round-trip through addLink/deleteLink;
// object edits round-trip through createObject/modifyProperty/deleteObject.
//
// MODIFY edits that carry multiple properties are flattened into one
// modifyProperty row per property, mirroring how the scenario_edits
// schema models per-(object, property) deltas. CREATE / DELETE / LINK
// edits map 1-to-1 because the scenario schema's ops carry the same
// shape.
//
// Unknown EditType values are skipped rather than panicking — a future
// EditType added to pkg/funnel that Vertex hasn't taught itself to fold
// onto a fork should simply be a no-op here, not a crash. Callers that
// need to surface "unsupported" should validate upstream.
func FunnelEditsToScenarioEdits(in []funnel.Edit) []ScenarioEdit {
	if len(in) == 0 {
		return nil
	}
	out := make([]ScenarioEdit, 0, len(in))
	for _, e := range in {
		switch e.Type {
		case funnel.EditTypeCreate:
			out = append(out, ScenarioEdit{
				Op:         OpCreateObject,
				ObjectType: e.ObjectType,
				ObjectID:   e.PrimaryKey,
				NewValue:   e.Properties,
			})
		case funnel.EditTypeModify:
			if len(e.Properties) == 0 {
				continue
			}
			for prop, v := range e.Properties {
				out = append(out, ScenarioEdit{
					Op:         OpModifyProperty,
					ObjectType: e.ObjectType,
					ObjectID:   e.PrimaryKey,
					Property:   prop,
					NewValue:   v,
				})
			}
		case funnel.EditTypeDelete:
			out = append(out, ScenarioEdit{
				Op:         OpDeleteObject,
				ObjectType: e.ObjectType,
				ObjectID:   e.PrimaryKey,
			})
		case funnel.EditTypeLinkCreate:
			out = append(out, ScenarioEdit{
				Op:       OpAddLink,
				LinkType: e.LinkTypeRID,
				SrcID:    e.PrimaryKey,
				DstID:    e.TargetPrimaryKey,
			})
		case funnel.EditTypeLinkDelete:
			out = append(out, ScenarioEdit{
				Op:       OpDeleteLink,
				LinkType: e.LinkTypeRID,
				SrcID:    e.PrimaryKey,
				DstID:    e.TargetPrimaryKey,
			})
		}
	}
	return out
}
