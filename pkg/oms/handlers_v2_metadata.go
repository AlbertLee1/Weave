package oms

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
)

// requirePreview checks that the ?preview=true query parameter is present.
// Returns false and writes a 400 error if missing.
func requirePreview(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Query().Get("preview") != "true" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("PreviewRequired", map[string]string{
			"reason": "This endpoint requires ?preview=true",
		}))
		return false
	}
	return true
}

// ListInterfaceTypesV2 handles GET /api/v2/ontologies/{ontologyApiName}/interfaceTypes.
// Requires ?preview=true query parameter.
func (h *OMSHandler) ListInterfaceTypesV2(w http.ResponseWriter, r *http.Request) {
	if !requirePreview(w, r) {
		return
	}

	ontologyRID := chi.URLParam(r, "ontologyApiName")
	list, err := h.repo.ListInterfaces(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListInterfaceTypesFailed", nil))
		return
	}

	if list == nil {
		list = []Interface{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": list})
}

// GetInterfaceTypeV2 handles GET /api/v2/ontologies/{ontologyApiName}/interfaceTypes/{interfaceType}.
func (h *OMSHandler) GetInterfaceTypeV2(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	interfaceIdentifier := chi.URLParam(r, "interfaceType")

	iface, err := h.repo.GetInterfaceByAPIName(r.Context(), ontologyRID, interfaceIdentifier)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("InterfaceTypeNotFound", map[string]string{
				"interfaceType": interfaceIdentifier,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetInterfaceTypeFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, iface)
}

// ListValueTypesV2 handles GET /api/v2/ontologies/{ontologyApiName}/valueTypes.
// Requires ?preview=true query parameter.
func (h *OMSHandler) ListValueTypesV2(w http.ResponseWriter, r *http.Request) {
	if !requirePreview(w, r) {
		return
	}

	list, err := h.repo.ListValueTypes(r.Context())
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListValueTypesFailed", nil))
		return
	}

	if list == nil {
		list = []ValueType{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": list})
}

// GetValueTypeV2 handles GET /api/v2/ontologies/{ontologyApiName}/valueTypes/{valueType}.
func (h *OMSHandler) GetValueTypeV2(w http.ResponseWriter, r *http.Request) {
	vtIdentifier := chi.URLParam(r, "valueType")

	vt, err := h.repo.GetValueTypeByAPIName(r.Context(), vtIdentifier)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ValueTypeNotFound", map[string]string{
				"valueType": vtIdentifier,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetValueTypeFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, vt)
}

// ListQueryTypesV2 handles GET /api/v2/ontologies/{ontologyApiName}/queryTypes.
func (h *OMSHandler) ListQueryTypesV2(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	list, err := h.repo.ListQueryTypes(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListQueryTypesFailed", nil))
		return
	}

	if list == nil {
		list = []QueryType{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": list})
}

// GetQueryTypeV2 handles GET /api/v2/ontologies/{ontologyApiName}/queryTypes/{queryApiName}.
func (h *OMSHandler) GetQueryTypeV2(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	queryIdentifier := chi.URLParam(r, "queryApiName")

	qt, err := h.repo.GetQueryTypeByAPIName(r.Context(), ontologyRID, queryIdentifier)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("QueryTypeNotFound", map[string]string{
				"queryApiName": queryIdentifier,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetQueryTypeFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, qt)
}

// --- ActionType extra V2 endpoints (US-012) ---

// GetActionTypeByRidV2 handles GET /api/v2/ontologies/{ontologyApiName}/actionTypes/byRid/{actionTypeRid}.
// Looks up an ActionType by its RID.
func (h *OMSHandler) GetActionTypeByRidV2(w http.ResponseWriter, r *http.Request) {
	actionRID := chi.URLParam(r, "actionTypeRid")

	at, err := h.repo.GetActionType(r.Context(), actionRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ActionTypeNotFound", map[string]string{
				"actionTypeRid": actionRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetActionTypeFailed", nil))
		return
	}

	wireData, err := at.ToWireJSON()
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("SerializationFailed", nil))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(wireData)
}

// getByRidBatchRequest is the request body for POST .../actionTypes/getByRidBatch.
type getByRidBatchRequest struct {
	RIDs []string `json:"rids"`
}

// GetActionTypesByRidBatchV2 handles POST /api/v2/ontologies/{ontologyApiName}/actionTypes/getByRidBatch.
// Batch lookup of ActionTypes by their RIDs.
func (h *OMSHandler) GetActionTypesByRidBatchV2(w http.ResponseWriter, r *http.Request) {
	var req getByRidBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "Failed to parse request body",
		}))
		return
	}

	wireList := make([]json.RawMessage, 0, len(req.RIDs))
	for _, rid := range req.RIDs {
		at, err := h.repo.GetActionType(r.Context(), rid)
		if err != nil {
			// Skip missing entries silently
			continue
		}
		data, err := at.ToWireJSON()
		if err != nil {
			continue
		}
		wireList = append(wireList, json.RawMessage(data))
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": wireList})
}

// GetActionTypeFullMetadataV2 handles GET /api/v2/ontologies/{ontologyApiName}/actionTypes/{actionTypeRid}/fullMetadata.
// Returns the ActionType with all metadata fields.
func (h *OMSHandler) GetActionTypeFullMetadataV2(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	actionIdentifier := chi.URLParam(r, "actionTypeRid")

	at, err := h.repo.GetActionTypeByAPIName(r.Context(), ontologyRID, actionIdentifier)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ActionTypeNotFound", map[string]string{
				"actionType": actionIdentifier,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetActionTypeFailed", nil))
		return
	}

	wireData, err := at.ToFullMetadataJSON()
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("SerializationFailed", nil))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(wireData)
}

// ListActionTypesFullMetadataV2 handles GET /api/v2/ontologies/{ontologyApiName}/actionTypesFullMetadata.
// Returns all ActionTypes with full metadata.
func (h *OMSHandler) ListActionTypesFullMetadataV2(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")

	list, err := h.repo.ListActionTypes(r.Context(), ontologyRID)
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
