package actions

import (
	"encoding/json"
	"fmt"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/rid"
)

// editTypeUpsert is an internal-only edit type for createOrModifyObject rules.
// It is resolved to CREATE or MODIFY by Executor.Prepare before publishing.
const editTypeUpsert funnel.EditType = "UPSERT"

// Rule defines an action rule.
type Rule struct {
	Type       string `json:"type"` // "createObject", "modifyObject", "deleteObject", "createLink", "deleteLink", "createOrModifyObject"
	ObjectType string `json:"objectType"`
	// For createObject/modifyObject — property bindings
	PropertyBindings map[string]PropertyBinding `json:"propertyBindings,omitempty"`
	// Link rule fields — for createLink rules
	LinkTypeAPIName        string `json:"linkTypeApiName,omitempty"`
	SourceObjectPrimaryKey string `json:"sourceObjectPrimaryKey,omitempty"` // parameter ID for source PK
	TargetObjectPrimaryKey string `json:"targetObjectPrimaryKey,omitempty"` // parameter ID for target PK
}

// PropertyBinding binds a property to a value source.
type PropertyBinding struct {
	Type  string      `json:"type"`            // "parameter", "static"
	Value interface{} `json:"value,omitempty"` // for static: the value; for parameter: the param ID
}

// ParseRules parses rules from JSON.
func ParseRules(data json.RawMessage) ([]Rule, error) {
	if len(data) == 0 || string(data) == "[]" || string(data) == "null" {
		return nil, nil
	}
	var rules []Rule
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("parse rules: %w", err)
	}
	return rules, nil
}

// ExecuteRules generates edits from rules and parameters.
func ExecuteRules(rules []Rule, params map[string]interface{}) ([]funnel.Edit, error) {
	var edits []funnel.Edit

	for _, rule := range rules {
		edit, err := executeRule(rule, params)
		if err != nil {
			return nil, err
		}
		edits = append(edits, edit)
	}

	return edits, nil
}

func executeRule(rule Rule, params map[string]interface{}) (funnel.Edit, error) {
	switch rule.Type {
	case "createObject":
		props := resolveBindings(rule.PropertyBindings, params)
		pk := rid.NewObjectRID() // generate a new primary key for created objects
		return funnel.Edit{
			Type:       funnel.EditTypeCreate,
			ObjectType: rule.ObjectType,
			PrimaryKey: pk,
			Properties: props,
		}, nil

	case "modifyObject":
		// For modify, the primary key comes from parameters
		// Convention: parameter named "primaryKey" or "{objectType}Id"
		pk := findPrimaryKey(rule.ObjectType, params)
		if pk == "" {
			return funnel.Edit{}, fmt.Errorf("modifyObject: primary key not found in parameters")
		}
		props := resolveBindings(rule.PropertyBindings, params)
		return funnel.Edit{
			Type:       funnel.EditTypeModify,
			ObjectType: rule.ObjectType,
			PrimaryKey: pk,
			Properties: props,
		}, nil

	case "deleteObject":
		pk := findPrimaryKey(rule.ObjectType, params)
		if pk == "" {
			return funnel.Edit{}, fmt.Errorf("deleteObject: primary key not found in parameters")
		}
		return funnel.Edit{
			Type:       funnel.EditTypeDelete,
			ObjectType: rule.ObjectType,
			PrimaryKey: pk,
		}, nil

	case "createLink":
		sourcePK := resolveStringParam(rule.SourceObjectPrimaryKey, params)
		if sourcePK == "" {
			return funnel.Edit{}, fmt.Errorf("createLink: source primary key not found in parameters for %q", rule.SourceObjectPrimaryKey)
		}
		targetPK := resolveStringParam(rule.TargetObjectPrimaryKey, params)
		if targetPK == "" {
			return funnel.Edit{}, fmt.Errorf("createLink: target primary key not found in parameters for %q", rule.TargetObjectPrimaryKey)
		}
		return funnel.Edit{
			Type:             funnel.EditTypeLinkCreate,
			PrimaryKey:       sourcePK,
			LinkTypeRID:      rule.LinkTypeAPIName, // resolved to RID in Executor.Prepare
			TargetPrimaryKey: targetPK,
		}, nil

	case "deleteLink":
		sourcePK := resolveStringParam(rule.SourceObjectPrimaryKey, params)
		if sourcePK == "" {
			return funnel.Edit{}, fmt.Errorf("deleteLink: source primary key not found in parameters for %q", rule.SourceObjectPrimaryKey)
		}
		targetPK := resolveStringParam(rule.TargetObjectPrimaryKey, params)
		if targetPK == "" {
			return funnel.Edit{}, fmt.Errorf("deleteLink: target primary key not found in parameters for %q", rule.TargetObjectPrimaryKey)
		}
		return funnel.Edit{
			Type:             funnel.EditTypeLinkDelete,
			PrimaryKey:       sourcePK,
			LinkTypeRID:      rule.LinkTypeAPIName, // resolved to RID in Executor.Prepare
			TargetPrimaryKey: targetPK,
		}, nil

	case "createOrModifyObject":
		pk := findPrimaryKey(rule.ObjectType, params)
		if pk == "" {
			pk = rid.NewObjectRID() // no PK → always create (new object)
		}
		props := resolveBindings(rule.PropertyBindings, params)
		return funnel.Edit{
			Type:       editTypeUpsert, // resolved to CREATE or MODIFY in Executor.Prepare
			ObjectType: rule.ObjectType,
			PrimaryKey: pk,
			Properties: props,
		}, nil

	default:
		return funnel.Edit{}, fmt.Errorf("unknown rule type: %q", rule.Type)
	}
}

func resolveBindings(bindings map[string]PropertyBinding, params map[string]interface{}) map[string]interface{} {
	props := make(map[string]interface{})
	for propName, binding := range bindings {
		switch binding.Type {
		case "parameter":
			paramID, ok := binding.Value.(string)
			if ok {
				props[propName] = params[paramID]
			}
		case "static":
			props[propName] = binding.Value
		}
	}
	return props
}

// resolveStringParam extracts a string value from params using the given key.
func resolveStringParam(key string, params map[string]interface{}) string {
	if v, ok := params[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func findPrimaryKey(objectType string, params map[string]interface{}) string {
	// Try common conventions
	candidates := []string{"primaryKey", objectType + "Id", "id"}
	for _, key := range candidates {
		if v, ok := params[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}
