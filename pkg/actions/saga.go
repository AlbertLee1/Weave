package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// SagaOptions controls per-invocation saga behaviour. IdempotencyKey is
// the dedupe token: when set, repeating the same key reads the prior
// result back instead of re-running. RequestedBy is recorded on the
// saga header row for audit; the executor falls back to the auth
// context user id when this is empty.
//
// US-469 CompensationStrategy chooses how runCompensations reacts to a
// compensator that fails: "best-effort" (default) skips the broken
// compensator (logging to DLQ + FailedCompensations) and continues the
// reverse walk so the rest of the prepared steps still get rolled back;
// "stop-on-first" stops walking as soon as one compensator fails (only
// the failure itself is enqueued to DLQ, but every later-walked step is
// recorded in FailedCompensations with phase="skipped" for operator
// visibility). Empty string defaults to best-effort.
type SagaOptions struct {
	IdempotencyKey       string
	RequestedBy          string
	CompensationStrategy SagaCompensationStrategy
}

// SagaCompensationStrategy is the US-469 enum controlling the rollback
// walk's reaction to a broken compensator.
type SagaCompensationStrategy string

const (
	// CompensationStrategyBestEffort attempts every compensator in
	// reverse order. A compensator that fails to prepare or commit is
	// logged + enqueued to DLQ but does NOT block the remaining
	// compensators from running. Pre-US-469 callers got this behaviour
	// implicitly; it remains the default when SagaOptions.CompensationStrategy
	// is the empty string.
	CompensationStrategyBestEffort SagaCompensationStrategy = "best-effort"
	// CompensationStrategyStopOnFirst halts the reverse walk as soon as
	// one compensator fails. The failure itself is enqueued to DLQ;
	// every subsequent (still-prepared) step is recorded in
	// FailedCompensations with phase="skipped" so an operator can see
	// what was left un-compensated, but no further publishes happen.
	CompensationStrategyStopOnFirst SagaCompensationStrategy = "stop-on-first"
)

// FailedCompensation phase constants used on FailedCompensationRef.Phase.
const (
	// FailedCompensationPhasePrepare is set when prepareCompensator failed
	// (e.g. the compensator's ActionType could not be resolved or its
	// rules could not be evaluated).
	FailedCompensationPhasePrepare = "prepare"
	// FailedCompensationPhaseCommit is set when prepareCompensator succeeded
	// but the compensation batch's CommitBatch / Publish failed.
	FailedCompensationPhaseCommit = "commit"
	// FailedCompensationPhaseSkipped is set under stop-on-first when a
	// prepared step's compensator was never attempted because the walk
	// halted on a prior failure.
	FailedCompensationPhaseSkipped = "skipped"
)

// FailedCompensationRef identifies a step whose compensator did not
// complete cleanly. It is the structured counterpart to the DLQEntries
// slice (which only carries raw DLQ row ids) — it tells the SDK / UI
// which primary step is affected and why, so callers can decide whether
// to retry, drop, or replay.
type FailedCompensationRef struct {
	// StepIndex is the zero-based index of the failing step in the
	// original saga request.
	StepIndex int `json:"stepIndex"`
	// StepID is the action_saga_steps row id when a SagaStore is wired;
	// empty in degraded mode.
	StepID string `json:"stepId,omitempty"`
	// ActionType is the primary (NOT compensator) action API name —
	// matches the StepIndex'th entry in the original ApplySagaRequest.
	ActionType string `json:"actionType"`
	// CompensateRID is the CompensateActionRID declared on the primary
	// ActionType. Empty if the primary action had no compensator
	// declared (skipped entries do not appear here either way).
	CompensateRID string `json:"compensateRid,omitempty"`
	// Phase is "prepare", "commit", or "skipped".
	Phase string `json:"phase"`
	// Reason is a human-readable description of the failure (or the
	// reason the walk skipped this step under stop-on-first).
	Reason string `json:"reason"`
	// DLQID is the action_saga_dlq.dlq_id for the row this failure
	// enqueued, when applicable. Empty for skipped entries (no DLQ row
	// is written for stop-on-first skips) and for degraded mode (no
	// SagaStore wired).
	DLQID string `json:"dlqId,omitempty"`
}

// NormalizeCompensationStrategy is the wire-side normaliser used by
// handlers + executor entry points: accepts case- and whitespace-loose
// variants of the two known values and rejects everything else with a
// descriptive error so the handler can surface a clean 400.
func NormalizeCompensationStrategy(s string) (SagaCompensationStrategy, error) {
	trimmed := strings.ToLower(strings.TrimSpace(s))
	switch trimmed {
	case "":
		return CompensationStrategyBestEffort, nil
	case string(CompensationStrategyBestEffort):
		return CompensationStrategyBestEffort, nil
	case string(CompensationStrategyStopOnFirst):
		return CompensationStrategyStopOnFirst, nil
	default:
		return "", fmt.Errorf("unknown compensationStrategy %q (expected %q or %q)",
			s, CompensationStrategyBestEffort, CompensationStrategyStopOnFirst)
	}
}

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
	// SagaID is the durable saga_id when the executor's SagaStore is
	// wired; empty in degraded mode (in-memory only).
	SagaID string `json:"sagaId,omitempty"`
	// Status mirrors the action_sagas.status column — SUCCESS,
	// COMPENSATED, or FAILED. Populated on the happy path AND on
	// failures so callers can distinguish "rolled back cleanly" from
	// "rollback itself failed and DLQ rows exist".
	Status string `json:"status,omitempty"`
	// IdempotencyKey echoes the caller-supplied dedupe token when set;
	// useful for clients that build saga lookup tables from the
	// response.
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	// Replayed indicates the response was served from a previously
	// stored result row (matched IdempotencyKey).
	Replayed bool `json:"replayed,omitempty"`
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
	// DLQEntries lists DLQ rows enqueued during compensation (one per
	// compensator that itself failed). Empty on the happy path and
	// when every compensator ran cleanly.
	DLQEntries []string `json:"dlqEntries,omitempty"`
	// CompensationStrategy echoes the strategy that drove the rollback
	// walk: "best-effort" (default — broken compensator does not block
	// the rest) or "stop-on-first" (broken compensator halts walking,
	// later steps recorded as skipped). Populated on every result —
	// even happy-path success — so SDK clients can confirm which mode
	// the server applied. (US-469)
	CompensationStrategy SagaCompensationStrategy `json:"compensationStrategy,omitempty"`
	// FailedCompensations is the structured per-step view of which
	// compensators did not run cleanly. Populated alongside DLQEntries
	// when the rollback walk encountered failures; empty on the happy
	// path and on a clean rollback. (US-469)
	FailedCompensations []FailedCompensationRef `json:"failedCompensations,omitempty"`
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
//
// Backwards-compatible thin wrapper around ApplyBatchSagaWithOptions for
// pre-US-369 callers that did not pass idempotency options.
func (e *Executor) ApplyBatchSaga(ctx context.Context, ontologyRID string, reqs []ApplyRequest) (*SagaResult, error) {
	return e.ApplyBatchSagaWithOptions(ctx, ontologyRID, reqs, SagaOptions{})
}

// ApplyBatchSagaWithOptions is the US-369 saga coordinator: persists the
// saga header, every step, edits, inverse edits, the lifecycle status,
// honours idempotency keys (replays the prior result on repeat), and
// enqueues failed compensators into the DLQ. Falls back to in-memory
// behaviour when no SagaStore is wired.
func (e *Executor) ApplyBatchSagaWithOptions(ctx context.Context, ontologyRID string, reqs []ApplyRequest, opts SagaOptions) (*SagaResult, error) {
	// Normalise the strategy once at entry — empty string defaults to
	// best-effort. Caller-side wire validation should already have
	// rejected unknown values via NormalizeCompensationStrategy, but
	// the executor stays robust if it sees an empty or unrecognised
	// value: fall back to best-effort silently.
	strategy := opts.CompensationStrategy
	if normalised, err := NormalizeCompensationStrategy(string(strategy)); err == nil {
		strategy = normalised
	} else {
		strategy = CompensationStrategyBestEffort
	}

	// Idempotency replay: if the same key has been seen, return the
	// stored result verbatim (with Replayed=true). Conflicts during
	// CreateSaga also fall back here so a concurrent retry races
	// cleanly.
	if e.sagaStore != nil && opts.IdempotencyKey != "" {
		if prior, err := e.sagaStore.GetSagaByIdempotencyKey(ctx, opts.IdempotencyKey); err == nil && prior != nil {
			return replayStoredSaga(prior)
		}
	}

	requestedBy := opts.RequestedBy
	if requestedBy == "" {
		if u := auth.UserFromContext(ctx); u != nil {
			requestedBy = u.ID
		}
	}

	sagaID := ""
	stepIDs := make([]string, len(reqs))
	if e.sagaStore != nil {
		sagaID = uuid.New().String()
		header := &Saga{
			SagaID:         sagaID,
			IdempotencyKey: opts.IdempotencyKey,
			Ontology:       ontologyRID,
			Status:         SagaStatusRunning,
			RequestedBy:    requestedBy,
		}
		if err := e.sagaStore.CreateSaga(ctx, header); err != nil {
			if errors.Is(err, ErrSagaIdempotencyConflict) && opts.IdempotencyKey != "" {
				// Race: a concurrent caller won. Replay their result.
				if prior, getErr := e.sagaStore.GetSagaByIdempotencyKey(ctx, opts.IdempotencyKey); getErr == nil && prior != nil {
					return replayStoredSaga(prior)
				}
			}
			return nil, fmt.Errorf("create saga: %w", err)
		}

		// One step row per request — all rows start in PENDING and
		// advance to APPLIED / COMPENSATED / COMPENSATION_FAILED below.
		for i := range reqs {
			stepIDs[i] = uuid.New().String()
			paramsJSON, _ := json.Marshal(reqs[i].Parameters)
			if len(paramsJSON) == 0 {
				paramsJSON = json.RawMessage("{}")
			}
			step := &SagaStep{
				StepID:     stepIDs[i],
				SagaID:     sagaID,
				StepIndex:  i,
				ActionType: reqs[i].ActionType,
				Parameters: paramsJSON,
				Status:     SagaStepStatusPending,
			}
			if err := e.sagaStore.CreateSagaStep(ctx, step); err != nil {
				_ = e.sagaStore.UpdateSagaStatus(ctx, sagaID, SagaUpdate{
					Status: SagaStatusFailed,
					FailureMessage: ptrString(fmt.Sprintf("create step %d: %v", i, err)),
				})
				return nil, fmt.Errorf("create saga step %d: %w", i, err)
			}
		}
	}

	prepared := make([]*PreparedAction, 0, len(reqs))
	primaryResults := make([]*ApplyResult, 0, len(reqs))

	for i := range reqs {
		p, err := e.Prepare(ctx, ontologyRID, &reqs[i])
		// US-471: enforce per-action optimistic locks at prepare time so
		// a stale ExpectedVersion(s) ref aborts the saga before any
		// commit and triggers the same rollback path as a prepare error.
		// The *StaleObjectError is preserved as BatchError.Cause so the
		// handler can surface 409 StaleObject if it inspects errors.As.
		if err == nil && hasOptimisticLockOptions(reqs[i].Options) {
			err = e.checkExpectedVersions(ctx, ontologyRID, p.Edits, reqs[i].Options)
		}
		if err != nil {
			failure := &BatchError{
				Phase:             classifyPrepareError(err),
				FailedActionIndex: i,
				ActionType:        reqs[i].ActionType,
				Message:           err.Error(),
				Cause:             err,
			}
			if e.sagaStore != nil {
				_ = e.sagaStore.UpdateSagaStep(ctx, stepIDs[i], SagaStepUpdate{Status: SagaStepStatusFailed})
				_ = e.sagaStore.UpdateSagaStatus(ctx, sagaID, SagaUpdate{Status: SagaStatusCompensating})
			}
			compOut := e.runCompensations(ctx, ontologyRID, prepared, reqs, sagaID, stepIDs, strategy)
			result := &SagaResult{
				Mode:                 "saga",
				SagaID:               sagaID,
				IdempotencyKey:       opts.IdempotencyKey,
				Applied:              primaryResults,
				Compensations:        compOut.compensations,
				AppliedEdits:         compOut.appliedEdits,
				BatchID:              compOut.batchID,
				Offset:               compOut.offset,
				Failure:              failure,
				DLQEntries:           compOut.dlqIDs,
				CompensationStrategy: strategy,
				FailedCompensations:  compOut.failedCompensations,
			}
			result.Status = sagaTerminalStatus(compOut.dlqIDs)
			e.persistSagaTerminal(ctx, sagaID, result, failure.Message)
			return result, failure
		}
		prepared = append(prepared, p)
		primaryResults = append(primaryResults, &ApplyResult{
			ActionRID: p.ActionType.RID,
			Edits:     p.Edits,
		})
		if e.sagaStore != nil {
			editsJSON, _ := MarshalEdits(p.Edits)
			_ = e.sagaStore.UpdateSagaStep(ctx, stepIDs[i], SagaStepUpdate{
				Status:    SagaStepStatusApplied,
				EditsJSON: editsJSON,
			})
		}
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
		if e.sagaStore != nil {
			_ = e.sagaStore.UpdateSagaStatus(ctx, sagaID, SagaUpdate{Status: SagaStatusCompensating})
		}
		compOut := e.runCompensations(ctx, ontologyRID, prepared, reqs, sagaID, stepIDs, strategy)
		result := &SagaResult{
			Mode:                 "saga",
			SagaID:               sagaID,
			IdempotencyKey:       opts.IdempotencyKey,
			Applied:              primaryResults,
			Compensations:        compOut.compensations,
			AppliedEdits:         compOut.appliedEdits,
			BatchID:              compOut.batchID,
			Offset:               compOut.offset,
			Failure:              be,
			DLQEntries:           compOut.dlqIDs,
			CompensationStrategy: strategy,
			FailedCompensations:  compOut.failedCompensations,
		}
		result.Status = sagaTerminalStatus(compOut.dlqIDs)
		e.persistSagaTerminal(ctx, sagaID, result, be.Message)
		return result, be
	}

	result := &SagaResult{
		Mode:                 "saga",
		SagaID:               sagaID,
		Status:               SagaStatusSuccess,
		IdempotencyKey:       opts.IdempotencyKey,
		BatchID:              br.BatchID,
		Offset:               br.Offset,
		Applied:              br.Results,
		AppliedEdits:         br.AppliedEdits,
		CompensationStrategy: strategy,
	}
	e.persistSagaTerminal(ctx, sagaID, result, "")
	return result, nil
}

// persistSagaTerminal writes the final saga lifecycle row and result
// snapshot when a SagaStore is wired. No-ops in degraded mode. Errors
// are logged in-line by the caller — saga results are still returned to
// the user even if the persistence flush fails.
func (e *Executor) persistSagaTerminal(ctx context.Context, sagaID string, result *SagaResult, failureMessage string) {
	if e.sagaStore == nil || sagaID == "" {
		return
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return
	}
	upd := SagaUpdate{
		Status:     result.Status,
		ResultJSON: resultJSON,
	}
	if failureMessage != "" {
		upd.FailureMessage = &failureMessage
	}
	_ = e.sagaStore.UpdateSagaStatus(ctx, sagaID, upd)
}

// sagaTerminalStatus picks the right action_sagas.status terminal
// value: COMPENSATED when the rollback ran cleanly, FAILED when at
// least one compensator entered the DLQ.
func sagaTerminalStatus(dlq []string) string {
	if len(dlq) > 0 {
		return SagaStatusFailed
	}
	return SagaStatusCompensated
}

// replayStoredSaga unmarshals a previously-persisted SagaResult from
// the saga header row and stamps Replayed=true so callers can
// distinguish replay from a fresh run.
func replayStoredSaga(prior *Saga) (*SagaResult, error) {
	if len(prior.ResultJSON) == 0 {
		return nil, fmt.Errorf("saga %s has no result snapshot", prior.SagaID)
	}
	var result SagaResult
	if err := json.Unmarshal(prior.ResultJSON, &result); err != nil {
		return nil, fmt.Errorf("decode saga result: %w", err)
	}
	result.Replayed = true
	if result.Status == SagaStatusSuccess {
		return &result, nil
	}
	// Replay still surfaces the failure error so callers get the same
	// HTTP semantics as the original run. Failure is already populated
	// in result.
	if result.Failure != nil {
		return &result, result.Failure
	}
	return &result, nil
}

func ptrString(s string) *string { return &s }

// RetrySagaDLQ replays a DLQ entry's edits as a fresh EditBatch. The
// caller is responsible for transitioning the DLQ row to RESOLVED on
// success or DROPPED on manual dismissal. Returns an error if the
// publish fails — the row stays PENDING so it can be retried again.
func (e *Executor) RetrySagaDLQ(ctx context.Context, entry *SagaDLQEntry) error {
	if entry == nil {
		return fmt.Errorf("nil entry")
	}
	if e.publisher == nil {
		return fmt.Errorf("no publisher configured")
	}
	var edits []funnel.Edit
	if len(entry.EditsJSON) > 0 {
		if err := json.Unmarshal(entry.EditsJSON, &edits); err != nil {
			return fmt.Errorf("decode dlq edits: %w", err)
		}
	}
	if len(edits) == 0 {
		// Nothing to publish — caller can transition to RESOLVED
		// without calling the publisher.
		return nil
	}
	batch := &funnel.EditBatch{
		ID:              uuid.New().String(),
		OntologyAPIName: entry.Ontology,
		Edits:           edits,
	}
	if _, err := e.publisher.Publish(batch); err != nil {
		return fmt.Errorf("publish dlq batch: %w", err)
	}
	return nil
}

// compensationOutcome bundles every piece of state the saga
// coordinator needs back from runCompensations. Refactored from a
// 5-tuple return so the US-469 additions (FailedCompensations) do not
// inflate the signature beyond readability.
type compensationOutcome struct {
	compensations       []*ApplyResult
	appliedEdits        []funnel.Edit
	batchID             string
	offset              uint64
	dlqIDs              []string
	failedCompensations []FailedCompensationRef
}

// runCompensations prepares and publishes the compensating actions for a
// slice of successfully-prepared primary actions. Walks the slice in
// REVERSE order (matches PRD "逆序执行补偿") and skips any action whose
// ActionType declares no CompensateActionRID. Returns the structured
// outcome via compensationOutcome.
//
// The compensation path never calls back into ApplyBatchSaga so a
// compensator whose own ActionType has a CompensateActionRID does NOT
// trigger a recursive rollback — saga depth is bounded at 1 deliberately.
//
// US-469 introduces an explicit CompensationStrategy:
//   - best-effort (default): a broken compensator is logged to DLQ +
//     FailedCompensations but the reverse walk continues — partial
//     compensation strictly better than none.
//   - stop-on-first: as soon as one compensator fails, halt walking.
//     The failure itself is DLQ-enqueued; every still-prepared step
//     later in the walk is recorded in FailedCompensations with
//     phase="skipped" for operator visibility but does NOT enter DLQ
//     (it was deliberately not attempted).
//
// reqs is the original ApplyRequest slice — required so failed entries
// can carry the primary action's API name on FailedCompensationRef.
//
// When sagaID and stepIDs are provided (i.e. the executor has a wired
// SagaStore) the per-step rows are advanced to COMPENSATED on the
// happy rollback path and to COMPENSATION_FAILED with a DLQ row when a
// compensator could not be prepared OR the compensation batch publish
// failed.
func (e *Executor) runCompensations(
	ctx context.Context,
	ontologyRID string,
	prepared []*PreparedAction,
	reqs []ApplyRequest,
	sagaID string,
	stepIDs []string,
	strategy SagaCompensationStrategy,
) compensationOutcome {
	if len(prepared) == 0 {
		return compensationOutcome{}
	}

	out := compensationOutcome{
		compensations:       make([]*ApplyResult, 0, len(prepared)),
		dlqIDs:              make([]string, 0),
		failedCompensations: make([]FailedCompensationRef, 0),
	}
	compensators := make([]*PreparedAction, 0, len(prepared))
	// Parallel slice of source-step indices so we can advance step
	// statuses after the compensation batch commits.
	compStepIdx := make([]int, 0, len(prepared))
	// stopped tracks whether stop-on-first has already triggered, in
	// which case the remaining steps go into failedCompensations with
	// phase="skipped" instead of being attempted.
	stopped := false

	for i := len(prepared) - 1; i >= 0; i-- {
		primary := prepared[i]
		// No compensator declared → silently skip (correct for
		// read-only / idempotent steps); this is NOT a failure and does
		// not produce a FailedCompensations entry.
		if primary.ActionType == nil || primary.ActionType.CompensateActionRID == "" {
			continue
		}

		if stopped {
			// Stop-on-first already triggered earlier in the walk. Every
			// remaining step with a declared compensator goes into
			// FailedCompensations as "skipped" so an operator can see
			// what was left un-rolled-back. No DLQ row — we never
			// attempted these.
			ref := FailedCompensationRef{
				StepIndex:     i,
				ActionType:    primaryActionTypeName(reqs, i, primary),
				CompensateRID: primary.ActionType.CompensateActionRID,
				Phase:         FailedCompensationPhaseSkipped,
				Reason:        "compensation halted by stop-on-first strategy after earlier failure",
			}
			if i < len(stepIDs) {
				ref.StepID = stepIDs[i]
			}
			out.failedCompensations = append(out.failedCompensations, ref)
			continue
		}

		comp, err := e.prepareCompensator(ctx, ontologyRID, primary)
		if err != nil {
			ref := FailedCompensationRef{
				StepIndex:     i,
				ActionType:    primaryActionTypeName(reqs, i, primary),
				CompensateRID: primary.ActionType.CompensateActionRID,
				Phase:         FailedCompensationPhasePrepare,
				Reason:        fmt.Sprintf("prepare compensator: %v", err),
			}
			if e.sagaStore != nil && sagaID != "" && i < len(stepIDs) {
				stepID := stepIDs[i]
				_ = e.sagaStore.UpdateSagaStep(ctx, stepID, SagaStepUpdate{Status: SagaStepStatusCompensationFailed})
				dlqID := uuid.New().String()
				_ = e.sagaStore.EnqueueDLQ(ctx, &SagaDLQEntry{
					DLQID:          dlqID,
					SagaID:         sagaID,
					StepID:         stepID,
					Ontology:       ontologyRID,
					EditsJSON:      json.RawMessage("[]"),
					FailureMessage: ref.Reason,
					Status:         SagaDLQStatusPending,
				})
				out.dlqIDs = append(out.dlqIDs, dlqID)
				ref.StepID = stepID
				ref.DLQID = dlqID
			}
			out.failedCompensations = append(out.failedCompensations, ref)
			if strategy == CompensationStrategyStopOnFirst {
				stopped = true
			}
			continue
		}
		compensators = append(compensators, comp)
		out.compensations = append(out.compensations, &ApplyResult{
			ActionRID: comp.ActionType.RID,
			Edits:     comp.Edits,
		})
		compStepIdx = append(compStepIdx, i)
	}

	if len(compensators) == 0 {
		// No compensators ever made it past prepare. Reset compensations
		// slice to nil so the response stays clean ("no rollback ran")
		// rather than emitting an empty array.
		out.compensations = nil
		return out
	}

	br, err := e.CommitBatch(ctx, ontologyRID, compensators)
	if err != nil {
		// Each compensator that we BUILT but couldn't COMMIT lands in
		// the DLQ + FailedCompensations as a commit-phase failure.
		// stop-on-first cannot bite here because the commit is a
		// single all-or-nothing batch — by the time we got here, every
		// prepared compensator either succeeded its prepare or was
		// already recorded as prepare-failure above. The commit error
		// affects all of them at once.
		for k, srcIdx := range compStepIdx {
			ref := FailedCompensationRef{
				StepIndex:     srcIdx,
				ActionType:    primaryActionTypeName(reqs, srcIdx, prepared[srcIdx]),
				CompensateRID: prepared[srcIdx].ActionType.CompensateActionRID,
				Phase:         FailedCompensationPhaseCommit,
				Reason:        fmt.Sprintf("commit compensation: %v", err),
			}
			if e.sagaStore != nil && sagaID != "" && srcIdx < len(stepIDs) {
				stepID := stepIDs[srcIdx]
				_ = e.sagaStore.UpdateSagaStep(ctx, stepID, SagaStepUpdate{Status: SagaStepStatusCompensationFailed})
				editsJSON, _ := MarshalEdits(compensators[k].Edits)
				dlqID := uuid.New().String()
				_ = e.sagaStore.EnqueueDLQ(ctx, &SagaDLQEntry{
					DLQID:          dlqID,
					SagaID:         sagaID,
					StepID:         stepID,
					Ontology:       ontologyRID,
					EditsJSON:      editsJSON,
					FailureMessage: ref.Reason,
					Status:         SagaDLQStatusPending,
				})
				out.dlqIDs = append(out.dlqIDs, dlqID)
				ref.StepID = stepID
				ref.DLQID = dlqID
			}
			out.failedCompensations = append(out.failedCompensations, ref)
		}
		return out
	}
	// Stamp the compensation batch ID onto the per-action compensator
	// results so downstream consumers can correlate them.
	for _, r := range out.compensations {
		r.BatchID = br.BatchID
		r.Offset = br.Offset
	}
	if e.sagaStore != nil && sagaID != "" {
		for k, srcIdx := range compStepIdx {
			if srcIdx >= len(stepIDs) {
				continue
			}
			inverseJSON, _ := MarshalEdits(compensators[k].Edits)
			_ = e.sagaStore.UpdateSagaStep(ctx, stepIDs[srcIdx], SagaStepUpdate{
				Status:           SagaStepStatusCompensated,
				InverseEditsJSON: inverseJSON,
			})
		}
	}
	out.appliedEdits = br.AppliedEdits
	out.batchID = br.BatchID
	out.offset = br.Offset
	return out
}

// primaryActionTypeName picks the best primary action API name for a
// FailedCompensationRef: prefer reqs[i].ActionType (the literal name the
// caller declared), fall back to the prepared action's ActionType when
// the slice is short. The dual lookup keeps the helper robust against
// out-of-bounds indexes from degraded code paths.
func primaryActionTypeName(reqs []ApplyRequest, idx int, prepared *PreparedAction) string {
	if idx >= 0 && idx < len(reqs) && reqs[idx].ActionType != "" {
		return reqs[idx].ActionType
	}
	if prepared != nil && prepared.ActionType != nil {
		return prepared.ActionType.APIName
	}
	return ""
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
