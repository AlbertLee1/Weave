package oms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/rid"
)

// --- Branch overlay helpers ---

// validateBranch checks if the request pins to a valid, open branch.
// Branch can come from ?branch= query OR X-Weave-Branch header
// (round 39 / Gap-T4). Returns the branch if valid, nil if no branch
// signal, or writes an error response and returns nil.
func (h *OMSHandler) validateBranch(w http.ResponseWriter, r *http.Request) (*OntologyBranch, bool) {
	branchID := ResolveBranchFromRequest(r)
	if branchID == "" || branchID == DefaultBranch {
		return nil, true // no branch, continue with normal flow
	}

	branch, err := h.repo.GetBranch(r.Context(), branchID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("BranchNotFound", map[string]string{
				"branchId": branchID,
			}))
			return nil, false
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetBranchFailed", nil))
		return nil, false
	}

	if branch.Status != "open" {
		apierror.WriteJSON(w, apierror.NewConflict("BranchNotOpen", map[string]string{
			"branchId": branchID,
			"status":   branch.Status,
		}))
		return nil, false
	}

	return branch, true
}

// writeBranchChange records a change to the branch overlay instead of writing to main tables.
func (h *OMSHandler) writeBranchChange(ctx context.Context, branchID, changeType, entityType, entityRID string, beforeState, afterState interface{}) error {
	var beforeJSON, afterJSON json.RawMessage
	if beforeState != nil {
		b, err := json.Marshal(beforeState)
		if err != nil {
			return err
		}
		beforeJSON = b
	}
	if afterState != nil {
		b, err := json.Marshal(afterState)
		if err != nil {
			return err
		}
		afterJSON = b
	}

	c := &BranchChange{
		ID:          uuid.New().String(),
		BranchID:    branchID,
		ChangeType:  changeType,
		EntityType:  entityType,
		EntityRID:   entityRID,
		BeforeState: beforeJSON,
		AfterState:  afterJSON,
	}
	return h.repo.CreateBranchChange(ctx, c)
}

// --- Request structs ---

// CreateOntologyRequest is the request body for creating an ontology.
type CreateOntologyRequest struct {
	APIName     string `json:"apiName"`
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
}

// CreateObjectTypeRequest is the request body for creating an object type.
type CreateObjectTypeRequest struct {
	APIName           string `json:"apiName"`
	DisplayName       string `json:"displayName"`
	PluralDisplayName string `json:"pluralDisplayName,omitempty"`
	Description       string `json:"description,omitempty"`
	// PrimaryKey is the legacy single-field key. Senders may provide either
	// this OR PrimaryKeys; if both are set, PrimaryKeys wins and PrimaryKey
	// is overwritten with PrimaryKeys[0]. At least one must be supplied.
	PrimaryKey string `json:"primaryKey,omitempty"`
	// PrimaryKeys (US-211) is the ordered list of property API names that
	// together form a composite key. A single-element array is equivalent
	// to a non-empty PrimaryKey. The route /objects/{objectType}/{key1}:{key2}
	// joins the values with ':' for retrieval.
	PrimaryKeys   []string `json:"primaryKeys,omitempty"`
	TitleProperty string   `json:"titleProperty,omitempty"`
	Status        string   `json:"status"`
	Visibility    string   `json:"visibility"`
	// ExtendsRID (US-212) optionally points at a parent ObjectType in the same
	// ontology. Validation: parent must exist, share the ontology, and not form
	// a cycle.
	ExtendsRID string `json:"extendsRid,omitempty"`
	// Classification (US-262) is an optional data-classification label chosen
	// from KnownClassifications(). Empty / omitted means "unspecified".
	// Unknown labels are rejected with a typed 400.
	Classification string `json:"classification,omitempty"`
	// AuditDataAccess (US-264) opts the ObjectType into per-read audit
	// logging. Defaults to false; admins flip the flag to emit an
	// audit_events row (action = "data.access") for every successful OSS
	// read against this type.
	AuditDataAccess bool `json:"auditDataAccess,omitempty"`
	// IsEvent / EventStartProp / EventEndProp (VTX-077) flag the ObjectType as
	// a timeline event so the Vertex Timeline can render each row as a bar from
	// EventStartProp to EventEndProp. Defaults to a non-event type.
	IsEvent        bool   `json:"isEvent,omitempty"`
	EventStartProp string `json:"eventStartProp,omitempty"`
	EventEndProp   string `json:"eventEndProp,omitempty"`
}

// UpdateObjectTypeRequest is the request body for updating an object type.
type UpdateObjectTypeRequest struct {
	DisplayName        string  `json:"displayName"`
	PluralDisplayName  string  `json:"pluralDisplayName,omitempty"`
	Description        string  `json:"description,omitempty"`
	TitleProperty      string  `json:"titleProperty,omitempty"`
	Status             string  `json:"status"`
	Visibility         string  `json:"visibility"`
	IconName           string  `json:"icon,omitempty"`
	Color              string  `json:"color,omitempty"`
	DeprecatedReason   string  `json:"deprecatedReason,omitempty"`
	DeprecatedDeadline *string `json:"deprecatedDeadline,omitempty"`
	// ExtendsRID (US-212) overwrites the parent pointer when non-nil. Pass an
	// empty-string pointer to clear the link; omit the field to leave it
	// untouched. Same shape as LinkType.InverseLinkRID.
	ExtendsRID *string `json:"extendsRid,omitempty"`
	// Classification (US-262) is a tri-state pointer: nil = leave the existing
	// label untouched, "" = clear, any known label = assign. Bare string would
	// collapse "omit" and "clear" into one, silently clearing classification
	// on every partial update.
	Classification *string `json:"classification,omitempty"`
	// AuditDataAccess (US-264) is a tri-state pointer: nil = leave unchanged,
	// true/false = toggle the per-read audit flag. Partial updates that omit
	// the field preserve the current setting so admins can edit other
	// attributes without accidentally disabling audit.
	AuditDataAccess *bool `json:"auditDataAccess,omitempty"`
	// IsEvent / EventStartProp / EventEndProp (VTX-077) are tri-state pointers
	// so a partial update that omits them preserves the existing timeline
	// configuration. nil = leave unchanged; an explicit value overwrites (an
	// empty-string prop clears the placement).
	IsEvent        *bool   `json:"isEvent,omitempty"`
	EventStartProp *string `json:"eventStartProp,omitempty"`
	EventEndProp   *string `json:"eventEndProp,omitempty"`
}

// UpdateOntologyRequest is the request body for updating an ontology.
type UpdateOntologyRequest struct {
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
}

// UpdatePropertyRequest is the request body for updating a property.
type UpdatePropertyRequest struct {
	DisplayName      string `json:"displayName,omitempty"`
	Description      string `json:"description,omitempty"`
	IsSearchable     *bool  `json:"isSearchable,omitempty"`
	IsSortable       *bool  `json:"isSortable,omitempty"`
	IsNullable       *bool  `json:"isNullable,omitempty"`
	Status           string `json:"status,omitempty"`
	DeprecatedReason string `json:"deprecatedReason,omitempty"`
	EditOnly         *bool  `json:"editOnly,omitempty"`
	// Classification (US-262) is a tri-state pointer: nil = preserve, "" =
	// clear, any known label = assign. See UpdateObjectTypeRequest.
	Classification *string `json:"classification,omitempty"`
}

// UpdateLinkTypeRequest is the request body for updating a link type.
type UpdateLinkTypeRequest struct {
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
	IsRequired  *bool  `json:"required,omitempty"`
	// InverseLinkRID, when non-nil, overwrites the inverse pointer. Pass an
	// empty-string pointer to clear the existing pairing; omit the field to
	// leave it untouched.
	InverseLinkRID *string `json:"inverseLinkRid,omitempty"`
	// PropagateMarkings (US-261) is a tri-state pointer: nil = leave the
	// existing setting untouched, false = disable propagation, true = enable
	// it. Bare bool would silently disable any LinkType that did not echo
	// the field on a partial update.
	PropagateMarkings *bool `json:"propagateMarkings,omitempty"`
	// TypeClasses (VTX-010) is a tri-state pointer to a slice: nil = leave
	// existing tags untouched, &[] = clear, non-empty = replace. Bare slice
	// would collapse "omit" and "clear" into one, silently wiping the tags
	// on every partial update.
	TypeClasses *[]string `json:"typeClasses,omitempty"`
}

// CreatePropertyRequest is the request body for creating a property.
type CreatePropertyRequest struct {
	APIName      string          `json:"apiName"`
	DisplayName  string          `json:"displayName,omitempty"`
	Description  string          `json:"description,omitempty"`
	BaseType     string          `json:"baseType"`
	TypeConfig   json.RawMessage `json:"typeConfig,omitempty"`
	IsArray      bool            `json:"isArray"`
	IsNullable   bool            `json:"isNullable"`
	IsSearchable bool            `json:"isSearchable"`
	IsSortable   bool            `json:"isSortable"`
	EditOnly     bool            `json:"editOnly,omitempty"`
	// Classification (US-262) is an optional label from KnownClassifications().
	// Empty means "unspecified". Unknown labels are rejected 400.
	Classification string `json:"classification,omitempty"`
	// SharedPropertyTypeAPIName (round 55) binds the Property to a
	// SharedProperty in the same ontology by api-name. When set, the
	// handler resolves the api-name → RID, validates that baseType and
	// isArray match the SharedProperty exactly, and stores the
	// resolved RID on Property.SharedPropertyRID. Mismatches are
	// rejected at write time so admins can't ship an inconsistent
	// schema that silently produces wrong-typed loads later.
	SharedPropertyTypeAPIName string `json:"sharedPropertyTypeApiName,omitempty"`
}

// CreateLinkTypeRequest is the request body for creating a link type.
type CreateLinkTypeRequest struct {
	APIName          string          `json:"apiName"`
	DisplayName      string          `json:"displayName"`
	Description      string          `json:"description,omitempty"`
	SourceObjectType string          `json:"objectTypeApiName"`
	TargetObjectType string          `json:"linkedObjectTypeApiName"`
	Cardinality      string          `json:"cardinality"`
	ForeignKeyConfig json.RawMessage `json:"foreignKeyConfig,omitempty"`
	JoinTableConfig  json.RawMessage `json:"joinTableConfig,omitempty"`
	IsRequired       bool            `json:"required"`
	InverseLinkRID   string          `json:"inverseLinkRid,omitempty"`
	// PropagateMarkings (US-261) opts the LinkType into automatic marking
	// inheritance: every LINK_CREATE event copies the source object's
	// `_markings` set into the target's. Default false preserves the
	// pre-US-261 behavior where link creation never touches markings.
	PropagateMarkings bool `json:"propagateMarkings,omitempty"`
	// TypeClasses (VTX-010) tags the LinkType with Vertex-graph rendering
	// labels (see oms.KnownVertexLinkTypeClasses). Unknown tags are rejected
	// with 400 InvalidParameter:typeClasses.
	TypeClasses []string `json:"typeClasses,omitempty"`
}

// CreateActionTypeRequest is the request body for creating an action type.
type CreateActionTypeRequest struct {
	APIName     string          `json:"apiName"`
	DisplayName string          `json:"displayName"`
	Description string          `json:"description,omitempty"`
	Status      string          `json:"status"`
	Parameters  json.RawMessage `json:"parameters"`
	Rules       json.RawMessage `json:"rules"`
	// SubmissionCriteria (round 135) is the criteria JSON evaluated by
	// pkg/actions.EvaluateCriteria at apply time. When a criteriaValidator
	// is wired the handler statically validates structure before persisting
	// so authoring mistakes surface as 422 instead of as runtime errors.
	SubmissionCriteria json.RawMessage `json:"submissionCriteria,omitempty"`
	// ImplementsMethodRID (US-214) optionally binds this ActionType to an
	// InterfaceMethod signature. When set the create handler validates that
	// the referenced method exists in the same ontology.
	ImplementsMethodRID string `json:"implementsMethodRid,omitempty"`
	// CompensateActionRID (US-239) optionally names another ActionType in
	// the same ontology whose rules compensate this action during a saga
	// rollback. Create-time validation checks that the referenced
	// ActionType exists and lives in the same ontology.
	CompensateActionRID string `json:"compensateActionRid,omitempty"`
	// ParameterSchema (US-245) optionally attaches a Draft-07 JSON Schema
	// that every Apply request must satisfy. When non-empty the Prepare
	// flow evaluates it after the legacy ParameterDef validator and emits
	// WEAVE_VALIDATION_SCHEMA 422 on violation.
	ParameterSchema json.RawMessage `json:"parameterSchema,omitempty"`
	// RequiresApproval (US-242) gates this ActionType behind human approval.
	// When true the Apply handler enqueues an ActionApproval row instead of
	// executing rules. Settable from the Ontology Manager builder.
	RequiresApproval bool `json:"requiresApproval,omitempty"`
	// Approvers (US-242) lists the role names (or user IDs) allowed to
	// approve a pending request for this gated action. Empty means nobody
	// can approve. Sent by the builder as a string slice.
	Approvers []string `json:"approvers,omitempty"`
}

// UpdateActionTypeRequest is the request body for updating an action type.
type UpdateActionTypeRequest struct {
	DisplayName        string          `json:"displayName"`
	Description        string          `json:"description,omitempty"`
	Status             string          `json:"status"`
	Parameters         json.RawMessage `json:"parameters"`
	Rules              json.RawMessage `json:"rules"`
	SubmissionCriteria json.RawMessage `json:"submissionCriteria,omitempty"`
	SideEffects        json.RawMessage `json:"sideEffects,omitempty"`
	// ImplementsMethodRID (US-214) is a tri-state pointer: nil = leave
	// unchanged, "" = clear the binding, non-empty = validate + assign.
	ImplementsMethodRID *string `json:"implementsMethodRid,omitempty"`
	// CompensateActionRID (US-239) is a tri-state pointer: nil = leave
	// unchanged, "" = clear the compensation pair, non-empty = validate +
	// assign. Prevents self-reference (an action cannot compensate itself).
	CompensateActionRID *string `json:"compensateActionRid,omitempty"`
	// ParameterSchema (US-245) is a tri-state pointer: nil = leave
	// unchanged, an explicit null/empty RawMessage = clear the schema,
	// non-empty = replace. Stored as JSONB on the action_types row.
	ParameterSchema *json.RawMessage `json:"parameterSchema,omitempty"`
	// RequiresApproval (US-242) is a tri-state pointer: nil = leave
	// unchanged, non-nil = replace the gate flag. The Ontology Manager
	// builder always sends an explicit value so a toggle round-trips.
	RequiresApproval *bool `json:"requiresApproval,omitempty"`
	// Approvers (US-242) is a tri-state pointer: nil = leave unchanged,
	// non-nil (incl. empty slice) = replace the approver roster.
	Approvers *[]string `json:"approvers,omitempty"`
}

// --- Admin handlers ---

// resolveOntologyRID resolves an ontology apiName or RID to the actual RID.
func (h *OMSHandler) resolveOntologyRID(ctx context.Context, apiNameOrRID string) (string, error) {
	o, err := h.repo.GetOntology(ctx, apiNameOrRID)
	if err != nil {
		return "", err
	}
	return o.RID, nil
}

// CreateOntology handles POST /api/admin/ontologies.
func (h *OMSHandler) CreateOntology(w http.ResponseWriter, r *http.Request) {
	var req CreateOntologyRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	if req.APIName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:apiName", map[string]string{
			"parameter": "apiName",
			"reason":    "apiName is required",
		}))
		return
	}
	if req.DisplayName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:displayName", map[string]string{
			"parameter": "displayName",
			"reason":    "displayName is required",
		}))
		return
	}

	o := &Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     req.APIName,
		DisplayName: req.DisplayName,
		Description: req.Description,
	}

	if err := h.repo.CreateOntology(r.Context(), o); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("OntologyAlreadyExists", map[string]string{
				"apiName": req.APIName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CreateOntologyFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, o)
}

// CreateObjectType handles POST /api/admin/ontologies/{ontologyApiName}/objectTypes.
func (h *OMSHandler) CreateObjectType(w http.ResponseWriter, r *http.Request) {
	ontologyRID, err := h.resolveOntologyRID(r.Context(), chi.URLParam(r, "ontologyApiName"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
				"ontologyApiName": chi.URLParam(r, "ontologyApiName"),
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ResolveOntologyFailed", nil))
		return
	}

	var req CreateObjectTypeRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	if req.APIName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:apiName", map[string]string{
			"parameter": "apiName",
			"reason":    "apiName is required",
		}))
		return
	}
	if req.DisplayName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:displayName", map[string]string{
			"parameter": "displayName",
			"reason":    "displayName is required",
		}))
		return
	}
	// US-211: accept either legacy primaryKey (string) or composite primaryKeys
	// ([]string). If both are present, primaryKeys wins and primaryKey is
	// rewritten to its first element so legacy single-PK consumers keep working.
	pkList := req.PrimaryKeys
	if len(pkList) == 0 && req.PrimaryKey != "" {
		pkList = []string{req.PrimaryKey}
	}
	if len(pkList) == 0 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:primaryKey", map[string]string{
			"parameter": "primaryKey",
			"reason":    "primaryKey or primaryKeys is required",
		}))
		return
	}
	for i, pk := range pkList {
		if pk == "" {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:primaryKeys", map[string]string{
				"parameter": "primaryKeys",
				"reason":    fmt.Sprintf("primaryKeys[%d] is empty", i),
			}))
			return
		}
	}

	status := req.Status
	if status == "" {
		status = "ACTIVE"
	}
	visibility := req.Visibility
	if visibility == "" {
		visibility = "NORMAL"
	}
	if !IsKnownClassification(req.Classification) {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:classification", map[string]string{
			"parameter": "classification",
			"reason":    "classification must be one of Public, Internal, Confidential, PII, Secret",
		}))
		return
	}

	ot := &ObjectType{
		RID:               rid.NewObjectTypeRID(),
		OntologyRID:       ontologyRID,
		APIName:           req.APIName,
		DisplayName:       req.DisplayName,
		PluralDisplayName: req.PluralDisplayName,
		Description:       req.Description,
		PrimaryKey:        pkList[0],
		PrimaryKeys:       pkList,
		TitleProperty:     req.TitleProperty,
		Status:            status,
		Visibility:        visibility,
		ExtendsRID:        req.ExtendsRID,
		Classification:    req.Classification,
		AuditDataAccess:   req.AuditDataAccess,
		IsEvent:           req.IsEvent,
		EventStartProp:    req.EventStartProp,
		EventEndProp:      req.EventEndProp,
	}

	// US-212: validate inheritance candidate. The parent must exist, live in the
	// same ontology, and not introduce a cycle once this row is added. The
	// candidate's RID is the freshly generated `ot.RID`, so the cycle check
	// degenerates to "parent does not already point back through ot.RID" — but
	// since ot is brand new, only the same-row self-loop case is reachable.
	if req.ExtendsRID != "" {
		parent, err := h.repo.GetObjectType(r.Context(), req.ExtendsRID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:extendsRid", map[string]string{
					"parameter": "extendsRid",
					"reason":    "parent ObjectType not found",
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("GetParentObjectTypeFailed", nil))
			return
		}
		if parent.OntologyRID != ontologyRID {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:extendsRid", map[string]string{
				"parameter": "extendsRid",
				"reason":    "parent ObjectType belongs to a different ontology",
			}))
			return
		}
		if err := ValidateInheritanceCandidate(r.Context(), h.repo, ot.RID, req.ExtendsRID); err != nil {
			if errors.Is(err, ErrInheritanceCycle) {
				apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:extendsRid", map[string]string{
					"parameter": "extendsRid",
					"reason":    "inheritance chain forms a cycle",
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("ValidateInheritanceFailed", nil))
			return
		}
	}

	// Branch overlay: if ?branch= is set, record as branch change instead of writing to main
	branch, ok := h.validateBranch(w, r)
	if !ok {
		return
	}
	if branch != nil {
		if err := h.writeBranchChange(r.Context(), branch.ID, "ADDED", "objectType", ot.RID, nil, ot); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("WriteBranchChangeFailed", nil))
			return
		}
		httputil.WriteJSON(w, http.StatusCreated, ot)
		return
	}

	if err := h.repo.CreateObjectType(r.Context(), ot); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("ObjectTypeAlreadyExists", map[string]string{
				"apiName": req.APIName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CreateObjectTypeFailed", nil))
		return
	}

	// DOG-003: bootstrap the Bleve index shell synchronously so an immediate
	// stream ingest against this ObjectType lands in a real index instead of
	// silently failing with "index not found".
	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	if ontologyAPIName != "" {
		_ = h.ensureObjectTypeIndex(ontologyAPIName, ot.APIName, nil)
	}

	httputil.WriteJSON(w, http.StatusCreated, ot)
}

// UpdateObjectType handles PUT /api/admin/objectTypes/{objectTypeRid}.
func (h *OMSHandler) UpdateObjectType(w http.ResponseWriter, r *http.Request) {
	objectTypeRID := chi.URLParam(r, "objectTypeRid")

	var req UpdateObjectTypeRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	// Get the existing object type first to merge fields
	existing, err := h.repo.GetObjectType(r.Context(), objectTypeRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
				"objectTypeRid": objectTypeRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetObjectTypeFailed", nil))
		return
	}

	// Build updated copy (leave existing intact for branch before-state)
	updated := *existing
	updated.DisplayName = req.DisplayName
	updated.PluralDisplayName = req.PluralDisplayName
	updated.Description = req.Description
	updated.TitleProperty = req.TitleProperty
	updated.Status = req.Status
	updated.Visibility = req.Visibility
	updated.IconName = req.IconName
	updated.Color = req.Color
	updated.DeprecatedReason = req.DeprecatedReason
	if req.DeprecatedDeadline != nil && *req.DeprecatedDeadline != "" {
		t, err := time.Parse(time.RFC3339, *req.DeprecatedDeadline)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:deprecatedDeadline", map[string]string{
				"parameter": "deprecatedDeadline",
				"reason":    "must be a valid RFC3339 timestamp",
			}))
			return
		}
		updated.DeprecatedDeadline = &t
	} else {
		updated.DeprecatedDeadline = nil
	}
	// US-262 classification tri-state: nil = preserve, "" = clear, any known
	// label = assign. Unknown labels reject BEFORE we touch any state so a bad
	// input never mutates the row.
	if req.Classification != nil {
		if !IsKnownClassification(*req.Classification) {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:classification", map[string]string{
				"parameter": "classification",
				"reason":    "classification must be one of Public, Internal, Confidential, PII, Secret",
			}))
			return
		}
		updated.Classification = *req.Classification
	}
	// US-264 audit-data-access tri-state: nil = preserve, explicit bool =
	// overwrite. No additional validation — the column is a plain flag.
	if req.AuditDataAccess != nil {
		updated.AuditDataAccess = *req.AuditDataAccess
	}
	// VTX-077 timeline event tri-state: nil = preserve, explicit value =
	// overwrite. Each field is independent so an admin can flip the isEvent
	// flag or re-point a start/end prop without touching the others.
	if req.IsEvent != nil {
		updated.IsEvent = *req.IsEvent
	}
	if req.EventStartProp != nil {
		updated.EventStartProp = *req.EventStartProp
	}
	if req.EventEndProp != nil {
		updated.EventEndProp = *req.EventEndProp
	}
	// US-212: ExtendsRID tri-state — nil pointer leaves unchanged, "" clears,
	// non-empty rewrites and is validated against the same rules as Create
	// (parent exists, same ontology, no cycle including the row's own RID so a
	// chain like A→B→C→A surfaces as 400).
	if req.ExtendsRID != nil {
		newParent := *req.ExtendsRID
		if newParent != "" {
			parent, perr := h.repo.GetObjectType(r.Context(), newParent)
			if perr != nil {
				if errors.Is(perr, ErrNotFound) {
					apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:extendsRid", map[string]string{
						"parameter": "extendsRid",
						"reason":    "parent ObjectType not found",
					}))
					return
				}
				apierror.WriteJSON(w, apierror.NewInternal("GetParentObjectTypeFailed", nil))
				return
			}
			if parent.OntologyRID != existing.OntologyRID {
				apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:extendsRid", map[string]string{
					"parameter": "extendsRid",
					"reason":    "parent ObjectType belongs to a different ontology",
				}))
				return
			}
			if verr := ValidateInheritanceCandidate(r.Context(), h.repo, existing.RID, newParent); verr != nil {
				if errors.Is(verr, ErrInheritanceCycle) {
					apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:extendsRid", map[string]string{
						"parameter": "extendsRid",
						"reason":    "inheritance chain forms a cycle",
					}))
					return
				}
				apierror.WriteJSON(w, apierror.NewInternal("ValidateInheritanceFailed", nil))
				return
			}
		}
		updated.ExtendsRID = newParent
	}

	// Branch overlay
	branch, ok := h.validateBranch(w, r)
	if !ok {
		return
	}
	if branch != nil {
		if err := h.writeBranchChange(r.Context(), branch.ID, "MODIFIED", "objectType", existing.RID, existing, &updated); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("WriteBranchChangeFailed", nil))
			return
		}
		httputil.WriteJSON(w, http.StatusOK, &updated)
		return
	}

	if err := h.repo.UpdateObjectType(r.Context(), &updated); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
				"objectTypeRid": objectTypeRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("UpdateObjectTypeFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, &updated)
}

// DeleteObjectType handles DELETE /api/admin/objectTypes/{objectTypeRid}.
func (h *OMSHandler) DeleteObjectType(w http.ResponseWriter, r *http.Request) {
	objectTypeRID := chi.URLParam(r, "objectTypeRid")

	// Check status: ACTIVE and PROMOTED object types cannot be deleted
	existing, err := h.repo.GetObjectType(r.Context(), objectTypeRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
				"objectTypeRid": objectTypeRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetObjectTypeFailed", nil))
		return
	}
	// Branch overlay: on branch, skip status check (branch is for schema experimentation)
	branch, ok := h.validateBranch(w, r)
	if !ok {
		return
	}
	if branch != nil {
		if err := h.writeBranchChange(r.Context(), branch.ID, "DELETED", "objectType", existing.RID, existing, nil); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("WriteBranchChangeFailed", nil))
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if existing.Status == "ACTIVE" || existing.Status == "PROMOTED" {
		apierror.WriteJSON(w, apierror.NewConflict("ObjectTypeNotDeletable", map[string]string{
			"objectTypeRid": objectTypeRID,
			"status":        existing.Status,
			"reason":        "cannot delete an object type with status ACTIVE or PROMOTED; deprecate it first",
		}))
		return
	}

	if err := h.repo.DeleteObjectType(r.Context(), objectTypeRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
				"objectTypeRid": objectTypeRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DeleteObjectTypeFailed", nil))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListPropertiesForObjectTypeAdmin handles
// GET /api/v2/ontologies/{ontologyApiName}/objectTypes/byRid/{objectTypeRid}/properties.
// It returns the raw Property rows for the given ObjectType (with ?branch= overlay
// when present), used by the Ontology Manager visual property editor.
func (h *OMSHandler) ListPropertiesForObjectTypeAdmin(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r)
	if !ok {
		return
	}
	objectTypeRID := chi.URLParam(r, "objectTypeRid")
	props, err := repo.ListProperties(r.Context(), objectTypeRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListPropertiesFailed", nil))
		return
	}
	if props == nil {
		props = []Property{}
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": props})
}

// CreateProperty handles POST /api/admin/objectTypes/{objectTypeRid}/properties.
func (h *OMSHandler) CreateProperty(w http.ResponseWriter, r *http.Request) {
	objectTypeRID := chi.URLParam(r, "objectTypeRid")

	var req CreatePropertyRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	if req.APIName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:apiName", map[string]string{
			"parameter": "apiName",
			"reason":    "apiName is required",
		}))
		return
	}
	if req.BaseType == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:baseType", map[string]string{
			"parameter": "baseType",
			"reason":    "baseType is required",
		}))
		return
	}
	if !IsKnownClassification(req.Classification) {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:classification", map[string]string{
			"parameter": "classification",
			"reason":    "classification must be one of Public, Internal, Confidential, PII, Secret",
		}))
		return
	}

	// Round 55: resolve sharedPropertyTypeAPIName → RID inside the
	// owning ontology and validate baseType / isArray match the
	// SharedProperty. Resolution happens BEFORE any write so a
	// failure leaves the repo untouched. Empty field = no binding,
	// preserving the round-54-era behavior bit-for-bit.
	var resolvedSharedPropertyRID string
	if req.SharedPropertyTypeAPIName != "" {
		ot, otErr := h.repo.GetObjectType(r.Context(), objectTypeRID)
		if otErr != nil || ot == nil {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
				"objectTypeRid": objectTypeRID,
			}))
			return
		}
		spList, spErr := h.repo.ListSharedProperties(r.Context(), ot.OntologyRID)
		if spErr != nil {
			apierror.WriteJSON(w, apierror.NewInternal("ListSharedPropertiesFailed", nil))
			return
		}
		var matched *SharedProperty
		for i := range spList {
			if spList[i].APIName == req.SharedPropertyTypeAPIName {
				matched = &spList[i]
				break
			}
		}
		if matched == nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("SharedPropertyTypeNotFound", map[string]string{
				"ontology":           ot.OntologyRID,
				"sharedPropertyType": req.SharedPropertyTypeAPIName,
			}))
			return
		}
		if matched.BaseType != req.BaseType {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("SharedPropertyTypeMismatch", map[string]string{
				"sharedPropertyType":     req.SharedPropertyTypeAPIName,
				"sharedPropertyBaseType": matched.BaseType,
				"propertyBaseType":       req.BaseType,
			}))
			return
		}
		if matched.IsArray != req.IsArray {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("SharedPropertyTypeMismatch", map[string]string{
				"sharedPropertyType":    req.SharedPropertyTypeAPIName,
				"sharedPropertyIsArray": strconv.FormatBool(matched.IsArray),
				"propertyIsArray":       strconv.FormatBool(req.IsArray),
			}))
			return
		}
		resolvedSharedPropertyRID = matched.RID
	}

	p := &Property{
		RID:               rid.NewPropertyRID(),
		ObjectTypeRID:     objectTypeRID,
		APIName:           req.APIName,
		DisplayName:       req.DisplayName,
		Description:       req.Description,
		BaseType:          req.BaseType,
		TypeConfig:        req.TypeConfig,
		IsArray:           req.IsArray,
		IsNullable:        req.IsNullable,
		IsSearchable:      req.IsSearchable,
		IsSortable:        req.IsSortable,
		IsEditOnly:        req.EditOnly,
		Classification:    req.Classification,
		SharedPropertyRID: resolvedSharedPropertyRID,
	}

	// Branch overlay
	branch, ok := h.validateBranch(w, r)
	if !ok {
		return
	}
	if branch != nil {
		if err := h.writeBranchChange(r.Context(), branch.ID, "ADDED", "property", p.RID, nil, p); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("WriteBranchChangeFailed", nil))
			return
		}
		httputil.WriteJSON(w, http.StatusCreated, p)
		return
	}

	if err := h.repo.CreateProperty(r.Context(), p); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("PropertyAlreadyExists", map[string]string{
				"apiName": req.APIName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CreatePropertyFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, p)
}

// DeleteProperty handles DELETE /api/admin/properties/{propertyRid}.
func (h *OMSHandler) DeleteProperty(w http.ResponseWriter, r *http.Request) {
	propertyRID := chi.URLParam(r, "propertyRid")

	// Branch overlay: fetch before state and record change
	branch, ok := h.validateBranch(w, r)
	if !ok {
		return
	}
	if branch != nil {
		existing, err := h.repo.GetProperty(r.Context(), propertyRID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				apierror.WriteJSON(w, apierror.NewNotFound("PropertyNotFound", map[string]string{
					"propertyRid": propertyRID,
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("GetPropertyFailed", nil))
			return
		}
		if err := h.writeBranchChange(r.Context(), branch.ID, "DELETED", "property", existing.RID, existing, nil); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("WriteBranchChangeFailed", nil))
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := h.repo.DeleteProperty(r.Context(), propertyRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("PropertyNotFound", map[string]string{
				"propertyRid": propertyRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DeletePropertyFailed", nil))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// CreateLinkType handles POST /api/admin/ontologies/{ontologyApiName}/linkTypes.
func (h *OMSHandler) CreateLinkType(w http.ResponseWriter, r *http.Request) {
	ontologyRID, err := h.resolveOntologyRID(r.Context(), chi.URLParam(r, "ontologyApiName"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
				"ontologyApiName": chi.URLParam(r, "ontologyApiName"),
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ResolveOntologyFailed", nil))
		return
	}

	var req CreateLinkTypeRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	if req.APIName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:apiName", map[string]string{
			"parameter": "apiName",
			"reason":    "apiName is required",
		}))
		return
	}
	if req.DisplayName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:displayName", map[string]string{
			"parameter": "displayName",
			"reason":    "displayName is required",
		}))
		return
	}

	if apiErr := validateLinkTypeClasses(req.TypeClasses); apiErr != nil {
		apierror.WriteJSON(w, apiErr)
		return
	}

	lt := &LinkType{
		RID:               rid.NewLinkTypeRID(),
		OntologyRID:       ontologyRID,
		APIName:           req.APIName,
		DisplayName:       req.DisplayName,
		Description:       req.Description,
		SourceObjectType:  req.SourceObjectType,
		TargetObjectType:  req.TargetObjectType,
		Cardinality:       req.Cardinality,
		ForeignKeyConfig:  req.ForeignKeyConfig,
		JoinTableConfig:   req.JoinTableConfig,
		IsRequired:        req.IsRequired,
		InverseLinkRID:    req.InverseLinkRID,
		PropagateMarkings: req.PropagateMarkings,
		TypeClasses:       normaliseLinkTypeClasses(req.TypeClasses),
	}

	if apiErr := h.validateInverseLinkPair(r.Context(), lt); apiErr != nil {
		apierror.WriteJSON(w, apiErr)
		return
	}

	// Branch overlay
	branch, ok := h.validateBranch(w, r)
	if !ok {
		return
	}
	if branch != nil {
		if err := h.writeBranchChange(r.Context(), branch.ID, "ADDED", "linkType", lt.RID, nil, lt); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("WriteBranchChangeFailed", nil))
			return
		}
		httputil.WriteJSON(w, http.StatusCreated, lt)
		return
	}

	if err := h.repo.CreateLinkType(r.Context(), lt); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("LinkTypeAlreadyExists", map[string]string{
				"apiName": req.APIName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CreateLinkTypeFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, lt)
}

// validateLinkTypeClasses (VTX-010) rejects unknown tags in
// LinkType.TypeClasses. Empty / nil slice is fine. Returns nil on success or
// a populated *apierror.APIError the caller can write verbatim.
func validateLinkTypeClasses(classes []string) *apierror.APIError {
	for _, c := range classes {
		if !IsKnownVertexLinkTypeClass(c) {
			return apierror.NewInvalidParameter("InvalidParameter:typeClasses", map[string]string{
				"parameter": "typeClasses",
				"reason":    "unknown type class: " + c,
			})
		}
	}
	return nil
}

// normaliseLinkTypeClasses returns a deduplicated, deterministically-ordered
// copy of classes. Empty / nil input returns nil to keep the wire shape
// compact (omitempty).
func normaliseLinkTypeClasses(classes []string) []string {
	if len(classes) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(classes))
	out := make([]string, 0, len(classes))
	for _, c := range classes {
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}

// validateInverseLinkPair enforces the endpoint-symmetry contract for
// bidirectional LinkTypes (US-209). When lt.InverseLinkRID is empty the call
// is a no-op. Otherwise the partner is looked up via h.repo.GetLinkType and
// the pair must:
//   - live in the same ontology
//   - satisfy partner.SourceObjectType == lt.TargetObjectType
//   - satisfy partner.TargetObjectType == lt.SourceObjectType
//
// Returns nil on success or a populated *apierror.APIError the caller can
// write verbatim. Self-reference is rejected because A's inverse being A
// itself makes no sense for the symmetric-endpoints invariant.
func (h *OMSHandler) validateInverseLinkPair(ctx context.Context, lt *LinkType) *apierror.APIError {
	if lt.InverseLinkRID == "" {
		return nil
	}
	if lt.InverseLinkRID == lt.RID {
		return apierror.NewInvalidParameter("InvalidParameter:inverseLinkRid", map[string]string{
			"parameter": "inverseLinkRid",
			"reason":    "inverseLinkRid must not reference the link type itself",
		})
	}
	partner, err := h.repo.GetLinkType(ctx, lt.InverseLinkRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return apierror.NewNotFound("InverseLinkTypeNotFound", map[string]string{
				"inverseLinkRid": lt.InverseLinkRID,
			})
		}
		return apierror.NewInternal("GetInverseLinkTypeFailed", nil)
	}
	if partner.OntologyRID != lt.OntologyRID {
		return apierror.NewInvalidParameter("InvalidParameter:inverseLinkRid", map[string]string{
			"parameter": "inverseLinkRid",
			"reason":    "inverse link must belong to the same ontology",
		})
	}
	if partner.SourceObjectType != lt.TargetObjectType || partner.TargetObjectType != lt.SourceObjectType {
		return apierror.NewInvalidParameter("InvalidParameter:inverseLinkRid", map[string]string{
			"parameter":                "inverseLinkRid",
			"reason":                   "inverse link endpoints must mirror this link (partner.source == this.target and partner.target == this.source)",
			"expectedSourceObjectType": lt.TargetObjectType,
			"expectedTargetObjectType": lt.SourceObjectType,
			"partnerSourceObjectType":  partner.SourceObjectType,
			"partnerTargetObjectType":  partner.TargetObjectType,
		})
	}
	return nil
}

// CreateActionType handles POST /api/admin/ontologies/{ontologyApiName}/actionTypes.
func (h *OMSHandler) CreateActionType(w http.ResponseWriter, r *http.Request) {
	ontologyRID, err := h.resolveOntologyRID(r.Context(), chi.URLParam(r, "ontologyApiName"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
				"ontologyApiName": chi.URLParam(r, "ontologyApiName"),
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ResolveOntologyFailed", nil))
		return
	}

	var req CreateActionTypeRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	if req.APIName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:apiName", map[string]string{
			"parameter": "apiName",
			"reason":    "apiName is required",
		}))
		return
	}
	if req.DisplayName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:displayName", map[string]string{
			"parameter": "displayName",
			"reason":    "displayName is required",
		}))
		return
	}

	status := req.Status
	if status == "" {
		status = "ACTIVE"
	}

	// Round 135: structurally validate submissionCriteria before persistence
	// so authoring mistakes (unknown type, missing field, malformed group)
	// surface as 422 here instead of as runtime errors at first apply.
	if h.criteriaValidator != nil && len(req.SubmissionCriteria) > 0 {
		if err := h.criteriaValidator(req.SubmissionCriteria); err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:submissionCriteria", map[string]string{
				"parameter": "submissionCriteria",
				"reason":    err.Error(),
			}))
			return
		}
	}

	at := &ActionType{
		RID:                 rid.NewActionTypeRID(),
		OntologyRID:         ontologyRID,
		APIName:             req.APIName,
		DisplayName:         req.DisplayName,
		Description:         req.Description,
		Status:              status,
		Parameters:          req.Parameters,
		Rules:               req.Rules,
		SubmissionCriteria:  req.SubmissionCriteria,
		ImplementsMethodRID: req.ImplementsMethodRID,
		CompensateActionRID: req.CompensateActionRID,
		ParameterSchema:     req.ParameterSchema,
		RequiresApproval:    req.RequiresApproval,
		Approvers:           req.Approvers,
	}

	// US-214: if the action claims an InterfaceMethod binding, verify the
	// method exists in the same ontology BEFORE we commit (or route to a
	// branch overlay). The lookup uses the narrow InterfaceMethodStore when
	// configured; degraded-mode routers that don't wire the store reject the
	// field so callers get a clean error instead of a dangling reference.
	if at.ImplementsMethodRID != "" {
		if apiErr := h.validateImplementsMethodRID(r.Context(), ontologyRID, at.ImplementsMethodRID); apiErr != nil {
			apierror.WriteJSON(w, apiErr)
			return
		}
	}
	// US-239: if the action declares a compensator, verify the partner
	// ActionType exists in the same ontology and is not a self-reference.
	if at.CompensateActionRID != "" {
		if apiErr := h.validateCompensateActionRID(r.Context(), ontologyRID, at.RID, at.CompensateActionRID); apiErr != nil {
			apierror.WriteJSON(w, apiErr)
			return
		}
	}

	// Branch overlay
	branch, ok := h.validateBranch(w, r)
	if !ok {
		return
	}
	if branch != nil {
		if err := h.writeBranchChange(r.Context(), branch.ID, "ADDED", "actionType", at.RID, nil, at); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("WriteBranchChangeFailed", nil))
			return
		}
		httputil.WriteJSON(w, http.StatusCreated, at)
		return
	}

	if err := h.repo.CreateActionType(r.Context(), at); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("ActionTypeAlreadyExists", map[string]string{
				"apiName": req.APIName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CreateActionTypeFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, at)
}

// UpdateActionType handles PUT /api/admin/actionTypes/{actionTypeRid}.
func (h *OMSHandler) UpdateActionType(w http.ResponseWriter, r *http.Request) {
	actionTypeRID := chi.URLParam(r, "actionTypeRid")

	var req UpdateActionTypeRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	existing, err := h.repo.GetActionType(r.Context(), actionTypeRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ActionTypeNotFound", map[string]string{
				"actionTypeRid": actionTypeRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetActionTypeFailed", nil))
		return
	}

	// Round 135: structurally validate submissionCriteria before persistence
	// (Update path mirrors Create). Validator runs only when the request
	// actually carried a non-empty criteria payload so omitting the field
	// to preserve existing criteria still works.
	if h.criteriaValidator != nil && len(req.SubmissionCriteria) > 0 {
		if err := h.criteriaValidator(req.SubmissionCriteria); err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:submissionCriteria", map[string]string{
				"parameter": "submissionCriteria",
				"reason":    err.Error(),
			}))
			return
		}
	}

	// Build updated copy (leave existing intact for branch before-state)
	updated := *existing
	updated.DisplayName = req.DisplayName
	updated.Description = req.Description
	updated.Status = req.Status
	updated.Parameters = req.Parameters
	updated.Rules = req.Rules
	if len(req.SubmissionCriteria) > 0 {
		updated.SubmissionCriteria = req.SubmissionCriteria
	}
	if len(req.SideEffects) > 0 {
		updated.SideEffects = req.SideEffects
	}
	// US-214 tri-state: nil=preserve, ""=clear, "X"=validate+assign.
	if req.ImplementsMethodRID != nil {
		updated.ImplementsMethodRID = *req.ImplementsMethodRID
		if updated.ImplementsMethodRID != "" {
			if apiErr := h.validateImplementsMethodRID(r.Context(), existing.OntologyRID, updated.ImplementsMethodRID); apiErr != nil {
				apierror.WriteJSON(w, apiErr)
				return
			}
		}
	}
	// US-239 tri-state: nil=preserve, ""=clear compensator, "X"=validate+assign.
	if req.CompensateActionRID != nil {
		updated.CompensateActionRID = *req.CompensateActionRID
		if updated.CompensateActionRID != "" {
			if apiErr := h.validateCompensateActionRID(r.Context(), existing.OntologyRID, existing.RID, updated.CompensateActionRID); apiErr != nil {
				apierror.WriteJSON(w, apiErr)
				return
			}
		}
	}
	// US-245 tri-state: nil=preserve, empty/null RawMessage=clear schema,
	// non-empty=replace. The Prepare path treats empty / null schemas as
	// no-ops so clearing is a safe operation.
	if req.ParameterSchema != nil {
		if hasParameterSchemaRaw(*req.ParameterSchema) {
			updated.ParameterSchema = *req.ParameterSchema
		} else {
			updated.ParameterSchema = nil
		}
	}
	// US-242 tri-state: nil=preserve, non-nil=replace the approval gate so the
	// Ontology Manager builder can toggle requiresApproval / approvers.
	if req.RequiresApproval != nil {
		updated.RequiresApproval = *req.RequiresApproval
	}
	if req.Approvers != nil {
		updated.Approvers = *req.Approvers
	}

	// Branch overlay
	branch, ok := h.validateBranch(w, r)
	if !ok {
		return
	}
	if branch != nil {
		if err := h.writeBranchChange(r.Context(), branch.ID, "MODIFIED", "actionType", existing.RID, existing, &updated); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("WriteBranchChangeFailed", nil))
			return
		}
		httputil.WriteJSON(w, http.StatusOK, &updated)
		return
	}

	if err := h.repo.UpdateActionType(r.Context(), &updated); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ActionTypeNotFound", map[string]string{
				"actionTypeRid": actionTypeRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("UpdateActionTypeFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, &updated)
}

// UpdateOntology handles PUT /api/admin/ontologies/{ontologyRid}.
func (h *OMSHandler) UpdateOntology(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyRid")

	var req UpdateOntologyRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	existing, err := h.repo.GetOntology(r.Context(), ontologyRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
				"ontologyRid": ontologyRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetOntologyFailed", nil))
		return
	}

	if req.DisplayName != "" {
		existing.DisplayName = req.DisplayName
	}
	existing.Description = req.Description

	if err := h.repo.UpdateOntology(r.Context(), existing); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
				"ontologyRid": ontologyRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("UpdateOntologyFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, existing)
}

// UpdateProperty handles PUT /api/admin/properties/{propertyRid}.
func (h *OMSHandler) UpdateProperty(w http.ResponseWriter, r *http.Request) {
	propertyRID := chi.URLParam(r, "propertyRid")

	var req UpdatePropertyRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	existing, err := h.repo.GetProperty(r.Context(), propertyRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("PropertyNotFound", map[string]string{
				"propertyRid": propertyRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetPropertyFailed", nil))
		return
	}

	// Build updated copy (leave existing intact for branch before-state)
	updated := *existing
	if req.DisplayName != "" {
		updated.DisplayName = req.DisplayName
	}
	if req.Description != "" {
		updated.Description = req.Description
	}
	if req.IsSearchable != nil {
		updated.IsSearchable = *req.IsSearchable
	}
	if req.IsSortable != nil {
		updated.IsSortable = *req.IsSortable
	}
	if req.IsNullable != nil {
		updated.IsNullable = *req.IsNullable
	}
	if req.Status != "" {
		updated.Status = req.Status
	}
	if req.DeprecatedReason != "" {
		updated.DeprecatedReason = req.DeprecatedReason
	}
	if req.EditOnly != nil {
		updated.IsEditOnly = *req.EditOnly
	}
	// US-262 classification tri-state: nil = preserve, "" = clear, any known
	// label = assign. Reject unknown BEFORE any persistence attempt.
	if req.Classification != nil {
		if !IsKnownClassification(*req.Classification) {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:classification", map[string]string{
				"parameter": "classification",
				"reason":    "classification must be one of Public, Internal, Confidential, PII, Secret",
			}))
			return
		}
		updated.Classification = *req.Classification
	}

	// Branch overlay
	branch, ok := h.validateBranch(w, r)
	if !ok {
		return
	}
	if branch != nil {
		if err := h.writeBranchChange(r.Context(), branch.ID, "MODIFIED", "property", existing.RID, existing, &updated); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("WriteBranchChangeFailed", nil))
			return
		}
		httputil.WriteJSON(w, http.StatusOK, &updated)
		return
	}

	if err := h.repo.UpdateProperty(r.Context(), &updated); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("PropertyNotFound", map[string]string{
				"propertyRid": propertyRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("UpdatePropertyFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, &updated)
}

// UpdateLinkType handles PUT /api/admin/linkTypes/{linkTypeRid}.
func (h *OMSHandler) UpdateLinkType(w http.ResponseWriter, r *http.Request) {
	linkTypeRID := chi.URLParam(r, "linkTypeRid")

	var req UpdateLinkTypeRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	existing, err := h.repo.GetLinkType(r.Context(), linkTypeRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("LinkTypeNotFound", map[string]string{
				"linkTypeRid": linkTypeRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetLinkTypeFailed", nil))
		return
	}

	// Build updated copy (leave existing intact for branch before-state)
	updated := *existing
	if req.DisplayName != "" {
		updated.DisplayName = req.DisplayName
	}
	updated.Description = req.Description
	if req.IsRequired != nil {
		updated.IsRequired = *req.IsRequired
	}
	if req.InverseLinkRID != nil {
		updated.InverseLinkRID = *req.InverseLinkRID
	}
	if req.PropagateMarkings != nil {
		updated.PropagateMarkings = *req.PropagateMarkings
	}
	if req.TypeClasses != nil {
		if apiErr := validateLinkTypeClasses(*req.TypeClasses); apiErr != nil {
			apierror.WriteJSON(w, apiErr)
			return
		}
		updated.TypeClasses = normaliseLinkTypeClasses(*req.TypeClasses)
	}

	if apiErr := h.validateInverseLinkPair(r.Context(), &updated); apiErr != nil {
		apierror.WriteJSON(w, apiErr)
		return
	}

	// Branch overlay
	branch, ok := h.validateBranch(w, r)
	if !ok {
		return
	}
	if branch != nil {
		if err := h.writeBranchChange(r.Context(), branch.ID, "MODIFIED", "linkType", existing.RID, existing, &updated); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("WriteBranchChangeFailed", nil))
			return
		}
		httputil.WriteJSON(w, http.StatusOK, &updated)
		return
	}

	if err := h.repo.UpdateLinkType(r.Context(), &updated); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("LinkTypeNotFound", map[string]string{
				"linkTypeRid": linkTypeRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("UpdateLinkTypeFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, &updated)
}

// DeleteLinkType handles DELETE /api/admin/linkTypes/{linkTypeRid}.
func (h *OMSHandler) DeleteLinkType(w http.ResponseWriter, r *http.Request) {
	linkTypeRID := chi.URLParam(r, "linkTypeRid")

	// Branch overlay
	branch, ok := h.validateBranch(w, r)
	if !ok {
		return
	}
	if branch != nil {
		existing, err := h.repo.GetLinkType(r.Context(), linkTypeRID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				apierror.WriteJSON(w, apierror.NewNotFound("LinkTypeNotFound", map[string]string{
					"linkTypeRid": linkTypeRID,
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("GetLinkTypeFailed", nil))
			return
		}
		if err := h.writeBranchChange(r.Context(), branch.ID, "DELETED", "linkType", existing.RID, existing, nil); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("WriteBranchChangeFailed", nil))
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := h.repo.DeleteLinkType(r.Context(), linkTypeRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("LinkTypeNotFound", map[string]string{
				"linkTypeRid": linkTypeRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DeleteLinkTypeFailed", nil))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// DeleteActionType handles DELETE /api/admin/actionTypes/{actionTypeRid}.
func (h *OMSHandler) DeleteActionType(w http.ResponseWriter, r *http.Request) {
	actionTypeRID := chi.URLParam(r, "actionTypeRid")

	// Branch overlay
	branch, ok := h.validateBranch(w, r)
	if !ok {
		return
	}
	if branch != nil {
		existing, err := h.repo.GetActionType(r.Context(), actionTypeRID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				apierror.WriteJSON(w, apierror.NewNotFound("ActionTypeNotFound", map[string]string{
					"actionTypeRid": actionTypeRID,
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("GetActionTypeFailed", nil))
			return
		}
		if err := h.writeBranchChange(r.Context(), branch.ID, "DELETED", "actionType", existing.RID, existing, nil); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("WriteBranchChangeFailed", nil))
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := h.repo.DeleteActionType(r.Context(), actionTypeRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ActionTypeNotFound", map[string]string{
				"actionTypeRid": actionTypeRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DeleteActionTypeFailed", nil))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListAllLinkTypes handles GET /api/admin/ontologies/{ontologyApiName}/linkTypes.
func (h *OMSHandler) ListAllLinkTypes(w http.ResponseWriter, r *http.Request) {
	ontologyAPIName := chi.URLParam(r, "ontologyApiName")

	linkTypes, err := h.repo.ListLinkTypes(r.Context(), ontologyAPIName)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListLinkTypesFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": linkTypes,
	})
}

// ListSharedPropertyTypesV2 handles
// GET /api/v2/ontologies/{ontologyApiName}/sharedPropertyTypes.
//
// Foundry OSv2 1:1 alignment: the SharedProperty repo surface
// (CreateSharedProperty / GetSharedProperty / ListSharedProperties /
// UpdateSharedProperty / DeleteSharedProperty) has been fully wired
// inside the OMS for several rounds, but the V2 read API never
// exposed it — SDKs that wanted to discover shared property types
// had to parse the bulky /fullMetadata response. This restores
// access at the canonical Foundry path. Envelope is {"data":[...]}
// to match ListLinkTypesForOntologyAdmin / ListInterfacesForOntologyAdmin;
// the list MUST serialize as `[]` rather than `null` so SDK iterators
// don't NPE on an empty ontology.
func (h *OMSHandler) ListSharedPropertyTypesV2(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r)
	if !ok {
		return
	}
	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	list, err := repo.ListSharedProperties(r.Context(), ontologyAPIName)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListSharedPropertyTypesFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if list == nil {
		list = []SharedProperty{}
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": list})
}

// GetSharedPropertyTypeByAPIName handles
// GET /api/v2/ontologies/{ontologyApiName}/sharedPropertyTypes/{sharedPropertyType}.
//
// Foundry-1:1 sibling of ListSharedPropertyTypesV2 — keyed by API
// name (not RID). The repo does not yet expose a native
// GetSharedPropertyByAPIName helper (unlike LinkType / ObjectType /
// ActionType / Interface / ValueType / QueryType), so this scans
// ListSharedProperties and filters; ontology-scoped sets are small
// in practice (< a few hundred entries even in large deployments)
// and the SDK-side cache absorbs repeat calls. A future round can
// promote this to a repo method when measurements show it matters.
func (h *OMSHandler) GetSharedPropertyTypeByAPIName(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r)
	if !ok {
		return
	}
	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	apiName := chi.URLParam(r, "sharedPropertyType")
	if apiName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingSharedPropertyType", map[string]string{
			"reason": "sharedPropertyType path parameter is required",
		}))
		return
	}
	if rejectVersionedRID(w, apiName, "sharedPropertyType", map[string]string{
		"ontologyApiName": ontologyAPIName,
	}) {
		return
	}
	list, err := repo.ListSharedProperties(r.Context(), ontologyAPIName)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("GetSharedPropertyTypeFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	for i := range list {
		if list[i].APIName == apiName {
			httputil.WriteJSON(w, http.StatusOK, list[i])
			return
		}
	}
	apierror.WriteJSON(w, apierror.NewNotFound("SharedPropertyTypeNotFound", map[string]string{
		"ontology":           ontologyAPIName,
		"sharedPropertyType": apiName,
	}))
}

// ListTypeGroupsForObjectTypeV2 handles
// GET /api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/typeGroups.
//
// Foundry-1:1 reverse-lookup: given an ObjectType API name, return
// every TypeGroup the ObjectType is assigned to. This is the
// V2/api-name sibling of the legacy admin RID-keyed endpoint
// (/api/admin/objectTypes/{objectTypeRid}/groups, registered as
// ListTypeGroupsForObjectType). SDKs and the SPA's ObjectType
// detail card use it to render category-aware chips without
// pulling /fullMetadata.
//
// Envelope is {"data":[...]} and NEVER null — an ObjectType with
// zero assignments returns {"data":[]} so SDK iterators don't NPE.
// Unknown ObjectType API name surfaces as 404 ObjectTypeNotFound,
// mirroring GetObjectType's error contract.
func (h *OMSHandler) ListTypeGroupsForObjectTypeV2(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r)
	if !ok {
		return
	}
	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	objectTypeAPIName := chi.URLParam(r, "objectTypeApiName")
	if objectTypeAPIName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingObjectType", map[string]string{
			"reason": "objectTypeAPIName path parameter is required",
		}))
		return
	}
	ot, err := repo.GetObjectTypeByAPIName(r.Context(), ontologyAPIName, objectTypeAPIName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
				"ontology":   ontologyAPIName,
				"objectType": objectTypeAPIName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetObjectTypeFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	list, err := repo.ListTypeGroupsForObjectType(r.Context(), ot.RID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListTypeGroupsForObjectTypeFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if list == nil {
		list = []TypeGroup{}
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": list})
}

// ListTypeGroupsV2 handles
// GET /api/v2/ontologies/{ontologyApiName}/typeGroups.
//
// Foundry OSv2 1:1 alignment: TypeGroup repo CRUD has been wired for
// many rounds but the V2 read API exposed NONE of it — TypeGroups
// were visible only by parsing /fullMetadata. Restores the canonical
// Foundry path with the {"data":[...]} envelope used by sibling list
// endpoints. Empty ontology returns `[]` rather than `null` so SDK
// iterators don't NPE.
func (h *OMSHandler) ListTypeGroupsV2(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r)
	if !ok {
		return
	}
	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	list, err := repo.ListTypeGroups(r.Context(), ontologyAPIName)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListTypeGroupsFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if list == nil {
		list = []TypeGroup{}
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": list})
}

// GetTypeGroupByAPIName handles
// GET /api/v2/ontologies/{ontologyApiName}/typeGroups/{typeGroup}.
//
// Foundry-1:1 sibling of ListTypeGroupsV2 — keyed by API name. As
// with round-8's sharedPropertyTypes handler, the repo has no native
// GetTypeGroupByAPIName helper, so this scans ListTypeGroups and
// filters; ontology-scoped sets are small in practice.
func (h *OMSHandler) GetTypeGroupByAPIName(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r)
	if !ok {
		return
	}
	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	apiName := chi.URLParam(r, "typeGroup")
	if apiName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingTypeGroup", map[string]string{
			"reason": "typeGroup path parameter is required",
		}))
		return
	}
	if rejectVersionedRID(w, apiName, "typeGroup", map[string]string{
		"ontologyApiName": ontologyAPIName,
	}) {
		return
	}
	list, err := repo.ListTypeGroups(r.Context(), ontologyAPIName)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("GetTypeGroupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	for i := range list {
		if list[i].APIName == apiName {
			httputil.WriteJSON(w, http.StatusOK, list[i])
			return
		}
	}
	apierror.WriteJSON(w, apierror.NewNotFound("TypeGroupNotFound", map[string]string{
		"ontology":  ontologyAPIName,
		"typeGroup": apiName,
	}))
}

// GetLinkTypeByAPIName handles
// GET /api/v2/ontologies/{ontologyApiName}/linkTypes/{linkType}.
//
// Foundry OSv2 1:1 alignment: SDKs hit this endpoint after a search
// response surfaces a linkType api name they need to render — without
// it, callers fall back to ListLinkTypes + client-side filter on every
// call. {linkType} is the API name (not the RID); pass the bare slug,
// not `ri.ontology.main.link-type.uuid`. RID-keyed mutations remain on
// the existing /linkTypes/byRid/{linkTypeRid} routes; this is the
// Foundry-shape read-only sibling that mirrors GET
// /objectTypes/{objectTypeApiName}.
func (h *OMSHandler) GetLinkTypeByAPIName(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r)
	if !ok {
		return
	}
	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	linkTypeAPIName := chi.URLParam(r, "linkType")
	if linkTypeAPIName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingLinkType", map[string]string{
			"reason": "linkType path parameter is required",
		}))
		return
	}
	if rejectVersionedRID(w, linkTypeAPIName, "linkType", map[string]string{
		"ontologyApiName": ontologyAPIName,
	}) {
		return
	}
	lt, err := repo.GetLinkTypeByAPIName(r.Context(), ontologyAPIName, linkTypeAPIName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("LinkTypeNotFound", map[string]string{
				"ontology": ontologyAPIName,
				"linkType": linkTypeAPIName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetLinkTypeFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, lt)
}

// ListLinkTypesForOntologyAdmin handles
// GET /api/v2/ontologies/{ontologyApiName}/linkTypes.
// Returns all LinkTypes for the ontology, used by the Ontology Manager visual
// link-type editor. Supports `?branch=` overlay for reads.
func (h *OMSHandler) ListLinkTypesForOntologyAdmin(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r)
	if !ok {
		return
	}
	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	list, err := repo.ListLinkTypes(r.Context(), ontologyAPIName)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListLinkTypesFailed", nil))
		return
	}
	if list == nil {
		list = []LinkType{}
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": list})
}

// ListActionTypesForOntologyAdmin handles
// GET /api/v2/ontologies/{ontologyApiName}/actionTypesAdmin.
// Returns all ActionTypes for the ontology including the internal rules /
// submissionCriteria / sideEffects payloads, for the Ontology Manager visual
// action-type builder. Supports `?branch=` overlay for reads.
func (h *OMSHandler) ListActionTypesForOntologyAdmin(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r)
	if !ok {
		return
	}
	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	list, err := repo.ListActionTypes(r.Context(), ontologyAPIName)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListActionTypesFailed", nil))
		return
	}
	wireList := make([]json.RawMessage, 0, len(list))
	for i := range list {
		data, err := list[i].ToFullMetadataJSON()
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("SerializationFailed", nil))
			return
		}
		wireList = append(wireList, json.RawMessage(data))
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": wireList})
}

// ListInterfacesForOntologyAdmin handles
// GET /api/v2/ontologies/{ontologyApiName}/interfacesAdmin.
// Returns all Interfaces for the ontology for the Ontology Manager visual
// interface editor. Supports `?branch=` overlay for reads.
func (h *OMSHandler) ListInterfacesForOntologyAdmin(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r)
	if !ok {
		return
	}
	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	list, err := repo.ListInterfaces(r.Context(), ontologyAPIName)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListInterfacesFailed", nil))
		return
	}
	if list == nil {
		list = []Interface{}
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": list})
}

// --- Interface request structs ---

// CreateInterfaceRequest is the request body for creating an interface.
type CreateInterfaceRequest struct {
	APIName           string          `json:"apiName"`
	DisplayName       string          `json:"displayName"`
	Description       string          `json:"description,omitempty"`
	ExtendsRID        string          `json:"extendsRid,omitempty"`
	SharedProperties  json.RawMessage `json:"sharedProperties,omitempty"`
	OutgoingLinkTypes json.RawMessage `json:"outgoingLinkTypes,omitempty"`
}

// UpdateInterfaceRequest is the request body for updating an interface.
type UpdateInterfaceRequest struct {
	DisplayName       string          `json:"displayName"`
	ExtendsRID        string          `json:"extendsRid,omitempty"`
	SharedProperties  json.RawMessage `json:"sharedProperties,omitempty"`
	OutgoingLinkTypes json.RawMessage `json:"outgoingLinkTypes,omitempty"`
}

// AttachInterfaceRequest is the request body for attaching an interface to an object type.
type AttachInterfaceRequest struct {
	InterfaceRID    string          `json:"interfaceRid"`
	PropertyMapping json.RawMessage `json:"propertyMapping,omitempty"`
}

// --- Interface handlers ---

// CreateInterface handles POST /api/admin/ontologies/{ontologyApiName}/interfaces.
func (h *OMSHandler) CreateInterface(w http.ResponseWriter, r *http.Request) {
	ontologyRID, err := h.resolveOntologyRID(r.Context(), chi.URLParam(r, "ontologyApiName"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
				"ontologyApiName": chi.URLParam(r, "ontologyApiName"),
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ResolveOntologyFailed", nil))
		return
	}

	var req CreateInterfaceRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	if req.APIName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:apiName", map[string]string{
			"parameter": "apiName",
			"reason":    "apiName is required",
		}))
		return
	}
	if req.DisplayName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:displayName", map[string]string{
			"parameter": "displayName",
			"reason":    "displayName is required",
		}))
		return
	}

	iface := &Interface{
		RID:               rid.NewInterfaceRID(),
		OntologyRID:       ontologyRID,
		APIName:           req.APIName,
		DisplayName:       req.DisplayName,
		ExtendsRID:        req.ExtendsRID,
		SharedProperties:  req.SharedProperties,
		OutgoingLinkTypes: req.OutgoingLinkTypes,
	}

	if err := h.repo.CreateInterface(r.Context(), iface); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("InterfaceAlreadyExists", map[string]string{
				"apiName": req.APIName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CreateInterfaceFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, iface)
}

// ListInterfaces handles GET /api/admin/ontologies/{ontologyApiName}/interfaces.
func (h *OMSHandler) ListInterfaces(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")

	list, err := h.repo.ListInterfaces(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListInterfacesFailed", nil))
		return
	}

	if list == nil {
		list = []Interface{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": list,
	})
}

// GetInterface handles GET /api/admin/interfaces/{interfaceRid}.
func (h *OMSHandler) GetInterface(w http.ResponseWriter, r *http.Request) {
	interfaceRID := chi.URLParam(r, "interfaceRid")

	iface, err := h.repo.GetInterface(r.Context(), interfaceRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("InterfaceNotFound", map[string]string{
				"interfaceRid": interfaceRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetInterfaceFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, iface)
}

// UpdateInterface handles PUT /api/admin/interfaces/{interfaceRid}.
func (h *OMSHandler) UpdateInterface(w http.ResponseWriter, r *http.Request) {
	interfaceRID := chi.URLParam(r, "interfaceRid")

	var req UpdateInterfaceRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	existing, err := h.repo.GetInterface(r.Context(), interfaceRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("InterfaceNotFound", map[string]string{
				"interfaceRid": interfaceRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetInterfaceFailed", nil))
		return
	}

	existing.DisplayName = req.DisplayName
	existing.ExtendsRID = req.ExtendsRID
	existing.SharedProperties = req.SharedProperties
	existing.OutgoingLinkTypes = req.OutgoingLinkTypes

	if err := h.repo.UpdateInterface(r.Context(), existing); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("InterfaceNotFound", map[string]string{
				"interfaceRid": interfaceRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("UpdateInterfaceFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, existing)
}

// DeleteInterface handles DELETE /api/admin/interfaces/{interfaceRid}.
func (h *OMSHandler) DeleteInterface(w http.ResponseWriter, r *http.Request) {
	interfaceRID := chi.URLParam(r, "interfaceRid")

	if err := h.repo.DeleteInterface(r.Context(), interfaceRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("InterfaceNotFound", map[string]string{
				"interfaceRid": interfaceRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DeleteInterfaceFailed", nil))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// AttachInterfaceHandler handles POST /api/admin/objectTypes/{objectTypeRid}/interfaces.
func (h *OMSHandler) AttachInterfaceHandler(w http.ResponseWriter, r *http.Request) {
	objectTypeRID := chi.URLParam(r, "objectTypeRid")

	var req AttachInterfaceRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	if req.InterfaceRID == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:interfaceRid", map[string]string{
			"parameter": "interfaceRid",
			"reason":    "interfaceRid is required",
		}))
		return
	}

	oti := &ObjectTypeInterface{
		ObjectTypeRID:   objectTypeRID,
		InterfaceRID:    req.InterfaceRID,
		PropertyMapping: req.PropertyMapping,
	}

	if err := h.repo.AttachInterface(r.Context(), oti); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("InterfaceAlreadyAttached", map[string]string{
				"interfaceRid": req.InterfaceRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("AttachInterfaceFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, oti)
}

// DetachInterface handles DELETE /api/admin/objectTypes/{objectTypeRid}/interfaces/{interfaceRid}.
func (h *OMSHandler) DetachInterface(w http.ResponseWriter, r *http.Request) {
	objectTypeRID := chi.URLParam(r, "objectTypeRid")
	interfaceRID := chi.URLParam(r, "interfaceRid")

	if err := h.repo.DetachInterface(r.Context(), objectTypeRID, interfaceRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("InterfaceNotAttached", map[string]string{
				"objectTypeRid": objectTypeRID,
				"interfaceRid":  interfaceRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DetachInterfaceFailed", nil))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListInterfaceObjectTypesV2 handles GET
// /api/v2/ontologies/{ontologyApiName}/interfaces/{interfaceRid}/objectTypes.
// Round 75: closes the m:n symmetry gap. The reverse direction
// (ListObjectTypeInterfaces — "which interfaces does X implement")
// was already wired; this surfaces the forward direction ("which
// ObjectTypes implement Y") so the Interface admin UI panel can
// render the implementor list without scanning every ObjectType
// and calling the reverse direction N times.
//
// Filter-not-key semantics: an unknown interfaceRid returns 200 +
// {data: []} rather than 404. Matches rounds 68/69/73's
// "render-cleanly-against-brand-new-entities" rule for SPA list
// panels.
func (h *OMSHandler) ListInterfaceObjectTypesV2(w http.ResponseWriter, r *http.Request) {
	interfaceRID := chi.URLParam(r, "interfaceRid")

	list, err := h.repo.ListInterfaceObjectTypes(r.Context(), interfaceRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListInterfaceObjectTypesFailed", nil))
		return
	}
	if list == nil {
		list = []ObjectType{}
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": list,
	})
}

// ListObjectTypeInterfaces handles GET /api/admin/objectTypes/{objectTypeRid}/interfaces.
func (h *OMSHandler) ListObjectTypeInterfaces(w http.ResponseWriter, r *http.Request) {
	objectTypeRID := chi.URLParam(r, "objectTypeRid")

	list, err := h.repo.ListObjectTypeInterfaces(r.Context(), objectTypeRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListObjectTypeInterfacesFailed", nil))
		return
	}

	if list == nil {
		list = []ObjectTypeInterface{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": list,
	})
}

// --- Shared Property request structs ---

// CreateSharedPropertyRequest is the request body for creating a shared property.
type CreateSharedPropertyRequest struct {
	APIName     string          `json:"apiName"`
	DisplayName string          `json:"displayName,omitempty"`
	Description string          `json:"description,omitempty"`
	BaseType    string          `json:"baseType"`
	TypeConfig  json.RawMessage `json:"typeConfig,omitempty"`
	IsArray     bool            `json:"isArray"`
}

// UpdateSharedPropertyRequest is the request body for updating a shared property.
type UpdateSharedPropertyRequest struct {
	DisplayName string          `json:"displayName,omitempty"`
	Description string          `json:"description,omitempty"`
	BaseType    string          `json:"baseType"`
	TypeConfig  json.RawMessage `json:"typeConfig,omitempty"`
	IsArray     bool            `json:"isArray"`
}

// --- Shared Property handlers ---

// CreateSharedProperty handles POST /api/admin/ontologies/{ontologyApiName}/shared-properties.
func (h *OMSHandler) CreateSharedProperty(w http.ResponseWriter, r *http.Request) {
	ontologyRID, err := h.resolveOntologyRID(r.Context(), chi.URLParam(r, "ontologyApiName"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
				"ontologyApiName": chi.URLParam(r, "ontologyApiName"),
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ResolveOntologyFailed", nil))
		return
	}

	var req CreateSharedPropertyRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	if req.APIName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:apiName", map[string]string{
			"parameter": "apiName",
			"reason":    "apiName is required",
		}))
		return
	}
	if req.BaseType == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:baseType", map[string]string{
			"parameter": "baseType",
			"reason":    "baseType is required",
		}))
		return
	}

	sp := &SharedProperty{
		RID:         rid.NewSharedPropertyRID(),
		OntologyRID: ontologyRID,
		APIName:     req.APIName,
		DisplayName: req.DisplayName,
		Description: req.Description,
		BaseType:    req.BaseType,
		TypeConfig:  req.TypeConfig,
		IsArray:     req.IsArray,
	}

	if err := h.repo.CreateSharedProperty(r.Context(), sp); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("SharedPropertyAlreadyExists", map[string]string{
				"apiName": req.APIName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CreateSharedPropertyFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, sp)
}

// ListSharedProperties handles GET /api/admin/ontologies/{ontologyApiName}/shared-properties.
func (h *OMSHandler) ListSharedProperties(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")

	list, err := h.repo.ListSharedProperties(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListSharedPropertiesFailed", nil))
		return
	}

	if list == nil {
		list = []SharedProperty{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": list,
	})
}

// GetSharedProperty handles GET /api/admin/shared-properties/{sharedPropertyRid}.
func (h *OMSHandler) GetSharedProperty(w http.ResponseWriter, r *http.Request) {
	spRID := chi.URLParam(r, "sharedPropertyRid")

	sp, err := h.repo.GetSharedProperty(r.Context(), spRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("SharedPropertyNotFound", map[string]string{
				"sharedPropertyRid": spRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetSharedPropertyFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, sp)
}

// UpdateSharedProperty handles PUT /api/admin/shared-properties/{sharedPropertyRid}.
func (h *OMSHandler) UpdateSharedProperty(w http.ResponseWriter, r *http.Request) {
	spRID := chi.URLParam(r, "sharedPropertyRid")

	var req UpdateSharedPropertyRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	existing, err := h.repo.GetSharedProperty(r.Context(), spRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("SharedPropertyNotFound", map[string]string{
				"sharedPropertyRid": spRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetSharedPropertyFailed", nil))
		return
	}

	existing.DisplayName = req.DisplayName
	existing.Description = req.Description
	existing.BaseType = req.BaseType
	existing.TypeConfig = req.TypeConfig
	existing.IsArray = req.IsArray

	if err := h.repo.UpdateSharedProperty(r.Context(), existing); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("SharedPropertyNotFound", map[string]string{
				"sharedPropertyRid": spRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("UpdateSharedPropertyFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, existing)
}

// DeleteSharedProperty handles DELETE /api/admin/shared-properties/{sharedPropertyRid}.
//
// Round 54: refuses 409 SharedPropertyInUse when downstream Properties
// still reference the SharedProperty (Foundry parity — silently leaving
// orphaned SharedPropertyRID strings is the worst failure mode because
// loads succeed but the references no longer resolve to anything).
// Admins are expected to clear consumers before retrying; a future
// round may add a "?cascade=true" override that detaches the
// SharedPropertyRID on consumers in the same transaction.
func (h *OMSHandler) DeleteSharedProperty(w http.ResponseWriter, r *http.Request) {
	spRID := chi.URLParam(r, "sharedPropertyRid")

	count, err := h.repo.CountPropertiesUsingSharedProperty(r.Context(), spRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("DeleteSharedPropertyFailed", nil))
		return
	}
	if count > 0 {
		apierror.WriteJSON(w, apierror.NewConflict("SharedPropertyInUse", map[string]string{
			"sharedPropertyRid": spRID,
			"usageCount":        strconv.Itoa(count),
		}))
		return
	}

	if err := h.repo.DeleteSharedProperty(r.Context(), spRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("SharedPropertyNotFound", map[string]string{
				"sharedPropertyRid": spRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DeleteSharedPropertyFailed", nil))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Type Group request structs ---

// CreateTypeGroupRequest is the request body for creating a type group.
type CreateTypeGroupRequest struct {
	APIName     string `json:"apiName"`
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
	Color       string `json:"color,omitempty"`
}

// UpdateTypeGroupRequest is the request body for updating a type group.
type UpdateTypeGroupRequest struct {
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
	Color       string `json:"color,omitempty"`
}

// --- Type Group handlers ---

// CreateTypeGroup handles POST /api/admin/ontologies/{ontologyApiName}/type-groups.
func (h *OMSHandler) CreateTypeGroup(w http.ResponseWriter, r *http.Request) {
	ontologyRID, err := h.resolveOntologyRID(r.Context(), chi.URLParam(r, "ontologyApiName"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
				"ontologyApiName": chi.URLParam(r, "ontologyApiName"),
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ResolveOntologyFailed", nil))
		return
	}

	var req CreateTypeGroupRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	if req.APIName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:apiName", map[string]string{
			"parameter": "apiName",
			"reason":    "apiName is required",
		}))
		return
	}
	if req.DisplayName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:displayName", map[string]string{
			"parameter": "displayName",
			"reason":    "displayName is required",
		}))
		return
	}

	tg := &TypeGroup{
		RID:         rid.NewTypeGroupRID(),
		OntologyRID: ontologyRID,
		APIName:     req.APIName,
		DisplayName: req.DisplayName,
		Description: req.Description,
		Color:       req.Color,
	}

	if err := h.repo.CreateTypeGroup(r.Context(), tg); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("TypeGroupAlreadyExists", map[string]string{
				"apiName": req.APIName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CreateTypeGroupFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, tg)
}

// ListTypeGroups handles GET /api/admin/ontologies/{ontologyApiName}/type-groups.
func (h *OMSHandler) ListTypeGroups(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")

	list, err := h.repo.ListTypeGroups(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListTypeGroupsFailed", nil))
		return
	}

	if list == nil {
		list = []TypeGroup{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": list,
	})
}

// GetTypeGroup handles GET /api/admin/type-groups/{typeGroupRid}.
func (h *OMSHandler) GetTypeGroup(w http.ResponseWriter, r *http.Request) {
	tgRID := chi.URLParam(r, "typeGroupRid")

	tg, err := h.repo.GetTypeGroup(r.Context(), tgRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("TypeGroupNotFound", map[string]string{
				"typeGroupRid": tgRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetTypeGroupFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, tg)
}

// UpdateTypeGroup handles PUT /api/admin/type-groups/{typeGroupRid}.
func (h *OMSHandler) UpdateTypeGroup(w http.ResponseWriter, r *http.Request) {
	tgRID := chi.URLParam(r, "typeGroupRid")

	var req UpdateTypeGroupRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	existing, err := h.repo.GetTypeGroup(r.Context(), tgRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("TypeGroupNotFound", map[string]string{
				"typeGroupRid": tgRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetTypeGroupFailed", nil))
		return
	}

	existing.DisplayName = req.DisplayName
	existing.Description = req.Description
	existing.Color = req.Color

	if err := h.repo.UpdateTypeGroup(r.Context(), existing); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("TypeGroupNotFound", map[string]string{
				"typeGroupRid": tgRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("UpdateTypeGroupFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, existing)
}

// DeleteTypeGroup handles DELETE /api/admin/type-groups/{typeGroupRid}.
//
// Round 58: refuses 409 TypeGroupInUse when ObjectTypes are still
// assigned via the object_type_groups join table. Mirror of round-
// 54 SharedProperty guard — same dangling-reference failure mode
// (assignment rows resolve to a non-existent typeGroupRid),
// same Foundry parity contract.
func (h *OMSHandler) DeleteTypeGroup(w http.ResponseWriter, r *http.Request) {
	tgRID := chi.URLParam(r, "typeGroupRid")

	count, err := h.repo.CountObjectTypesInTypeGroup(r.Context(), tgRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("DeleteTypeGroupFailed", nil))
		return
	}
	if count > 0 {
		apierror.WriteJSON(w, apierror.NewConflict("TypeGroupInUse", map[string]string{
			"typeGroupRid": tgRID,
			"usageCount":   strconv.Itoa(count),
		}))
		return
	}

	if err := h.repo.DeleteTypeGroup(r.Context(), tgRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("TypeGroupNotFound", map[string]string{
				"typeGroupRid": tgRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DeleteTypeGroupFailed", nil))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// AssignTypeGroup handles POST /api/admin/objectTypes/{objectTypeRid}/groups/{typeGroupRid}.
func (h *OMSHandler) AssignTypeGroup(w http.ResponseWriter, r *http.Request) {
	objectTypeRID := chi.URLParam(r, "objectTypeRid")
	typeGroupRID := chi.URLParam(r, "typeGroupRid")

	if err := h.repo.AssignTypeGroup(r.Context(), objectTypeRID, typeGroupRID); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("TypeGroupAlreadyAssigned", map[string]string{
				"objectTypeRid": objectTypeRID,
				"typeGroupRid":  typeGroupRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("AssignTypeGroupFailed", nil))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RemoveTypeGroup handles DELETE /api/admin/objectTypes/{objectTypeRid}/groups/{typeGroupRid}.
func (h *OMSHandler) RemoveTypeGroup(w http.ResponseWriter, r *http.Request) {
	objectTypeRID := chi.URLParam(r, "objectTypeRid")
	typeGroupRID := chi.URLParam(r, "typeGroupRid")

	if err := h.repo.RemoveTypeGroup(r.Context(), objectTypeRID, typeGroupRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("TypeGroupNotAssigned", map[string]string{
				"objectTypeRid": objectTypeRID,
				"typeGroupRid":  typeGroupRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("RemoveTypeGroupFailed", nil))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListTypeGroupsForObjectType handles GET /api/admin/objectTypes/{objectTypeRid}/groups.
func (h *OMSHandler) ListTypeGroupsForObjectType(w http.ResponseWriter, r *http.Request) {
	objectTypeRID := chi.URLParam(r, "objectTypeRid")

	list, err := h.repo.ListTypeGroupsForObjectType(r.Context(), objectTypeRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListTypeGroupsForObjectTypeFailed", nil))
		return
	}

	if list == nil {
		list = []TypeGroup{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": list,
	})
}

// --- Value Type request structs ---

// CreateValueTypeRequest is the request body for creating a value type.
type CreateValueTypeRequest struct {
	APIName     string          `json:"apiName"`
	DisplayName string          `json:"displayName"`
	BaseType    string          `json:"baseType"`
	Constraints json.RawMessage `json:"constraints,omitempty"`
}

// UpdateValueTypeRequest is the request body for updating a value type.
type UpdateValueTypeRequest struct {
	DisplayName string          `json:"displayName"`
	BaseType    string          `json:"baseType"`
	Constraints json.RawMessage `json:"constraints,omitempty"`
	Version     int             `json:"version"`
}

// --- Value Type handlers ---

// CreateValueType handles POST /api/admin/value-types.
func (h *OMSHandler) CreateValueType(w http.ResponseWriter, r *http.Request) {
	var req CreateValueTypeRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	if req.APIName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:apiName", map[string]string{
			"parameter": "apiName",
			"reason":    "apiName is required",
		}))
		return
	}
	if req.DisplayName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:displayName", map[string]string{
			"parameter": "displayName",
			"reason":    "displayName is required",
		}))
		return
	}
	if req.BaseType == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:baseType", map[string]string{
			"parameter": "baseType",
			"reason":    "baseType is required",
		}))
		return
	}

	vt := &ValueType{
		RID:         rid.NewValueTypeRID(),
		APIName:     req.APIName,
		DisplayName: req.DisplayName,
		BaseType:    req.BaseType,
		Constraints: req.Constraints,
		Version:     1,
	}

	if err := h.repo.CreateValueType(r.Context(), vt); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("ValueTypeAlreadyExists", map[string]string{
				"apiName": req.APIName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CreateValueTypeFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, vt)
}

// ListValueTypes handles GET /api/admin/value-types.
func (h *OMSHandler) ListValueTypes(w http.ResponseWriter, r *http.Request) {
	list, err := h.repo.ListValueTypes(r.Context())
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListValueTypesFailed", nil))
		return
	}

	if list == nil {
		list = []ValueType{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": list,
	})
}

// GetValueType handles GET /api/admin/value-types/{valueTypeRid}.
func (h *OMSHandler) GetValueType(w http.ResponseWriter, r *http.Request) {
	vtRID := chi.URLParam(r, "valueTypeRid")

	vt, err := h.repo.GetValueType(r.Context(), vtRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ValueTypeNotFound", map[string]string{
				"valueTypeRid": vtRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetValueTypeFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, vt)
}

// UpdateValueType handles PUT /api/admin/value-types/{valueTypeRid}.
func (h *OMSHandler) UpdateValueType(w http.ResponseWriter, r *http.Request) {
	vtRID := chi.URLParam(r, "valueTypeRid")

	var req UpdateValueTypeRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	existing, err := h.repo.GetValueType(r.Context(), vtRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ValueTypeNotFound", map[string]string{
				"valueTypeRid": vtRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetValueTypeFailed", nil))
		return
	}

	existing.DisplayName = req.DisplayName
	existing.BaseType = req.BaseType
	existing.Constraints = req.Constraints
	if req.Version > 0 {
		existing.Version = req.Version
	}

	if err := h.repo.UpdateValueType(r.Context(), existing); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ValueTypeNotFound", map[string]string{
				"valueTypeRid": vtRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("UpdateValueTypeFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, existing)
}

// DeleteValueType handles DELETE /api/admin/value-types/{valueTypeRid}.
func (h *OMSHandler) DeleteValueType(w http.ResponseWriter, r *http.Request) {
	vtRID := chi.URLParam(r, "valueTypeRid")

	if err := h.repo.DeleteValueType(r.Context(), vtRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ValueTypeNotFound", map[string]string{
				"valueTypeRid": vtRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DeleteValueTypeFailed", nil))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListValueTypeUsages handles GET /api/v2/ontologies/{ontologyApiName}/valueTypes/byRid/{valueTypeRid}/usages.
// It resolves the ValueType by RID, then fans out to the repository to find
// every Property whose base_type references the ValueType's apiName. The
// response envelope mirrors other V2 list endpoints ({data: [...]}).
//
// ValueTypes are global, not ontology-scoped, but the route is mounted
// under /ontologies/... to keep URL conventions consistent with the rest
// of the admin surface. The ontologyAPIName URL segment is intentionally
// not used for filtering — a single ValueType may be referenced by
// Properties on ObjectTypes belonging to any ontology, and the admin view
// must surface all of them.
func (h *OMSHandler) ListValueTypeUsages(w http.ResponseWriter, r *http.Request) {
	vtRID := chi.URLParam(r, "valueTypeRid")

	vt, err := h.repo.GetValueType(r.Context(), vtRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ValueTypeNotFound", map[string]string{
				"valueTypeRid": vtRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetValueTypeFailed", nil))
		return
	}

	usages, err := h.repo.ListPropertyUsagesByBaseType(r.Context(), vt.APIName)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListValueTypeUsagesFailed", nil))
		return
	}
	if usages == nil {
		usages = []PropertyUsage{}
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": usages})
}

// ListActionLogs handles GET /api/admin/actionTypes/{actionTypeRid}/logs.
func (h *OMSHandler) ListActionLogs(w http.ResponseWriter, r *http.Request) {
	actionTypeRID := chi.URLParam(r, "actionTypeRid")

	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 500 {
				n = 500
			}
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	logs, err := h.repo.ListActionLogs(r.Context(), actionTypeRID, limit, offset)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListActionLogsFailed", nil))
		return
	}
	if logs == nil {
		logs = []ActionLog{}
	}

	total, err := h.repo.CountActionLogs(r.Context(), actionTypeRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("CountActionLogsFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data":  logs,
		"total": total,
	})
}

// --- Search handler ---

// SearchOntologyResources handles GET /api/admin/ontologies/{ontologyApiName}/search?q=xxx.
func (h *OMSHandler) SearchOntologyResources(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	query := r.URL.Query().Get("q")
	if query == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:q", map[string]string{
			"parameter": "q",
			"reason":    "search query is required",
		}))
		return
	}

	results, err := h.repo.SearchOntologyResources(r.Context(), ontologyRID, query)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("SearchFailed", nil))
		return
	}
	if results == nil {
		results = []SearchResult{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": results,
	})
}

// --- Export handler ---

// ExportOntology handles GET /api/admin/ontologies/{ontologyApiName}/export.
func (h *OMSHandler) ExportOntology(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")

	ontology, err := h.repo.GetOntology(r.Context(), ontologyRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
				"ontologyApiName": ontologyRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetOntologyFailed", nil))
		return
	}

	objectTypes, err := h.repo.ListObjectTypes(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListObjectTypesFailed", nil))
		return
	}
	// Load properties for each object type
	for i := range objectTypes {
		props, err := h.repo.ListProperties(r.Context(), objectTypes[i].RID)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("ListPropertiesFailed", nil))
			return
		}
		objectTypes[i].Properties = props
	}

	linkTypes, err := h.repo.ListLinkTypes(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListLinkTypesFailed", nil))
		return
	}

	actionTypes, err := h.repo.ListActionTypes(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListActionTypesFailed", nil))
		return
	}

	interfaces, err := h.repo.ListInterfaces(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListInterfacesFailed", nil))
		return
	}

	if objectTypes == nil {
		objectTypes = []ObjectType{}
	}
	if linkTypes == nil {
		linkTypes = []LinkType{}
	}
	if actionTypes == nil {
		actionTypes = []ActionType{}
	}
	if interfaces == nil {
		interfaces = []Interface{}
	}

	export := OntologyExport{
		Ontology:    *ontology,
		ObjectTypes: objectTypes,
		LinkTypes:   linkTypes,
		ActionTypes: actionTypes,
		Interfaces:  interfaces,
	}

	httputil.WriteJSON(w, http.StatusOK, export)
}

// --- Import handler ---

// ImportOntology handles POST /api/admin/ontologies/import.
func (h *OMSHandler) ImportOntology(w http.ResponseWriter, r *http.Request) {
	var export OntologyExport
	if err := httputil.ReadJSON(r, &export); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	if export.Ontology.APIName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:apiName", map[string]string{
			"parameter": "ontology.apiName",
			"reason":    "ontology apiName is required",
		}))
		return
	}

	// Create ontology with new RID
	ontology := &Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     export.Ontology.APIName,
		DisplayName: export.Ontology.DisplayName,
		Description: export.Ontology.Description,
	}
	if err := h.repo.CreateOntology(r.Context(), ontology); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("OntologyAlreadyExists", map[string]string{
				"apiName": ontology.APIName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CreateOntologyFailed", nil))
		return
	}

	// Map old RIDs to new RIDs for object types (needed for link types and
	// US-212 inheritance: ExtendsRID may point at any other ObjectType in the
	// export, including one that appears later in the slice). Pre-allocate
	// new RIDs so the parent pointer can be remapped regardless of order.
	otRIDMap := make(map[string]string, len(export.ObjectTypes))
	for _, ot := range export.ObjectTypes {
		otRIDMap[ot.RID] = rid.NewObjectTypeRID()
	}

	// Create object types
	for _, ot := range export.ObjectTypes {
		oldRID := ot.RID
		extendsRID := ""
		if ot.ExtendsRID != "" {
			if mapped, ok := otRIDMap[ot.ExtendsRID]; ok {
				extendsRID = mapped
			} else {
				extendsRID = ot.ExtendsRID
			}
		}
		newOT := &ObjectType{
			RID:               otRIDMap[oldRID],
			OntologyRID:       ontology.RID,
			APIName:           ot.APIName,
			DisplayName:       ot.DisplayName,
			PluralDisplayName: ot.PluralDisplayName,
			Description:       ot.Description,
			PrimaryKey:        ot.PrimaryKey,
			PrimaryKeys:       ot.EffectivePrimaryKeys(),
			TitleProperty:     ot.TitleProperty,
			Status:            ot.Status,
			Visibility:        ot.Visibility,
			IconName:          ot.IconName,
			Color:             ot.Color,
			ExtendsRID:        extendsRID,
		}
		if newOT.Status == "" {
			newOT.Status = "ACTIVE"
		}
		if newOT.Visibility == "" {
			newOT.Visibility = "NORMAL"
		}
		if err := h.repo.CreateObjectType(r.Context(), newOT); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("CreateObjectTypeFailed", map[string]string{
				"apiName": ot.APIName,
				"reason":  err.Error(),
			}))
			return
		}

		// Create properties
		for _, p := range ot.Properties {
			newProp := &Property{
				RID:           rid.NewPropertyRID(),
				ObjectTypeRID: newOT.RID,
				APIName:       p.APIName,
				DisplayName:   p.DisplayName,
				Description:   p.Description,
				BaseType:      p.BaseType,
				TypeConfig:    p.TypeConfig,
				IsArray:       p.IsArray,
				IsNullable:    p.IsNullable,
				IsSearchable:  p.IsSearchable,
				IsSortable:    p.IsSortable,
				Status:        p.Status,
			}
			if newProp.Status == "" {
				newProp.Status = "ACTIVE"
			}
			if err := h.repo.CreateProperty(r.Context(), newProp); err != nil {
				apierror.WriteJSON(w, apierror.NewInternal("CreatePropertyFailed", map[string]string{
					"apiName": p.APIName,
					"reason":  err.Error(),
				}))
				return
			}
		}
	}

	// Create link types
	for _, lt := range export.LinkTypes {
		newLT := &LinkType{
			RID:         rid.NewLinkTypeRID(),
			OntologyRID: ontology.RID,
			APIName:     lt.APIName,
			DisplayName: lt.DisplayName,
			Description: lt.Description,
			SourceObjectType: func() string {
				if mapped, ok := otRIDMap[lt.SourceObjectType]; ok {
					return mapped
				}
				return lt.SourceObjectType
			}(),
			TargetObjectType: func() string {
				if mapped, ok := otRIDMap[lt.TargetObjectType]; ok {
					return mapped
				}
				return lt.TargetObjectType
			}(),
			Cardinality:      lt.Cardinality,
			ForeignKeyConfig: lt.ForeignKeyConfig,
			JoinTableConfig:  lt.JoinTableConfig,
			IsRequired:       lt.IsRequired,
		}
		if err := h.repo.CreateLinkType(r.Context(), newLT); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("CreateLinkTypeFailed", map[string]string{
				"apiName": lt.APIName,
				"reason":  err.Error(),
			}))
			return
		}
	}

	// Create action types
	for _, at := range export.ActionTypes {
		newAT := &ActionType{
			RID:         rid.NewActionTypeRID(),
			OntologyRID: ontology.RID,
			APIName:     at.APIName,
			DisplayName: at.DisplayName,
			Description: at.Description,
			Status:      at.Status,
			Parameters:  at.Parameters,
			Rules:       at.Rules,
		}
		if newAT.Status == "" {
			newAT.Status = "ACTIVE"
		}
		if err := h.repo.CreateActionType(r.Context(), newAT); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("CreateActionTypeFailed", map[string]string{
				"apiName": at.APIName,
				"reason":  err.Error(),
			}))
			return
		}
	}

	// Create interfaces
	for _, iface := range export.Interfaces {
		newIface := &Interface{
			RID:              rid.NewInterfaceRID(),
			OntologyRID:      ontology.RID,
			APIName:          iface.APIName,
			DisplayName:      iface.DisplayName,
			ExtendsRID:       iface.ExtendsRID,
			SharedProperties: iface.SharedProperties,
		}
		if err := h.repo.CreateInterface(r.Context(), newIface); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("CreateInterfaceFailed", map[string]string{
				"apiName": iface.APIName,
				"reason":  err.Error(),
			}))
			return
		}
	}

	httputil.WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"ontology": ontology,
		"message":  "import successful",
	})
}

// --- Security Policy request structs ---

// CreateSecurityPolicyRequest is the request body for creating a security policy.
type CreateSecurityPolicyRequest struct {
	PolicyType string          `json:"policyType"`
	Rules      json.RawMessage `json:"rules"`
}

// UpdateSecurityPolicyRequest is the request body for updating a security policy.
type UpdateSecurityPolicyRequest struct {
	PolicyType string          `json:"policyType"`
	Rules      json.RawMessage `json:"rules"`
}

// --- Security Policy handlers ---

// CreateSecurityPolicy handles POST /api/admin/objectTypes/{objectTypeRid}/securityPolicies.
func (h *OMSHandler) CreateSecurityPolicy(w http.ResponseWriter, r *http.Request) {
	objectTypeRID := chi.URLParam(r, "objectTypeRid")

	var req CreateSecurityPolicyRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	if req.PolicyType == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:policyType", map[string]string{
			"parameter": "policyType",
			"reason":    "policyType is required",
		}))
		return
	}
	if len(req.Rules) == 0 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:rules", map[string]string{
			"parameter": "rules",
			"reason":    "rules is required",
		}))
		return
	}

	sp := &SecurityPolicy{
		RID:           rid.NewSecurityPolicyRID(),
		ObjectTypeRID: objectTypeRID,
		PolicyType:    req.PolicyType,
		Rules:         req.Rules,
	}

	if err := h.repo.CreateSecurityPolicy(r.Context(), sp); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("CreateSecurityPolicyFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, sp)
}

// ListSecurityPolicies handles GET /api/admin/objectTypes/{objectTypeRid}/securityPolicies.
func (h *OMSHandler) ListSecurityPolicies(w http.ResponseWriter, r *http.Request) {
	objectTypeRID := chi.URLParam(r, "objectTypeRid")

	list, err := h.repo.ListSecurityPolicies(r.Context(), objectTypeRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListSecurityPoliciesFailed", nil))
		return
	}

	if list == nil {
		list = []SecurityPolicy{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": list,
	})
}

// GetSecurityPolicy handles GET /api/admin/securityPolicies/{securityPolicyRid}.
func (h *OMSHandler) GetSecurityPolicy(w http.ResponseWriter, r *http.Request) {
	spRID := chi.URLParam(r, "securityPolicyRid")

	sp, err := h.repo.GetSecurityPolicy(r.Context(), spRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("SecurityPolicyNotFound", map[string]string{
				"securityPolicyRid": spRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetSecurityPolicyFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, sp)
}

// UpdateSecurityPolicy handles PUT /api/admin/securityPolicies/{securityPolicyRid}.
func (h *OMSHandler) UpdateSecurityPolicy(w http.ResponseWriter, r *http.Request) {
	spRID := chi.URLParam(r, "securityPolicyRid")

	var req UpdateSecurityPolicyRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	existing, err := h.repo.GetSecurityPolicy(r.Context(), spRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("SecurityPolicyNotFound", map[string]string{
				"securityPolicyRid": spRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetSecurityPolicyFailed", nil))
		return
	}

	existing.PolicyType = req.PolicyType
	existing.Rules = req.Rules

	if err := h.repo.UpdateSecurityPolicy(r.Context(), existing); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("SecurityPolicyNotFound", map[string]string{
				"securityPolicyRid": spRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("UpdateSecurityPolicyFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, existing)
}

// DeleteSecurityPolicy handles DELETE /api/admin/securityPolicies/{securityPolicyRid}.
func (h *OMSHandler) DeleteSecurityPolicy(w http.ResponseWriter, r *http.Request) {
	spRID := chi.URLParam(r, "securityPolicyRid")

	if err := h.repo.DeleteSecurityPolicy(r.Context(), spRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("SecurityPolicyNotFound", map[string]string{
				"securityPolicyRid": spRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DeleteSecurityPolicyFailed", nil))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Snapshot handlers ---

// CreateSnapshotRequest is the request body for creating a snapshot.
type CreateSnapshotRequest struct {
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
}

// CreateSnapshot handles POST /api/admin/ontologies/{ontologyApiName}/snapshots.
func (h *OMSHandler) CreateSnapshot(w http.ResponseWriter, r *http.Request) {
	ontologyRID, err := h.resolveOntologyRID(r.Context(), chi.URLParam(r, "ontologyApiName"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
				"ontologyApiName": chi.URLParam(r, "ontologyApiName"),
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ResolveOntologyFailed", nil))
		return
	}

	var req CreateSnapshotRequest
	// Body is optional for snapshots
	_ = httputil.ReadJSON(r, &req)

	// Get ontology
	ontology, err := h.repo.GetOntology(r.Context(), ontologyRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
				"ontologyApiName": ontologyRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetOntologyFailed", nil))
		return
	}

	// List all object types with properties
	objectTypes, err := h.repo.ListObjectTypes(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListObjectTypesFailed", nil))
		return
	}
	for i := range objectTypes {
		props, err := h.repo.ListProperties(r.Context(), objectTypes[i].RID)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("ListPropertiesFailed", nil))
			return
		}
		objectTypes[i].Properties = props
	}

	// List link types
	linkTypes, err := h.repo.ListLinkTypes(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListLinkTypesFailed", nil))
		return
	}

	// List action types
	actionTypes, err := h.repo.ListActionTypes(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListActionTypesFailed", nil))
		return
	}

	// List interfaces
	interfaces, err := h.repo.ListInterfaces(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListInterfacesFailed", nil))
		return
	}

	if objectTypes == nil {
		objectTypes = []ObjectType{}
	}
	if linkTypes == nil {
		linkTypes = []LinkType{}
	}
	if actionTypes == nil {
		actionTypes = []ActionType{}
	}
	if interfaces == nil {
		interfaces = []Interface{}
	}

	// Serialize snapshot data
	export := OntologyExport{
		Ontology:    *ontology,
		ObjectTypes: objectTypes,
		LinkTypes:   linkTypes,
		ActionTypes: actionTypes,
		Interfaces:  interfaces,
	}
	snapshotData, err := json.Marshal(export)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("SerializationFailed", nil))
		return
	}

	// Increment version
	version, err := h.repo.IncrementOntologyVersion(r.Context(), ontologyRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
				"ontologyApiName": ontologyRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("IncrementVersionFailed", nil))
		return
	}

	// Store snapshot
	snapshot := &OntologySnapshot{
		OntologyRID: ontologyRID,
		Version:     version,
		Label:       req.Label,
		Description: req.Description,
		Snapshot:    snapshotData,
		CreatedBy:   "system",
	}
	if err := h.repo.CreateSnapshot(r.Context(), snapshot); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("CreateSnapshotFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, snapshot)
}

// ListSnapshots handles GET /api/admin/ontologies/{ontologyApiName}/snapshots.
func (h *OMSHandler) ListSnapshots(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")

	snapshots, err := h.repo.ListSnapshots(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListSnapshotsFailed", nil))
		return
	}
	if snapshots == nil {
		snapshots = []OntologySnapshot{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": snapshots,
	})
}

// GetSnapshot handles GET /api/admin/ontologies/{ontologyApiName}/snapshots/{version}.
func (h *OMSHandler) GetSnapshot(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	versionStr := chi.URLParam(r, "version")

	version, err := strconv.Atoi(versionStr)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:version", map[string]string{
			"parameter": "version",
			"reason":    "version must be an integer",
		}))
		return
	}

	snapshot, err := h.repo.GetSnapshot(r.Context(), ontologyRID, version)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("SnapshotNotFound", map[string]string{
				"ontologyRid": ontologyRID,
				"version":     versionStr,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetSnapshotFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, snapshot)
}

// --- Request structs for DatasourceBinding ---

// CreateDatasourceBindingRequest is the request body for creating a datasource binding.
type CreateDatasourceBindingRequest struct {
	DatasetRID    string          `json:"datasetRid"`
	Branch        string          `json:"branch,omitempty"`
	ColumnMapping json.RawMessage `json:"columnMapping"`
	IsPrimary     bool            `json:"isPrimary"`
}

// UpdateDatasourceBindingRequest is the request body for updating a datasource binding.
type UpdateDatasourceBindingRequest struct {
	DatasetRID    string          `json:"datasetRid"`
	Branch        string          `json:"branch,omitempty"`
	ColumnMapping json.RawMessage `json:"columnMapping"`
	IsPrimary     bool            `json:"isPrimary"`
}

// --- DatasourceBinding handlers ---

// CreateDatasourceBinding handles POST /api/admin/objectTypes/{objectTypeRid}/datasourceBindings.
func (h *OMSHandler) CreateDatasourceBinding(w http.ResponseWriter, r *http.Request) {
	objectTypeRID := chi.URLParam(r, "objectTypeRid")

	var req CreateDatasourceBindingRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	if req.DatasetRID == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:datasetRid", map[string]string{
			"parameter": "datasetRid",
			"reason":    "datasetRid is required",
		}))
		return
	}

	branch := req.Branch
	if branch == "" {
		branch = "main"
	}

	colMapping := req.ColumnMapping
	if len(colMapping) == 0 {
		colMapping = json.RawMessage(`{}`)
	}

	db := &DatasourceBinding{
		RID:           rid.NewDatasourceBindingRID(),
		ObjectTypeRID: objectTypeRID,
		DatasetRID:    req.DatasetRID,
		Branch:        branch,
		ColumnMapping: colMapping,
		IsPrimary:     req.IsPrimary,
	}

	if err := h.repo.CreateDatasourceBinding(r.Context(), db); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("CreateDatasourceBindingFailed", nil))
		return
	}

	// US-377: synchronously derive column-level lineage edges from the
	// new binding's column_mapping. Failures here are logged via the
	// returned error envelope but do not roll back the binding write —
	// the lineage view is a derived index, not authoritative state, and
	// a future binding update will re-derive it.
	h.deriveColumnLineageOnBindingChange(r.Context(), db)

	httputil.WriteJSON(w, http.StatusCreated, db)
}

// ListDatasourceBindings handles GET /api/admin/objectTypes/{objectTypeRid}/datasourceBindings.
func (h *OMSHandler) ListDatasourceBindings(w http.ResponseWriter, r *http.Request) {
	objectTypeRID := chi.URLParam(r, "objectTypeRid")

	list, err := h.repo.ListDatasourceBindings(r.Context(), objectTypeRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListDatasourceBindingsFailed", nil))
		return
	}

	if list == nil {
		list = []DatasourceBinding{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": list,
	})
}

// GetDatasourceBinding handles GET /api/admin/datasourceBindings/{datasourceBindingRid}.
func (h *OMSHandler) GetDatasourceBinding(w http.ResponseWriter, r *http.Request) {
	dbRID := chi.URLParam(r, "datasourceBindingRid")

	db, err := h.repo.GetDatasourceBinding(r.Context(), dbRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("DatasourceBindingNotFound", map[string]string{
				"datasourceBindingRid": dbRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetDatasourceBindingFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, db)
}

// UpdateDatasourceBinding handles PUT /api/admin/datasourceBindings/{datasourceBindingRid}.
func (h *OMSHandler) UpdateDatasourceBinding(w http.ResponseWriter, r *http.Request) {
	dbRID := chi.URLParam(r, "datasourceBindingRid")

	var req UpdateDatasourceBindingRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	existing, err := h.repo.GetDatasourceBinding(r.Context(), dbRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("DatasourceBindingNotFound", map[string]string{
				"datasourceBindingRid": dbRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetDatasourceBindingFailed", nil))
		return
	}

	if req.DatasetRID != "" {
		existing.DatasetRID = req.DatasetRID
	}
	if req.Branch != "" {
		existing.Branch = req.Branch
	}
	if len(req.ColumnMapping) > 0 {
		existing.ColumnMapping = req.ColumnMapping
	}
	existing.IsPrimary = req.IsPrimary

	if err := h.repo.UpdateDatasourceBinding(r.Context(), existing); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("DatasourceBindingNotFound", map[string]string{
				"datasourceBindingRid": dbRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("UpdateDatasourceBindingFailed", nil))
		return
	}

	// US-377: re-derive column-level lineage edges from the updated
	// column_mapping (Replace-by-binding semantics keep the edge set in
	// lockstep with the binding payload).
	h.deriveColumnLineageOnBindingChange(r.Context(), existing)

	httputil.WriteJSON(w, http.StatusOK, existing)
}

// DeleteDatasourceBinding handles DELETE /api/admin/datasourceBindings/{datasourceBindingRid}.
func (h *OMSHandler) DeleteDatasourceBinding(w http.ResponseWriter, r *http.Request) {
	dbRID := chi.URLParam(r, "datasourceBindingRid")

	if err := h.repo.DeleteDatasourceBinding(r.Context(), dbRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("DatasourceBindingNotFound", map[string]string{
				"datasourceBindingRid": dbRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DeleteDatasourceBindingFailed", nil))
		return
	}

	// US-377: cascade-clear derived column lineage edges so the lineage
	// view does not retain dangling pointers to the deleted binding.
	if h.columnLineageStore != nil {
		_, _ = h.columnLineageStore.DeleteColumnLineageForBinding(r.Context(), dbRID)
	}

	w.WriteHeader(http.StatusNoContent)
}

// deriveColumnLineageOnBindingChange recomputes column-level lineage
// edges from the binding's column_mapping and atomically replaces the
// edge set owned by the binding RID. A nil column lineage store
// short-circuits — degraded-mode bootstraps that have not wired the
// store skip derivation silently. Property-lookup or store failures are
// swallowed: the lineage view is a derived index that the next
// successful binding update will rebuild.
func (h *OMSHandler) deriveColumnLineageOnBindingChange(ctx context.Context, db *DatasourceBinding) {
	if h.columnLineageStore == nil || db == nil || db.RID == "" {
		return
	}
	props, err := h.repo.ListProperties(ctx, db.ObjectTypeRID)
	if err != nil {
		return
	}
	edges, err := DeriveColumnLineageEdges(db, props)
	if err != nil {
		return
	}
	_ = h.columnLineageStore.ReplaceColumnLineageForBinding(ctx, db.RID, edges)
}

// --- Request structs for QueryType ---

// CreateQueryTypeRequest is the request body for creating a query type.
type CreateQueryTypeRequest struct {
	APIName     string          `json:"apiName"`
	DisplayName string          `json:"displayName"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
	Output      json.RawMessage `json:"output"`
	Query       json.RawMessage `json:"query"`
	Status      string          `json:"status,omitempty"`
}

// UpdateQueryTypeRequest is the request body for updating a query type.
type UpdateQueryTypeRequest struct {
	DisplayName string          `json:"displayName"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Output      json.RawMessage `json:"output,omitempty"`
	Query       json.RawMessage `json:"query,omitempty"`
	Status      string          `json:"status,omitempty"`
}

// --- QueryType handlers ---

// CreateQueryType handles POST /api/v2/ontologies/{ontologyApiName}/queryTypes.
func (h *OMSHandler) CreateQueryType(w http.ResponseWriter, r *http.Request) {
	ontologyRID, err := h.resolveOntologyRID(r.Context(), chi.URLParam(r, "ontologyApiName"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
				"ontologyApiName": chi.URLParam(r, "ontologyApiName"),
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ResolveOntologyFailed", nil))
		return
	}

	var req CreateQueryTypeRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	if req.APIName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:apiName", map[string]string{
			"parameter": "apiName",
			"reason":    "apiName is required",
		}))
		return
	}
	if req.DisplayName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:displayName", map[string]string{
			"parameter": "displayName",
			"reason":    "displayName is required",
		}))
		return
	}

	status := req.Status
	if status == "" {
		status = "ACTIVE"
	}
	params := req.Parameters
	if len(params) == 0 {
		params = json.RawMessage(`[]`)
	}
	output := req.Output
	if len(output) == 0 {
		output = json.RawMessage(`{}`)
	}
	query := req.Query
	if len(query) == 0 {
		query = json.RawMessage(`{}`)
	}

	qt := &QueryType{
		RID:         rid.NewQueryTypeRID(),
		OntologyRID: ontologyRID,
		APIName:     req.APIName,
		DisplayName: req.DisplayName,
		Description: req.Description,
		Parameters:  params,
		Output:      output,
		Query:       query,
		Status:      status,
	}

	if err := h.repo.CreateQueryType(r.Context(), qt); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("QueryTypeAlreadyExists", map[string]string{
				"apiName": req.APIName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CreateQueryTypeFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, qt)
}

// ListQueryTypes handles GET /api/admin/ontologies/{ontologyApiName}/queryTypes.
func (h *OMSHandler) ListQueryTypes(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")

	list, err := h.repo.ListQueryTypes(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListQueryTypesFailed", nil))
		return
	}

	if list == nil {
		list = []QueryType{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": list,
	})
}

// GetQueryType handles GET /api/v2/ontologies/{ontologyApiName}/queryTypes/byRid/{queryTypeRid}.
func (h *OMSHandler) GetQueryType(w http.ResponseWriter, r *http.Request) {
	qtRID := chi.URLParam(r, "queryTypeRid")

	qt, err := h.repo.GetQueryType(r.Context(), qtRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("QueryTypeNotFound", map[string]string{
				"queryTypeRid": qtRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetQueryTypeFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, qt)
}

// UpdateQueryType handles PUT /api/v2/ontologies/{ontologyApiName}/queryTypes/byRid/{queryTypeRid}.
func (h *OMSHandler) UpdateQueryType(w http.ResponseWriter, r *http.Request) {
	qtRID := chi.URLParam(r, "queryTypeRid")

	var req UpdateQueryTypeRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	existing, err := h.repo.GetQueryType(r.Context(), qtRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("QueryTypeNotFound", map[string]string{
				"queryTypeRid": qtRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetQueryTypeFailed", nil))
		return
	}

	if req.DisplayName != "" {
		existing.DisplayName = req.DisplayName
	}
	existing.Description = req.Description
	if len(req.Parameters) > 0 {
		existing.Parameters = req.Parameters
	}
	if len(req.Output) > 0 {
		existing.Output = req.Output
	}
	if len(req.Query) > 0 {
		existing.Query = req.Query
	}
	if req.Status != "" {
		existing.Status = req.Status
	}

	if err := h.repo.UpdateQueryType(r.Context(), existing); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("QueryTypeNotFound", map[string]string{
				"queryTypeRid": qtRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("UpdateQueryTypeFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, existing)
}

// DeleteQueryType handles DELETE /api/v2/ontologies/{ontologyApiName}/queryTypes/byRid/{queryTypeRid}.
func (h *OMSHandler) DeleteQueryType(w http.ResponseWriter, r *http.Request) {
	qtRID := chi.URLParam(r, "queryTypeRid")

	if err := h.repo.DeleteQueryType(r.Context(), qtRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("QueryTypeNotFound", map[string]string{
				"queryTypeRid": qtRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DeleteQueryTypeFailed", nil))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ExecuteQueryType handles POST /api/v2/ontologies/{ontology}/queries/{queryApiName}/execute.
// When the QueryType has a non-empty FunctionRID and a QueryExecutor is wired,
// the handler dispatches execution to the backing function and responds with
// the Foundry ExecuteQueryResponse envelope {value: <DataValue>} — for every
// result shape, including struct/map values. Otherwise it falls back to
// returning raw metadata for backward compatibility.
func (h *OMSHandler) ExecuteQueryType(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	queryAPIName := chi.URLParam(r, "queryApiName")

	qt, err := h.repo.GetQueryTypeByAPIName(r.Context(), ontologyRID, queryAPIName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("QueryTypeNotFound", map[string]string{
				"queryApiName": queryAPIName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetQueryTypeFailed", nil))
		return
	}

	var inputParams map[string]interface{}
	if err := httputil.ReadJSON(r, &inputParams); err != nil {
		inputParams = map[string]interface{}{}
	}
	// Extract "parameters" sub-key if present (Foundry wire format).
	if nested, ok := inputParams["parameters"].(map[string]interface{}); ok {
		inputParams = nested
	}

	// If the query has a backing function and an executor is wired, dispatch.
	if qt.FunctionRID != "" && h.queryExecutor != nil {
		result, err := h.queryExecutor.Execute(r.Context(), qt, inputParams)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewBadRequest("QueryExecutionFailed", map[string]string{
				"error": err.Error(),
			}))
			return
		}
		// Interpret the function result: if it's a map, check for error/value keys.
		if m, ok := result.(map[string]interface{}); ok {
			if errMsg, ok := m["error"]; ok {
				if s, ok := errMsg.(string); ok && s != "" {
					apierror.WriteJSON(w, apierror.NewBadRequest("QueryFunctionError", map[string]string{
						"error": s,
					}))
					return
				}
			}
			// Functions following the {value: ...} convention already carry
			// the Foundry envelope payload — re-emit it as exactly {value}.
			if v, ok := m["value"]; ok {
				httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
					"value": v,
				})
				return
			}
			// Bare map (struct DataValue): wrap it. Foundry's
			// ExecuteQueryResponse is always {value: <DataValue>}, and OSDK
			// clients unconditionally unwrap .value.
			httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
				"value": m,
			})
			return
		}
		// Non-map result: wrap in {value: ...}
		httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"value": result,
		})
		return
	}

	// Fallback: return metadata without execution.
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"queryTypeRid": qt.RID,
		"apiName":      qt.APIName,
		"query":        qt.Query,
		"parameters":   inputParams,
	})
}
