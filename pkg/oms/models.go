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
	Ontology         Ontology         `json:"ontology"`
	ObjectTypes      []ObjectType     `json:"objectTypes"`
	LinkTypes        []LinkType       `json:"linkTypes"`
	ActionTypes      []ActionType     `json:"actionTypes"`
	Interfaces       []Interface      `json:"interfaces"`
	SharedProperties []SharedProperty `json:"sharedProperties"`
	ValueTypes       []ValueType      `json:"valueTypes"`
	TypeGroups       []TypeGroup      `json:"typeGroups"`
	Functions        []Function       `json:"functions"`
	QueryTypes       []QueryType      `json:"queryTypes"`
}

// Ontology represents a top-level ontology container.
type Ontology struct {
	RID            string    `json:"rid"`
	APIName        string    `json:"apiName"`
	DisplayName    string    `json:"displayName"`
	Description    string    `json:"description,omitempty"`
	CurrentVersion int       `json:"currentVersion"`
	CreatedAt      time.Time `json:"-"`
	UpdatedAt      time.Time `json:"-"`
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

// ToFullMetadataJSON returns the V2 wire format JSON for ObjectType
// with all metadata fields included (properties with full detail, etc.).
func (ot *ObjectType) ToFullMetadataJSON() ([]byte, error) {
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
	if ot.IconName != "" {
		wire["icon"] = ot.IconName
	}
	if ot.Color != "" {
		wire["color"] = ot.Color
	}

	if len(ot.Properties) > 0 {
		props := make(map[string]interface{})
		for _, p := range ot.Properties {
			entry := map[string]interface{}{
				"dataType": p.DataTypeJSON(),
				"rid":      p.RID,
			}
			if p.DisplayName != "" {
				entry["displayName"] = p.DisplayName
			}
			if p.Description != "" {
				entry["description"] = p.Description
			}
			props[p.APIName] = entry
		}
		wire["properties"] = props
	}

	return json.Marshal(wire)
}

// Property defines a property on an ObjectType.
type Property struct {
	RID               string          `json:"rid"`
	ObjectTypeRID     string          `json:"-"`
	APIName           string          `json:"apiName"`
	DisplayName       string          `json:"displayName,omitempty"`
	Description       string          `json:"description,omitempty"`
	BaseType          string          `json:"baseType"`
	TypeConfig        json.RawMessage `json:"typeConfig,omitempty"`
	IsArray           bool            `json:"isArray"`
	IsNullable        bool            `json:"isNullable"`
	IsSearchable      bool            `json:"isSearchable"`
	IsSortable        bool            `json:"isSortable"`
	Status            string          `json:"status,omitempty"`
	DeprecatedReason  string          `json:"deprecatedReason,omitempty"`
	SharedPropertyRID string          `json:"sharedPropertyRid,omitempty"`
	// Derived marks a property as computed at query time (e.g. withProperties
	// aggregations). Derived properties cannot be primary keys, cannot be
	// text-searched, and cannot be written through Action edits (US-004).
	Derived bool `json:"derived,omitempty"`
	// IsEditOnly marks a property as "edit-only" — once a user edit writes a
	// value to it, concurrent ingest edits must not overwrite that value
	// regardless of the active conflict-resolution strategy (US-025 / US-055).
	IsEditOnly bool      `json:"editOnly,omitempty"`
	CreatedAt  time.Time `json:"-"`
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
		"apiName":                 lt.APIName,
		"displayName":             lt.DisplayName,
		"rid":                     lt.RID,
		"objectTypeApiName":       lt.SourceObjectType,
		"linkedObjectTypeApiName": lt.TargetObjectType,
		"cardinality":             lt.Cardinality,
		"required":                lt.IsRequired,
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

// actionParamDef is the internal (stored) array-element format.
type actionParamDef struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

// parametersToV2 converts the stored array-of-objects parameters format
// into the Foundry OSv2 Record<ParameterId, ActionParameterV2> wire format.
// Input:  [{"id":"name","type":"string","required":true,"description":"..."}]
// Output: {"name":{"dataType":{"type":"string"},"required":true,"description":"..."}}
func parametersToV2(raw json.RawMessage) map[string]interface{} {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "[]" {
		return map[string]interface{}{}
	}

	var defs []actionParamDef
	if err := json.Unmarshal(raw, &defs); err != nil {
		return map[string]interface{}{}
	}

	result := make(map[string]interface{}, len(defs))
	for _, d := range defs {
		entry := map[string]interface{}{
			"dataType": map[string]interface{}{"type": d.Type},
			"required": d.Required,
		}
		if d.Description != "" {
			entry["description"] = d.Description
		}
		result[d.ID] = entry
	}
	return result
}

// ToWireJSON returns the V2 wire format JSON for ActionType.
func (at *ActionType) ToWireJSON() ([]byte, error) {
	wire := map[string]interface{}{
		"apiName":     at.APIName,
		"displayName": at.DisplayName,
		"rid":         at.RID,
		"status":      at.Status,
		"parameters":  parametersToV2(at.Parameters),
	}
	if at.Description != "" {
		wire["description"] = at.Description
	}
	return json.Marshal(wire)
}

// ToFullMetadataJSON returns the V2 wire format JSON for ActionType
// with all metadata fields included (rules, submissionCriteria, sideEffects, etc.).
func (at *ActionType) ToFullMetadataJSON() ([]byte, error) {
	wire := map[string]interface{}{
		"apiName":     at.APIName,
		"displayName": at.DisplayName,
		"rid":         at.RID,
		"status":      at.Status,
		"parameters":  parametersToV2(at.Parameters),
	}
	if at.Description != "" {
		wire["description"] = at.Description
	}
	if len(at.Rules) > 0 && string(at.Rules) != "null" {
		wire["rules"] = json.RawMessage(at.Rules)
	}
	if len(at.SubmissionCriteria) > 0 && string(at.SubmissionCriteria) != "null" {
		wire["submissionCriteria"] = json.RawMessage(at.SubmissionCriteria)
	}
	if len(at.SideEffects) > 0 && string(at.SideEffects) != "null" {
		wire["sideEffects"] = json.RawMessage(at.SideEffects)
	}
	if at.FunctionRID != "" {
		wire["functionRid"] = at.FunctionRID
	}
	wire["isFunctionBacked"] = at.IsFunctionBacked
	return json.Marshal(wire)
}

// InterfaceLinkType defines an outgoing link type declared on an interface.
type InterfaceLinkType struct {
	APIName                 string `json:"apiName"`
	DisplayName             string `json:"displayName"`
	LinkedEntityTypeAPIName string `json:"linkedEntityTypeApiName"`
	LinkedEntityTypeRID     string `json:"linkedEntityTypeRid,omitempty"`
	Cardinality             string `json:"cardinality"`
	Required                bool   `json:"required"`
	Description             string `json:"description,omitempty"`
}

// Interface defines a shared contract for ObjectTypes.
type Interface struct {
	RID               string          `json:"rid"`
	OntologyRID       string          `json:"-"`
	APIName           string          `json:"apiName"`
	DisplayName       string          `json:"displayName"`
	ExtendsRID        string          `json:"extendsRid,omitempty"`
	SharedProperties  json.RawMessage `json:"sharedProperties,omitempty"`
	OutgoingLinkTypes json.RawMessage `json:"outgoingLinkTypes,omitempty"`
	CreatedAt         time.Time       `json:"-"`
}

// ObjectTypeInterface represents an object type implementing an interface.
type ObjectTypeInterface struct {
	ObjectTypeRID   string          `json:"objectTypeRid"`
	InterfaceRID    string          `json:"interfaceRid"`
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
	PrevEdits     json.RawMessage `json:"prevEdits,omitempty"` // US-104: pre-edit state for undo (parallel to Edits)
	Status        string          `json:"status"`
	ErrorMessage  string          `json:"errorMessage,omitempty"`
	CreatedAt     time.Time       `json:"createdAt"`
}

// DatasourceBinding connects an ObjectType to its underlying data source.
type DatasourceBinding struct {
	RID           string          `json:"rid"`
	ObjectTypeRID string          `json:"-"`
	DatasetRID    string          `json:"datasetRid"`
	Branch        string          `json:"branch"`
	ColumnMapping json.RawMessage `json:"columnMapping"`
	IsPrimary     bool            `json:"isPrimary"`
	CreatedAt     time.Time       `json:"-"`
}

// QueryType defines a predefined filter+aggregation combo stored as metadata.
type QueryType struct {
	RID         string          `json:"rid"`
	OntologyRID string          `json:"-"`
	APIName     string          `json:"apiName"`
	DisplayName string          `json:"displayName"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
	Output      json.RawMessage `json:"output"`
	Query       json.RawMessage `json:"query"`
	FunctionRID string          `json:"functionRid,omitempty"`
	Status      string          `json:"status"`
	CreatedAt   time.Time       `json:"-"`
}

// Function represents a stored JavaScript function in the ontology.
type Function struct {
	RID         string    `json:"rid"`
	OntologyRID string    `json:"-"`
	Name        string    `json:"name"`
	Version     int       `json:"version"`
	SourceCode  string    `json:"sourceCode"`
	CreatedBy   string    `json:"createdBy"`
	CreatedAt   time.Time `json:"createdAt"`
}

// OntologyBranch represents a branch for isolated ontology schema changes.
type OntologyBranch struct {
	ID          string    `json:"id"`
	OntologyRID string    `json:"ontologyRid"`
	Name        string    `json:"name"`
	BaseVersion int64     `json:"baseVersion"`
	Status      string    `json:"status"` // open, merged, closed
	CreatedBy   string    `json:"createdBy"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// BranchChange records a single change made on a branch.
type BranchChange struct {
	ID          string          `json:"id"`
	BranchID    string          `json:"branchId"`
	ChangeType  string          `json:"changeType"` // ADDED, MODIFIED, DELETED
	EntityType  string          `json:"entityType"`
	EntityRID   string          `json:"entityRid"`
	BeforeState json.RawMessage `json:"beforeState,omitempty"`
	AfterState  json.RawMessage `json:"afterState,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
}

// OntologyProposal represents a merge proposal from a branch.
type OntologyProposal struct {
	ID          string    `json:"id"`
	BranchID    string    `json:"branchId"`
	OntologyRID string    `json:"ontologyRid"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status"` // pending, approved, rejected, merged
	Author      string    `json:"author"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ProposalReview records a reviewer's decision on a proposal.
type ProposalReview struct {
	ID         string    `json:"id"`
	ProposalID string    `json:"proposalId"`
	Reviewer   string    `json:"reviewer"`
	Decision   string    `json:"decision"` // approve, reject
	Reason     string    `json:"reason,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

// AutomationRule defines an automation rule for scheduled or event-driven execution.
type AutomationRule struct {
	ID            string          `json:"id"`
	OntologyRID   string          `json:"ontologyRid"`
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	Status        string          `json:"status"`        // active, paused, disabled
	TriggerType   string          `json:"triggerType"`    // schedule, dataChange, manual
	TriggerConfig json.RawMessage `json:"triggerConfig"`
	Effects       json.RawMessage `json:"effects"`
	RetryPolicy   json.RawMessage `json:"retryPolicy,omitempty"`
	CreatedBy     string          `json:"createdBy"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}

// AutomationExecution records a single execution of an automation rule.
type AutomationExecution struct {
	ID           string          `json:"id"`
	RuleID       string          `json:"ruleId"`
	TriggerEvent json.RawMessage `json:"triggerEvent"`
	StartedAt    time.Time       `json:"startedAt"`
	CompletedAt  *time.Time      `json:"completedAt,omitempty"`
	Status       string          `json:"status"` // running, success, error, retrying
	Error        string          `json:"error,omitempty"`
	RetryCount   int             `json:"retryCount"`
	Result       json.RawMessage `json:"result,omitempty"`
}

// Notification represents a user notification (US-130).
type Notification struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Type      string    `json:"type"`
	Link      string    `json:"link,omitempty"`
	Read      bool      `json:"read"`
	CreatedAt time.Time `json:"createdAt"`
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
