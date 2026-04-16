package oms

import (
	"errors"
	"net/http"

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
