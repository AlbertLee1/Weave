package oms

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
)

// US-385 — Branch 合并冲突检测.
//
// PostBranchDiff and MergeBranch are the explicit branch-merge surfaces that
// sit alongside the proposal-based merge flow (MergeProposal). They expose:
//
//   - POST /branches/{branchId}/diff — categorised diff (added/modified/
//     deleted) with conflict annotations for entries whose main-side state
//     drifted since the branch's recorded before_state.
//   - POST /branches/{branchId}/merge — direct merge with explicit
//     conflictResolution: { "<entityType>:<apiName>": "use-branch"|"use-main" }.
//
// Conflict identity uses "<entityType>:<apiName>" so callers can disambiguate
// across entityTypes that share a bare apiName (an objectType named "Order"
// vs an actionType named "Order"). For DELETED changes the apiName is read
// from BeforeState; for ADDED / MODIFIED it comes from AfterState.

// AnnotatedDiffEntry extends BranchDiffEntry with the entity's apiName, the
// stable "<entityType>:<apiName>" resolution key, and a conflict flag.
type AnnotatedDiffEntry struct {
	EntityType    string          `json:"entityType"`
	EntityRID     string          `json:"entityRid"`
	APIName       string          `json:"apiName"`
	ResolutionKey string          `json:"resolutionKey"`
	ChangeType    string          `json:"changeType"`
	HasConflict   bool            `json:"hasConflict"`
	Before        json.RawMessage `json:"before,omitempty"`
	After         json.RawMessage `json:"after,omitempty"`
}

// AnnotatedMergeConflict mirrors MergeConflict but includes the apiName + the
// resolution key the merge handler will look up in conflictResolution.
type AnnotatedMergeConflict struct {
	EntityType    string          `json:"entityType"`
	EntityRID     string          `json:"entityRid"`
	APIName       string          `json:"apiName"`
	ResolutionKey string          `json:"resolutionKey"`
	ChangeType    string          `json:"changeType"`
	BranchState   json.RawMessage `json:"branchState,omitempty"`
	MainState     json.RawMessage `json:"mainState,omitempty"`
}

// BranchDiffPostResponse is the wire shape returned by POST /diff.
type BranchDiffPostResponse struct {
	Branch       *OntologyBranch          `json:"branch"`
	Added        []AnnotatedDiffEntry     `json:"added"`
	Modified     []AnnotatedDiffEntry     `json:"modified"`
	Deleted      []AnnotatedDiffEntry     `json:"deleted"`
	Conflicts    []AnnotatedMergeConflict `json:"conflicts"`
	HasConflicts bool                     `json:"hasConflicts"`
}

// MergeBranchRequest is the body of POST /merge.
type MergeBranchRequest struct {
	// ConflictResolution maps "<entityType>:<apiName>" to "use-branch" or
	// "use-main". Keys absent from the map are treated as unresolved
	// conflicts — the handler returns 409 with the conflict list rather than
	// silently picking a side.
	ConflictResolution map[string]string `json:"conflictResolution,omitempty"`
}

const (
	resolutionUseBranch = "use-branch"
	resolutionUseMain   = "use-main"
)

// MergeBranchResponse is the wire shape returned by POST /merge.
type MergeBranchResponse struct {
	Branch       *OntologyBranch `json:"branch"`
	AppliedCount int             `json:"appliedCount"`
	SkippedCount int             `json:"skippedCount"`
}

// PostBranchDiff handles POST /api/v2/ontologies/{ontologyApiName}/branches/{branchId}/diff.
func (h *OMSHandler) PostBranchDiff(w http.ResponseWriter, r *http.Request) {
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

	resp, err := h.buildBranchDiff(r.Context(), branch, changes)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("DetectConflictsFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// buildBranchDiff is the shared core for both diff and merge handlers.
func (h *OMSHandler) buildBranchDiff(ctx context.Context, branch *OntologyBranch, changes []BranchChange) (*BranchDiffPostResponse, error) {
	// Resolve per-change conflict status by re-running detectConflicts on
	// the live change set. detectConflicts itself returns the MergeConflict
	// shape (without apiName) — we annotate here.
	conflicts, err := h.detectConflicts(ctx, changes)
	if err != nil {
		return nil, err
	}
	conflictByRID := make(map[string]MergeConflict, len(conflicts))
	for _, c := range conflicts {
		conflictByRID[c.EntityRID] = c
	}

	resp := &BranchDiffPostResponse{
		Branch:    branch,
		Added:     []AnnotatedDiffEntry{},
		Modified:  []AnnotatedDiffEntry{},
		Deleted:   []AnnotatedDiffEntry{},
		Conflicts: []AnnotatedMergeConflict{},
	}

	for _, c := range changes {
		api := apiNameFromChange(c)
		key := resolutionKey(c.EntityType, api)
		_, hasConflict := conflictByRID[c.EntityRID]
		entry := AnnotatedDiffEntry{
			EntityType:    c.EntityType,
			EntityRID:     c.EntityRID,
			APIName:       api,
			ResolutionKey: key,
			ChangeType:    c.ChangeType,
			HasConflict:   hasConflict,
			Before:        c.BeforeState,
			After:         c.AfterState,
		}
		switch c.ChangeType {
		case "ADDED":
			resp.Added = append(resp.Added, entry)
		case "MODIFIED":
			resp.Modified = append(resp.Modified, entry)
		case "DELETED":
			resp.Deleted = append(resp.Deleted, entry)
		}
	}

	for _, c := range conflicts {
		// Pull apiName from whichever side actually carries the entity
		// payload — branch state on MODIFIED, branch state on DELETED, or
		// (rare) main state if the branch payload is missing.
		api := apiNameFromState(c.BranchState)
		if api == "" {
			api = apiNameFromState(c.MainState)
		}
		resp.Conflicts = append(resp.Conflicts, AnnotatedMergeConflict{
			EntityType:    c.EntityType,
			EntityRID:     c.EntityRID,
			APIName:       api,
			ResolutionKey: resolutionKey(c.EntityType, api),
			ChangeType:    c.ChangeType,
			BranchState:   c.BranchState,
			MainState:     c.MainState,
		})
	}
	resp.HasConflicts = len(resp.Conflicts) > 0

	stableSort := func(s []AnnotatedDiffEntry) {
		sort.SliceStable(s, func(i, j int) bool {
			if s[i].EntityType != s[j].EntityType {
				return s[i].EntityType < s[j].EntityType
			}
			return s[i].APIName < s[j].APIName
		})
	}
	stableSort(resp.Added)
	stableSort(resp.Modified)
	stableSort(resp.Deleted)
	sort.SliceStable(resp.Conflicts, func(i, j int) bool {
		return resp.Conflicts[i].ResolutionKey < resp.Conflicts[j].ResolutionKey
	})
	return resp, nil
}

// MergeBranch handles POST /api/v2/ontologies/{ontologyApiName}/branches/{branchId}/merge.
func (h *OMSHandler) MergeBranch(w http.ResponseWriter, r *http.Request) {
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

	if branch.Status != "open" {
		apierror.WriteJSON(w, apierror.NewConflict("BranchNotOpen", map[string]string{
			"branchId": branchID,
			"status":   branch.Status,
		}))
		return
	}

	var req MergeBranchRequest
	if r.ContentLength > 0 {
		if err := httputil.ReadJSON(r, &req); err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
				"reason": "invalid JSON",
			}))
			return
		}
	}

	for k, v := range req.ConflictResolution {
		if v != resolutionUseBranch && v != resolutionUseMain {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidConflictResolution", map[string]string{
				"resolutionKey": k,
				"value":         v,
				"allowed":       resolutionUseBranch + "|" + resolutionUseMain,
			}))
			return
		}
	}

	changes, err := h.repo.ListBranchChanges(r.Context(), branchID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListBranchChangesFailed", nil))
		return
	}

	conflicts, err := h.detectConflicts(r.Context(), changes)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("DetectConflictsFailed", nil))
		return
	}

	if len(conflicts) > 0 {
		// Build the annotated conflict list once and reuse it for the
		// unresolved-check + the 409 payload.
		annotated := make([]AnnotatedMergeConflict, 0, len(conflicts))
		unresolved := make([]AnnotatedMergeConflict, 0)
		for _, c := range conflicts {
			api := apiNameFromState(c.BranchState)
			if api == "" {
				api = apiNameFromState(c.MainState)
			}
			ac := AnnotatedMergeConflict{
				EntityType:    c.EntityType,
				EntityRID:     c.EntityRID,
				APIName:       api,
				ResolutionKey: resolutionKey(c.EntityType, api),
				ChangeType:    c.ChangeType,
				BranchState:   c.BranchState,
				MainState:     c.MainState,
			}
			annotated = append(annotated, ac)
			if _, ok := req.ConflictResolution[ac.ResolutionKey]; !ok {
				unresolved = append(unresolved, ac)
			}
		}
		if len(unresolved) > 0 {
			httputil.WriteJSON(w, http.StatusConflict, map[string]interface{}{
				"errorCode":  "MERGE_CONFLICT",
				"conflicts":  annotated,
				"unresolved": unresolved,
			})
			return
		}
	}

	// Index conflicts by EntityRID so per-change apply/skip decisions can
	// look up their resolution key without re-deriving apiName.
	conflictKeys := make(map[string]string, len(conflicts))
	for _, c := range conflicts {
		api := apiNameFromState(c.BranchState)
		if api == "" {
			api = apiNameFromState(c.MainState)
		}
		conflictKeys[c.EntityRID] = resolutionKey(c.EntityType, api)
	}

	applied := 0
	skipped := 0
	for _, c := range changes {
		// If this change is a conflict, honour the resolution; otherwise
		// always apply.
		if key, isConflict := conflictKeys[c.EntityRID]; isConflict {
			res := req.ConflictResolution[key]
			if res == resolutionUseMain {
				skipped++
				continue
			}
			// "use-branch" → fall through to apply.
		}
		if err := h.applyBranchChange(r.Context(), branch.OntologyRID, c); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("ApplyBranchChangeFailed", nil))
			return
		}
		applied++
	}

	if applied > 0 {
		if _, err := h.repo.IncrementOntologyVersion(r.Context(), branch.OntologyRID); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("IncrementVersionFailed", nil))
			return
		}
	}

	if err := h.repo.UpdateBranchStatus(r.Context(), branchID, "merged"); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("UpdateBranchStatusFailed", nil))
		return
	}
	branch.Status = "merged"

	httputil.WriteJSON(w, http.StatusOK, MergeBranchResponse{
		Branch:       branch,
		AppliedCount: applied,
		SkippedCount: skipped,
	})
}

// applyBranchChange dispatches a single BranchChange against main, mirroring
// applyBranchChanges but exposing per-change failure so the merge loop can
// stop cleanly on the first error.
func (h *OMSHandler) applyBranchChange(ctx context.Context, ontologyRID string, c BranchChange) error {
	switch c.ChangeType {
	case "ADDED":
		return h.applyAdd(ctx, ontologyRID, c.EntityType, c.AfterState)
	case "MODIFIED":
		return h.applyModify(ctx, ontologyRID, c.EntityType, c.AfterState)
	case "DELETED":
		return h.applyDelete(ctx, c.EntityType, c.EntityRID)
	}
	return nil
}

// apiNameFromChange picks the right state to source apiName from based on
// changeType — ADDED/MODIFIED carry it on AfterState, DELETED on BeforeState.
func apiNameFromChange(c BranchChange) string {
	if c.ChangeType == "DELETED" {
		return apiNameFromState(c.BeforeState)
	}
	if name := apiNameFromState(c.AfterState); name != "" {
		return name
	}
	return apiNameFromState(c.BeforeState)
}

// apiNameFromState extracts the entity's "apiName" from a JSON payload. All
// branch-tracked entity types (ObjectType / Property / LinkType / ActionType)
// expose apiName at the top level of their wire shape — see models.go.
func apiNameFromState(state json.RawMessage) string {
	if len(state) == 0 {
		return ""
	}
	var probe struct {
		APIName string `json:"apiName"`
	}
	if err := json.Unmarshal(state, &probe); err != nil {
		return ""
	}
	return probe.APIName
}

// resolutionKey is the canonical conflictResolution map key shape:
// "<entityType>:<apiName>". Empty apiName falls back to entityType only so
// the key remains parseable even when the state JSON lacked apiName.
// entityType is already lowercase by storage convention (objectType /
// actionType / linkType / property).
func resolutionKey(entityType, apiName string) string {
	if apiName == "" {
		return entityType
	}
	return entityType + ":" + apiName
}
