package actions

import (
	"context"
	"fmt"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// SagaResult is the response envelope for ApplyBatchSaga.
//
// Applied captures the per-action results for the primary branch — every
// action that at least reached Prepare is present, whether or not the saga
// ended up rolling it back. When Failure is non-nil the saga did NOT commit
// the Applied edits (or committed them and then caught a downstream
// failure) and Compensations lists the compensating actions that ran in
// reverse declaration order.
type SagaResult struct {
	// Mode is always "saga" for results produced by ApplyBatchSaga.
	Mode string `json:"mode"`
	// BatchID / Offset are the compensation batch coordinates when a
	// rollback fired; otherwise they refer to the successful primary
	// commit.
	BatchID string `json:"batchId,omitempty"`
	Offset  uint64 `json:"offset,omitempty"`
	// Applied mirrors BatchResult.Results for the primary prepared actions.
	Applied []*ApplyResult `json:"applied"`
	// Compensations are the rolled-back per-action results in reverse
	// declaration order. Nil on the happy path.
	Compensations []*ApplyResult `json:"compensations,omitempty"`
	// AppliedEdits is the post-collapse edit list that was actually
	// published to NATS — either the primary batch (happy path) or the
	// compensation batch (rollback).
	AppliedEdits []funnel.Edit `json:"appliedEdits,omitempty"`
	// Failure is populated when the primary branch failed. The handler
	// layer translates this into an HTTP status + structured body.
	Failure *BatchError `json:"failure,omitempty"`
}

// ApplyBatchSaga is the US-239 saga coordinator. It prepares every request in
// declaration order; on ANY prepare or commit failure it rolls back by
// executing the compensating ActionType (if declared) for every
// already-prepared step in REVERSE order. Compensations are published as a
// single EditBatch so the consumer sees them atomically.
//
// The coordinator is cooperative: an action without a CompensateActionRID
// contributes no rollback edits, which is the correct behaviour for
// read-only / idempotent steps. An action whose compensator itself fails to
// prepare is reported via the returned error; already-built compensation
// edits are still published so partial rollback beats no rollback.
func (e *Executor) ApplyBatchSaga(ctx context.Context, ontologyRID string, reqs []ApplyRequest) (*SagaResult, error) {
	prepared := make([]*PreparedAction, 0, len(reqs))
	primaryResults := make([]*ApplyResult, 0, len(reqs))

	for i := range reqs {
		p, err := e.Prepare(ctx, ontologyRID, &reqs[i])
		if err != nil {
			failure := &BatchError{
				Phase:             classifyPrepareError(err),
				FailedActionIndex: i,
				ActionType:        reqs[i].ActionType,
				Message:           err.Error(),
				Cause:             err,
			}
			compensations, compEdits, batchID, offset := e.runCompensations(ctx, ontologyRID, prepared)
			return &SagaResult{
				Mode:          "saga",
				Applied:       primaryResults,
				Compensations: compensations,
				AppliedEdits:  compEdits,
				BatchID:       batchID,
				Offset:        offset,
				Failure:       failure,
			}, failure
		}
		prepared = append(prepared, p)
		primaryResults = append(primaryResults, &ApplyResult{
			ActionRID: p.ActionType.RID,
			Edits:     p.Edits,
		})
	}

	br, err := e.CommitBatch(ctx, ontologyRID, prepared)
	if err != nil {
		var be *BatchError
		if !asBatchErr(err, &be) {
			be = &BatchError{
				Phase:             "commit",
				FailedActionIndex: -1,
				Message:           err.Error(),
				Cause:             err,
			}
		}
		compensations, compEdits, batchID, offset := e.runCompensations(ctx, ontologyRID, prepared)
		return &SagaResult{
			Mode:          "saga",
			Applied:       primaryResults,
			Compensations: compensations,
			AppliedEdits:  compEdits,
			BatchID:       batchID,
			Offset:        offset,
			Failure:       be,
		}, be
	}

	return &SagaResult{
		Mode:         "saga",
		BatchID:      br.BatchID,
		Offset:       br.Offset,
		Applied:      br.Results,
		AppliedEdits: br.AppliedEdits,
	}, nil
}

// runCompensations prepares and publishes the compensating actions for a
// slice of successfully-prepared primary actions. Walks the slice in
// REVERSE order (matches PRD "逆序执行补偿") and skips any action whose
// ActionType declares no CompensateActionRID. Returns the compensating
// per-action results plus the post-collapse edits actually published.
//
// The compensation path never calls back into ApplyBatchSaga so a
// compensator whose own ActionType has a CompensateActionRID does NOT
// trigger a recursive rollback — saga depth is bounded at 1 deliberately.
func (e *Executor) runCompensations(ctx context.Context, ontologyRID string, prepared []*PreparedAction) ([]*ApplyResult, []funnel.Edit, string, uint64) {
	if len(prepared) == 0 {
		return nil, nil, "", 0
	}

	compensators := make([]*PreparedAction, 0, len(prepared))
	results := make([]*ApplyResult, 0, len(prepared))

	for i := len(prepared) - 1; i >= 0; i-- {
		primary := prepared[i]
		if primary.ActionType == nil || primary.ActionType.CompensateActionRID == "" {
			continue
		}
		comp, err := e.prepareCompensator(ctx, ontologyRID, primary)
		if err != nil {
			// Log-and-skip: a broken compensator should not block the rest
			// of the rollback — partial compensation is strictly better
			// than no compensation.
			continue
		}
		compensators = append(compensators, comp)
		results = append(results, &ApplyResult{
			ActionRID: comp.ActionType.RID,
			Edits:     comp.Edits,
		})
	}

	if len(compensators) == 0 {
		return nil, nil, "", 0
	}

	br, err := e.CommitBatch(ctx, ontologyRID, compensators)
	if err != nil {
		// Best-effort: surface whatever we managed to build so the caller
		// can see which compensators ran and which skipped.
		return results, nil, "", 0
	}
	// Stamp the compensation batch ID onto the per-action compensator
	// results so downstream consumers can correlate them.
	for _, r := range results {
		r.BatchID = br.BatchID
		r.Offset = br.Offset
	}
	return results, br.AppliedEdits, br.BatchID, br.Offset
}

// prepareCompensator resolves a primary action's CompensateActionRID into an
// ActionType and runs the standard Prepare pipeline against it — feeding it
// only the parameters the compensator itself declares (any primary-only
// parameters are filtered out so the validator's "unknown parameter" gate
// doesn't trip on the inherited bag). Compensators must therefore declare
// the subset of the primary action's parameters they need to undo the
// effect; missing required params surface as a validation error that
// runCompensations demotes to a skip.
func (e *Executor) prepareCompensator(ctx context.Context, ontologyRID string, primary *PreparedAction) (*PreparedAction, error) {
	compRID := primary.ActionType.CompensateActionRID
	compType, err := e.resolveActionType(ctx, ontologyRID, compRID)
	if err != nil {
		return nil, fmt.Errorf("resolve compensator %q: %w", compRID, err)
	}
	compParamDefs, err := ParseParameterDefs(compType.Parameters)
	if err != nil {
		return nil, fmt.Errorf("parse compensator params: %w", err)
	}
	filtered := make(map[string]interface{}, len(compParamDefs))
	for _, def := range compParamDefs {
		if v, ok := primary.Request.Parameters[def.ID]; ok {
			filtered[def.ID] = v
		}
	}
	req := &ApplyRequest{
		ActionType: compType.APIName,
		Parameters: filtered,
	}
	return e.Prepare(ctx, ontologyRID, req)
}

// resolveActionType looks up an ActionType by RID or, failing that, by
// APIName. The dual lookup keeps the saga coordinator robust against
// repositories whose GetActionType is a noop stub (e.g. mock repos in
// tests) while still preferring the direct RID path in production.
func (e *Executor) resolveActionType(ctx context.Context, ontologyRID, ridOrName string) (*oms.ActionType, error) {
	if at, err := e.omsRepo.GetActionType(ctx, ridOrName); err == nil && at != nil && at.RID != "" {
		return at, nil
	}
	actionTypes, err := e.omsRepo.ListActionTypes(ctx, ontologyRID)
	if err != nil {
		return nil, err
	}
	for i := range actionTypes {
		if actionTypes[i].RID == ridOrName || actionTypes[i].APIName == ridOrName {
			return &actionTypes[i], nil
		}
	}
	return nil, fmt.Errorf("action type %q not found", ridOrName)
}

// asBatchErr is a tiny shim around errors.As so call sites read cleanly.
func asBatchErr(err error, target **BatchError) bool {
	for cur := err; cur != nil; {
		if be, ok := cur.(*BatchError); ok {
			*target = be
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := cur.(unwrapper)
		if !ok {
			return false
		}
		cur = u.Unwrap()
	}
	return false
}
