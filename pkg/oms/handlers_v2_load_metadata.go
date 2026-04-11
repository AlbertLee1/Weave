package oms

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
)

// loadMetadataRequest is the request body for POST /api/v2/ontologies/{ontology}/metadata.
// Each key acts as a selector — if the key is present (even as empty {}), that subset is loaded.
type loadMetadataRequest struct {
	ObjectTypes    *json.RawMessage `json:"objectTypes,omitempty"`
	LinkTypes      *json.RawMessage `json:"linkTypes,omitempty"`
	ActionTypes    *json.RawMessage `json:"actionTypes,omitempty"`
	InterfaceTypes *json.RawMessage `json:"interfaceTypes,omitempty"`
	QueryTypes     *json.RawMessage `json:"queryTypes,omitempty"`
}

// LoadMetadataV2 handles POST /api/v2/ontologies/{ontologyApiName}/metadata.
// Returns OntologyFullMetadata filtered to only the requested subsets.
func (h *OMSHandler) LoadMetadataV2(w http.ResponseWriter, r *http.Request) {
	ontologyIdentifier := chi.URLParam(r, "ontologyApiName")

	var req loadMetadataRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "Failed to parse request body",
		}))
		return
	}

	// Resolve ontology (supports both apiName and RID)
	ontology, err := h.repo.GetOntology(r.Context(), ontologyIdentifier)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
				"ontologyApiName": ontologyIdentifier,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetOntologyFailed", nil))
		return
	}

	result := map[string]interface{}{
		"ontology": ontology,
	}

	if req.ObjectTypes != nil {
		objectTypes, err := h.repo.ListObjectTypes(r.Context(), ontologyIdentifier)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("ListObjectTypesFailed", nil))
			return
		}
		if objectTypes == nil {
			objectTypes = []ObjectType{}
		}
		result["objectTypes"] = objectTypes
	}

	if req.LinkTypes != nil {
		linkTypes, err := h.repo.ListLinkTypes(r.Context(), ontologyIdentifier)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("ListLinkTypesFailed", nil))
			return
		}
		if linkTypes == nil {
			linkTypes = []LinkType{}
		}
		result["linkTypes"] = linkTypes
	}

	if req.ActionTypes != nil {
		actionTypes, err := h.repo.ListActionTypes(r.Context(), ontologyIdentifier)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("ListActionTypesFailed", nil))
			return
		}
		if actionTypes == nil {
			actionTypes = []ActionType{}
		}
		result["actionTypes"] = actionTypes
	}

	if req.InterfaceTypes != nil {
		interfaces, err := h.repo.ListInterfaces(r.Context(), ontologyIdentifier)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("ListInterfaceTypesFailed", nil))
			return
		}
		if interfaces == nil {
			interfaces = []Interface{}
		}
		result["interfaceTypes"] = interfaces
	}

	if req.QueryTypes != nil {
		queryTypes, err := h.repo.ListQueryTypes(r.Context(), ontologyIdentifier)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("ListQueryTypesFailed", nil))
			return
		}
		if queryTypes == nil {
			queryTypes = []QueryType{}
		}
		result["queryTypes"] = queryTypes
	}

	httputil.WriteJSON(w, http.StatusOK, result)
}
