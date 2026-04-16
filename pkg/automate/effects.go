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
	FunctionRid       string                 `json:"functionRid,omitempty"`
	Parameters        map[string]interface{} `json:"parameters,omitempty"`
	Channel           string                 `json:"channel,omitempty"`    // "platform" or "email"
	Template          string                 `json:"template,omitempty"`   // notification body template
	Recipients        []string               `json:"recipients,omitempty"` // user IDs to notify
}

// ActionApplier executes actions. Implemented by wrapping actions.Executor.Apply.
type ActionApplier interface {
	ApplyAction(ctx context.Context, ontologyRID, actionType string, parameters map[string]interface{}) error
}

// AutomateFunctionDispatcher executes functions for automation effects.
// Delegates to GojaDispatcher or HTTPDispatcher based on functionRid prefix.
type AutomateFunctionDispatcher interface {
	DispatchFunction(ctx context.Context, functionRid string, parameters map[string]interface{}) (interface{}, error)
}

// NotificationCreator creates platform notifications for users.
type NotificationCreator interface {
	CreateNotificationForUser(ctx context.Context, userID, title, body, nType, link string) error
}

// TriggerEventData carries event data for template variable resolution.
type TriggerEventData struct {
	PrimaryKey string
	EditType   string
	ObjectType string
	Properties map[string]interface{}
}

// EffectResult holds the result of a single effect execution.
type EffectResult struct {
	Result interface{} `json:"result,omitempty"`
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

// resolveTemplateStringWithChain replaces ${event.*} and ${effects[N].result} template variables.
func resolveTemplateStringWithChain(s string, data *TriggerEventData, results []EffectResult) string {
	s = resolveTemplateString(s, data)

	// Handle ${effects[N].result}
	for i, r := range results {
		placeholder := fmt.Sprintf("${effects[%d].result}", i)
		if strings.Contains(s, placeholder) {
			s = strings.ReplaceAll(s, placeholder, fmt.Sprint(r.Result))
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

// resolveParametersWithChain resolves templates including ${effects[N].result} chain references.
func resolveParametersWithChain(params map[string]interface{}, data *TriggerEventData, results []EffectResult) map[string]interface{} {
	if params == nil {
		return nil
	}
	resolved := make(map[string]interface{}, len(params))
	for k, v := range params {
		switch val := v.(type) {
		case string:
			resolved[k] = resolveTemplateStringWithChain(val, data, results)
		default:
			resolved[k] = v
		}
	}
	return resolved
}

// processEffects executes the effects for an automation rule.
// Returns effect results for storage (e.g., function return values) and any error.
func processEffects(ctx context.Context, effects json.RawMessage, ontologyRID string, data *TriggerEventData, applier ActionApplier, funcDispatcher AutomateFunctionDispatcher, notifier NotificationCreator) ([]EffectResult, error) {
	parsed, err := ParseEffects(effects)
	if err != nil {
		return nil, err
	}

	var results []EffectResult

	for _, effect := range parsed {
		switch effect.Type {
		case "executeAction":
			if applier == nil {
				results = append(results, EffectResult{})
				continue
			}
			resolvedParams := resolveParametersWithChain(effect.Parameters, data, results)
			if err := applier.ApplyAction(ctx, ontologyRID, effect.ActionTypeApiName, resolvedParams); err != nil {
				return results, fmt.Errorf("executeAction %q failed: %w", effect.ActionTypeApiName, err)
			}
			results = append(results, EffectResult{})

		case "executeFunction":
			if funcDispatcher == nil {
				results = append(results, EffectResult{})
				continue
			}
			resolvedParams := resolveParametersWithChain(effect.Parameters, data, results)
			result, err := funcDispatcher.DispatchFunction(ctx, effect.FunctionRid, resolvedParams)
			if err != nil {
				return results, fmt.Errorf("executeFunction %q failed: %w", effect.FunctionRid, err)
			}
			results = append(results, EffectResult{Result: result})

		case "notification":
			if notifier == nil || effect.Channel != "platform" {
				// Email channel or no notifier: graceful skip
				results = append(results, EffectResult{})
				continue
			}
			resolvedBody := resolveTemplateStringWithChain(effect.Template, data, results)
			title := "Automation Notification"
			for _, recipient := range effect.Recipients {
				if err := notifier.CreateNotificationForUser(ctx, recipient, title, resolvedBody, "automation", ""); err != nil {
					return results, fmt.Errorf("notification to %q failed: %w", recipient, err)
				}
			}
			results = append(results, EffectResult{})

		default:
			results = append(results, EffectResult{})
		}
	}

	return results, nil
}
