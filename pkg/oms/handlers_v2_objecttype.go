package oms

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
)

// --- ObjectType extra V2 endpoints (US-013) ---

// GetObjectTypeFullMetadataV2 handles GET /api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/fullMetadata.
// Returns the ObjectType with all metadata fields including full property detail.
func (h *OMSHandler) GetObjectTypeFullMetadataV2(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	otIdentifier := chi.URLParam(r, "objectTypeApiName")

	ot, err := h.repo.GetObjectTypeByAPIName(r.Context(), ontologyRID, otIdentifier)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
				"objectType": otIdentifier,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetObjectTypeFailed", nil))
		return
	}

	// Load properties for full metadata
	props, err := h.repo.ListProperties(r.Context(), ot.RID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListPropertiesFailed", nil))
		return
	}
	ot.Properties = props

	wireData, err := ot.ToFullMetadataJSON()
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("SerializationFailed", nil))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(wireData)
}

// GetObjectTypesByRidBatchV2 handles POST /api/v2/ontologies/{ontologyApiName}/objectTypes/getByRidBatch.
// Batch lookup of ObjectTypes by their RIDs.
func (h *OMSHandler) GetObjectTypesByRidBatchV2(w http.ResponseWriter, r *http.Request) {
	var req getByRidBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "Failed to parse request body",
		}))
		return
	}

	wireList := make([]json.RawMessage, 0, len(req.RIDs))
	for _, rid := range req.RIDs {
		ot, err := h.repo.GetObjectType(r.Context(), rid)
		if err != nil {
			// Skip missing entries silently
			continue
		}
		data, err := ot.ToWireJSON()
		if err != nil {
			continue
		}
		wireList = append(wireList, json.RawMessage(data))
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": wireList})
}

// PostObjectTypeEditsHistoryV2 handles POST /api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/editsHistory.
// Returns the edit history (action logs) for an ObjectType.
func (h *OMSHandler) PostObjectTypeEditsHistoryV2(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	otIdentifier := chi.URLParam(r, "objectTypeApiName")

	// Resolve ObjectType to verify it exists
	ot, err := h.repo.GetObjectTypeByAPIName(r.Context(), ontologyRID, otIdentifier)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
				"objectType": otIdentifier,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetObjectTypeFailed", nil))
		return
	}

	// Fetch action logs scoped to this ObjectType's RID
	logs, err := h.repo.ListActionLogs(r.Context(), ot.RID, 0, 0)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListEditsHistoryFailed", nil))
		return
	}

	if logs == nil {
		logs = []ActionLog{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": logs})
}
