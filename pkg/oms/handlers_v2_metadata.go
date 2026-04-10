package oms

import (
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
