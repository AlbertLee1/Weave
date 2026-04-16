package oms

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/rid"
)

// CreateProposalRequest is the request body for creating a proposal.
type CreateProposalRequest struct {
	BranchID    string `json:"branchId"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
}

// ProposalDetailResponse extends OntologyProposal with reviews.
type ProposalDetailResponse struct {
	OntologyProposal
	Reviews []ProposalReview `json:"reviews"`
}

// CreateProposal handles POST /api/v2/ontologies/{ontologyApiName}/proposals.
func (h *OMSHandler) CreateProposal(w http.ResponseWriter, r *http.Request) {
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

	var req CreateProposalRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	if req.BranchID == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:branchId", map[string]string{
			"parameter": "branchId",
			"reason":    "branchId is required",
		}))
		return
	}

	if req.Title == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:title", map[string]string{
			"parameter": "title",
			"reason":    "title is required",
		}))
		return
	}

	// Validate branch exists and is open
	branch, err := h.repo.GetBranch(r.Context(), req.BranchID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("BranchNotFound", map[string]string{
				"branchId": req.BranchID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetBranchFailed", nil))
		return
	}

	if branch.Status != "open" {
		apierror.WriteJSON(w, apierror.NewConflict("BranchNotOpen", map[string]string{
			"branchId": req.BranchID,
			"status":   branch.Status,
		}))
		return
	}

	proposal := &OntologyProposal{
		ID:          rid.NewProposalRID(),
		BranchID:    req.BranchID,
		OntologyRID: ontologyRID,
		Title:       req.Title,
		Description: req.Description,
		Status:      "pending",
		Author:      req.Author,
	}

	if err := h.repo.CreateProposal(r.Context(), proposal); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("ProposalAlreadyExists", map[string]string{
				"title": req.Title,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CreateProposalFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, proposal)
}

// ListProposals handles GET /api/v2/ontologies/{ontologyApiName}/proposals.
func (h *OMSHandler) ListProposals(w http.ResponseWriter, r *http.Request) {
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

	list, err := h.repo.ListProposals(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListProposalsFailed", nil))
		return
	}

	// Apply optional status filter
	statusFilter := r.URL.Query().Get("status")
	var filtered []OntologyProposal
	for _, p := range list {
		if statusFilter == "" || p.Status == statusFilter {
			filtered = append(filtered, p)
		}
	}
	if filtered == nil {
		filtered = []OntologyProposal{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": filtered,
	})
}

// ReviewRequest is the request body for approving or rejecting a proposal.
type ReviewRequest struct {
	Reviewer string `json:"reviewer"`
	Reason   string `json:"reason,omitempty"`
}

// ApproveProposal handles POST /api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/approve.
func (h *OMSHandler) ApproveProposal(w http.ResponseWriter, r *http.Request) {
	h.reviewProposal(w, r, "approve")
}

// RejectProposal handles POST /api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/reject.
func (h *OMSHandler) RejectProposal(w http.ResponseWriter, r *http.Request) {
	h.reviewProposal(w, r, "reject")
}

// reviewProposal is the shared implementation for approve and reject.
func (h *OMSHandler) reviewProposal(w http.ResponseWriter, r *http.Request, decision string) {
	proposalID := chi.URLParam(r, "proposalId")

	var req ReviewRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	if req.Reviewer == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:reviewer", map[string]string{
			"parameter": "reviewer",
			"reason":    "reviewer is required",
		}))
		return
	}

	// Load proposal
	proposal, err := h.repo.GetProposal(r.Context(), proposalID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ProposalNotFound", map[string]string{
				"proposalId": proposalID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetProposalFailed", nil))
		return
	}

	// Only pending or approved proposals can be reviewed
	if proposal.Status != "pending" && proposal.Status != "approved" {
		apierror.WriteJSON(w, apierror.NewConflict("ProposalNotReviewable", map[string]string{
			"proposalId": proposalID,
			"status":     proposal.Status,
		}))
		return
	}

	// Self-review check
	if req.Reviewer == proposal.Author {
		apierror.WriteJSON(w, apierror.NewPermissionDenied("SelfReviewNotAllowed", map[string]string{
			"reason": "proposal author cannot review their own proposal",
		}))
		return
	}

	// Create the review
	review := &ProposalReview{
		ID:         rid.NewProposalReviewRID(),
		ProposalID: proposalID,
		Reviewer:   req.Reviewer,
		Decision:   decision,
		Reason:     req.Reason,
	}
	if err := h.repo.CreateProposalReview(r.Context(), review); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("CreateReviewFailed", nil))
		return
	}

	// Recalculate proposal status from all reviews
	reviews, err := h.repo.ListProposalReviews(r.Context(), proposalID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListReviewsFailed", nil))
		return
	}

	newStatus := computeProposalStatus(reviews)
	if newStatus != proposal.Status {
		if err := h.repo.UpdateProposalStatus(r.Context(), proposalID, newStatus); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("UpdateProposalStatusFailed", nil))
			return
		}
		proposal.Status = newStatus
	}

	httputil.WriteJSON(w, http.StatusOK, proposal)
}

// computeProposalStatus determines the proposal status from its reviews.
// Any rejection → rejected; at least 1 approval and no rejections → approved.
func computeProposalStatus(reviews []ProposalReview) string {
	approvals := 0
	for _, r := range reviews {
		if r.Decision == "reject" {
			return "rejected"
		}
		if r.Decision == "approve" {
			approvals++
		}
	}
	if approvals > 0 {
		return "approved"
	}
	return "pending"
}

// MergeConflict represents a conflict detected during branch merge.
type MergeConflict struct {
	EntityType  string          `json:"entityType"`
	EntityRID   string          `json:"entityRid"`
	ChangeType  string          `json:"changeType"`
	BranchState json.RawMessage `json:"branchState,omitempty"`
	MainState   json.RawMessage `json:"mainState,omitempty"`
}

// MergeProposal handles POST /api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/merge.
func (h *OMSHandler) MergeProposal(w http.ResponseWriter, r *http.Request) {
	proposalID := chi.URLParam(r, "proposalId")

	// Load proposal
	proposal, err := h.repo.GetProposal(r.Context(), proposalID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ProposalNotFound", map[string]string{
				"proposalId": proposalID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetProposalFailed", nil))
		return
	}

	// Only approved proposals can be merged
	if proposal.Status != "approved" {
		apierror.WriteJSON(w, apierror.NewConflict("ProposalNotApproved", map[string]string{
			"proposalId": proposalID,
			"status":     proposal.Status,
		}))
		return
	}

	// Load branch
	branch, err := h.repo.GetBranch(r.Context(), proposal.BranchID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("GetBranchFailed", nil))
		return
	}

	// Load branch changes
	changes, err := h.repo.ListBranchChanges(r.Context(), branch.ID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListBranchChangesFailed", nil))
		return
	}

	// Conflict detection: if main was modified since branch was created
	currentVersion, err := h.repo.GetOntologyVersion(r.Context(), proposal.OntologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("GetOntologyVersionFailed", nil))
		return
	}

	if int64(currentVersion) > branch.BaseVersion {
		conflicts, err := h.detectConflicts(r.Context(), changes)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("DetectConflictsFailed", nil))
			return
		}
		if len(conflicts) > 0 {
			httputil.WriteJSON(w, http.StatusConflict, map[string]interface{}{
				"errorCode": "MERGE_CONFLICT",
				"conflicts": conflicts,
			})
			return
		}
	}

	// Apply branch changes to main tables
	if err := h.applyBranchChanges(r.Context(), proposal.OntologyRID, changes); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ApplyBranchChangesFailed", nil))
		return
	}

	// Increment ontology version
	newVersion, err := h.repo.IncrementOntologyVersion(r.Context(), proposal.OntologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("IncrementVersionFailed", nil))
		return
	}

	// Create snapshot recording the merge
	snapshotData, _ := json.Marshal(map[string]interface{}{
		"mergedProposalId": proposal.ID,
		"mergedBranchId":   branch.ID,
		"changesCount":     len(changes),
	})
	_ = h.repo.CreateSnapshot(r.Context(), &OntologySnapshot{
		OntologyRID: proposal.OntologyRID,
		Version:     newVersion,
		Label:       "merge:" + proposal.Title,
		Snapshot:    snapshotData,
		CreatedBy:   "system",
	})

	// Mark proposal and branch as merged
	if err := h.repo.UpdateProposalStatus(r.Context(), proposalID, "merged"); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("UpdateProposalStatusFailed", nil))
		return
	}
	if err := h.repo.UpdateBranchStatus(r.Context(), branch.ID, "merged"); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("UpdateBranchStatusFailed", nil))
		return
	}

	proposal.Status = "merged"
	httputil.WriteJSON(w, http.StatusOK, proposal)
}

// detectConflicts checks if any MODIFIED or DELETED branch changes conflict
// with the current main state. A conflict exists when the entity's current
// state on main differs from the branch change's before_state.
func (h *OMSHandler) detectConflicts(ctx context.Context, changes []BranchChange) ([]MergeConflict, error) {
	var conflicts []MergeConflict

	for _, c := range changes {
		if c.ChangeType == "ADDED" {
			continue // ADDED entities can't conflict with main
		}

		// Fetch current entity from main
		currentJSON, err := h.fetchEntityJSON(ctx, c.EntityType, c.EntityRID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				// Entity was deleted on main — conflict for MODIFIED, not for DELETED
				if c.ChangeType == "MODIFIED" {
					conflicts = append(conflicts, MergeConflict{
						EntityType:  c.EntityType,
						EntityRID:   c.EntityRID,
						ChangeType:  c.ChangeType,
						BranchState: c.BeforeState,
					})
				}
				continue
			}
			return nil, err
		}

		// Compare current main state with the branch's recorded before_state
		if !jsonEqual(c.BeforeState, currentJSON) {
			conflicts = append(conflicts, MergeConflict{
				EntityType:  c.EntityType,
				EntityRID:   c.EntityRID,
				ChangeType:  c.ChangeType,
				BranchState: c.BeforeState,
				MainState:   currentJSON,
			})
		}
	}

	return conflicts, nil
}

// fetchEntityJSON retrieves the current entity from main and marshals to JSON.
func (h *OMSHandler) fetchEntityJSON(ctx context.Context, entityType, entityRID string) (json.RawMessage, error) {
	var entity interface{}
	var err error

	switch entityType {
	case "objectType":
		entity, err = h.repo.GetObjectType(ctx, entityRID)
	case "property":
		entity, err = h.repo.GetProperty(ctx, entityRID)
	case "linkType":
		entity, err = h.repo.GetLinkType(ctx, entityRID)
	case "actionType":
		entity, err = h.repo.GetActionType(ctx, entityRID)
	default:
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(entity)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// applyBranchChanges applies each branch change to the main tables.
// ontologyRID is used to restore json:"-" fields lost during serialization.
func (h *OMSHandler) applyBranchChanges(ctx context.Context, ontologyRID string, changes []BranchChange) error {
	for _, c := range changes {
		switch c.ChangeType {
		case "ADDED":
			if err := h.applyAdd(ctx, ontologyRID, c.EntityType, c.AfterState); err != nil {
				return err
			}
		case "MODIFIED":
			if err := h.applyModify(ctx, ontologyRID, c.EntityType, c.AfterState); err != nil {
				return err
			}
		case "DELETED":
			if err := h.applyDelete(ctx, c.EntityType, c.EntityRID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *OMSHandler) applyAdd(ctx context.Context, ontologyRID, entityType string, afterState json.RawMessage) error {
	switch entityType {
	case "objectType":
		var ot ObjectType
		if err := json.Unmarshal(afterState, &ot); err != nil {
			return err
		}
		ot.OntologyRID = ontologyRID // restore json:"-" field
		return h.repo.CreateObjectType(ctx, &ot)
	case "property":
		var p Property
		if err := json.Unmarshal(afterState, &p); err != nil {
			return err
		}
		return h.repo.CreateProperty(ctx, &p)
	case "linkType":
		var lt LinkType
		if err := json.Unmarshal(afterState, &lt); err != nil {
			return err
		}
		lt.OntologyRID = ontologyRID // restore json:"-" field
		return h.repo.CreateLinkType(ctx, &lt)
	case "actionType":
		var at ActionType
		if err := json.Unmarshal(afterState, &at); err != nil {
			return err
		}
		at.OntologyRID = ontologyRID // restore json:"-" field
		return h.repo.CreateActionType(ctx, &at)
	}
	return nil
}

func (h *OMSHandler) applyModify(ctx context.Context, ontologyRID, entityType string, afterState json.RawMessage) error {
	switch entityType {
	case "objectType":
		var ot ObjectType
		if err := json.Unmarshal(afterState, &ot); err != nil {
			return err
		}
		ot.OntologyRID = ontologyRID // restore json:"-" field
		return h.repo.UpdateObjectType(ctx, &ot)
	case "property":
		var p Property
		if err := json.Unmarshal(afterState, &p); err != nil {
			return err
		}
		return h.repo.UpdateProperty(ctx, &p)
	case "linkType":
		var lt LinkType
		if err := json.Unmarshal(afterState, &lt); err != nil {
			return err
		}
		lt.OntologyRID = ontologyRID // restore json:"-" field
		return h.repo.UpdateLinkType(ctx, &lt)
	case "actionType":
		var at ActionType
		if err := json.Unmarshal(afterState, &at); err != nil {
			return err
		}
		at.OntologyRID = ontologyRID // restore json:"-" field
		return h.repo.UpdateActionType(ctx, &at)
	}
	return nil
}

func (h *OMSHandler) applyDelete(ctx context.Context, entityType, entityRID string) error {
	switch entityType {
	case "objectType":
		return h.repo.DeleteObjectType(ctx, entityRID)
	case "property":
		return h.repo.DeleteProperty(ctx, entityRID)
	case "linkType":
		return h.repo.DeleteLinkType(ctx, entityRID)
	case "actionType":
		return h.repo.DeleteActionType(ctx, entityRID)
	}
	return nil
}

// jsonEqual compares two JSON payloads semantically (order-independent).
func jsonEqual(a, b json.RawMessage) bool {
	var av, bv interface{}
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}

// GetProposal handles GET /api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}.
func (h *OMSHandler) GetProposal(w http.ResponseWriter, r *http.Request) {
	proposalID := chi.URLParam(r, "proposalId")

	proposal, err := h.repo.GetProposal(r.Context(), proposalID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ProposalNotFound", map[string]string{
				"proposalId": proposalID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetProposalFailed", nil))
		return
	}

	reviews, err := h.repo.ListProposalReviews(r.Context(), proposalID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListProposalReviewsFailed", nil))
		return
	}
	if reviews == nil {
		reviews = []ProposalReview{}
	}

	resp := ProposalDetailResponse{
		OntologyProposal: *proposal,
		Reviews:          reviews,
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}
