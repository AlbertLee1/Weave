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
	Type       string `json:"type"` // "createObject", "modifyObject", "deleteObject", "createLink", "deleteLink", "createOrModifyObject", "createInterfaceObject", "modifyInterfaceObject", "deleteInterfaceObject", "executeFunction", "if", "foreach", "switch"
	ObjectType string `json:"objectType,omitempty"`
	// For createObject/modifyObject — property bindings
	PropertyBindings map[string]PropertyBinding `json:"propertyBindings,omitempty"`
	// Link rule fields — for createLink rules
	LinkTypeAPIName        string `json:"linkTypeApiName,omitempty"`
	SourceObjectPrimaryKey string `json:"sourceObjectPrimaryKey,omitempty"` // parameter ID for source PK
	TargetObjectPrimaryKey string `json:"targetObjectPrimaryKey,omitempty"` // parameter ID for target PK
	// Interface rule fields — objectType resolved from parameters at runtime
	InterfaceAPIName string `json:"interfaceApiName,omitempty"`
	// FunctionRID identifies the Function invoked by an executeFunction rule
	// (US-222). The Function returns an EditBatch whose edits are merged into
	// the action's edit list alongside any sibling rules.
	FunctionRID string `json:"functionRid,omitempty"`

	// Control flow fields (US-244).
	// `if` rules branch on Condition; `Then` runs on true, `Else` on false.
	Condition *Condition `json:"condition,omitempty"`
	Then      []Rule     `json:"then,omitempty"`
	Else      []Rule     `json:"else,omitempty"`
	// `foreach` iterates over the array at ItemsParameter; each element is
	// exposed to child rules under ItemVariable (and optionally IndexVariable).
	ItemsParameter string `json:"itemsParameter,omitempty"`
	ItemVariable   string `json:"itemVariable,omitempty"`
	IndexVariable  string `json:"indexVariable,omitempty"`
	// Child rules for `foreach`. Reused as the body container so authors have
	// one obvious place to put the loop body.
	Rules []Rule `json:"rules,omitempty"`
	// `switch` matches the value at On against each case's When, falling
	// through to Default when no case matches (or a no-op when Default is empty).
	On      string       `json:"on,omitempty"`
	Cases   []SwitchCase `json:"cases,omitempty"`
	Default []Rule       `json:"default,omitempty"`
}

// Condition is the predicate for an `if` rule. Leaf conditions read a
// parameter and apply Operator against Value; logical conditions combine
// child conditions via All (and), Any (or), or Not (not).
//
// Supported leaf operators: "eq", "ne", "gt", "gte", "lt", "lte", "in",
// "notIn", "exists", "notExists", "truthy", "falsy".
// Supported logical operators: "and", "or", "not".
type Condition struct {
	Parameter string      `json:"parameter,omitempty"`
	Operator  string      `json:"operator"`
	Value     interface{} `json:"value,omitempty"`
	All       []Condition `json:"all,omitempty"`
	Any       []Condition `json:"any,omitempty"`
	Not       *Condition  `json:"not,omitempty"`
}

// SwitchCase is one arm of a `switch` rule. When equals the current value of
// the On parameter, Rules runs.
type SwitchCase struct {
	When  interface{} `json:"when"`
	Rules []Rule      `json:"rules"`
}

// MaxRuleNestingDepth caps the nesting of control-flow rules (if/foreach/
// switch) so a misconfigured ActionType cannot trigger runaway recursion.
// A plain simple-rule body does NOT count towards the depth; only wrapping
// another if/foreach/switch inside one of those bodies does.
const MaxRuleNestingDepth = 5

// IsExecuteFunction reports whether the rule delegates edit generation to a
// Function via the action executor's FunctionDispatcher.
func (r Rule) IsExecuteFunction() bool {
	return r.Type == "executeFunction"
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
//
// executeFunction rules are silently skipped: they require a context and a
// FunctionDispatcher that this pure helper does not own. The Executor handles
// them in a parallel pass and merges the function-derived edits with the slice
// returned here (US-222).
//
// Control-flow rules (`if`, `foreach`, `switch` — US-244) are expanded
// recursively. Nesting deeper than MaxRuleNestingDepth levels is rejected.
func ExecuteRules(rules []Rule, params map[string]interface{}) ([]funnel.Edit, error) {
	return executeRulesWithDepth(rules, params, 0)
}

func executeRulesWithDepth(rules []Rule, params map[string]interface{}, depth int) ([]funnel.Edit, error) {
	var edits []funnel.Edit
	for _, rule := range rules {
		if rule.IsExecuteFunction() {
			continue
		}
		if isControlFlowRule(rule.Type) {
			if depth >= MaxRuleNestingDepth {
				return nil, fmt.Errorf("rule nesting exceeds max depth %d", MaxRuleNestingDepth)
			}
			childEdits, err := executeControlFlowRule(rule, params, depth+1)
			if err != nil {
				return nil, err
			}
			edits = append(edits, childEdits...)
			continue
		}
		edit, err := executeRule(rule, params)
		if err != nil {
			return nil, err
		}
		edits = append(edits, edit)
	}
	return edits, nil
}

func isControlFlowRule(t string) bool {
	switch t {
	case "if", "foreach", "switch":
		return true
	}
	return false
}

func executeControlFlowRule(rule Rule, params map[string]interface{}, depth int) ([]funnel.Edit, error) {
	switch rule.Type {
	case "if":
		if rule.Condition == nil {
			return nil, fmt.Errorf("if: condition is required")
		}
		ok, err := evaluateCondition(rule.Condition, params)
		if err != nil {
			return nil, fmt.Errorf("if: %w", err)
		}
		branch := rule.Then
		if !ok {
			branch = rule.Else
		}
		return executeRulesWithDepth(branch, params, depth)

	case "foreach":
		if rule.ItemsParameter == "" {
			return nil, fmt.Errorf("foreach: itemsParameter is required")
		}
		if rule.ItemVariable == "" {
			return nil, fmt.Errorf("foreach: itemVariable is required")
		}
		raw, ok := params[rule.ItemsParameter]
		if !ok {
			return nil, fmt.Errorf("foreach: parameter %q missing", rule.ItemsParameter)
		}
		items, ok := raw.([]interface{})
		if !ok {
			return nil, fmt.Errorf("foreach: parameter %q must be a slice, got %T", rule.ItemsParameter, raw)
		}
		var all []funnel.Edit
		for i, item := range items {
			childParams := cloneParams(params)
			childParams[rule.ItemVariable] = item
			if rule.IndexVariable != "" {
				childParams[rule.IndexVariable] = i
			}
			childEdits, err := executeRulesWithDepth(rule.Rules, childParams, depth)
			if err != nil {
				return nil, fmt.Errorf("foreach[%d]: %w", i, err)
			}
			all = append(all, childEdits...)
		}
		return all, nil

	case "switch":
		if rule.On == "" {
			return nil, fmt.Errorf("switch: on is required")
		}
		v := params[rule.On]
		for _, c := range rule.Cases {
			if valuesEqual(v, c.When) {
				return executeRulesWithDepth(c.Rules, params, depth)
			}
		}
		return executeRulesWithDepth(rule.Default, params, depth)
	}
	return nil, fmt.Errorf("unknown control-flow rule type: %q", rule.Type)
}

func cloneParams(params map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(params)+2)
	for k, v := range params {
		out[k] = v
	}
	return out
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

	case "createInterfaceObject":
		objectType := resolveObjectTypeParam(params)
		if objectType == "" {
			return funnel.Edit{}, fmt.Errorf("createInterfaceObject: objectType not found in parameters")
		}
		props := resolveBindings(rule.PropertyBindings, params)
		pk := rid.NewObjectRID()
		return funnel.Edit{
			Type:       funnel.EditTypeCreate,
			ObjectType: objectType,
			PrimaryKey: pk,
			Properties: props,
		}, nil

	case "modifyInterfaceObject":
		objectType := resolveObjectTypeParam(params)
		if objectType == "" {
			return funnel.Edit{}, fmt.Errorf("modifyInterfaceObject: objectType not found in parameters")
		}
		pk := findPrimaryKey(objectType, params)
		if pk == "" {
			return funnel.Edit{}, fmt.Errorf("modifyInterfaceObject: primary key not found in parameters")
		}
		props := resolveBindings(rule.PropertyBindings, params)
		return funnel.Edit{
			Type:       funnel.EditTypeModify,
			ObjectType: objectType,
			PrimaryKey: pk,
			Properties: props,
		}, nil

	case "deleteInterfaceObject":
		objectType := resolveObjectTypeParam(params)
		if objectType == "" {
			return funnel.Edit{}, fmt.Errorf("deleteInterfaceObject: objectType not found in parameters")
		}
		pk := findPrimaryKey(objectType, params)
		if pk == "" {
			return funnel.Edit{}, fmt.Errorf("deleteInterfaceObject: primary key not found in parameters")
		}
		return funnel.Edit{
			Type:       funnel.EditTypeDelete,
			ObjectType: objectType,
			PrimaryKey: pk,
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

// resolveObjectTypeParam extracts the concrete objectType API name from params.
// Used by interface-backed rules where objectType is determined at runtime.
func resolveObjectTypeParam(params map[string]interface{}) string {
	for _, key := range []string{"objectType", "objectTypeApiName"} {
		if v, ok := params[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// isInterfaceRule returns true if the rule type is an interface-backed variant.
func isInterfaceRule(ruleType string) bool {
	switch ruleType {
	case "createInterfaceObject", "modifyInterfaceObject", "deleteInterfaceObject":
		return true
	}
	return false
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
