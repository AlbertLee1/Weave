package actions

import (
	"context"
	"fmt"

	"github.com/liyang/weave/pkg/functions"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// FunctionLookup is a narrow interface for fetching function source code by RID.
// Follows the codebase pattern of narrow in-package interfaces (see
// pkg/index/rebuild.go RebuildRepo) to avoid expanding oms.Repository.
type FunctionLookup interface {
	GetFunction(ctx context.Context, rid string) (*oms.Function, error)
}

// GojaDispatcher is a FunctionDispatcher that executes function-backed actions
// via the embedded Goja JavaScript runtime. It looks up the function source by
// RID and passes the action parameters as the JS main(input) argument.
//
// The JS function must return {edits: Edit[]} where each Edit has
// type/objectType/primaryKey/properties fields. A non-empty "error" field in
// the return value is treated as a function-level rejection.
type GojaDispatcher struct {
	runtime *functions.Runtime
	lookup  FunctionLookup
}

// NewGojaDispatcher creates a GojaDispatcher wired to the given runtime and
// function lookup. Both must be non-nil.
func NewGojaDispatcher(rt *functions.Runtime, lookup FunctionLookup) *GojaDispatcher {
	return &GojaDispatcher{runtime: rt, lookup: lookup}
}

// Dispatch looks up the function source by actionType.FunctionRID, executes it
// via the Goja runtime with the action parameters, and converts the result into
// funnel.Edit slice.
func (d *GojaDispatcher) Dispatch(ctx context.Context, at *oms.ActionType, params map[string]interface{}) ([]funnel.Edit, error) {
	if at == nil {
		return nil, fmt.Errorf("goja dispatcher: action type is nil")
	}
	if at.FunctionRID == "" {
		return nil, fmt.Errorf("goja dispatcher: action type %q has empty FunctionRID", at.APIName)
	}

	fn, err := d.lookup.GetFunction(ctx, at.FunctionRID)
	if err != nil {
		return nil, fmt.Errorf("goja dispatcher: lookup function %q: %w", at.FunctionRID, err)
	}

	// Build the input envelope matching the FunctionRequest wire shape so JS
	// function authors see the same structure regardless of dispatch mode.
	input := map[string]interface{}{
		"actionTypeRid":     at.RID,
		"actionTypeApiName": at.APIName,
		"functionRid":       at.FunctionRID,
		"parameters":        params,
	}

	result, err := d.runtime.Execute(ctx, fn.SourceCode, input)
	if err != nil {
		return nil, fmt.Errorf("goja dispatcher: execute function %q: %w", at.FunctionRID, err)
	}

	return convertGojaResult(at.FunctionRID, result)
}

// convertGojaResult validates the Goja return value matches the
// {edits: Edit[]} shape and converts it to funnel.Edit slice.
// Structural validation is delegated to ValidateRawFunctionOutput so both
// Goja and HTTP paths share the same InvalidFunctionOutput error semantics.
func convertGojaResult(rid string, result interface{}) ([]funnel.Edit, error) {
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return nil, ValidateRawFunctionOutput(result) // returns InvalidFunctionOutput
	}

	// Check for error field before structural validation.
	if errMsg, ok := resultMap["error"]; ok {
		if s, ok := errMsg.(string); ok && s != "" {
			return nil, fmt.Errorf("goja dispatcher: function %q reported error: %s", rid, s)
		}
	}

	// Validate the raw output structure ({edits: Edit[]} with valid fields).
	if err := ValidateRawFunctionOutput(result); err != nil {
		return nil, err
	}

	editsSlice := resultMap["edits"].([]interface{})
	edits := make([]funnel.Edit, 0, len(editsSlice))
	for i, raw := range editsSlice {
		editMap := raw.(map[string]interface{})

		fe := FunctionEdit{
			Type:       asString(editMap["type"]),
			ObjectType: asString(editMap["objectType"]),
			PrimaryKey: asString(editMap["primaryKey"]),
		}
		if props, ok := editMap["properties"].(map[string]interface{}); ok {
			fe.Properties = props
		}

		edit, err := fe.ToFunnelEdit()
		if err != nil {
			return nil, fmt.Errorf("goja dispatcher: convert edit %d from %q: %w", i, rid, err)
		}
		edits = append(edits, edit)
	}

	return edits, nil
}

// asString safely extracts a string from an interface{}, returning "" for nil
// or non-string values.
func asString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
