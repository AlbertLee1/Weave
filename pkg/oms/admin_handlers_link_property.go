package oms

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/rid"
	"github.com/liyang/weave/pkg/types"
)

// US-210 Link Properties: admin CRUD for link_properties rows + a PUT
// endpoint for edge-property values. The schema rows are what let a modeler
// declare that a MANY_TO_MANY link carries typed properties (e.g.
// membership.role); the edge values live in link_edges.edge_properties JSONB
// and are written via PutLinkEdgeProperties.

// CreateLinkPropertyRequest is the request body for POST
// /api/v2/ontologies/{o}/links/{linkTypeRid}/properties.
type CreateLinkPropertyRequest struct {
	APIName     string          `json:"apiName"`
	DisplayName string          `json:"displayName,omitempty"`
	Description string          `json:"description,omitempty"`
	BaseType    string          `json:"baseType"`
	TypeConfig  json.RawMessage `json:"typeConfig,omitempty"`
	IsArray     bool            `json:"isArray"`
	IsNullable  bool            `json:"isNullable"`
}

// UpdateLinkPropertyRequest is the request body for PUT
// /api/v2/ontologies/{o}/links/properties/byRid/{linkPropertyRid}.
type UpdateLinkPropertyRequest struct {
	DisplayName string          `json:"displayName,omitempty"`
	Description string          `json:"description,omitempty"`
	BaseType    string          `json:"baseType,omitempty"`
	TypeConfig  json.RawMessage `json:"typeConfig,omitempty"`
	IsArray     *bool           `json:"isArray,omitempty"`
	IsNullable  *bool           `json:"isNullable,omitempty"`
}

// PutLinkEdgePropertiesRequest is the request body for PUT
// /api/v2/ontologies/{o}/links/{linkTypeRid}/edges/{sourcePk}/{targetPk}/properties.
// Values map edge-property apiName → value. The map fully replaces whatever
// was previously stored in link_edges.edge_properties for that edge.
type PutLinkEdgePropertiesRequest struct {
	Values map[string]interface{} `json:"values"`
}

// --- Schema CRUD handlers ---

// linkPropertyStoreOrError returns the configured LinkPropertyStore or writes
// a 503 NotConfigured response. The error path lets degraded-mode test
// routers boot without wiring a store.
func (h *OMSHandler) linkPropertyStoreOrError(w http.ResponseWriter) (LinkPropertyStore, bool) {
	if h.linkPropertyStore == nil {
		apierror.WriteJSON(w, apierror.NewInternal("LinkPropertyStoreNotConfigured", nil))
		return nil, false
	}
	return h.linkPropertyStore, true
}

// linkEdgeStoreOrError returns the configured LinkEdgeStore or writes a 503
// NotConfigured response.
func (h *OMSHandler) linkEdgeStoreOrError(w http.ResponseWriter) (LinkEdgeStore, bool) {
	if h.linkEdgeStore == nil {
		apierror.WriteJSON(w, apierror.NewInternal("LinkEdgeStoreNotConfigured", nil))
		return nil, false
	}
	return h.linkEdgeStore, true
}

// ListLinkProperties handles GET /api/v2/ontologies/{o}/links/{linkTypeRid}/properties.
func (h *OMSHandler) ListLinkProperties(w http.ResponseWriter, r *http.Request) {
	store, ok := h.linkPropertyStoreOrError(w)
	if !ok {
		return
	}
	linkTypeRID := chi.URLParam(r, "linkTypeRid")
	if _, err := h.repo.GetLinkType(r.Context(), linkTypeRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("LinkTypeNotFound", map[string]string{
				"linkTypeRid": linkTypeRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetLinkTypeFailed", nil))
		return
	}
	props, err := store.ListLinkProperties(r.Context(), linkTypeRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListLinkPropertiesFailed", nil))
		return
	}
	if props == nil {
		props = []LinkProperty{}
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": props})
}

// CreateLinkProperty handles POST /api/v2/ontologies/{o}/links/{linkTypeRid}/properties.
func (h *OMSHandler) CreateLinkProperty(w http.ResponseWriter, r *http.Request) {
	store, ok := h.linkPropertyStoreOrError(w)
	if !ok {
		return
	}
	linkTypeRID := chi.URLParam(r, "linkTypeRid")
	if _, err := h.repo.GetLinkType(r.Context(), linkTypeRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("LinkTypeNotFound", map[string]string{
				"linkTypeRid": linkTypeRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetLinkTypeFailed", nil))
		return
	}

	var req CreateLinkPropertyRequest
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
	if !types.BaseType(req.BaseType).IsValid() {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:baseType", map[string]string{
			"parameter": "baseType",
			"reason":    "unknown baseType",
			"value":     req.BaseType,
		}))
		return
	}

	lp := &LinkProperty{
		RID:         rid.NewPropertyRID(),
		LinkTypeRID: linkTypeRID,
		APIName:     req.APIName,
		DisplayName: req.DisplayName,
		Description: req.Description,
		BaseType:    req.BaseType,
		TypeConfig:  req.TypeConfig,
		IsArray:     req.IsArray,
		IsNullable:  req.IsNullable,
	}

	if err := store.CreateLinkProperty(r.Context(), lp); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("LinkPropertyAlreadyExists", map[string]string{
				"apiName": req.APIName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CreateLinkPropertyFailed", nil))
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, lp)
}

// UpdateLinkProperty handles PUT /api/v2/ontologies/{o}/links/properties/byRid/{linkPropertyRid}.
func (h *OMSHandler) UpdateLinkProperty(w http.ResponseWriter, r *http.Request) {
	store, ok := h.linkPropertyStoreOrError(w)
	if !ok {
		return
	}
	linkPropertyRID := chi.URLParam(r, "linkPropertyRid")
	existing, err := store.GetLinkProperty(r.Context(), linkPropertyRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("LinkPropertyNotFound", map[string]string{
				"linkPropertyRid": linkPropertyRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetLinkPropertyFailed", nil))
		return
	}

	var req UpdateLinkPropertyRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}
	updated := *existing
	if req.DisplayName != "" {
		updated.DisplayName = req.DisplayName
	}
	if req.Description != "" {
		updated.Description = req.Description
	}
	if req.BaseType != "" {
		if !types.BaseType(req.BaseType).IsValid() {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:baseType", map[string]string{
				"parameter": "baseType",
				"reason":    "unknown baseType",
				"value":     req.BaseType,
			}))
			return
		}
		updated.BaseType = req.BaseType
	}
	if len(req.TypeConfig) > 0 {
		updated.TypeConfig = req.TypeConfig
	}
	if req.IsArray != nil {
		updated.IsArray = *req.IsArray
	}
	if req.IsNullable != nil {
		updated.IsNullable = *req.IsNullable
	}

	if err := store.UpdateLinkProperty(r.Context(), &updated); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("LinkPropertyNotFound", map[string]string{
				"linkPropertyRid": linkPropertyRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("UpdateLinkPropertyFailed", nil))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, &updated)
}

// DeleteLinkProperty handles DELETE /api/v2/ontologies/{o}/links/properties/byRid/{linkPropertyRid}.
func (h *OMSHandler) DeleteLinkProperty(w http.ResponseWriter, r *http.Request) {
	store, ok := h.linkPropertyStoreOrError(w)
	if !ok {
		return
	}
	linkPropertyRID := chi.URLParam(r, "linkPropertyRid")
	if err := store.DeleteLinkProperty(r.Context(), linkPropertyRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("LinkPropertyNotFound", map[string]string{
				"linkPropertyRid": linkPropertyRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DeleteLinkPropertyFailed", nil))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Edge value handler ---

// PutLinkEdgeProperties handles PUT
// /api/v2/ontologies/{o}/links/{linkTypeRid}/edges/{sourcePk}/{targetPk}/properties.
// The body's `values` map is validated against the LinkType's declared
// LinkProperty schema (unknown keys are rejected, required fields enforced)
// and then written to link_edges.edge_properties for that edge.
//
// Upsert semantics: if the edge row does not exist, it is created. This lets
// callers author edge metadata without a separate create-edge round-trip,
// mirroring how the funnel's LINK_CREATE applies edges.
func (h *OMSHandler) PutLinkEdgeProperties(w http.ResponseWriter, r *http.Request) {
	propStore, ok := h.linkPropertyStoreOrError(w)
	if !ok {
		return
	}
	edgeStore, ok := h.linkEdgeStoreOrError(w)
	if !ok {
		return
	}
	linkTypeRID := chi.URLParam(r, "linkTypeRid")
	sourcePK := chi.URLParam(r, "sourcePk")
	targetPK := chi.URLParam(r, "targetPk")
	if sourcePK == "" || targetPK == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:edgeEndpoints", map[string]string{
			"reason": "sourcePk and targetPk are required",
		}))
		return
	}
	if _, err := h.repo.GetLinkType(r.Context(), linkTypeRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("LinkTypeNotFound", map[string]string{
				"linkTypeRid": linkTypeRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetLinkTypeFailed", nil))
		return
	}

	var req PutLinkEdgePropertiesRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}
	if req.Values == nil {
		req.Values = map[string]interface{}{}
	}

	schema, err := propStore.ListLinkProperties(r.Context(), linkTypeRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListLinkPropertiesFailed", nil))
		return
	}
	if apiErr := validateEdgeValuesAgainstSchema(schema, req.Values); apiErr != nil {
		apierror.WriteJSON(w, apiErr)
		return
	}

	edgeJSON, err := json.Marshal(req.Values)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("MarshalEdgePropertiesFailed", nil))
		return
	}
	if err := edgeStore.UpsertLinkEdge(r.Context(), &LinkEdge{
		LinkTypeRID:    linkTypeRID,
		SourceObjectPK: sourcePK,
		TargetObjectPK: targetPK,
		EdgeProperties: edgeJSON,
	}); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("UpsertLinkEdgeFailed", nil))
		return
	}

	edge, err := edgeStore.GetLinkEdge(r.Context(), linkTypeRID, sourcePK, targetPK)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("GetLinkEdgeFailed", nil))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, edge)
}

// validateEdgeValuesAgainstSchema rejects unknown apiNames and enforces
// required fields (isNullable=false without a value). Deep type validation
// of the supplied values is left to the runtime layer — we only enforce the
// shape invariants callers can reasonably expect at write time.
func validateEdgeValuesAgainstSchema(schema []LinkProperty, values map[string]interface{}) *apierror.APIError {
	declared := make(map[string]LinkProperty, len(schema))
	for _, lp := range schema {
		declared[lp.APIName] = lp
	}
	for k := range values {
		if _, ok := declared[k]; !ok {
			return apierror.NewInvalidParameter("InvalidParameter:edgeProperty", map[string]string{
				"parameter": k,
				"reason":    "not declared on this link type",
			})
		}
	}
	for _, lp := range schema {
		if lp.IsNullable {
			continue
		}
		v, present := values[lp.APIName]
		if !present || v == nil {
			return apierror.NewInvalidParameter("InvalidParameter:edgeProperty", map[string]string{
				"parameter": lp.APIName,
				"reason":    "required edge property missing",
			})
		}
	}
	return nil
}
