package oms

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/rid"
)

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
	DisplayName       string `json:"displayName"`
	PluralDisplayName string `json:"pluralDisplayName,omitempty"`
	Description       string `json:"description,omitempty"`
	TitleProperty     string `json:"titleProperty,omitempty"`
	Status            string `json:"status"`
	Visibility        string `json:"visibility"`
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
	DisplayName string          `json:"displayName"`
	Description string          `json:"description,omitempty"`
	Status      string          `json:"status"`
	Parameters  json.RawMessage `json:"parameters"`
	Rules       json.RawMessage `json:"rules"`
}

// --- Admin handlers ---

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
		apierror.WriteJSON(w, apierror.NewNotFound("CreateOntologyFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, o)
}

// CreateObjectType handles POST /api/admin/ontologies/{ontologyApiName}/objectTypes.
func (h *OMSHandler) CreateObjectType(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")

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

	if err := h.repo.CreateObjectType(r.Context(), ot); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("ObjectTypeAlreadyExists", map[string]string{
				"apiName": req.APIName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewNotFound("CreateObjectTypeFailed", nil))
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
		apierror.WriteJSON(w, apierror.NewNotFound("GetObjectTypeFailed", nil))
		return
	}

	existing.DisplayName = req.DisplayName
	existing.PluralDisplayName = req.PluralDisplayName
	existing.Description = req.Description
	existing.TitleProperty = req.TitleProperty
	existing.Status = req.Status
	existing.Visibility = req.Visibility

	if err := h.repo.UpdateObjectType(r.Context(), existing); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
				"objectTypeRid": objectTypeRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewNotFound("UpdateObjectTypeFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, existing)
}

// DeleteObjectType handles DELETE /api/admin/objectTypes/{objectTypeRid}.
func (h *OMSHandler) DeleteObjectType(w http.ResponseWriter, r *http.Request) {
	objectTypeRID := chi.URLParam(r, "objectTypeRid")

	if err := h.repo.DeleteObjectType(r.Context(), objectTypeRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
				"objectTypeRid": objectTypeRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewNotFound("DeleteObjectTypeFailed", nil))
		return
	}

	w.WriteHeader(http.StatusNoContent)
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
	}

	if err := h.repo.CreateProperty(r.Context(), p); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("PropertyAlreadyExists", map[string]string{
				"apiName": req.APIName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewNotFound("CreatePropertyFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, p)
}

// DeleteProperty handles DELETE /api/admin/properties/{propertyRid}.
func (h *OMSHandler) DeleteProperty(w http.ResponseWriter, r *http.Request) {
	propertyRID := chi.URLParam(r, "propertyRid")

	if err := h.repo.DeleteProperty(r.Context(), propertyRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("PropertyNotFound", map[string]string{
				"propertyRid": propertyRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewNotFound("DeletePropertyFailed", nil))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// CreateLinkType handles POST /api/admin/ontologies/{ontologyApiName}/linkTypes.
func (h *OMSHandler) CreateLinkType(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")

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

	if err := h.repo.CreateLinkType(r.Context(), lt); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("LinkTypeAlreadyExists", map[string]string{
				"apiName": req.APIName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewNotFound("CreateLinkTypeFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, lt)
}

// CreateActionType handles POST /api/admin/ontologies/{ontologyApiName}/actionTypes.
func (h *OMSHandler) CreateActionType(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")

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

	if err := h.repo.CreateActionType(r.Context(), at); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("ActionTypeAlreadyExists", map[string]string{
				"apiName": req.APIName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewNotFound("CreateActionTypeFailed", nil))
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
		apierror.WriteJSON(w, apierror.NewNotFound("GetActionTypeFailed", nil))
		return
	}

	existing.DisplayName = req.DisplayName
	existing.Description = req.Description
	existing.Status = req.Status
	existing.Parameters = req.Parameters
	existing.Rules = req.Rules

	if err := h.repo.UpdateActionType(r.Context(), existing); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ActionTypeNotFound", map[string]string{
				"actionTypeRid": actionTypeRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewNotFound("UpdateActionTypeFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, existing)
}
