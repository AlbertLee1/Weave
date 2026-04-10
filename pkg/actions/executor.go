package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// ApplyOptions controls single-action apply behavior (Foundry OSv2).
type ApplyOptions struct {
	Mode        string `json:"mode"`        // VALIDATE_ONLY | VALIDATE_AND_EXECUTE (default)
	ReturnEdits string `json:"returnEdits"` // ALL | ALL_V2_WITH_DELETIONS | NONE (default ALL)
}

// BatchApplyOptions controls batch apply behavior (Foundry OSv2).
type BatchApplyOptions struct {
	ReturnEdits string `json:"returnEdits"` // ALL | NONE (default ALL)
}

// ApplyRequest is the request to apply an action.
type ApplyRequest struct {
	ActionType string                 `json:"actionType"` // API name
	Parameters map[string]interface{} `json:"parameters"`
	Options    *ApplyOptions          `json:"options,omitempty"`
}

// ApplyResult is the result of applying an action.
type ApplyResult struct {
	ActionRID string        `json:"actionRid"`
	Edits     []funnel.Edit `json:"edits"`
	BatchID   string        `json:"batchId"`
	Offset    uint64        `json:"offset"`
}

// ValidationResult is the response for VALIDATE_ONLY mode.
type ValidationResult struct {
	Result string `json:"result"` // VALID | INVALID
	// SubmissionCriteria may carry per-criterion results in the future.
}

// ValidateOnlyResponse is the response envelope for VALIDATE_ONLY apply.
type ValidateOnlyResponse struct {
	Validation *ValidationResult `json:"validation"`
}

// Publisher is the minimal contract the Executor needs from the funnel
// publisher. Defined here (rather than depending on the concrete
// *funnel.Publisher) so tests can inject fakes and detect whether a publish
// was issued.
type Publisher interface {
	Publish(batch *funnel.EditBatch) (uint64, error)
}

// Executor executes actions.
type Executor struct {
	omsRepo            oms.Repository
	publisher          Publisher
	functionDispatcher FunctionDispatcher
}

// NewExecutor creates a new action executor. The publisher may be nil in unit
// tests that do not need NATS (edits are still computed, just not committed).
// A function dispatcher can be attached after construction via
// SetFunctionDispatcher; it stays nil by default so legacy callers see no
// behavioral change.
func NewExecutor(omsRepo oms.Repository, publisher Publisher) *Executor {
	return &Executor{
		omsRepo:   omsRepo,
		publisher: publisher,
	}
}

// SetFunctionDispatcher attaches a FunctionDispatcher used for action types
// flagged IsFunctionBacked. Passing nil restores rules-only behavior. Safe to
// call once at boot before the executor is shared with handlers.
func (e *Executor) SetFunctionDispatcher(d FunctionDispatcher) {
	e.functionDispatcher = d
}

// PreparedAction is the output of Executor.Prepare: everything computable for
// an action without touching NATS, the action log, or side effects. Prepared
// actions are safe to discard when any sibling action in a batch fails
// validation — that is how atomic-batch semantics are implemented.
type PreparedAction struct {
	ActionType *oms.ActionType
	UserID     string
	Request    *ApplyRequest
	// Edits are pre-collapse, per-action. Cross-action collapse happens in
	// CommitBatch over the flattened slice of all prepared actions.
	Edits []funnel.Edit
}

// BatchResult is the response payload for ApplyBatchAtomic / ApplyBatchBestEffort.
type BatchResult struct {
	Mode         string         `json:"mode"`
	BatchID      string         `json:"batchId,omitempty"`
	Offset       uint64         `json:"offset,omitempty"`
	Results      []*ApplyResult `json:"results"`
	AppliedEdits []funnel.Edit  `json:"appliedEdits,omitempty"`
	Failures     []BatchFailure `json:"failures,omitempty"`
}

// BatchFailure captures a single action's failure in bestEffort mode.
type BatchFailure struct {
	Index      int    `json:"index"`
	ActionType string `json:"actionType"`
	Phase      string `json:"phase"`
	Message    string `json:"message"`
}

// BatchError is the structured error returned by ApplyBatchAtomic on failure.
// It carries enough metadata for the HTTP handler to build a deterministic
// response body (phase, failedActionIndex, actionType).
type BatchError struct {
	Phase             string
	FailedActionIndex int
	ActionType        string
	Message           string
}

func (e *BatchError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("action %d (%s) %s: %s", e.FailedActionIndex, e.ActionType, e.Phase, e.Message)
}

// Prepare runs the pure, fallible part of applying an action: lookup action
// type, validate parameters, evaluate submission criteria, execute rules. It
// does NOT publish to NATS, write the action log, or fire side effects. The
// returned PreparedAction can be committed later via CommitBatch.
func (e *Executor) Prepare(ctx context.Context, ontologyRID string, req *ApplyRequest) (*PreparedAction, error) {
	// Step 1: Look up ActionType
	actionTypes, err := e.omsRepo.ListActionTypes(ctx, ontologyRID)
	if err != nil {
		return nil, fmt.Errorf("list action types: %w", err)
	}

	var actionType *oms.ActionType
	for i := range actionTypes {
		if actionTypes[i].APIName == req.ActionType {
			actionType = &actionTypes[i]
			break
		}
	}
	if actionType == nil {
		return nil, fmt.Errorf("action type %q not found", req.ActionType)
	}

	// Step 2: Parse parameter definitions
	paramDefs, err := ParseParameterDefs(actionType.Parameters)
	if err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	// Step 3: Validate parameters
	if err := ValidateParameters(paramDefs, req.Parameters); err != nil {
		return nil, fmt.Errorf("validate params: %w", err)
	}

	// Step 4: Extract UserID from auth context
	userID := "system"
	if u := auth.UserFromContext(ctx); u != nil {
		userID = u.ID
	}

	// Step 5: Evaluate submission criteria
	actCtx := ActionContext{Parameters: req.Parameters, UserID: userID}
	if err := EvaluateCriteria(actionType.SubmissionCriteria, actCtx); err != nil {
		return nil, fmt.Errorf("submission criteria: %w", err)
	}

	// Step 6: Function-backed branch (Tier 3.2). When the action type is
	// flagged IsFunctionBacked AND a dispatcher is configured on this
	// executor, delegate edit generation to the function and skip the
	// local rules path. If the dispatcher is nil (e.g. dev environment
	// without the function service), fall through to the rules path so
	// the action still degrades gracefully.
	if actionType.IsFunctionBacked && e.functionDispatcher != nil {
		fnEdits, err := e.functionDispatcher.Dispatch(ctx, actionType, req.Parameters)
		if err != nil {
			return nil, fmt.Errorf("function dispatch: %w", err)
		}
		return &PreparedAction{
			ActionType: actionType,
			UserID:     userID,
			Request:    req,
			Edits:      fnEdits,
		}, nil
	}

	// Step 7: Parse rules
	rules, err := ParseRules(actionType.Rules)
	if err != nil {
		return nil, fmt.Errorf("parse rules: %w", err)
	}

	// Step 8: Execute rules to generate edits (pre-collapse).
	edits, err := ExecuteRules(rules, req.Parameters)
	if err != nil {
		return nil, fmt.Errorf("execute rules: %w", err)
	}

	return &PreparedAction{
		ActionType: actionType,
		UserID:     userID,
		Request:    req,
		Edits:      edits,
	}, nil
}

// CommitBatch publishes one EditBatch for the combined edits of the given
// prepared actions, writes one action log per prepared action on success, and
// fires per-action side effects. Returns a populated BatchResult.
//
// CommitBatch preserves request order when flattening edits from prepared
// actions so cross-action MODIFY chains collapse in the caller's intended
// order (later actions win).
func (e *Executor) CommitBatch(ctx context.Context, prepared []*PreparedAction) (*BatchResult, error) {
	result := &BatchResult{
		Mode:    "atomic",
		Results: make([]*ApplyResult, 0, len(prepared)),
	}
	if len(prepared) == 0 {
		return result, nil
	}

	// Flatten all edits preserving request order, then collapse globally.
	var all []funnel.Edit
	for _, p := range prepared {
		all = append(all, p.Edits...)
	}
	collapsed := CollapseEdits(all)

	// Pre-populate per-action results with the caller-visible per-action edits.
	// This mirrors the legacy single-action Apply response shape.
	for _, p := range prepared {
		result.Results = append(result.Results, &ApplyResult{
			ActionRID: p.ActionType.RID,
			Edits:     p.Edits,
		})
	}

	// Empty post-collapse batch is a valid successful no-op: nothing to publish.
	if len(collapsed) == 0 {
		return result, nil
	}

	batch := &funnel.EditBatch{
		ID:        uuid.New().String(),
		Edits:     collapsed,
		UserID:    prepared[0].UserID,
		Timestamp: time.Now(),
	}

	var offset uint64
	if e.publisher != nil {
		var err error
		offset, err = e.publisher.Publish(batch)
		if err != nil {
			return nil, &BatchError{
				Phase:             "publish",
				FailedActionIndex: -1,
				ActionType:        "",
				Message:           fmt.Sprintf("publish edits: %v", err),
			}
		}
	}

	result.BatchID = batch.ID
	result.Offset = offset
	result.AppliedEdits = collapsed
	for _, r := range result.Results {
		r.BatchID = batch.ID
		r.Offset = offset
	}

	// Write one action log per prepared action (best-effort, non-fatal).
	for i, p := range prepared {
		paramsJSON, _ := json.Marshal(p.Request.Parameters)
		editsJSON, _ := json.Marshal(p.Edits)
		logRow := &oms.ActionLog{
			ActionTypeRID: p.ActionType.RID,
			UserID:        p.UserID,
			Parameters:    paramsJSON,
			Edits:         editsJSON,
			Status:        "SUCCESS",
		}
		if logErr := e.omsRepo.InsertActionLog(ctx, logRow); logErr != nil {
			log.Printf("actions: failed to write action log for action %d: %v", i, logErr)
		}
	}

	// Fire per-action side effects (best-effort, non-blocking).
	for i, p := range prepared {
		ExecuteSideEffects(p.ActionType.SideEffects, ActionResult{
			ActionRID: p.ActionType.RID,
			BatchID:   batch.ID,
			Edits:     result.Results[i].Edits,
		})
	}

	return result, nil
}

// Apply executes a single action. Preserved as the backwards-compatible entry
// point; internally it routes through Prepare + CommitBatch so there is a
// single code path for action execution.
func (e *Executor) Apply(ctx context.Context, ontologyRID string, req *ApplyRequest) (*ApplyResult, error) {
	prep, err := e.Prepare(ctx, ontologyRID, req)
	if err != nil {
		return nil, err
	}

	// Short-circuit the noop path to match the legacy nil-edits shape.
	if len(prep.Edits) == 0 {
		return &ApplyResult{
			ActionRID: prep.ActionType.RID,
			Edits:     nil,
		}, nil
	}

	br, err := e.CommitBatch(ctx, []*PreparedAction{prep})
	if err != nil {
		// Unwrap a *BatchError so the legacy Apply caller sees a plain
		// string-shaped error (preserves wire compatibility with callers
		// that predate BatchError).
		var bErr *BatchError
		if errors.As(err, &bErr) {
			return nil, fmt.Errorf("%s", bErr.Message)
		}
		return nil, err
	}
	if len(br.Results) == 0 {
		return &ApplyResult{ActionRID: prep.ActionType.RID}, nil
	}
	return br.Results[0], nil
}

// ApplyBatchAtomic prepares every request and, if all succeed, commits once.
// On any prepare failure it returns a *BatchError with the failing action's
// index and phase, and NOTHING is published. This is the default ApplyBatch
// semantics (Option B in the design).
func (e *Executor) ApplyBatchAtomic(ctx context.Context, ontologyRID string, reqs []ApplyRequest) (*BatchResult, error) {
	prepared := make([]*PreparedAction, 0, len(reqs))
	for i := range reqs {
		p, err := e.Prepare(ctx, ontologyRID, &reqs[i])
		if err != nil {
			return nil, &BatchError{
				Phase:             classifyPrepareError(err),
				FailedActionIndex: i,
				ActionType:        reqs[i].ActionType,
				Message:           err.Error(),
			}
		}
		prepared = append(prepared, p)
	}
	return e.CommitBatch(ctx, prepared)
}

// ApplyBatchBestEffort prepares every request and commits the ones that
// succeeded in a single batch. Failures are reported alongside successes in
// the returned BatchResult; a publish failure is still returned as an error
// because "commit what you can" cannot partially commit a single NATS message.
func (e *Executor) ApplyBatchBestEffort(ctx context.Context, ontologyRID string, reqs []ApplyRequest) (*BatchResult, error) {
	var prepared []*PreparedAction
	var failures []BatchFailure
	for i := range reqs {
		p, err := e.Prepare(ctx, ontologyRID, &reqs[i])
		if err != nil {
			failures = append(failures, BatchFailure{
				Index:      i,
				ActionType: reqs[i].ActionType,
				Phase:      classifyPrepareError(err),
				Message:    err.Error(),
			})
			continue
		}
		prepared = append(prepared, p)
	}

	result, err := e.CommitBatch(ctx, prepared)
	if err != nil {
		return nil, err
	}
	result.Mode = "bestEffort"
	result.Failures = failures
	return result, nil
}

// classifyPrepareError maps a Prepare-time error to one of the design doc's
// phase labels so the caller can render a structured failure response.
// Today every Prepare error is a validation-class failure; the "internal"
// label is reserved for structural errors (failure to list action types,
// parse parameter defs, parse rules, or unknown rule types escaping
// ExecuteRules).
func classifyPrepareError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "list action types"),
		strings.Contains(msg, "parse params"),
		strings.Contains(msg, "parse rules"),
		strings.Contains(msg, "execute rules"),
		strings.Contains(msg, "function dispatch"):
		return "internal"
	default:
		return "validation"
	}
}
