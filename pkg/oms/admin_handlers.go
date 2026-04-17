package oms

import (
	"context"
	"encoding/json"
	"errors"
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

// validateBranch checks if the ?branch= query parameter references a valid, open branch.
// Returns the branch if valid, nil if no branch parameter, or writes an error response and returns nil.
func (h *OMSHandler) validateBranch(w http.ResponseWriter, r *http.Request) (*OntologyBranch, bool) {
	branchID := r.URL.Query().Get("branch")
	if branchID == "" {
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
	PrimaryKey        string `json:"primaryKey"`
	TitleProperty     string `json:"titleProperty,omitempty"`
	Status            string `json:"status"`
	Visibility        string `json:"visibility"`
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
}

// UpdateLinkTypeRequest is the request body for updating a link type.
type UpdateLinkTypeRequest struct {
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
	IsRequired  *bool  `json:"required,omitempty"`
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
}

// CreateActionTypeRequest is the request body for creating an action type.
type CreateActionTypeRequest struct {
	APIName     string          `json:"apiName"`
	DisplayName string          `json:"displayName"`
	Description string          `json:"description,omitempty"`
	Status      string          `json:"status"`
	Parameters  json.RawMessage `json:"parameters"`
	Rules       json.RawMessage `json:"rules"`
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
	if req.PrimaryKey == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:primaryKey", map[string]string{
			"parameter": "primaryKey",
			"reason":    "primaryKey is required",
		}))
		return
	}

	status := req.Status
	if status == "" {
		status = "ACTIVE"
	}
	visibility := req.Visibility
	if visibility == "" {
		visibility = "NORMAL"
	}

	ot := &ObjectType{
		RID:               rid.NewObjectTypeRID(),
		OntologyRID:       ontologyRID,
		APIName:           req.APIName,
		DisplayName:       req.DisplayName,
		PluralDisplayName: req.PluralDisplayName,
		Description:       req.Description,
		PrimaryKey:        req.PrimaryKey,
		TitleProperty:     req.TitleProperty,
		Status:            status,
		Visibility:        visibility,
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

	p := &Property{
		RID:           rid.NewPropertyRID(),
		ObjectTypeRID: objectTypeRID,
		APIName:       req.APIName,
		DisplayName:   req.DisplayName,
		Description:   req.Description,
		BaseType:      req.BaseType,
		TypeConfig:    req.TypeConfig,
		IsArray:       req.IsArray,
		IsNullable:    req.IsNullable,
		IsSearchable:  req.IsSearchable,
		IsSortable:    req.IsSortable,
		IsEditOnly:    req.EditOnly,
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

	lt := &LinkType{
		RID:              rid.NewLinkTypeRID(),
		OntologyRID:      ontologyRID,
		APIName:          req.APIName,
		DisplayName:      req.DisplayName,
		Description:      req.Description,
		SourceObjectType: req.SourceObjectType,
		TargetObjectType: req.TargetObjectType,
		Cardinality:      req.Cardinality,
		ForeignKeyConfig: req.ForeignKeyConfig,
		JoinTableConfig:  req.JoinTableConfig,
		IsRequired:       req.IsRequired,
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

	at := &ActionType{
		RID:         rid.NewActionTypeRID(),
		OntologyRID: ontologyRID,
		APIName:     req.APIName,
		DisplayName: req.DisplayName,
		Description: req.Description,
		Status:      status,
		Parameters:  req.Parameters,
		Rules:       req.Rules,
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
	ontologyApiName := chi.URLParam(r, "ontologyApiName")

	linkTypes, err := h.repo.ListLinkTypes(r.Context(), ontologyApiName)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListLinkTypesFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": linkTypes,
	})
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
	ontologyApiName := chi.URLParam(r, "ontologyApiName")
	list, err := repo.ListLinkTypes(r.Context(), ontologyApiName)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListLinkTypesFailed", nil))
		return
	}
	if list == nil {
		list = []LinkType{}
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": list})
}

// --- Interface request structs ---

// CreateInterfaceRequest is the request body for creating an interface.
type CreateInterfaceRequest struct {
	APIName          string          `json:"apiName"`
	DisplayName      string          `json:"displayName"`
	Description      string          `json:"description,omitempty"`
	ExtendsRID       string          `json:"extendsRid,omitempty"`
	SharedProperties json.RawMessage `json:"sharedProperties,omitempty"`
}

// UpdateInterfaceRequest is the request body for updating an interface.
type UpdateInterfaceRequest struct {
	DisplayName      string          `json:"displayName"`
	ExtendsRID       string          `json:"extendsRid,omitempty"`
	SharedProperties json.RawMessage `json:"sharedProperties,omitempty"`
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
		RID:              rid.NewInterfaceRID(),
		OntologyRID:      ontologyRID,
		APIName:          req.APIName,
		DisplayName:      req.DisplayName,
		ExtendsRID:       req.ExtendsRID,
		SharedProperties: req.SharedProperties,
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
func (h *OMSHandler) DeleteSharedProperty(w http.ResponseWriter, r *http.Request) {
	spRID := chi.URLParam(r, "sharedPropertyRid")

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
func (h *OMSHandler) DeleteTypeGroup(w http.ResponseWriter, r *http.Request) {
	tgRID := chi.URLParam(r, "typeGroupRid")

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

	// Map old RIDs to new RIDs for object types (needed for link types)
	otRIDMap := make(map[string]string) // old RID -> new RID

	// Create object types
	for _, ot := range export.ObjectTypes {
		oldRID := ot.RID
		newOT := &ObjectType{
			RID:               rid.NewObjectTypeRID(),
			OntologyRID:       ontology.RID,
			APIName:           ot.APIName,
			DisplayName:       ot.DisplayName,
			PluralDisplayName: ot.PluralDisplayName,
			Description:       ot.Description,
			PrimaryKey:        ot.PrimaryKey,
			TitleProperty:     ot.TitleProperty,
			Status:            ot.Status,
			Visibility:        ot.Visibility,
			IconName:          ot.IconName,
			Color:             ot.Color,
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
		otRIDMap[oldRID] = newOT.RID

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

	w.WriteHeader(http.StatusNoContent)
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

// CreateQueryType handles POST /api/admin/ontologies/{ontologyApiName}/queryTypes.
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

// GetQueryType handles GET /api/admin/queryTypes/{queryTypeRid}.
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

// UpdateQueryType handles PUT /api/admin/queryTypes/{queryTypeRid}.
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

// DeleteQueryType handles DELETE /api/admin/queryTypes/{queryTypeRid}.
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
// the handler dispatches execution to the backing function. Otherwise it falls
// back to returning raw metadata for backward compatibility.
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
			httputil.WriteJSON(w, http.StatusOK, m)
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
