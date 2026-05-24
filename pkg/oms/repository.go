package oms

import (
	"context"
	"encoding/json"
)

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
	GetActionTypeByAPIName(ctx context.Context, ontologyRID, apiNameOrRID string) (*ActionType, error)
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
	GetValueTypeByAPIName(ctx context.Context, ridOrApiName string) (*ValueType, error)
	ListValueTypes(ctx context.Context) ([]ValueType, error)
	UpdateValueType(ctx context.Context, vt *ValueType) error
	DeleteValueType(ctx context.Context, rid string) error
	// ListPropertyUsagesByBaseType returns every Property whose base_type
	// references the given ValueType apiName, joined with its ObjectType so
	// the admin "Used By" view can render human-readable identifiers
	// without a per-row fanout. Properties on archived/deprecated
	// ObjectTypes are included — the operator wants the complete reverse
	// reference set before deleting a ValueType.
	ListPropertyUsagesByBaseType(ctx context.Context, baseType string) ([]PropertyUsage, error)

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

	// Function
	CreateFunction(ctx context.Context, fn *Function) error
	GetFunction(ctx context.Context, rid string) (*Function, error)
	GetFunctionByName(ctx context.Context, ontologyRID, name string) (*Function, error)
	GetFunctionByNameVersion(ctx context.Context, ontologyRID, name, version string) (*Function, error)
	ListFunctions(ctx context.Context, ontologyRID string) ([]Function, error)
	ListFunctionVersionsByName(ctx context.Context, ontologyRID, name string) ([]Function, error)
	UpdateFunction(ctx context.Context, fn *Function) error
	DeleteFunction(ctx context.Context, rid string) error

	// QueryType
	CreateQueryType(ctx context.Context, qt *QueryType) error
	GetQueryType(ctx context.Context, rid string) (*QueryType, error)
	GetQueryTypeByAPIName(ctx context.Context, ontologyRID, apiName string) (*QueryType, error)
	ListQueryTypes(ctx context.Context, ontologyRID string) ([]QueryType, error)
	UpdateQueryType(ctx context.Context, qt *QueryType) error
	DeleteQueryType(ctx context.Context, rid string) error

	// ActionLog
	InsertActionLog(ctx context.Context, log *ActionLog) error
	GetActionLog(ctx context.Context, id int64) (*ActionLog, error)
	ListActionLogs(ctx context.Context, actionTypeRID string, limit, offset int) ([]ActionLog, error)
	CountActionLogs(ctx context.Context, actionTypeRID string) (int, error)
	UpdateActionLogStatus(ctx context.Context, id int64, status string) error
	// UpdateActionLogSideEffectStatus persists the per-effect dispatch
	// outcomes (PRD-V2 Gap-A4) for the given action_logs row. status is a
	// JSON array of {type, status, attempts, error, durationMs} objects.
	// nil/empty payload is allowed and stores SQL NULL — callers can use
	// this to clear the column or skip the update entirely.
	UpdateActionLogSideEffectStatus(ctx context.Context, id int64, status json.RawMessage) error

	// InsertSideEffectDLQRow appends one entry to the side-effect dead
	// letter queue (PRD-V2 Gap-A4 round 33). Called by the executor
	// after a SideEffectOutcome surfaces Status=failed (round-30 retry
	// loop exhausted). The (action_log_id, effect_index) pair is
	// uniquely-keyed so a second commit of the same row (extremely
	// unlikely; would indicate a duplicate executor run) is rejected
	// with ErrDuplicate rather than silently double-queued.
	InsertSideEffectDLQRow(ctx context.Context, row *SideEffectDLQRow) error

	// ListSideEffectDLQByActionLog returns the DLQ rows associated with
	// the given action_log id, ordered by effect_index ascending. Empty
	// slice when the action had no failed side effects. Used by the
	// admin / debug surface to render "what failed for this action".
	ListSideEffectDLQByActionLog(ctx context.Context, actionLogID int64) ([]SideEffectDLQRow, error)

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

	// ObjectEmbedding (Tier 3.1) — pgvector-backed nearest-neighbor search.
	UpsertObjectEmbedding(ctx context.Context, e *ObjectEmbedding) error
	GetObjectEmbedding(ctx context.Context, objectTypeRID, primaryKey, model string) (*ObjectEmbedding, error)
	FindNearestNeighbors(ctx context.Context, objectTypeRID string, queryVector []float32, k int, model string) ([]NearestNeighborResult, error)

	// OntologyBranch (Phase 2: Ontology Branching)
	CreateBranch(ctx context.Context, b *OntologyBranch) error
	GetBranch(ctx context.Context, id string) (*OntologyBranch, error)
	ListBranches(ctx context.Context, ontologyRID string) ([]OntologyBranch, error)
	CloseBranch(ctx context.Context, id string) error
	UpdateBranchStatus(ctx context.Context, id, status string) error
	UpdateBranchBaseVersion(ctx context.Context, id string, baseVersion int64) error
	CreateBranchChange(ctx context.Context, c *BranchChange) error
	ListBranchChanges(ctx context.Context, branchID string) ([]BranchChange, error)
	UpdateBranchChangeBeforeState(ctx context.Context, id string, beforeState json.RawMessage) error

	// AutomationRule (US-124: Automate Engine)
	CreateAutomationRule(ctx context.Context, rule *AutomationRule) error
	GetAutomationRule(ctx context.Context, id string) (*AutomationRule, error)
	ListAutomationRules(ctx context.Context, ontologyRID string) ([]AutomationRule, error)
	UpdateAutomationRule(ctx context.Context, rule *AutomationRule) error
	DeleteAutomationRule(ctx context.Context, id string) error
	InsertExecution(ctx context.Context, exec *AutomationExecution) error
	UpdateExecution(ctx context.Context, exec *AutomationExecution) error
	ListExecutions(ctx context.Context, ruleID string) ([]AutomationExecution, error)

	// Notification (US-130)
	CreateNotification(ctx context.Context, n *Notification) error
	ListNotifications(ctx context.Context, userID string, unreadOnly bool) ([]Notification, error)
	MarkNotificationRead(ctx context.Context, id string) error

	// OntologyProposal (US-117)
	CreateProposal(ctx context.Context, p *OntologyProposal) error
	GetProposal(ctx context.Context, id string) (*OntologyProposal, error)
	ListProposals(ctx context.Context, ontologyRID string) ([]OntologyProposal, error)
	UpdateProposalStatus(ctx context.Context, id, status string) error
	CreateProposalReview(ctx context.Context, r *ProposalReview) error
	ListProposalReviews(ctx context.Context, proposalID string) ([]ProposalReview, error)
}
