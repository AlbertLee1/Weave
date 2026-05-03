package oms

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/rid"
)

// CreateBranchRequest is the request body for creating a branch.
type CreateBranchRequest struct {
	Name      string `json:"name"`
	CreatedBy string `json:"createdBy,omitempty"`
	// ParentBranchID (US-383) optionally chains the new branch off another
	// branch in the same ontology. When set, metadata reads under the new
	// branch fall back through the parent's overlay before consulting main.
	// Empty / omitted means the parent is the canonical "main" trunk.
	ParentBranchID string `json:"parentBranchId,omitempty"`
	// BaseTx (US-383) pins the new branch to a dataset_transactions tx_id
	// checkpoint (US-379). Empty means HEAD at branch creation. Validation
	// is loose (we accept any string) because dataset_transactions only
	// exists from migration 000090 onward — a pre-US-379 caller can't
	// supply a value but should still be able to create a branch.
	BaseTx string `json:"baseTx,omitempty"`
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

	// Validate parent_branch (if supplied) belongs to the same ontology and
	// is itself open — chaining off a merged/closed branch would silently
	// freeze stale overlays so we reject it up front.
	if req.ParentBranchID != "" {
		parent, err := h.repo.GetBranch(r.Context(), req.ParentBranchID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				apierror.WriteJSON(w, apierror.NewInvalidParameter("ParentBranchNotFound", map[string]string{
					"parentBranchId": req.ParentBranchID,
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("GetParentBranchFailed", nil))
			return
		}
		if parent.OntologyRID != ontologyRID {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("ParentBranchOntologyMismatch", map[string]string{
				"parentBranchId":   req.ParentBranchID,
				"parentOntologyId": parent.OntologyRID,
			}))
			return
		}
		if parent.Status != "open" {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("ParentBranchNotOpen", map[string]string{
				"parentBranchId": req.ParentBranchID,
				"status":         parent.Status,
			}))
			return
		}
	}

	// Snapshot current ontology version as base_version
	version, err := h.repo.GetOntologyVersion(r.Context(), ontologyRID)
	if err != nil {
		version = 0
	}

	branch := &OntologyBranch{
		ID:             rid.NewBranchRID(),
		OntologyRID:    ontologyRID,
		Name:           req.Name,
		BaseVersion:    int64(version),
		ParentBranchID: req.ParentBranchID,
		BaseTx:         req.BaseTx,
		Status:         "open",
		CreatedBy:      req.CreatedBy,
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

// RebaseBranch handles POST /api/v2/ontologies/{ontologyApiName}/branches/{branchId}/rebase.
func (h *OMSHandler) RebaseBranch(w http.ResponseWriter, r *http.Request) {
	branchID := chi.URLParam(r, "branchId")

	// Load branch
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

	// Only open branches can be rebased
	if branch.Status != "open" {
		apierror.WriteJSON(w, apierror.NewConflict("BranchNotOpen", map[string]string{
			"branchId": branchID,
			"status":   branch.Status,
		}))
		return
	}

	// Get current main version
	currentVersion, err := h.repo.GetOntologyVersion(r.Context(), branch.OntologyRID)
	if err != nil {
		currentVersion = 0
	}

	// Already up to date
	if int64(currentVersion) <= branch.BaseVersion {
		httputil.WriteJSON(w, http.StatusOK, branch)
		return
	}

	// Load branch changes
	changes, err := h.repo.ListBranchChanges(r.Context(), branchID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListBranchChangesFailed", nil))
		return
	}

	// Detect conflicts using the same logic as merge
	conflicts, err := h.detectConflicts(r.Context(), changes)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("DetectConflictsFailed", nil))
		return
	}

	if len(conflicts) > 0 {
		httputil.WriteJSON(w, http.StatusConflict, map[string]interface{}{
			"errorCode": "REBASE_CONFLICT",
			"conflicts": conflicts,
		})
		return
	}

	// No conflicts — update before_state for MODIFIED/DELETED changes to current main state
	for _, c := range changes {
		if c.ChangeType == "ADDED" {
			continue
		}
		currentJSON, err := h.fetchEntityJSON(r.Context(), c.EntityType, c.EntityRID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue // entity deleted on main — already handled by conflict detection
			}
			apierror.WriteJSON(w, apierror.NewInternal("FetchEntityFailed", nil))
			return
		}
		if err := h.repo.UpdateBranchChangeBeforeState(r.Context(), c.ID, currentJSON); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("UpdateBeforeStateFailed", nil))
			return
		}
	}

	// Update base version
	if err := h.repo.UpdateBranchBaseVersion(r.Context(), branchID, int64(currentVersion)); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("UpdateBranchBaseVersionFailed", nil))
		return
	}

	branch.BaseVersion = int64(currentVersion)
	httputil.WriteJSON(w, http.StatusOK, branch)
}

// BranchDiffEntry represents a single change in a branch diff.
type BranchDiffEntry struct {
	EntityType string          `json:"entityType"`
	EntityRID  string          `json:"entityRid"`
	ChangeType string          `json:"changeType"`
	Before     json.RawMessage `json:"before"`
	After      json.RawMessage `json:"after"`
}

// GetBranchDiff handles GET /api/v2/ontologies/{ontologyApiName}/branches/{branchId}/diff.
func (h *OMSHandler) GetBranchDiff(w http.ResponseWriter, r *http.Request) {
	branchID := chi.URLParam(r, "branchId")

	if _, err := h.repo.GetBranch(r.Context(), branchID); err != nil {
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

	entries := make([]BranchDiffEntry, len(changes))
	for i, c := range changes {
		entries[i] = BranchDiffEntry{
			EntityType: c.EntityType,
			EntityRID:  c.EntityRID,
			ChangeType: c.ChangeType,
			Before:     c.BeforeState,
			After:      c.AfterState,
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].EntityType != entries[j].EntityType {
			return entries[i].EntityType < entries[j].EntityType
		}
		return entries[i].ChangeType < entries[j].ChangeType
	})

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": entries,
	})
}

// GetBranchBreakingChanges handles
// GET /api/v2/ontologies/{ontologyApiName}/branches/{branchId}/breaking-changes.
// It returns a BreakingChangesReport covering property deletions, type
// narrowing, newly-required fields, and primary-key changes — annotated with
// the ActionTypes and SavedObjectSets that reference the affected schema.
func (h *OMSHandler) GetBranchBreakingChanges(w http.ResponseWriter, r *http.Request) {
	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	ontology, err := h.repo.GetOntology(r.Context(), ontologyAPIName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
				"ontologyApiName": ontologyAPIName,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ResolveOntologyFailed", nil))
		return
	}

	branchID := chi.URLParam(r, "branchId")
	if _, err := h.repo.GetBranch(r.Context(), branchID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("BranchNotFound", map[string]string{
				"branchId": branchID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetBranchFailed", nil))
		return
	}

	report, err := DetectBreakingChanges(r.Context(), h.repo, h.savedObjectSetLister, ontology.RID, ontology.APIName, branchID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("DetectBreakingChangesFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, report)
}
