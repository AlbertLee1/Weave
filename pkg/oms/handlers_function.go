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

// CreateFunctionRequest is the request body for creating a function.
type CreateFunctionRequest struct {
	Name       string          `json:"name"`
	SourceCode string          `json:"sourceCode"`
	Runtime    string          `json:"runtime,omitempty"`
	Signature  json.RawMessage `json:"signature,omitempty"`
	CreatedBy  string          `json:"createdBy,omitempty"`
}

// UpdateFunctionRequest is the request body for updating a function. Pointer
// fields distinguish "omit ⇒ preserve" from "send empty ⇒ clear" for the
// fields where that matters; bare strings/ints keep the legacy "empty ⇒
// preserve" semantics the original handler shipped with.
type UpdateFunctionRequest struct {
	Name       string           `json:"name,omitempty"`
	SourceCode string           `json:"sourceCode,omitempty"`
	Version    int              `json:"version,omitempty"`
	Runtime    *string          `json:"runtime,omitempty"`
	Signature  *json.RawMessage `json:"signature,omitempty"`
}

// CreateFunction handles POST /api/v2/ontologies/{ontologyApiName}/functions.
func (h *OMSHandler) CreateFunction(w http.ResponseWriter, r *http.Request) {
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

	var req CreateFunctionRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	if req.Name == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:name", map[string]string{
			"parameter": "name",
			"reason":    "name is required",
		}))
		return
	}
	if req.SourceCode == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:sourceCode", map[string]string{
			"parameter": "sourceCode",
			"reason":    "sourceCode is required",
		}))
		return
	}

	fn := &Function{
		RID:         rid.NewFunctionRID(),
		OntologyRID: ontologyRID,
		Name:        req.Name,
		Version:     1,
		SourceCode:  req.SourceCode,
		Runtime:     req.Runtime,
		Signature:   req.Signature,
		CreatedBy:   req.CreatedBy,
	}
	if err := fn.Validate(); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:function", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	fn.Runtime = fn.NormalisedRuntime()

	if err := h.repo.CreateFunction(r.Context(), fn); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("FunctionAlreadyExists", map[string]string{
				"name": req.Name,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CreateFunctionFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, fn)
}

// ListFunctions handles GET /api/v2/ontologies/{ontologyApiName}/functions.
func (h *OMSHandler) ListFunctions(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")

	list, err := h.repo.ListFunctions(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListFunctionsFailed", nil))
		return
	}

	if list == nil {
		list = []Function{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": list,
	})
}

// GetFunctionV2 handles GET /api/v2/ontologies/{ontologyApiName}/functions/{functionRid}.
func (h *OMSHandler) GetFunctionV2(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	fnIdentifier := chi.URLParam(r, "functionRid")

	fn, err := h.repo.GetFunctionByName(r.Context(), ontologyRID, fnIdentifier)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("FunctionNotFound", map[string]string{
				"functionRid": fnIdentifier,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetFunctionFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, fn)
}

// UpdateFunction handles PUT /api/v2/ontologies/{ontologyApiName}/functions/{functionRid}.
func (h *OMSHandler) UpdateFunction(w http.ResponseWriter, r *http.Request) {
	fnRID := chi.URLParam(r, "functionRid")

	var req UpdateFunctionRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	existing, err := h.repo.GetFunction(r.Context(), fnRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("FunctionNotFound", map[string]string{
				"functionRid": fnRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetFunctionFailed", nil))
		return
	}

	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.SourceCode != "" {
		existing.SourceCode = req.SourceCode
	}
	if req.Version > 0 {
		existing.Version = req.Version
	}
	if req.Runtime != nil {
		existing.Runtime = *req.Runtime
	}
	if req.Signature != nil {
		existing.Signature = *req.Signature
	}
	if err := existing.Validate(); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:function", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	existing.Runtime = existing.NormalisedRuntime()

	if err := h.repo.UpdateFunction(r.Context(), existing); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("FunctionNotFound", map[string]string{
				"functionRid": fnRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("UpdateFunctionFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, existing)
}

// DeleteFunction handles DELETE /api/v2/ontologies/{ontologyApiName}/functions/{functionRid}.
func (h *OMSHandler) DeleteFunction(w http.ResponseWriter, r *http.Request) {
	fnRID := chi.URLParam(r, "functionRid")

	if err := h.repo.DeleteFunction(r.Context(), fnRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("FunctionNotFound", map[string]string{
				"functionRid": fnRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DeleteFunctionFailed", nil))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
