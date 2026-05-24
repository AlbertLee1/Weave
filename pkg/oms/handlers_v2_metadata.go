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

// --- InterfaceType OutgoingLinkTypes V2 endpoints (US-025) ---

// ListInterfaceOutgoingLinkTypesV2 handles GET /api/v2/ontologies/{ontologyApiName}/interfaceTypes/{interfaceType}/outgoingLinkTypes.
func (h *OMSHandler) ListInterfaceOutgoingLinkTypesV2(w http.ResponseWriter, r *http.Request) {
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

	var linkTypes []InterfaceLinkType
	if len(iface.OutgoingLinkTypes) > 0 {
		if err := json.Unmarshal(iface.OutgoingLinkTypes, &linkTypes); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("ParseOutgoingLinkTypesFailed", nil))
			return
		}
	}
	if linkTypes == nil {
		linkTypes = []InterfaceLinkType{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": linkTypes})
}

// GetInterfaceOutgoingLinkTypeV2 handles GET /api/v2/ontologies/{ontologyApiName}/interfaceTypes/{interfaceType}/outgoingLinkTypes/{interfaceLinkType}.
func (h *OMSHandler) GetInterfaceOutgoingLinkTypeV2(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	interfaceIdentifier := chi.URLParam(r, "interfaceType")
	linkTypeIdentifier := chi.URLParam(r, "interfaceLinkType")

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

	var linkTypes []InterfaceLinkType
	if len(iface.OutgoingLinkTypes) > 0 {
		if err := json.Unmarshal(iface.OutgoingLinkTypes, &linkTypes); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("ParseOutgoingLinkTypesFailed", nil))
			return
		}
	}

	for _, lt := range linkTypes {
		if lt.APIName == linkTypeIdentifier {
			httputil.WriteJSON(w, http.StatusOK, lt)
			return
		}
	}

	apierror.WriteJSON(w, apierror.NewNotFound("InterfaceLinkTypeNotFound", map[string]string{
		"interfaceLinkType": linkTypeIdentifier,
	}))
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
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
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

// GetTypeGroupsByRidBatchV2 handles POST /api/v2/ontologies/{ontologyApiName}/typeGroups/getByRidBatch.
// Round 87 extends the batch-get-by-RID convention to TypeGroups,
// the navigation-pane categorisation primitive. The SPA Browser
// sidebar and Explorer faceting controls render N type-groups at
// a time; without a batch surface a 50-group list needed 50
// round-trips to label them. Reuses shared getByRidBatchRequest.
// Missing RIDs silently skipped (same convention). TypeGroup
// serialises directly via the JSON encoder — same as Interface
// / ValueType / SharedProperty, no ToWireJSON helper.
func (h *OMSHandler) GetTypeGroupsByRidBatchV2(w http.ResponseWriter, r *http.Request) {
	var req getByRidBatchRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	out := make([]*TypeGroup, 0, len(req.RIDs))
	for _, rid := range req.RIDs {
		tg, err := h.repo.GetTypeGroup(r.Context(), rid)
		if err != nil {
			continue
		}
		out = append(out, tg)
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": out})
}

// GetSharedPropertyTypesByRidBatchV2 handles POST /api/v2/ontologies/{ontologyApiName}/sharedPropertyTypes/getByRidBatch.
// Round 85 extends the batch-get-by-RID convention beyond the five
// core metadata kinds (objectTypes / actionTypes / linkTypes /
// interfaceTypes / valueTypes) to SharedPropertyTypes — the
// reusable property definitions used by Interfaces and as an
// include-pool for ObjectType properties. Property editors and
// interface designers rendering many shared-property metadatas
// no longer need N round-trips. Reuses shared getByRidBatchRequest.
// Missing RIDs silently skipped (same convention). SharedProperty
// serialises directly via the JSON encoder — no ToWireJSON helper.
func (h *OMSHandler) GetSharedPropertyTypesByRidBatchV2(w http.ResponseWriter, r *http.Request) {
	var req getByRidBatchRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	out := make([]*SharedProperty, 0, len(req.RIDs))
	for _, rid := range req.RIDs {
		sp, err := h.repo.GetSharedProperty(r.Context(), rid)
		if err != nil {
			continue
		}
		out = append(out, sp)
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": out})
}

// GetValueTypesByRidBatchV2 handles POST /api/v2/ontologies/{ontologyApiName}/valueTypes/getByRidBatch.
// Round 83 closes the batch-get symmetry across all five metadata
// kinds (objectTypes + actionTypes + linkTypes round-79 +
// interfaceTypes round-81 + this). Property-editor dropdowns and
// unit-suggestion panels rendering many value-type metadatas no
// longer need N round-trips to label N RIDs. Reuses shared
// getByRidBatchRequest. Missing RIDs silently skipped (convention
// across all five surfaces). ValueType serialises directly via
// the JSON encoder — no ToWireJSON helper.
func (h *OMSHandler) GetValueTypesByRidBatchV2(w http.ResponseWriter, r *http.Request) {
	var req getByRidBatchRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	out := make([]*ValueType, 0, len(req.RIDs))
	for _, rid := range req.RIDs {
		vt, err := h.repo.GetValueType(r.Context(), rid)
		if err != nil {
			continue
		}
		out = append(out, vt)
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": out})
}

// GetInterfaceTypesByRidBatchV2 handles POST /api/v2/ontologies/{ontologyApiName}/interfaceTypes/getByRidBatch.
// Round 81 closes the batch-get symmetry across all four metadata
// kinds (objectTypes + actionTypes + linkTypes round-79 + this).
// SDK callers rendering many interface metadatas previously needed
// N round-trips. Reuses shared getByRidBatchRequest. Missing RIDs
// silently skipped (matches the established convention across the
// other three surfaces). Interface struct serialises directly via
// the JSON encoder — no ToWireJSON helper needed because Interface
// has no signature-style fields requiring re-marshalling.
func (h *OMSHandler) GetInterfaceTypesByRidBatchV2(w http.ResponseWriter, r *http.Request) {
	var req getByRidBatchRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	out := make([]*Interface, 0, len(req.RIDs))
	for _, rid := range req.RIDs {
		iface, err := h.repo.GetInterface(r.Context(), rid)
		if err != nil {
			continue
		}
		out = append(out, iface)
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": out})
}

// GetLinkTypesByRidBatchV2 handles POST /api/v2/ontologies/{ontologyApiName}/linkTypes/getByRidBatch.
// Batch lookup of LinkTypes by their RIDs. Round 79 closes the
// symmetry gap with GetObjectTypesByRidBatchV2 + GetActionTypesByRid
// BatchV2 — SDK callers rendering many link metadatas (ObjectList
// link columns, scenario diff badges) had to issue N round-trips.
// Reuses the shared getByRidBatchRequest type. Missing RIDs are
// silently skipped so the response carries only resolvable rows
// (matches the existing batch convention so partial-render logic
// stays portable across object/action/link batch surfaces).
func (h *OMSHandler) GetLinkTypesByRidBatchV2(w http.ResponseWriter, r *http.Request) {
	var req getByRidBatchRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	wireList := make([]json.RawMessage, 0, len(req.RIDs))
	for _, rid := range req.RIDs {
		lt, err := h.repo.GetLinkType(r.Context(), rid)
		if err != nil {
			// Skip missing entries silently — matches existing
			// objectTypes / actionTypes batch behaviour.
			continue
		}
		data, err := lt.ToWireJSON()
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
	if !requirePreview(w, r) {
		return
	}
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
	if !requirePreview(w, r) {
		return
	}
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
