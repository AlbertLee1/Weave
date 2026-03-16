package actions

import (
	"encoding/json"
	"fmt"

	"github.com/liyang/weave/pkg/types"
)

// ParameterDef defines an action parameter.
type ParameterDef struct {
	ID          string `json:"id"`
	Type        string `json:"type"`        // base type
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

// ValidateParameters validates action parameters against their definitions.
func ValidateParameters(paramDefs []ParameterDef, params map[string]interface{}) error {
	// Check required params are present
	for _, def := range paramDefs {
		val, exists := params[def.ID]
		if def.Required && (!exists || val == nil) {
			return fmt.Errorf("required parameter %q is missing", def.ID)
		}
		if exists && val != nil {
			// Type validation using the types package
			dt := types.DataType{Type: types.BaseType(def.Type)}
			if err := types.Validate(val, dt, !def.Required); err != nil {
				return fmt.Errorf("parameter %q: %w", def.ID, err)
			}
		}
	}

	// Check no extra params
	defIDs := make(map[string]bool)
	for _, def := range paramDefs {
		defIDs[def.ID] = true
	}
	for k := range params {
		if !defIDs[k] {
			return fmt.Errorf("unknown parameter %q", k)
		}
	}

	return nil
}

// ParseParameterDefs parses parameter definitions from JSON.
func ParseParameterDefs(data json.RawMessage) ([]ParameterDef, error) {
	if len(data) == 0 || string(data) == "[]" || string(data) == "null" {
		return nil, nil
	}
	var defs []ParameterDef
	if err := json.Unmarshal(data, &defs); err != nil {
		return nil, fmt.Errorf("parse parameter defs: %w", err)
	}
	return defs, nil
}
