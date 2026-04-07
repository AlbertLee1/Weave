package oms

import "context"

// Repository defines the data access interface for ontology metadata.
type Repository interface {
	// Ontology
	CreateOntology(ctx context.Context, o *Ontology) error
	GetOntology(ctx context.Context, rid string) (*Ontology, error)
	ListOntologies(ctx context.Context) ([]Ontology, error)
	UpdateOntology(ctx context.Context, o *Ontology) error

	// ObjectType
	CreateObjectType(ctx context.Context, ot *ObjectType) error
	GetObjectType(ctx context.Context, rid string) (*ObjectType, error)
	GetObjectTypeByAPIName(ctx context.Context, ontologyRID, apiName string) (*ObjectType, error)
	ListObjectTypes(ctx context.Context, ontologyRID string) ([]ObjectType, error)
	UpdateObjectType(ctx context.Context, ot *ObjectType) error
	DeleteObjectType(ctx context.Context, rid string) error

	// Property
	CreateProperty(ctx context.Context, p *Property) error
	GetProperty(ctx context.Context, rid string) (*Property, error)
	ListProperties(ctx context.Context, objectTypeRID string) ([]Property, error)
	UpdateProperty(ctx context.Context, p *Property) error
	DeleteProperty(ctx context.Context, rid string) error

	// LinkType
	CreateLinkType(ctx context.Context, lt *LinkType) error
	GetLinkType(ctx context.Context, rid string) (*LinkType, error)
	GetLinkTypeByAPIName(ctx context.Context, ontologyRID, apiName string) (*LinkType, error)
	ListOutgoingLinkTypes(ctx context.Context, objectTypeRID string) ([]LinkType, error)
	ListIncomingLinkTypes(ctx context.Context, objectTypeRID string) ([]LinkType, error)
	ListLinkTypes(ctx context.Context, ontologyRID string) ([]LinkType, error)
	UpdateLinkType(ctx context.Context, lt *LinkType) error
	DeleteLinkType(ctx context.Context, rid string) error

	// LinkEdge (M2M junction-table CRUD)
	UpsertLinkEdge(ctx context.Context, edge *LinkEdge) error
	DeleteLinkEdge(ctx context.Context, linkTypeRID, sourcePK, targetPK string) error
	DeleteAllLinkEdgesForSource(ctx context.Context, linkTypeRID, sourcePK string) error

	// ActionType
	CreateActionType(ctx context.Context, at *ActionType) error
	GetActionType(ctx context.Context, rid string) (*ActionType, error)
	ListActionTypes(ctx context.Context, ontologyRID string) ([]ActionType, error)
	UpdateActionType(ctx context.Context, at *ActionType) error
	DeleteActionType(ctx context.Context, rid string) error

	// Interface
	CreateInterface(ctx context.Context, iface *Interface) error
	GetInterface(ctx context.Context, rid string) (*Interface, error)
	GetInterfaceByAPIName(ctx context.Context, ontologyRID, apiName string) (*Interface, error)
	ListInterfaces(ctx context.Context, ontologyRID string) ([]Interface, error)
	UpdateInterface(ctx context.Context, iface *Interface) error
	DeleteInterface(ctx context.Context, rid string) error
	AttachInterface(ctx context.Context, oti *ObjectTypeInterface) error
	DetachInterface(ctx context.Context, objectTypeRID, interfaceRID string) error
	ListObjectTypeInterfaces(ctx context.Context, objectTypeRID string) ([]ObjectTypeInterface, error)
	ListInterfaceObjectTypes(ctx context.Context, interfaceRID string) ([]ObjectType, error)

	// SharedProperty
	CreateSharedProperty(ctx context.Context, sp *SharedProperty) error
	GetSharedProperty(ctx context.Context, rid string) (*SharedProperty, error)
	ListSharedProperties(ctx context.Context, ontologyRID string) ([]SharedProperty, error)
	UpdateSharedProperty(ctx context.Context, sp *SharedProperty) error
	DeleteSharedProperty(ctx context.Context, rid string) error

	// TypeGroup
	CreateTypeGroup(ctx context.Context, tg *TypeGroup) error
	GetTypeGroup(ctx context.Context, rid string) (*TypeGroup, error)
	ListTypeGroups(ctx context.Context, ontologyRID string) ([]TypeGroup, error)
	UpdateTypeGroup(ctx context.Context, tg *TypeGroup) error
	DeleteTypeGroup(ctx context.Context, rid string) error
	AssignTypeGroup(ctx context.Context, objectTypeRID, typeGroupRID string) error
	RemoveTypeGroup(ctx context.Context, objectTypeRID, typeGroupRID string) error
	ListTypeGroupsForObjectType(ctx context.Context, objectTypeRID string) ([]TypeGroup, error)

	// ValueType
	CreateValueType(ctx context.Context, vt *ValueType) error
	GetValueType(ctx context.Context, rid string) (*ValueType, error)
	ListValueTypes(ctx context.Context) ([]ValueType, error)
	UpdateValueType(ctx context.Context, vt *ValueType) error
	DeleteValueType(ctx context.Context, rid string) error

	// SecurityPolicy
	CreateSecurityPolicy(ctx context.Context, sp *SecurityPolicy) error
	GetSecurityPolicy(ctx context.Context, rid string) (*SecurityPolicy, error)
	ListSecurityPolicies(ctx context.Context, objectTypeRID string) ([]SecurityPolicy, error)
	UpdateSecurityPolicy(ctx context.Context, sp *SecurityPolicy) error
	DeleteSecurityPolicy(ctx context.Context, rid string) error

	// DatasourceBinding
	CreateDatasourceBinding(ctx context.Context, db *DatasourceBinding) error
	GetDatasourceBinding(ctx context.Context, rid string) (*DatasourceBinding, error)
	ListDatasourceBindings(ctx context.Context, objectTypeRID string) ([]DatasourceBinding, error)
	UpdateDatasourceBinding(ctx context.Context, db *DatasourceBinding) error
	DeleteDatasourceBinding(ctx context.Context, rid string) error

	// QueryType
	CreateQueryType(ctx context.Context, qt *QueryType) error
	GetQueryType(ctx context.Context, rid string) (*QueryType, error)
	GetQueryTypeByAPIName(ctx context.Context, ontologyRID, apiName string) (*QueryType, error)
	ListQueryTypes(ctx context.Context, ontologyRID string) ([]QueryType, error)
	UpdateQueryType(ctx context.Context, qt *QueryType) error
	DeleteQueryType(ctx context.Context, rid string) error

	// ActionLog
	InsertActionLog(ctx context.Context, log *ActionLog) error
	ListActionLogs(ctx context.Context, actionTypeRID string, limit, offset int) ([]ActionLog, error)
	CountActionLogs(ctx context.Context, actionTypeRID string) (int, error)

	// ObjectHistory (Tier 2.3)
	InsertObjectHistory(ctx context.Context, h *ObjectHistory) error
	ListObjectHistory(ctx context.Context, objectTypeRID, primaryKey string, limit int) ([]ObjectHistory, error)
	GetObjectVersionCount(ctx context.Context, objectTypeRID, primaryKey string) (int64, error)

	// Search
	SearchOntologyResources(ctx context.Context, ontologyRID, query string) ([]SearchResult, error)

	// Snapshot
	CreateSnapshot(ctx context.Context, snapshot *OntologySnapshot) error
	ListSnapshots(ctx context.Context, ontologyRID string) ([]OntologySnapshot, error)
	GetSnapshot(ctx context.Context, ontologyRID string, version int) (*OntologySnapshot, error)
	GetOntologyVersion(ctx context.Context, ontologyRID string) (int, error)
	IncrementOntologyVersion(ctx context.Context, ontologyRID string) (int, error)
}
