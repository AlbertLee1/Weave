package sdkgen

import (
	"encoding/json"
	"time"
)

// OntologySchema is the resolved schema used for code generation.
//
// ServerURL / GeneratedAt / Previous are runtime-only (json:"-") fields the
// handler or CLI populates before calling Generator.Generate to drive the
// emitted .weave-sdk.json metadata file and CHANGELOG.md. They are stripped
// from metadata snapshots to keep serialization cycles bounded.
type OntologySchema struct {
	Ontology    OntologyMeta       `json:"ontology"`
	ObjectTypes []ObjectTypeSchema `json:"objectTypes"`
	LinkTypes   []LinkTypeSchema   `json:"linkTypes"`
	ActionTypes []ActionTypeSchema `json:"actionTypes"`
	Interfaces  []InterfaceSchema  `json:"interfaces"`
	Functions   []FunctionSchema   `json:"functions,omitempty"`

	ServerURL   string          `json:"-"`
	GeneratedAt time.Time       `json:"-"`
	Previous    *OntologySchema `json:"-"`
}

// OntologyMeta holds basic ontology identifiers.
type OntologyMeta struct {
	RID         string `json:"rid"`
	APIName     string `json:"apiName"`
	DisplayName string `json:"displayName"`
	Version     int    `json:"version"`
}

// ObjectTypeSchema is the resolved form of an ObjectType for SDK generation.
type ObjectTypeSchema struct {
	RID         string           `json:"rid"`
	APIName     string           `json:"apiName"`
	DisplayName string           `json:"displayName"`
	PrimaryKey  string           `json:"primaryKey"`
	Properties  []PropertySchema `json:"properties"`
}

// PropertySchema is a resolved property definition.
type PropertySchema struct {
	APIName  string `json:"apiName"`
	BaseType string `json:"baseType"`
	IsArray  bool   `json:"isArray"`
}

// LinkTypeSchema is a resolved link type definition.
type LinkTypeSchema struct {
	APIName          string `json:"apiName"`
	SourceObjectType string `json:"sourceObjectType"`
	TargetObjectType string `json:"targetObjectType"`
	Cardinality      string `json:"cardinality"`
}

// ActionTypeSchema is a resolved action type with parsed parameters.
type ActionTypeSchema struct {
	APIName     string              `json:"apiName"`
	DisplayName string              `json:"displayName"`
	Parameters  []ActionParamSchema `json:"parameters"`
}

// ActionParamSchema is a parsed action parameter.
type ActionParamSchema struct {
	ID       string `json:"id"`
	BaseType string `json:"baseType"`
	Required bool   `json:"required"`
}

// InterfaceSchema is a resolved interface definition.
type InterfaceSchema struct {
	APIName     string `json:"apiName"`
	DisplayName string `json:"displayName"`
}

// FunctionSchema is a resolved Function definition for SDK generation. The
// SDK emits a typed Execute<Name> wrapper per function so callers don't have
// to remember the wire-format function reference (name@version).
type FunctionSchema struct {
	RID     string `json:"rid"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// ParseActionParameters converts a stored JSON array of action parameter
// definitions into structured ActionParamSchema values.
func ParseActionParameters(raw json.RawMessage) []ActionParamSchema {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "[]" {
		return []ActionParamSchema{}
	}

	type paramDef struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Required bool   `json:"required"`
	}

	var defs []paramDef
	if err := json.Unmarshal(raw, &defs); err != nil {
		return []ActionParamSchema{}
	}

	params := make([]ActionParamSchema, 0, len(defs))
	for _, d := range defs {
		params = append(params, ActionParamSchema{
			ID:       d.ID,
			BaseType: d.Type,
			Required: d.Required,
		})
	}
	return params
}
