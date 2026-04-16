package automate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Effect represents a single automation effect.
type Effect struct {
	Type              string                 `json:"type"`
	ActionTypeApiName string                 `json:"actionTypeApiName,omitempty"`
	Parameters        map[string]interface{} `json:"parameters,omitempty"`
}

// ActionApplier executes actions. Implemented by wrapping actions.Executor.Apply.
type ActionApplier interface {
	ApplyAction(ctx context.Context, ontologyRID, actionType string, parameters map[string]interface{}) error
}

// TriggerEventData carries event data for template variable resolution.
type TriggerEventData struct {
	PrimaryKey string
	EditType   string
	ObjectType string
	Properties map[string]interface{}
}

// ParseEffects parses the effects JSON array into Effect structs.
func ParseEffects(raw json.RawMessage) ([]Effect, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var effects []Effect
	if err := json.Unmarshal(raw, &effects); err != nil {
		return nil, fmt.Errorf("invalid effects JSON: %w", err)
	}
	return effects, nil
}

// resolveTemplateString replaces ${event.*} template variables in a string.
func resolveTemplateString(s string, data *TriggerEventData) string {
	if data == nil || !strings.Contains(s, "${") {
		return s
	}

	s = strings.ReplaceAll(s, "${event.primaryKey}", data.PrimaryKey)
	s = strings.ReplaceAll(s, "${event.editType}", data.EditType)
	s = strings.ReplaceAll(s, "${event.objectType}", data.ObjectType)

	// Handle ${event.properties.fieldName}
	if data.Properties != nil {
		for key, val := range data.Properties {
			placeholder := "${event.properties." + key + "}"
			if strings.Contains(s, placeholder) {
				s = strings.ReplaceAll(s, placeholder, fmt.Sprint(val))
			}
		}
	}

	return s
}

// resolveParameters resolves template variables in all string parameter values.
func resolveParameters(params map[string]interface{}, data *TriggerEventData) map[string]interface{} {
	if params == nil {
		return nil
	}
	resolved := make(map[string]interface{}, len(params))
	for k, v := range params {
		switch val := v.(type) {
		case string:
			resolved[k] = resolveTemplateString(val, data)
		default:
			resolved[k] = v
		}
	}
	return resolved
}

// processEffects executes the effects for an automation rule.
func processEffects(ctx context.Context, effects json.RawMessage, ontologyRID string, data *TriggerEventData, applier ActionApplier) error {
	parsed, err := ParseEffects(effects)
	if err != nil {
		return err
	}

	for _, effect := range parsed {
		switch effect.Type {
		case "executeAction":
			if applier == nil {
				continue
			}
			resolvedParams := resolveParameters(effect.Parameters, data)
			if err := applier.ApplyAction(ctx, ontologyRID, effect.ActionTypeApiName, resolvedParams); err != nil {
				return fmt.Errorf("executeAction %q failed: %w", effect.ActionTypeApiName, err)
			}
		default:
			// Unknown effect types are skipped (future: executeFunction, notification)
		}
	}

	return nil
}
