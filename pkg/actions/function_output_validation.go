package actions

import (
	"fmt"

	"github.com/liyang/weave/pkg/apierror"
)

// validEditTypes is the set of valid edit type strings a function may return.
var validEditTypes = map[string]bool{
	"CREATE": true,
	"MODIFY": true,
	"DELETE": true,
}

// ValidateFunctionOutput validates that a FunctionResponse has the expected
// {edits: Edit[]} shape with each edit containing a valid type, non-empty
// objectType, and non-empty primaryKey. Returns *apierror.APIError with
// ErrorName "InvalidFunctionOutput" (HTTP 400) on validation failure.
func ValidateFunctionOutput(resp *FunctionResponse) error {
	if resp == nil {
		return apierror.NewInvalidParameter("InvalidFunctionOutput", map[string]string{
			"reason": "function returned nil response",
		})
	}

	for i, edit := range resp.Edits {
		if err := validateFunctionEdit(i, edit); err != nil {
			return err
		}
	}
	return nil
}

// validateFunctionEdit checks a single FunctionEdit for required fields.
func validateFunctionEdit(index int, edit FunctionEdit) *apierror.APIError {
	idx := fmt.Sprintf("%d", index)

	if edit.Type == "" {
		return apierror.NewInvalidParameter("InvalidFunctionOutput", map[string]string{
			"editIndex": idx,
			"field":     "type",
			"reason":    "edit type is required",
		})
	}
	if !validEditTypes[edit.Type] {
		return apierror.NewInvalidParameter("InvalidFunctionOutput", map[string]string{
			"editIndex": idx,
			"field":     "type",
			"reason":    fmt.Sprintf("unknown edit type %q, expected CREATE, MODIFY, or DELETE", edit.Type),
		})
	}

	if edit.ObjectType == "" {
		return apierror.NewInvalidParameter("InvalidFunctionOutput", map[string]string{
			"editIndex": idx,
			"field":     "objectType",
			"reason":    "objectType is required",
		})
	}

	if edit.PrimaryKey == "" {
		return apierror.NewInvalidParameter("InvalidFunctionOutput", map[string]string{
			"editIndex": idx,
			"field":     "primaryKey",
			"reason":    "primaryKey is required",
		})
	}

	return nil
}

// ValidateRawFunctionOutput validates raw function output (typically from the
// Goja runtime) against the expected {edits: Edit[]} shape. Returns
// *apierror.APIError with ErrorName "InvalidFunctionOutput" on failure.
func ValidateRawFunctionOutput(result interface{}) error {
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return apierror.NewInvalidParameter("InvalidFunctionOutput", map[string]string{
			"reason": fmt.Sprintf("function returned %T, expected {edits: Edit[]}", result),
		})
	}

	editsRaw, ok := resultMap["edits"]
	if !ok {
		return apierror.NewInvalidParameter("InvalidFunctionOutput", map[string]string{
			"reason": "function output missing required 'edits' key",
		})
	}

	editsSlice, ok := editsRaw.([]interface{})
	if !ok {
		return apierror.NewInvalidParameter("InvalidFunctionOutput", map[string]string{
			"reason": fmt.Sprintf("'edits' is %T, expected array", editsRaw),
		})
	}

	for i, raw := range editsSlice {
		editMap, ok := raw.(map[string]interface{})
		if !ok {
			return apierror.NewInvalidParameter("InvalidFunctionOutput", map[string]string{
				"editIndex": fmt.Sprintf("%d", i),
				"reason":    fmt.Sprintf("edit is %T, expected object", raw),
			})
		}

		fe := FunctionEdit{
			Type:       asString(editMap["type"]),
			ObjectType: asString(editMap["objectType"]),
			PrimaryKey: asString(editMap["primaryKey"]),
		}
		if err := validateFunctionEdit(i, fe); err != nil {
			return err
		}
	}

	return nil
}
