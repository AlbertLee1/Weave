package oms

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
)

// OMSHandler provides HTTP handlers for OMS V2 and admin endpoints.
type OMSHandler struct {
	repo Repository
}

// NewOMSHandler creates a new OMSHandler with the given repository.
func NewOMSHandler(repo Repository) *OMSHandler {
	return &OMSHandler{repo: repo}
}

// ListOntologies handles GET /api/v2/ontologies.
func (h *OMSHandler) ListOntologies(w http.ResponseWriter, r *http.Request) {
	list, err := h.repo.ListOntologies(r.Context())
	if err != nil {
		apierror.WriteJSON(w, apierror.NewNotFound("ListOntologiesFailed", nil))
		return
	}
	if list == nil {
		list = []Ontology{}
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": list})
}

// GetOntology handles GET /api/v2/ontologies/{ontologyApiName}.
func (h *OMSHandler) GetOntology(w http.ResponseWriter, r *http.Request) {
	rid := chi.URLParam(r, "ontologyApiName")
	o, err := h.repo.GetOntology(r.Context(), rid)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{"ontologyApiName": rid}))
			return
		}
		apierror.WriteJSON(w, apierror.NewNotFound("GetOntologyFailed", nil))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, o)
}

// ListObjectTypes handles GET /api/v2/ontologies/{ontologyApiName}/objectTypes.
func (h *OMSHandler) ListObjectTypes(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	list, err := h.repo.ListObjectTypes(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewNotFound("ListObjectTypesFailed", nil))
		return
	}
	if list == nil {
		list = []ObjectType{}
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": list})
}

// GetObjectType handles GET /api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}.
func (h *OMSHandler) GetObjectType(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	apiName := chi.URLParam(r, "objectTypeApiName")

	ot, err := h.repo.GetObjectTypeByAPIName(r.Context(), ontologyRID, apiName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
				"ontologyApiName":  ontologyRID,
				"objectTypeApiName": apiName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewNotFound("GetObjectTypeFailed", nil))
		return
	}

	wireData, err := ot.ToWireJSON()
	if err != nil {
		apierror.WriteJSON(w, apierror.NewNotFound("SerializationFailed", nil))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(wireData)
}

// ListOutgoingLinkTypes handles GET /api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/outgoingLinkTypes.
func (h *OMSHandler) ListOutgoingLinkTypes(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	apiName := chi.URLParam(r, "objectTypeApiName")

	// Resolve apiName to objectType RID
	ot, err := h.repo.GetObjectTypeByAPIName(r.Context(), ontologyRID, apiName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
				"objectTypeApiName": apiName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewNotFound("GetObjectTypeFailed", nil))
		return
	}

	list, err := h.repo.ListOutgoingLinkTypes(r.Context(), ot.RID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewNotFound("ListOutgoingLinkTypesFailed", nil))
		return
	}

	// Build wire JSON for each link type
	wireList := make([]json.RawMessage, 0, len(list))
	for i := range list {
		data, err := list[i].ToWireJSON()
		if err != nil {
			apierror.WriteJSON(w, apierror.NewNotFound("SerializationFailed", nil))
			return
		}
		wireList = append(wireList, json.RawMessage(data))
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": wireList})
}

// ListActionTypes handles GET /api/v2/ontologies/{ontologyApiName}/actionTypes.
func (h *OMSHandler) ListActionTypes(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	list, err := h.repo.ListActionTypes(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewNotFound("ListActionTypesFailed", nil))
		return
	}

	wireList := make([]json.RawMessage, 0, len(list))
	for i := range list {
		data, err := list[i].ToWireJSON()
		if err != nil {
			apierror.WriteJSON(w, apierror.NewNotFound("SerializationFailed", nil))
			return
		}
		wireList = append(wireList, json.RawMessage(data))
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": wireList})
}

// GetActionType handles GET /api/v2/ontologies/{ontologyApiName}/actionTypes/{actionTypeRid}.
func (h *OMSHandler) GetActionType(w http.ResponseWriter, r *http.Request) {
	actionRID := chi.URLParam(r, "actionTypeRid")
	at, err := h.repo.GetActionType(r.Context(), actionRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ActionTypeNotFound", map[string]string{
				"actionTypeRid": actionRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewNotFound("GetActionTypeFailed", nil))
		return
	}

	wireData, err := at.ToWireJSON()
	if err != nil {
		apierror.WriteJSON(w, apierror.NewNotFound("SerializationFailed", nil))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(wireData)
}
