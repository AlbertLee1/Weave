package oms

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/rid"
)

// CreateBranchRequest is the request body for creating a branch.
type CreateBranchRequest struct {
	Name      string `json:"name"`
	CreatedBy string `json:"createdBy,omitempty"`
}

// BranchDetailResponse extends OntologyBranch with a change count.
type BranchDetailResponse struct {
	OntologyBranch
	ChangeCount int `json:"changeCount"`
}

// CreateBranch handles POST /api/v2/ontologies/{ontologyApiName}/branches.
func (h *OMSHandler) CreateBranch(w http.ResponseWriter, r *http.Request) {
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

	var req CreateBranchRequest
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

	// Snapshot current ontology version as base_version
	version, err := h.repo.GetOntologyVersion(r.Context(), ontologyRID)
	if err != nil {
		version = 0
	}

	branch := &OntologyBranch{
		ID:          rid.NewBranchRID(),
		OntologyRID: ontologyRID,
		Name:        req.Name,
		BaseVersion: int64(version),
		Status:      "open",
		CreatedBy:   req.CreatedBy,
	}

	if err := h.repo.CreateBranch(r.Context(), branch); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("BranchAlreadyExists", map[string]string{
				"name": req.Name,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CreateBranchFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, branch)
}

// ListBranches handles GET /api/v2/ontologies/{ontologyApiName}/branches.
func (h *OMSHandler) ListBranches(w http.ResponseWriter, r *http.Request) {
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

	list, err := h.repo.ListBranches(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListBranchesFailed", nil))
		return
	}

	// Filter to open branches only
	var open []OntologyBranch
	for _, b := range list {
		if b.Status == "open" {
			open = append(open, b)
		}
	}
	if open == nil {
		open = []OntologyBranch{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": open,
	})
}

// GetBranch handles GET /api/v2/ontologies/{ontologyApiName}/branches/{branchId}.
func (h *OMSHandler) GetBranch(w http.ResponseWriter, r *http.Request) {
	branchID := chi.URLParam(r, "branchId")

	branch, err := h.repo.GetBranch(r.Context(), branchID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("BranchNotFound", map[string]string{
				"branchId": branchID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetBranchFailed", nil))
		return
	}

	changes, err := h.repo.ListBranchChanges(r.Context(), branchID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListBranchChangesFailed", nil))
		return
	}

	resp := BranchDetailResponse{
		OntologyBranch: *branch,
		ChangeCount:    len(changes),
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// CloseBranch handles DELETE /api/v2/ontologies/{ontologyApiName}/branches/{branchId}.
func (h *OMSHandler) CloseBranch(w http.ResponseWriter, r *http.Request) {
	branchID := chi.URLParam(r, "branchId")

	if err := h.repo.CloseBranch(r.Context(), branchID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("BranchNotFound", map[string]string{
				"branchId": branchID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CloseBranchFailed", nil))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
