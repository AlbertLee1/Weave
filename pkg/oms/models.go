package oms

import (
	"encoding/json"
	"time"
)

// SearchResult represents a search hit across ontology resources.
type SearchResult struct {
	RID          string `json:"rid"`
	ResourceType string `json:"resourceType"`
	APIName      string `json:"apiName"`
	DisplayName  string `json:"displayName"`
	Description  string `json:"description,omitempty"`
	Status       string `json:"status,omitempty"`
}

// OntologySnapshot represents a versioned snapshot of an ontology's state.
type OntologySnapshot struct {
	ID          int64           `json:"id"`
	OntologyRID string          `json:"ontologyRid"`
	Version     int             `json:"version"`
	Label       string          `json:"label,omitempty"`
	Description string          `json:"description,omitempty"`
	Snapshot    json.RawMessage `json:"snapshot"`
	CreatedBy   string          `json:"createdBy"`
	CreatedAt   time.Time       `json:"createdAt"`
}

// OntologyExport represents the full export format for an ontology.
type OntologyExport struct {
	Ontology    Ontology     `json:"ontology"`
	ObjectTypes []ObjectType `json:"objectTypes"`
	LinkTypes   []LinkType   `json:"linkTypes"`
	ActionTypes []ActionType `json:"actionTypes"`
	Interfaces  []Interface  `json:"interfaces"`
}

// Ontology represents a top-level ontology container.
type Ontology struct {
	RID         string    `json:"rid"`
	APIName     string    `json:"apiName"`
	DisplayName string    `json:"displayName"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"-"`
	UpdatedAt   time.Time `json:"-"`
}

// ObjectType defines a type of object in the ontology.
type ObjectType struct {
	RID                string     `json:"rid"`
	OntologyRID        string     `json:"-"`
	APIName            string     `json:"apiName"`
	DisplayName        string     `json:"displayName"`
	PluralDisplayName  string     `json:"pluralDisplayName,omitempty"`
	Description        string     `json:"description,omitempty"`
	PrimaryKey         string     `json:"primaryKey"`
	TitleProperty      string     `json:"titleProperty,omitempty"`
	Status             string     `json:"status"`
	Visibility         string     `json:"visibility"`
	IconName           string     `json:"icon,omitempty"`
	Color              string     `json:"color,omitempty"`
	DeprecatedReason   string     `json:"deprecatedReason,omitempty"`
	DeprecatedDeadline *time.Time `json:"deprecatedDeadline,omitempty"`
	Properties         []Property `json:"properties,omitempty"`
	CreatedAt          time.Time  `json:"-"`
	UpdatedAt          time.Time  `json:"-"`
}

// ToWireJSON returns the V2 wire format JSON for ObjectType.
func (ot *ObjectType) ToWireJSON() ([]byte, error) {
	wire := map[string]interface{}{
		"apiName":     ot.APIName,
		"displayName": ot.DisplayName,
		"status":      ot.Status,
		"primaryKey":  ot.PrimaryKey,
		"rid":         ot.RID,
		"visibility":  ot.Visibility,
	}

	if ot.PluralDisplayName != "" {
		wire["pluralDisplayName"] = ot.PluralDisplayName
	}
	if ot.Description != "" {
		wire["description"] = ot.Description
	}
	if ot.TitleProperty != "" {
		wire["titleProperty"] = ot.TitleProperty
	}

	if len(ot.Properties) > 0 {
		props := make(map[string]interface{})
		for _, p := range ot.Properties {
			props[p.APIName] = map[string]interface{}{
				"dataType": p.DataTypeJSON(),
				"rid":      p.RID,
			}
		}
		wire["properties"] = props
	}

	return json.Marshal(wire)
}

// Property defines a property on an ObjectType.
type Property struct {
	RID           string          `json:"rid"`
	ObjectTypeRID string          `json:"-"`
	APIName       string          `json:"apiName"`
	DisplayName   string          `json:"displayName,omitempty"`
	Description   string          `json:"description,omitempty"`
	BaseType      string          `json:"baseType"`
	TypeConfig    json.RawMessage `json:"typeConfig,omitempty"`
	IsArray       bool            `json:"isArray"`
	IsNullable    bool            `json:"isNullable"`
	IsSearchable  bool            `json:"isSearchable"`
	IsSortable       bool            `json:"isSortable"`
	Status           string          `json:"status,omitempty"`
	DeprecatedReason string          `json:"deprecatedReason,omitempty"`
	CreatedAt        time.Time       `json:"-"`
}

// DataTypeJSON returns the Palantir V2 dataType JSON representation.
func (p *Property) DataTypeJSON() map[string]interface{} {
	if p.IsArray {
		return map[string]interface{}{
			"type":    "array",
			"subType": map[string]interface{}{"type": p.BaseType},
		}
	}
	dt := map[string]interface{}{"type": p.BaseType}
	if len(p.TypeConfig) > 0 && string(p.TypeConfig) != "{}" {
		var extra map[string]interface{}
		if json.Unmarshal(p.TypeConfig, &extra) == nil {
			for k, v := range extra {
				dt[k] = v
			}
		}
	}
	return dt
}

// LinkType defines a relationship between two ObjectTypes.
type LinkType struct {
	RID              string          `json:"rid"`
	OntologyRID      string          `json:"-"`
	APIName          string          `json:"apiName"`
	DisplayName      string          `json:"displayName"`
	Description      string          `json:"description,omitempty"`
	SourceObjectType string          `json:"objectTypeApiName"`
	TargetObjectType string          `json:"linkedObjectTypeApiName"`
	Cardinality      string          `json:"cardinality"`
	ForeignKeyConfig json.RawMessage `json:"foreignKeyConfig,omitempty"`
	JoinTableConfig  json.RawMessage `json:"joinTableConfig,omitempty"`
	IsRequired       bool            `json:"required"`
	CreatedAt        time.Time       `json:"-"`
}

// ToWireJSON returns the V2 wire format JSON for LinkType.
func (lt *LinkType) ToWireJSON() ([]byte, error) {
	wire := map[string]interface{}{
		"apiName":                lt.APIName,
		"displayName":           lt.DisplayName,
		"rid":                   lt.RID,
		"objectTypeApiName":     lt.SourceObjectType,
		"linkedObjectTypeApiName": lt.TargetObjectType,
		"cardinality":           lt.Cardinality,
		"required":              lt.IsRequired,
	}
	if lt.Description != "" {
		wire["description"] = lt.Description
	}
	return json.Marshal(wire)
}

// ActionType defines an action that can be performed.
type ActionType struct {
	RID                string          `json:"rid"`
	OntologyRID        string          `json:"-"`
	APIName            string          `json:"apiName"`
	DisplayName        string          `json:"displayName"`
	Description        string          `json:"description,omitempty"`
	Status             string          `json:"status"`
	Parameters         json.RawMessage `json:"parameters"`
	Rules              json.RawMessage `json:"rules"`
	SubmissionCriteria json.RawMessage `json:"submissionCriteria,omitempty"`
	SideEffects        json.RawMessage `json:"sideEffects,omitempty"`
	FunctionRID        string          `json:"functionRid,omitempty"`
	IsFunctionBacked   bool            `json:"isFunctionBacked"`
	CreatedAt          time.Time       `json:"-"`
}

// ToWireJSON returns the V2 wire format JSON for ActionType.
func (at *ActionType) ToWireJSON() ([]byte, error) {
	wire := map[string]interface{}{
		"apiName":     at.APIName,
		"displayName": at.DisplayName,
		"rid":         at.RID,
		"status":      at.Status,
		"parameters":  json.RawMessage(at.Parameters),
	}
	if at.Description != "" {
		wire["description"] = at.Description
	}
	return json.Marshal(wire)
}

// Interface defines a shared contract for ObjectTypes.
type Interface struct {
	RID              string          `json:"rid"`
	OntologyRID      string          `json:"-"`
	APIName          string          `json:"apiName"`
	DisplayName      string          `json:"displayName"`
	ExtendsRID       string          `json:"extendsRid,omitempty"`
	SharedProperties json.RawMessage `json:"sharedProperties,omitempty"`
	CreatedAt        time.Time       `json:"-"`
}

// ObjectTypeInterface represents an object type implementing an interface.
type ObjectTypeInterface struct {
	ObjectTypeRID  string          `json:"objectTypeRid"`
	InterfaceRID   string          `json:"interfaceRid"`
	PropertyMapping json.RawMessage `json:"propertyMapping"`
}

// ValueType defines a reusable custom value type (e.g., currency, email).
type ValueType struct {
	RID         string          `json:"rid"`
	APIName     string          `json:"apiName"`
	DisplayName string          `json:"displayName"`
	BaseType    string          `json:"baseType"`
	Constraints json.RawMessage `json:"constraints,omitempty"`
	Version     int             `json:"version"`
	CreatedAt   time.Time       `json:"-"`
}

// SharedProperty defines a reusable property definition across object types.
type SharedProperty struct {
	RID         string          `json:"rid"`
	OntologyRID string          `json:"-"`
	APIName     string          `json:"apiName"`
	DisplayName string          `json:"displayName,omitempty"`
	Description string          `json:"description,omitempty"`
	BaseType    string          `json:"baseType"`
	TypeConfig  json.RawMessage `json:"typeConfig,omitempty"`
	IsArray     bool            `json:"isArray"`
	CreatedAt   time.Time       `json:"-"`
}

// SecurityPolicy controls access to object types and their properties.
type SecurityPolicy struct {
	RID           string          `json:"rid"`
	ObjectTypeRID string          `json:"objectTypeRid"`
	PolicyType    string          `json:"policyType"` // "OBJECT" or "PROPERTY"
	Rules         json.RawMessage `json:"rules"`
	CreatedAt     time.Time       `json:"-"`
}

// ActionLog records the execution of an action.
type ActionLog struct {
	ID            int64           `json:"id"`
	ActionTypeRID string          `json:"actionTypeRid"`
	UserID        string          `json:"userId"`
	Parameters    json.RawMessage `json:"parameters"`
	Edits         json.RawMessage `json:"edits"`
	Status        string          `json:"status"`
	ErrorMessage  string          `json:"errorMessage,omitempty"`
	CreatedAt     time.Time       `json:"createdAt"`
}

// TypeGroup organizes object types into categories.
type TypeGroup struct {
	RID         string    `json:"rid"`
	OntologyRID string    `json:"-"`
	APIName     string    `json:"apiName"`
	DisplayName string    `json:"displayName"`
	Description string    `json:"description,omitempty"`
	Color       string    `json:"color,omitempty"`
	CreatedAt   time.Time `json:"-"`
}
