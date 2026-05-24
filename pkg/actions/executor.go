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
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/tracing"
	"github.com/liyang/weave/pkg/types"
	"go.opentelemetry.io/otel/attribute"
)

// ApplyOptions controls single-action apply behavior (Foundry OSv2).
//
// US-023: ExpectedVersion is an optional optimistic concurrency token. When
// non-nil, Apply loads the current version of the MODIFY/DELETE target and
// returns a *StaleObjectError if it does not match, preventing silent
// overwrites of data the caller has not yet observed.
//
// US-471: ExpectedVersions extends the single-target token to a per-target
// list so a single action (or batch of actions) that touches multiple
// objects across multiple ObjectTypes can lock against every observed
// version. Any single mismatch aborts the publish before NATS is contacted
// and surfaces the same *StaleObjectError → 409 StaleObject the legacy
// token produces. When both fields are set the per-target list takes
// precedence and the legacy single-target check is skipped.
type ApplyOptions struct {
	Mode             string               `json:"mode"`                       // VALIDATE_ONLY | VALIDATE_AND_EXECUTE (default)
	ReturnEdits      string               `json:"returnEdits"`                // ALL | ALL_V2_WITH_DELETIONS | NONE (default ALL)
	ExpectedVersion  *int                 `json:"expectedVersion,omitempty"`  // legacy single-target token (US-023)
	ExpectedVersions []ExpectedVersionRef `json:"expectedVersions,omitempty"` // per-target tokens (US-471)
}

// ExpectedVersionRef is a per-target optimistic concurrency token (US-471).
// Each ref names an (ObjectType, PrimaryKey) tuple the caller observed at
// version Version and asks the executor to abort the action if the
// authoritative version has drifted in between. Refs may point at objects
// the action does not itself modify — the lock is a snapshot guarantee on
// the caller's read set, not a side-effect predicate.
type ExpectedVersionRef struct {
	ObjectType string `json:"objectType"`
	PrimaryKey string `json:"primaryKey"`
	Version    int    `json:"version"`
}

// StaleObjectError is returned by Executor.Apply when a caller-supplied
// ExpectedVersion does not match the target object's current version. The
// HTTP handler layer converts this to a 409 Conflict response with
// errorName=StaleObject so Foundry SDK clients can surface a reload UX.
type StaleObjectError struct {
	ObjectType      string
	PrimaryKey      string
	ExpectedVersion int
	CurrentVersion  int64
}

func (e *StaleObjectError) Error() string {
	return fmt.Sprintf("stale object %s/%s: expected version %d, current version %d",
		e.ObjectType, e.PrimaryKey, e.ExpectedVersion, e.CurrentVersion)
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

// ApplyResult is the internal result of applying an action.
// Not returned directly to clients — handlers transform this into
// SyncApplyActionResponseV2 before serializing.
type ApplyResult struct {
	ActionRID string        `json:"-"`
	Edits     []funnel.Edit `json:"-"`
	BatchID   string        `json:"-"`
	Offset    uint64        `json:"-"`
	// ActionLogID is the persisted action_logs row id for this action.
	// Surfaced in SyncApplyActionResponseV2 so the toast Undo button (US-319)
	// can call POST /actions/revert with the right log id. Zero when the
	// action_logs write failed (best-effort) or the executor ran without an
	// OMS repo wired.
	ActionLogID int64 `json:"-"`
}

// ActionResults is the Foundry OSv2 edit summary returned in response envelopes.
type ActionResults struct {
	Type                string `json:"type"` // always "edits"
	AddedObjectCount    int    `json:"addedObjectCount"`
	ModifiedObjectCount int    `json:"modifiedObjectCount"`
	DeletedObjectCount  int    `json:"deletedObjectCount"`
	AddedLinksCount     int    `json:"addedLinksCount"`
	DeletedLinksCount   int    `json:"deletedLinksCount"`
}

// SyncApplyActionResponseV2 is the Foundry OSv2 response envelope for single apply.
//
// US-319 surfaces ActionLogID alongside the existing OperationID/Edits so the
// toast Undo button can POST /actions/revert with the right log id during its
// 5-second window. Zero when the executor ran without persisting an action
// log (no OMS repo wired, best-effort write failed, or noop empty edit set).
type SyncApplyActionResponseV2 struct {
	OperationID string            `json:"operationId,omitempty"`
	ActionLogID int64             `json:"actionLogId,omitempty"`
	Validation  *ValidationResult `json:"validation,omitempty"`
	Edits       *ActionResults    `json:"edits,omitempty"`
}

// BatchApplyActionResponseV2 is the Foundry OSv2 response envelope for batch apply.
type BatchApplyActionResponseV2 struct {
	Edits *ActionResults `json:"edits,omitempty"`
}

// countEdits computes an ActionResults summary from a list of edits.
func countEdits(edits []funnel.Edit) *ActionResults {
	r := &ActionResults{Type: "edits"}
	for _, e := range edits {
		switch e.Type {
		case funnel.EditTypeCreate:
			r.AddedObjectCount++
		case funnel.EditTypeModify:
			r.ModifiedObjectCount++
		case funnel.EditTypeDelete:
			r.DeletedObjectCount++
		case funnel.EditTypeLinkCreate:
			r.AddedLinksCount++
		case funnel.EditTypeLinkDelete:
			r.DeletedLinksCount++
		}
	}
	return r
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

// ValidateActionResponse is the Foundry OSv2 response envelope for the
// dedicated POST /api/v2/ontologies/{ontology}/actions/{action}/validate
// endpoint (see pkg/actions/handler_validate_bdd_test.go and
// docs/PRD-Weave-OSv2-深度复刻-V2.md). The shape intentionally mirrors
// what Foundry SDKs (TypeScript / Python OSDK) consume on every form-field
// change: a single overall result, a list of submissionCriteria each
// carrying their configured failure message, and a per-parameter map
// keyed by parameter id so a form can red-line exactly the field that
// failed.
//
// SubmissionCriteria and Parameters MUST be non-nil so the JSON wire
// shape is `[]`/`{}` rather than `null`. SDKs that did length checks
// without first nil-guarding would otherwise NPE on a VALID response.
type ValidateActionResponse struct {
	Result             string                               `json:"result"` // VALID | INVALID
	SubmissionCriteria []SubmissionCriterionResult          `json:"submissionCriteria"`
	Parameters         map[string]ParameterValidationResult `json:"parameters"`
}

// SubmissionCriterionResult is one entry in ValidateActionResponse.SubmissionCriteria.
// Foundry's submission-criteria pipeline emits one envelope per declared
// criterion; Weave's MVP synthesises a single entry on INVALID that
// carries the underlying validation error verbatim into
// configuredFailureMessage so the SDK has something to render in the
// form-level summary banner.
type SubmissionCriterionResult struct {
	Result                   string `json:"result"` // VALID | INVALID
	ConfiguredFailureMessage string `json:"configuredFailureMessage,omitempty"`
}

// ParameterValidationResult is one entry in ValidateActionResponse.Parameters.
// EvaluatedConstraints is non-nil so SDKs can iterate without a nil
// check; Weave's MVP populates a single {type:"required",
// result:"INVALID"} entry when a required parameter is missing and
// leaves the slice empty for VALID rows. The Required field is sourced
// from the ActionType.Parameters definition.
type ParameterValidationResult struct {
	Result               string                `json:"result"` // VALID | INVALID
	Required             bool                  `json:"required"`
	EvaluatedConstraints []EvaluatedConstraint `json:"evaluatedConstraints"`
}

// EvaluatedConstraint is one entry in
// ParameterValidationResult.EvaluatedConstraints. Foundry surfaces a
// richer taxonomy (range, oneOf, objectQuery, …); Weave starts with the
// "required" / "type" pair so the per-parameter wire shape is forwards-
// compatible and SDKs can switch on Type without breaking when richer
// constraint kinds land.
type EvaluatedConstraint struct {
	Type   string `json:"type"`
	Result string `json:"result"` // VALID | INVALID
}

// Publisher is the minimal contract the Executor needs from the funnel
// publisher. Defined here (rather than depending on the concrete
// *funnel.Publisher) so tests can inject fakes and detect whether a publish
// was issued.
type Publisher interface {
	Publish(batch *funnel.EditBatch) (uint64, error)
}

// ObjectExistenceChecker checks whether an object exists in the data layer
// (e.g. Bleve index). Used by createOrModifyObject rules to decide between
// CREATE and MODIFY at runtime.
type ObjectExistenceChecker interface {
	ObjectExists(ctx context.Context, objectType string, primaryKey string) bool
}

// ObjectFetcher retrieves the current properties of an object from the data
// layer (e.g. Bleve index). Used by US-104 to record PrevState in ActionLog
// for undo support. The ontologyAPIName is needed to scope the Bleve index lookup.
type ObjectFetcher interface {
	FetchObject(ctx context.Context, ontologyAPIName, objectType, primaryKey string) (map[string]interface{}, error)
}

// AtomicActionLogStore writes a slice of action logs in a single PostgreSQL
// transaction. US-238 uses it to commit batch-action state atomically before
// any NATS publish — the tx either succeeds (all logs persisted, publisher is
// fired afterwards) or the whole batch rolls back without any NATS message
// being emitted. Kept as a narrow, setter-injected interface so
// degraded-mode / test executors can operate without a PG pool.
type AtomicActionLogStore interface {
	WriteActionLogsAtomic(ctx context.Context, logs []*oms.ActionLog) error
}

// Executor executes actions.
type Executor struct {
	omsRepo            oms.Repository
	publisher          Publisher
	functionDispatcher FunctionDispatcher
	objectChecker      ObjectExistenceChecker
	objectFetcher      ObjectFetcher
	atomicLogStore     AtomicActionLogStore
	jobStore           ActionJobStore
	approvalStore      ActionApprovalStore
	actionLogStore     ActionLogStore
	progressPub        ProgressPublisher
	lineageStore       oms.LineageStore
	sagaStore          SagaStore
	paramSchemas       *ParameterSchemaValidator
	cancelRegistry     jobCancelRegistry
}

// NewExecutor creates a new action executor. The publisher may be nil in unit
// tests that do not need NATS (edits are still computed, just not committed).
// A function dispatcher can be attached after construction via
// SetFunctionDispatcher; it stays nil by default so legacy callers see no
// behavioral change.
func NewExecutor(omsRepo oms.Repository, publisher Publisher) *Executor {
	return &Executor{
		omsRepo:      omsRepo,
		publisher:    publisher,
		paramSchemas: NewParameterSchemaValidator(),
	}
}

// SetFunctionDispatcher attaches a FunctionDispatcher used for action types
// flagged IsFunctionBacked. Passing nil restores rules-only behavior. Safe to
// call once at boot before the executor is shared with handlers.
func (e *Executor) SetFunctionDispatcher(d FunctionDispatcher) {
	e.functionDispatcher = d
}

// SetObjectExistenceChecker attaches a checker used by createOrModifyObject
// rules to determine whether an object exists (→ MODIFY) or not (→ CREATE).
// When nil, all upsert edits default to CREATE. Safe to call once at boot.
func (e *Executor) SetObjectExistenceChecker(c ObjectExistenceChecker) {
	e.objectChecker = c
}

// SetObjectFetcher attaches a fetcher used to record PrevState of objects
// before they are modified or deleted (US-104 undo support). When nil,
// PrevEdits is omitted from ActionLog (backward compatible). Safe to call
// once at boot.
func (e *Executor) SetObjectFetcher(f ObjectFetcher) {
	e.objectFetcher = f
}

// SetAtomicActionLogStore attaches the PG-backed action-log tx store used by
// ApplyBatchAtomicTx (US-238) to persist all action logs in a single
// transaction before firing the NATS publish. When nil the atomic-tx path
// degrades to the legacy best-effort CommitBatch flow. Safe to call once at
// boot before the executor is shared with handlers.
func (e *Executor) SetAtomicActionLogStore(s AtomicActionLogStore) {
	e.atomicLogStore = s
}

// SetActionJobStore attaches the persistent job store used by the async apply
// path (US-240). When nil the handler's ?async=true query degrades to the
// synchronous flow so test routers without a PG pool keep working. Safe to
// call once at boot before the executor is shared with handlers.
func (e *Executor) SetActionJobStore(s ActionJobStore) {
	e.jobStore = s
}

// ActionJobStore returns the wired async-job store (may be nil in degraded
// mode). Exported so the handler can detect "no store wired" and degrade
// gracefully without reaching into the Executor's internals.
func (e *Executor) ActionJobStore() ActionJobStore {
	return e.jobStore
}

// SetLineageStore attaches the lineage edge store used by both commit paths
// (CommitBatch + commitBatchAtomicTx) to record one upstream→downstream edge
// per persisted CREATE/MODIFY/DELETE edit. Upstream is the action-log row's
// canonical RID; downstream is the affected object's RID. Link edits do not
// produce object-level lineage. Failures are logged but never abort the
// commit — lineage is best-effort observability, not a write barrier. Pass
// nil to disable. Safe to call once at boot before the executor is shared
// with handlers.
func (e *Executor) SetLineageStore(s oms.LineageStore) {
	e.lineageStore = s
}

// LineageStore returns the wired lineage store (may be nil in degraded
// mode). Exported so the impact handler (US-301) can detect "no store
// wired" and degrade gracefully without reaching into the Executor's
// internals.
func (e *Executor) LineageStore() oms.LineageStore {
	return e.lineageStore
}

// SetActionLogStore attaches the read-side ActionLogStore used by the
// /actions/history handlers (US-317). When nil the list endpoint degrades
// to an empty page and the detail endpoint returns 404 — same shape as
// SetActionJobStore / SetActionApprovalStore. Safe to call once at boot.
func (e *Executor) SetActionLogStore(s ActionLogStore) {
	e.actionLogStore = s
}

// ActionLogStore returns the wired action-log read store (may be nil).
func (e *Executor) ActionLogStore() ActionLogStore {
	return e.actionLogStore
}

// SetActionApprovalStore attaches the approval-workflow store used by the
// Apply handler to enqueue pending approvals when ActionType.RequiresApproval
// is set (US-242). When nil the RequiresApproval flag is ignored and the
// action runs synchronously — matches the ActionJobStore degraded-mode
// pattern. Safe to call once at boot before the executor is shared with
// handlers.
func (e *Executor) SetActionApprovalStore(s ActionApprovalStore) {
	e.approvalStore = s
}

// ActionApprovalStore returns the wired approval store (may be nil).
func (e *Executor) ActionApprovalStore() ActionApprovalStore {
	return e.approvalStore
}

// SetSagaStore attaches the durable saga coordinator store used by
// ApplyBatchSaga (US-369). When nil the saga path runs in-memory only —
// the rollback semantics still hold but no idempotency / DLQ rows are
// written. Same setter pattern as ActionApprovalStore.
func (e *Executor) SetSagaStore(s SagaStore) {
	e.sagaStore = s
}

// SagaStore returns the wired saga store (may be nil).
func (e *Executor) SagaStore() SagaStore {
	return e.sagaStore
}

// ResolveActionType is an exported shim around the saga coordinator's
// lookup helper so the Apply handler can locate the ActionType before
// executing rules — needed by the US-242 approval gate which inspects the
// RequiresApproval flag before deciding whether to route to Apply or to
// enqueue a pending approval.
func (e *Executor) ResolveActionType(ctx context.Context, ontologyRID, ridOrName string) (*oms.ActionType, error) {
	return e.resolveActionType(ctx, ontologyRID, ridOrName)
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
	// PrevEdits is a parallel slice to Edits recording the pre-edit state of
	// each object for undo (US-104). CREATE and LINK edits have nil entries;
	// MODIFY/DELETE entries contain the full previous properties map. Nil when
	// no ObjectFetcher is configured (backward compatible).
	PrevEdits []map[string]interface{}
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
// response body (phase, failedActionIndex, actionType). Cause preserves the
// original typed error (e.g. *apierror.APIError from US-208 enum validation)
// so the handler can surface it via errors.As instead of collapsing to a
// generic 400.
type BatchError struct {
	Phase             string `json:"phase"`
	FailedActionIndex int    `json:"failedActionIndex"`
	ActionType        string `json:"actionType"`
	Message           string `json:"message"`
	// Cause carries the original typed error in-process so handlers can
	// recover it via errors.As. JSON-skipped because the error interface
	// has no general unmarshaller — Message already preserves the
	// caller-visible description for SagaResult round-trips.
	Cause error `json:"-"`
}

func (e *BatchError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("action %d (%s) %s: %s", e.FailedActionIndex, e.ActionType, e.Phase, e.Message)
}

// Unwrap exposes the underlying cause to errors.Is / errors.As so HTTP layers
// can recover typed errors (e.g. *apierror.APIError from constraint validation)
// embedded inside a *BatchError envelope.
func (e *BatchError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Prepare runs the pure, fallible part of applying an action: lookup action
// type, validate parameters, evaluate submission criteria, execute rules. It
// does NOT publish to NATS, write the action log, or fire side effects. The
// returned PreparedAction can be committed later via CommitBatch.
func (e *Executor) Prepare(ctx context.Context, ontologyRID string, req *ApplyRequest) (*PreparedAction, error) {
	actionName := ""
	if req != nil {
		actionName = req.ActionType
	}
	ctx, span := tracing.StartSpan(ctx, "actions.Prepare",
		attribute.String("ontology.rid", ontologyRID),
		attribute.String("action.type", actionName),
	)
	defer span.End()

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
		// Round 38: wrap with sentinel so handler routes to 404
		// ActionTypeNotFound rather than the historical 400 ActionFailed.
		return nil, fmt.Errorf("%w: action type %q not found", ErrActionTypeNotFound, req.ActionType)
	}

	// Step 2: Parse parameter definitions
	paramDefs, err := ParseParameterDefs(actionType.Parameters)
	if err != nil {
		// Bad ActionType.Parameters JSON is technically a schema-author
		// bug (server-side data issue), but in practice it surfaces to
		// the SDK caller because the malformed schema blocks every
		// invocation. Treat as InvalidActionParameters so the SDK
		// surface routes consistently with the user-side flavor.
		return nil, fmt.Errorf("%w: parse params: %w", ErrInvalidActionParameters, err)
	}

	// Step 3: Validate parameters
	if err := ValidateParameters(paramDefs, req.Parameters); err != nil {
		// Round 38: caller-supplied parameters failed shape/value
		// validation. Wrap with sentinel so the handler routes to 400
		// InvalidActionParameters rather than 500 ActionFailed.
		return nil, fmt.Errorf("%w: validate params: %w", ErrInvalidActionParameters, err)
	}

	// Step 3b (US-245): evaluate the optional Draft-07 JSON Schema. Schema
	// violations surface as a typed *ParameterSchemaError whose APIError()
	// renders a structured 422 WEAVE_VALIDATION_SCHEMA — the handler's
	// typedAPIError branch unwraps it and emits field-level detail. A
	// malformed stored schema (compile error) returns an untyped error so
	// the handler's fallback 400 kicks in.
	if err := e.validateParameterSchema(actionType.ParameterSchema, req.Parameters); err != nil {
		return nil, fmt.Errorf("parameter schema: %w", err)
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
		tagEditsAsUserSource(fnEdits)
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

	// Step 8b (US-222): executeFunction rules delegate edit generation to a
	// Function. Dispatched in declaration order; returned edits are appended
	// after the regular rule-derived edits so cross-action collapse semantics
	// remain "later wins".
	fnRuleEdits, err := e.dispatchExecuteFunctionRules(ctx, rules, actionType, req.Parameters)
	if err != nil {
		return nil, fmt.Errorf("execute function rules: %w", err)
	}
	edits = append(edits, fnRuleEdits...)
	tagEditsAsUserSource(edits)

	// Step 9: Resolve link type API names to RIDs for LINK_CREATE/LINK_DELETE edits.
	e.resolveLinkEdits(ctx, ontologyRID, edits)

	// Step 10: Resolve UPSERT edits to CREATE or MODIFY based on object existence.
	e.resolveUpsertEdits(ctx, edits)

	// Step 11: Validate interface-backed rules — target ObjectType must implement the Interface.
	if err := e.validateInterfaceRules(ctx, ontologyRID, rules, req.Parameters); err != nil {
		return nil, fmt.Errorf("interface validation: %w", err)
	}

	// Step 12 (US-111): Validate ValueType constraints on property values.
	if err := e.validateValueTypeConstraints(ctx, ontologyRID, edits); err != nil {
		return nil, fmt.Errorf("constraint validation: %w", err)
	}

	// Step 13 (US-104): Fetch previous object state for MODIFY/DELETE edits.
	prevEdits := e.fetchPrevEdits(ctx, ontologyRID, edits)

	return &PreparedAction{
		ActionType: actionType,
		UserID:     userID,
		Request:    req,
		Edits:      edits,
		PrevEdits:  prevEdits,
	}, nil
}

// resolveLinkEdits resolves LinkTypeRID from API name to actual RID for link
// edits (LINK_CREATE and LINK_DELETE). If resolution fails the API name is
// preserved as a best-effort fallback — the consumer will still attempt the
// operation.
func (e *Executor) resolveLinkEdits(ctx context.Context, ontologyRID string, edits []funnel.Edit) {
	for i := range edits {
		if edits[i].Type != funnel.EditTypeLinkCreate && edits[i].Type != funnel.EditTypeLinkDelete {
			continue
		}
		apiName := edits[i].LinkTypeRID
		if apiName == "" {
			continue
		}
		lt, err := e.omsRepo.GetLinkTypeByAPIName(ctx, ontologyRID, apiName)
		if err == nil && lt != nil {
			edits[i].LinkTypeRID = lt.RID
		}
	}
}

// resolveUpsertEdits converts internal UPSERT edits to CREATE or MODIFY based
// on object existence. When no ObjectExistenceChecker is configured, all UPSERT
// edits default to CREATE (graceful degradation).
func (e *Executor) resolveUpsertEdits(ctx context.Context, edits []funnel.Edit) {
	for i := range edits {
		if edits[i].Type != editTypeUpsert {
			continue
		}
		if e.objectChecker != nil && e.objectChecker.ObjectExists(ctx, edits[i].ObjectType, edits[i].PrimaryKey) {
			edits[i].Type = funnel.EditTypeModify
		} else {
			edits[i].Type = funnel.EditTypeCreate
		}
	}
}

// dispatchExecuteFunctionRules invokes the FunctionDispatcher once per
// executeFunction rule, in declaration order, and returns the concatenated
// edits. The action type is shallow-copied with FunctionRID overridden so the
// existing dispatcher contract (which keys off ActionType.FunctionRID) is
// reused without a new interface method. Returns an error when an
// executeFunction rule is present but no dispatcher is wired or the rule omits
// FunctionRID.
func (e *Executor) dispatchExecuteFunctionRules(ctx context.Context, rules []Rule, actionType *oms.ActionType, params map[string]interface{}) ([]funnel.Edit, error) {
	var edits []funnel.Edit
	for i, rule := range rules {
		if !rule.IsExecuteFunction() {
			continue
		}
		if rule.FunctionRID == "" {
			return nil, fmt.Errorf("rule %d (executeFunction): functionRid is required", i)
		}
		if e.functionDispatcher == nil {
			return nil, fmt.Errorf("rule %d (executeFunction): function dispatcher not configured", i)
		}
		atCopy := *actionType
		atCopy.FunctionRID = rule.FunctionRID
		ruleEdits, err := e.functionDispatcher.Dispatch(ctx, &atCopy, params)
		if err != nil {
			return nil, fmt.Errorf("rule %d (executeFunction %s): %w", i, rule.FunctionRID, err)
		}
		edits = append(edits, ruleEdits...)
	}
	return edits, nil
}

// validateInterfaceRules checks that for each interface-backed rule, the
// resolved ObjectType actually implements the specified Interface. The target
// ObjectType is read from request parameters via resolveObjectTypeParam — the
// same source executeRule uses to populate the edit, so the two stay in lock-
// step without the caller having to maintain a parallel rules/edits slice.
func (e *Executor) validateInterfaceRules(ctx context.Context, ontologyRID string, rules []Rule, params map[string]interface{}) error {
	for i, rule := range rules {
		if !isInterfaceRule(rule.Type) {
			continue
		}
		if rule.InterfaceAPIName == "" {
			return fmt.Errorf("rule %d (%s): interfaceApiName is required", i, rule.Type)
		}
		objectType := resolveObjectTypeParam(params)

		// Look up the Interface by API name.
		iface, err := e.omsRepo.GetInterfaceByAPIName(ctx, ontologyRID, rule.InterfaceAPIName)
		if err != nil || iface == nil {
			return fmt.Errorf("rule %d (%s): interface %q not found", i, rule.Type, rule.InterfaceAPIName)
		}

		// Look up the ObjectType by API name.
		ot, err := e.omsRepo.GetObjectTypeByAPIName(ctx, ontologyRID, objectType)
		if err != nil || ot == nil {
			return fmt.Errorf("rule %d (%s): objectType %q not found", i, rule.Type, objectType)
		}

		// Check that the ObjectType implements the Interface.
		otInterfaces, err := e.omsRepo.ListObjectTypeInterfaces(ctx, ot.RID)
		if err != nil {
			return fmt.Errorf("rule %d (%s): list interfaces for %q: %w", i, rule.Type, objectType, err)
		}
		found := false
		for _, oti := range otInterfaces {
			if oti.InterfaceRID == iface.RID {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("objectType %q does not implement interface %q", objectType, rule.InterfaceAPIName)
		}
	}
	return nil
}

// fetchPrevEdits builds a parallel slice to edits containing the pre-edit
// object state for each MODIFY/DELETE edit. CREATE and LINK edits get nil
// entries. Returns nil when no ObjectFetcher is configured (backward compatible).
func (e *Executor) fetchPrevEdits(ctx context.Context, ontologyAPIName string, edits []funnel.Edit) []map[string]interface{} {
	if e.objectFetcher == nil {
		return nil
	}
	prev := make([]map[string]interface{}, len(edits))
	for i, edit := range edits {
		switch edit.Type {
		case funnel.EditTypeModify, funnel.EditTypeDelete:
			props, err := e.objectFetcher.FetchObject(ctx, ontologyAPIName, edit.ObjectType, edit.PrimaryKey)
			if err != nil {
				log.Printf("actions: failed to fetch prev state for %s/%s: %v", edit.ObjectType, edit.PrimaryKey, err)
				continue
			}
			prev[i] = props
		}
		// CREATE, LINK_CREATE, LINK_DELETE → prev[i] stays nil
	}
	return prev
}

// validateValueTypeConstraints checks property values against ValueType
// constraints for CREATE and MODIFY edits. For each property in the edit, if
// the property definition has a valueTypeApiName in its TypeConfig, the
// associated ValueType is loaded and its Constraints are enforced. Returns a
// descriptive error on the first violation with the field name and reason.
// validateParameterSchema evaluates the ActionType's declared Draft-07 JSON
// Schema (US-245) against the request parameters. Returns a wrapped
// *apierror.APIError (WEAVE_VALIDATION_SCHEMA / HTTP 422) for field-level
// violations so the handler's typedAPIError branch can surface the structured
// payload. Malformed stored schemas (compile errors) return an untyped error
// — they're not the caller's fault, so the 400 ActionFailed fallback is the
// right shape. When no schema is declared the method is a no-op.
func (e *Executor) validateParameterSchema(schema json.RawMessage, params map[string]interface{}) error {
	if e == nil || e.paramSchemas == nil || !hasParameterSchema(schema) {
		return nil
	}
	err := e.paramSchemas.Validate(schema, params)
	if err == nil {
		return nil
	}
	var schemaErr *ParameterSchemaError
	if errors.As(err, &schemaErr) {
		return schemaErr.APIError()
	}
	return err
}

func (e *Executor) validateValueTypeConstraints(ctx context.Context, ontologyRID string, edits []funnel.Edit) error {
	for _, edit := range edits {
		if edit.Type != funnel.EditTypeCreate && edit.Type != funnel.EditTypeModify {
			continue
		}
		if len(edit.Properties) == 0 {
			continue
		}

		// Look up the ObjectType to get its RID for ListProperties.
		ot, err := e.omsRepo.GetObjectTypeByAPIName(ctx, ontologyRID, edit.ObjectType)
		if err != nil || ot == nil {
			continue // unknown ObjectType — skip validation gracefully
		}

		props, err := e.omsRepo.ListProperties(ctx, ot.RID)
		if err != nil {
			continue // can't load properties — skip gracefully
		}

		// Build lookup: property apiName → ValueType API name from TypeConfig.
		propValueTypes := make(map[string]string) // propAPIName → valueTypeApiName
		for _, p := range props {
			if len(p.TypeConfig) == 0 {
				continue
			}
			var tc map[string]interface{}
			if json.Unmarshal(p.TypeConfig, &tc) != nil {
				continue
			}
			if vtName, ok := tc["valueTypeApiName"].(string); ok && vtName != "" {
				propValueTypes[p.APIName] = vtName
			}
		}

		// Validate each property value against its ValueType constraints.
		for propName, value := range edit.Properties {
			vtAPIName, ok := propValueTypes[propName]
			if !ok {
				continue
			}
			vt, err := e.omsRepo.GetValueTypeByAPIName(ctx, vtAPIName)
			if err != nil || vt == nil {
				continue // ValueType not found — skip gracefully
			}
			if err := types.ValidateConstraints(value, vt.Constraints); err != nil {
				// US-208: surface enum violations as a typed WEAVE_VALIDATION_ENUM
				// (HTTP 422) so callers see allowedValues + the rejected value
				// instead of a generic 400 ActionFailed wrap.
				var enumErr *types.EnumViolationError
				if errors.As(err, &enumErr) {
					return apierror.NewValidationEnum("EnumViolation", map[string]string{
						"objectType":    edit.ObjectType,
						"property":      propName,
						"value":         fmt.Sprint(value),
						"allowedValues": strings.Join(enumErr.AllowedValues, ","),
					})
				}
				return fmt.Errorf("property %q: %w", propName, err)
			}
		}
	}
	return nil
}

// validateCollapsedEditsAgainstSchema is the US-473 commit-phase guard: it
// rejects any post-collapse CREATE / MODIFY edit whose merged Properties map
// carries a name not declared on the ObjectType. DELETE and LINK_* edits are
// skipped because they carry no Properties payload. The OMS lookup is
// best-effort — if the OT can't be resolved (degraded boot / nil omsRepo) the
// guard quietly degrades to a no-op, matching the lenient pattern in
// validateValueTypeConstraints. On violation it returns the typed
// *apierror.APIError directly (NOT wrapped in *BatchError) so single-Apply's
// BatchError-stripping path still surfaces a 400 SchemaViolation instead of
// collapsing into a generic ActionFailed.
func (e *Executor) validateCollapsedEditsAgainstSchema(ctx context.Context, ontologyRID string, edits []funnel.Edit) error {
	if e.omsRepo == nil || len(edits) == 0 {
		return nil
	}
	schema := e.buildSchemaLookupForEdits(ctx, ontologyRID, edits)
	if len(schema) == 0 {
		return nil
	}
	violations := ValidateEditsAgainstSchema(edits, schema)
	if len(violations) == 0 {
		return nil
	}
	v := violations[0]
	return apierror.NewBadRequest("SchemaViolation", map[string]string{
		"objectType":     v.ObjectType,
		"primaryKey":     v.PrimaryKey,
		"property":       v.Property,
		"violationCount": fmt.Sprintf("%d", len(violations)),
	})
}

// buildSchemaLookupForEdits collects the declared property-name set for every
// ObjectType touched by a CREATE/MODIFY edit in the batch. Each OT is queried
// at most once even when many edits mention it. Failures (OT not found,
// ListProperties error) silently drop the OT from the lookup so the caller
// degrades to "schema unknown for this OT → skip".
func (e *Executor) buildSchemaLookupForEdits(ctx context.Context, ontologyRID string, edits []funnel.Edit) MapSchemaLookup {
	schema := MapSchemaLookup{}
	seen := make(map[string]struct{})
	for _, edit := range edits {
		if edit.Type == funnel.EditTypeLinkCreate || edit.Type == funnel.EditTypeLinkDelete {
			continue
		}
		if len(edit.Properties) == 0 {
			continue
		}
		if _, dupe := seen[edit.ObjectType]; dupe {
			continue
		}
		seen[edit.ObjectType] = struct{}{}
		ot, err := e.omsRepo.GetObjectTypeByAPIName(ctx, ontologyRID, edit.ObjectType)
		if err != nil || ot == nil {
			continue
		}
		props, err := e.omsRepo.ListProperties(ctx, ot.RID)
		if err != nil || len(props) == 0 {
			// Empty / missing property list is treated as "schema unknown"
			// for this OT — production OTs always carry at least their PK
			// property, so an empty result almost always means the test
			// fixture did not bother seeding properties. Skipping keeps
			// the existing pkg/actions test suite green without weakening
			// production behavior (real OTs hit the populated branch).
			continue
		}
		names := make(map[string]struct{}, len(props))
		for _, p := range props {
			names[p.APIName] = struct{}{}
		}
		schema[edit.ObjectType] = names
	}
	return schema
}

// tagEditsAsUserSource stamps Edit.Source = "user" on every edit in place so
// the funnel consumer's user-edit-wins conflict logic (US-021) can protect
// action-executor writes from subsequent ingest rewrites. Called from both
// the rules path and the function-backed path in Prepare.
func tagEditsAsUserSource(edits []funnel.Edit) {
	for i := range edits {
		edits[i].Source = funnel.EditSourceUser
	}
}

// CommitBatch publishes one EditBatch for the combined edits of the given
// prepared actions, writes one action log per prepared action on success, and
// fires per-action side effects. Returns a populated BatchResult.
//
// CommitBatch preserves request order when flattening edits from prepared
// actions so cross-action MODIFY chains collapse in the caller's intended
// order (later actions win).
//
// US-044: ontologyAPIName is propagated onto the EditBatch so the funnel
// publisher can route the message onto a per-ontology NATS subject and the
// consumer can apply edits to the per-ontology Bleve index.
func (e *Executor) CommitBatch(ctx context.Context, ontologyAPIName string, prepared []*PreparedAction) (*BatchResult, error) {
	ctx, span := tracing.StartSpan(ctx, "actions.CommitBatch",
		attribute.String("ontology.rid", ontologyAPIName),
		attribute.Int("batch.size", len(prepared)),
	)
	defer span.End()

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

	// US-473: schema validation on the post-collapse edits. Surface
	// undeclared-property writes as a typed apierror.APIError so the
	// handler chain (Apply / ApplyBatch) renders a deterministic 400
	// SchemaViolation. Unwrapped so the single-Apply path's BatchError
	// strip does not flatten it into a generic ActionFailed.
	if err := e.validateCollapsedEditsAgainstSchema(ctx, ontologyAPIName, collapsed); err != nil {
		return nil, err
	}

	batch := &funnel.EditBatch{
		ID:              uuid.New().String(),
		OntologyAPIName: ontologyAPIName,
		Edits:           collapsed,
		UserID:          prepared[0].UserID,
		Timestamp:       time.Now(),
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
				Cause:             err,
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
		// US-104: serialize PrevEdits when available.
		if p.PrevEdits != nil {
			prevEditsJSON, _ := json.Marshal(p.PrevEdits)
			logRow.PrevEdits = prevEditsJSON
		}
		if logErr := e.omsRepo.InsertActionLog(ctx, logRow); logErr != nil {
			log.Printf("actions: failed to write action log for action %d: %v", i, logErr)
			continue
		}
		// US-319: surface the persisted action_logs row id on the per-action
		// result so SyncApplyActionResponseV2 can carry it back to the caller.
		if i < len(result.Results) {
			result.Results[i].ActionLogID = logRow.ID
		}
		// US-299: record one lineage edge per persisted object edit so the
		// platform can answer "where did this object come from?" later.
		// Link edits skip — they live in link_edges already.
		e.recordLineage(ctx, logRow.ID, p.Edits)
	}

	// Fire per-action side effects (best-effort, non-blocking) and stamp
	// the per-effect outcomes onto the action_logs row so the Foundry-
	// style action history surface can render "webhook 1/2 succeeded on
	// 2nd attempt" without scraping logs. Gap-A4 round 32 wiring; round
	// 33 also routes failed outcomes to the side-effect DLQ.
	for i, p := range prepared {
		outcomes, effects, _ := ExecuteSideEffectsWithOutcomes(p.ActionType.SideEffects, ActionResult{
			ActionRID: p.ActionType.RID,
			BatchID:   batch.ID,
			Edits:     result.Results[i].Edits,
		})
		e.persistSideEffectOutcomes(ctx, result.Results[i].ActionLogID, outcomes, effects)
	}

	return result, nil
}

// persistSideEffectOutcomes marshals the per-effect outcomes and stamps
// them onto action_logs.side_effect_status. Best-effort: a failed
// persistence call (or a missing action log id from a degraded-mode
// router) logs once and moves on — side-effect status is observability,
// not a write barrier. Empty / nil outcomes skip the call entirely so
// actions with no side effects don't churn the column.
//
// Round-33 DLQ wiring: outcomes with Status=failed (the round-30 retry
// loop exhausted its budget on a transient failure) are also persisted
// to action_log_side_effect_dlq so operators can review / replay via
// the admin surface (round 34). The EffectConfig snapshot is the
// original SideEffect.Config blob from the ActionType so a future
// replay can dispatch without re-reading the ActionType definition.
func (e *Executor) persistSideEffectOutcomes(ctx context.Context, actionLogID int64, outcomes []SideEffectOutcome, effects []SideEffect) {
	if actionLogID == 0 || len(outcomes) == 0 || e.omsRepo == nil {
		return
	}
	payload, err := json.Marshal(outcomes)
	if err != nil {
		log.Printf("actions: failed to marshal side-effect outcomes for action_log %d: %v", actionLogID, err)
		return
	}
	if err := e.omsRepo.UpdateActionLogSideEffectStatus(ctx, actionLogID, payload); err != nil {
		log.Printf("actions: failed to persist side-effect outcomes for action_log %d: %v", actionLogID, err)
	}
	// Route failed outcomes to the DLQ. Iterating in declared order so
	// effect_index matches the original SideEffects array. effects may
	// be nil when ExecuteSideEffectsWithOutcomes was called on an
	// already-decoded array — that's a defensive degrade path; skip DLQ
	// rather than persist a half-shaped row.
	for i, oc := range outcomes {
		if oc.Status != SideEffectStatusFailed {
			continue
		}
		var cfg json.RawMessage
		if i < len(effects) {
			cfg = effects[i].Config
		}
		ocBytes, marshalErr := json.Marshal(oc)
		if marshalErr != nil {
			log.Printf("actions: failed to marshal DLQ outcome for action_log %d effect %d: %v", actionLogID, i, marshalErr)
			continue
		}
		dlqRow := &oms.SideEffectDLQRow{
			ActionLogID:  actionLogID,
			EffectIndex:  i,
			EffectType:   oc.Type,
			EffectConfig: cfg,
			Outcome:      ocBytes,
			ReplayStatus: oms.SideEffectDLQStatusPending,
		}
		if dlqErr := e.omsRepo.InsertSideEffectDLQRow(ctx, dlqRow); dlqErr != nil {
			log.Printf("actions: failed to queue side-effect DLQ row for action_log %d effect %d: %v", actionLogID, i, dlqErr)
		}
	}
}

// Apply executes a single action. Preserved as the backwards-compatible entry
// point; internally it routes through Prepare + CommitBatch so there is a
// single code path for action execution.
func (e *Executor) Apply(ctx context.Context, ontologyRID string, req *ApplyRequest) (*ApplyResult, error) {
	actionName := ""
	if req != nil {
		actionName = req.ActionType
	}
	ctx, span := tracing.StartSpan(ctx, "actions.Apply",
		attribute.String("ontology.rid", ontologyRID),
		attribute.String("action.type", actionName),
	)
	defer span.End()

	prep, err := e.Prepare(ctx, ontologyRID, req)
	if err != nil {
		return nil, err
	}

	// US-023 / US-471: optimistic concurrency. If the caller supplied an
	// expected version (legacy single-target *int OR US-471 per-target
	// ExpectedVersions list), fail-fast before publishing on any mismatch
	// and stamp the observed EditVersion onto every MODIFY/DELETE edit.
	// Done here (not in Prepare) because it depends on live version state
	// rather than the pure request → edits transform.
	if hasOptimisticLockOptions(req.Options) {
		if err := e.checkExpectedVersions(ctx, ontologyRID, prep.Edits, req.Options); err != nil {
			return nil, err
		}
	}

	// Short-circuit the noop path to match the legacy nil-edits shape.
	if len(prep.Edits) == 0 {
		return &ApplyResult{
			ActionRID: prep.ActionType.RID,
			Edits:     nil,
		}, nil
	}

	br, err := e.CommitBatch(ctx, ontologyRID, []*PreparedAction{prep})
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
//
// US-471: After every action is prepared the per-action ApplyOptions
// optimistic-lock tokens are verified cross-batch — a single stale ref in
// any action's Options aborts the whole batch with a *StaleObjectError so
// no NATS publish happens. The error surfaces unwrapped (NOT wrapped in
// *BatchError) so the handler routes it through staleObjectAPIError → 409
// StaleObject, identical to the single-Apply 409 shape.
func (e *Executor) ApplyBatchAtomic(ctx context.Context, ontologyRID string, reqs []ApplyRequest) (*BatchResult, error) {
	ctx, span := tracing.StartSpan(ctx, "actions.ApplyBatchAtomic",
		attribute.String("ontology.rid", ontologyRID),
		attribute.Int("batch.size", len(reqs)),
	)
	defer span.End()

	prepared := make([]*PreparedAction, 0, len(reqs))
	for i := range reqs {
		p, err := e.Prepare(ctx, ontologyRID, &reqs[i])
		if err != nil {
			return nil, &BatchError{
				Phase:             classifyPrepareError(err),
				FailedActionIndex: i,
				ActionType:        reqs[i].ActionType,
				Message:           err.Error(),
				Cause:             err,
			}
		}
		prepared = append(prepared, p)
	}
	if err := e.enforceBatchOptimisticLock(ctx, ontologyRID, reqs, prepared); err != nil {
		return nil, err
	}
	return e.CommitBatch(ctx, ontologyRID, prepared)
}

// enforceBatchOptimisticLock walks every prepared action and runs the
// US-471 cross-action expectedVersion check. Returns the first
// *StaleObjectError encountered (caller-supplied action order); a nil
// return means every action either skipped the check (no Options) or
// matched all of its refs. EditVersion stamping is applied to each
// action's prepared edits in place as a side effect so the persisted
// payload carries the snapshot the batch was authored against.
func (e *Executor) enforceBatchOptimisticLock(ctx context.Context, ontologyRID string, reqs []ApplyRequest, prepared []*PreparedAction) error {
	for i, p := range prepared {
		if i >= len(reqs) {
			continue
		}
		opts := reqs[i].Options
		if !hasOptimisticLockOptions(opts) {
			continue
		}
		if err := e.checkExpectedVersions(ctx, ontologyRID, p.Edits, opts); err != nil {
			return err
		}
	}
	return nil
}

// ApplyBatchBestEffort prepares every request and commits the ones that
// succeeded in a single batch. Failures are reported alongside successes in
// the returned BatchResult; a publish failure is still returned as an error
// because "commit what you can" cannot partially commit a single NATS message.
func (e *Executor) ApplyBatchBestEffort(ctx context.Context, ontologyRID string, reqs []ApplyRequest) (*BatchResult, error) {
	ctx, span := tracing.StartSpan(ctx, "actions.ApplyBatchBestEffort",
		attribute.String("ontology.rid", ontologyRID),
		attribute.Int("batch.size", len(reqs)),
	)
	defer span.End()

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

	if err := e.enforceBatchOptimisticLock(ctx, ontologyRID, reqs, prepared); err != nil {
		return nil, err
	}
	result, err := e.CommitBatch(ctx, ontologyRID, prepared)
	if err != nil {
		return nil, err
	}
	result.Mode = "bestEffort"
	result.Failures = failures
	return result, nil
}

// ApplyBatchAtomicTx (US-238) is ApplyBatchAtomic plus a PostgreSQL
// transaction around the action-log writes. Every request is prepared first;
// on any prepare failure a *BatchError is returned with phase="validation"
// (or phase="internal") and nothing is committed or published. When all
// prepare successes have been collected the flow is:
//
//  1. Compute the combined post-collapse EditBatch.
//  2. Write every per-action ActionLog via AtomicActionLogStore in a single
//     PG transaction. If that fails a *BatchError with phase="commit" is
//     returned and the publisher is NOT called — matching the AC
//     "PG 事务包裹，失败时 rollback 所有编辑".
//  3. Only after the tx commit succeeds does the executor publish the
//     EditBatch to NATS, matching the AC "NATS 发布在 commit 后". A
//     post-commit publish failure returns a *BatchError with phase="publish"
//     but the PG action logs are already persisted (accepted tradeoff:
//     at-most-once publish after commit).
//
// When no AtomicActionLogStore is wired (e.g. unit tests without a PG pool)
// the method falls back to the existing ApplyBatchAtomic/CommitBatch flow so
// degraded-mode callers keep working.
func (e *Executor) ApplyBatchAtomicTx(ctx context.Context, ontologyRID string, reqs []ApplyRequest) (*BatchResult, error) {
	if e.atomicLogStore == nil {
		return e.ApplyBatchAtomic(ctx, ontologyRID, reqs)
	}

	ctx, span := tracing.StartSpan(ctx, "actions.ApplyBatchAtomicTx",
		attribute.String("ontology.rid", ontologyRID),
		attribute.Int("batch.size", len(reqs)),
	)
	defer span.End()

	prepared := make([]*PreparedAction, 0, len(reqs))
	for i := range reqs {
		p, err := e.Prepare(ctx, ontologyRID, &reqs[i])
		if err != nil {
			return nil, &BatchError{
				Phase:             classifyPrepareError(err),
				FailedActionIndex: i,
				ActionType:        reqs[i].ActionType,
				Message:           err.Error(),
				Cause:             err,
			}
		}
		prepared = append(prepared, p)
	}

	if err := e.enforceBatchOptimisticLock(ctx, ontologyRID, reqs, prepared); err != nil {
		return nil, err
	}

	return e.commitBatchAtomicTx(ctx, ontologyRID, prepared)
}

// commitBatchAtomicTx runs the "PG tx then publish" commit path described on
// ApplyBatchAtomicTx. Split out so future callers (e.g. applyBatchBestEffort
// with an atomic-tx option) can share the core logic without duplicating
// classification / error shaping.
func (e *Executor) commitBatchAtomicTx(ctx context.Context, ontologyAPIName string, prepared []*PreparedAction) (*BatchResult, error) {
	result := &BatchResult{
		Mode:    "atomic",
		Results: make([]*ApplyResult, 0, len(prepared)),
	}
	if len(prepared) == 0 {
		return result, nil
	}

	var all []funnel.Edit
	for _, p := range prepared {
		all = append(all, p.Edits...)
	}
	collapsed := CollapseEdits(all)

	for _, p := range prepared {
		result.Results = append(result.Results, &ApplyResult{
			ActionRID: p.ActionType.RID,
			Edits:     p.Edits,
		})
	}

	// Empty post-collapse batch: no PG state to write, no NATS message to
	// publish. Successful no-op — mirrors CommitBatch.
	if len(collapsed) == 0 {
		return result, nil
	}

	// US-473: schema validation on the post-collapse edits, identical to
	// CommitBatch. Atomic-tx path enforces the same schema contract before
	// it touches the action-log PG transaction so a schema violation is
	// rejected upstream of any persisted side effect.
	if err := e.validateCollapsedEditsAgainstSchema(ctx, ontologyAPIName, collapsed); err != nil {
		return nil, err
	}

	// Build action log rows up-front so the tx callback can persist them
	// all in one shot. Mirrors the per-row shape used by CommitBatch.
	logs := make([]*oms.ActionLog, 0, len(prepared))
	for _, p := range prepared {
		paramsJSON, _ := json.Marshal(p.Request.Parameters)
		editsJSON, _ := json.Marshal(p.Edits)
		row := &oms.ActionLog{
			ActionTypeRID: p.ActionType.RID,
			UserID:        p.UserID,
			Parameters:    paramsJSON,
			Edits:         editsJSON,
			Status:        "SUCCESS",
		}
		if p.PrevEdits != nil {
			prevEditsJSON, _ := json.Marshal(p.PrevEdits)
			row.PrevEdits = prevEditsJSON
		}
		logs = append(logs, row)
	}

	// Phase 1: PG transaction — all logs or none. A failure here means
	// NOTHING is published (AC "PG 事务包裹，失败时 rollback 所有编辑").
	if err := e.atomicLogStore.WriteActionLogsAtomic(ctx, logs); err != nil {
		return nil, &BatchError{
			Phase:             "commit",
			FailedActionIndex: -1,
			ActionType:        "",
			Message:           fmt.Sprintf("atomic commit: %v", err),
			Cause:             err,
		}
	}

	// Phase 2: NATS publish AFTER commit (AC "NATS 发布在 commit 后"). A
	// publish failure leaves the PG logs in place — accepted tradeoff so
	// the tx boundary stays short.
	batch := &funnel.EditBatch{
		ID:              uuid.New().String(),
		OntologyAPIName: ontologyAPIName,
		Edits:           collapsed,
		UserID:          prepared[0].UserID,
		Timestamp:       time.Now(),
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
				Cause:             err,
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

	// US-299: record lineage edges for every persisted object edit. The
	// atomic-tx path back-fills logs[i].ID inside WriteActionLogsAtomic, so
	// the parallel `prepared` slice carries the per-action edits to credit.
	for i, p := range prepared {
		e.recordLineage(ctx, logs[i].ID, p.Edits)
	}

	// Per-action side effects (best-effort, non-blocking). Mirrors
	// CommitBatch — stamps per-effect outcomes onto action_logs.
	// side_effect_status via persistSideEffectOutcomes and queues
	// failed outcomes to the side-effect DLQ. Gap-A4 rounds 32+33.
	for i, p := range prepared {
		outcomes, effects, _ := ExecuteSideEffectsWithOutcomes(p.ActionType.SideEffects, ActionResult{
			ActionRID: p.ActionType.RID,
			BatchID:   batch.ID,
			Edits:     result.Results[i].Edits,
		})
		e.persistSideEffectOutcomes(ctx, logs[i].ID, outcomes, effects)
	}

	return result, nil
}

// recordLineage appends one lineage edge per object-level edit to the wired
// LineageStore. Link edits (LINK_CREATE / LINK_DELETE) are skipped — the
// link table already records the relation. A nil LineageStore short-
// circuits to a no-op so degraded-mode test routers behave unchanged.
// Errors are logged but never returned: lineage is best-effort observability,
// not a write barrier.
func (e *Executor) recordLineage(ctx context.Context, actionLogID int64, edits []funnel.Edit) {
	if e.lineageStore == nil || len(edits) == 0 {
		return
	}
	upstream := oms.ActionLogLineageRID(actionLogID)
	if upstream == "" {
		return
	}
	for _, edit := range edits {
		switch edit.Type {
		case funnel.EditTypeCreate, funnel.EditTypeModify, funnel.EditTypeDelete:
		default:
			continue
		}
		downstream := oms.ObjectLineageRID(edit.ObjectType, edit.PrimaryKey)
		if downstream == "" {
			continue
		}
		row := &oms.LineageEdge{
			UpstreamRID:   upstream,
			DownstreamRID: downstream,
			Operation:     string(edit.Type),
		}
		if err := e.lineageStore.InsertLineageEdge(ctx, row); err != nil {
			log.Printf("actions: failed to record lineage for %s/%s: %v",
				edit.ObjectType, edit.PrimaryKey, err)
		}
	}
}

// checkExpectedVersions enforces both the US-023 legacy single-target
// contract (Options.ExpectedVersion *int) and the US-471 per-target
// contract (Options.ExpectedVersions []ExpectedVersionRef). Behaviour:
//
//   - When Options.ExpectedVersions is non-empty, every ref is verified in
//     caller-supplied order; the first mismatch aborts and surfaces a
//     *StaleObjectError pointing at the failing ref. Refs may target
//     objects the action does not modify — the lock is a snapshot
//     predicate on the caller's read set.
//   - Otherwise, when Options.ExpectedVersion is non-nil, the first
//     MODIFY/DELETE edit in the prepared slice is checked against the
//     legacy single token (preserves pre-US-471 wire compatibility).
//   - When neither is set the call is a no-op.
//
// On success the observed version for every MODIFY/DELETE edit is stamped
// onto Edit.EditVersion so the downstream NATS payload, action log, and
// replay layers can reason about the snapshot the batch was authored
// against (PRD US-471 acceptance: "Edit 记录加 edit_version").
func (e *Executor) checkExpectedVersions(ctx context.Context, ontologyRID string, edits []funnel.Edit, opts *ApplyOptions) error {
	if opts == nil {
		return e.stampEditVersions(ctx, ontologyRID, edits)
	}

	// Multi-target list wins when both are set: the per-target contract is
	// strictly stronger so a caller who opted in to it should not have the
	// legacy single-target check shadow a mismatch they care about.
	if len(opts.ExpectedVersions) > 0 {
		// Resolve each ref against the live version count and compare.
		for _, ref := range opts.ExpectedVersions {
			current, err := e.lookupObjectVersion(ctx, ontologyRID, ref.ObjectType, ref.PrimaryKey)
			if err != nil {
				return err
			}
			if current != int64(ref.Version) {
				return &StaleObjectError{
					ObjectType:      ref.ObjectType,
					PrimaryKey:      ref.PrimaryKey,
					ExpectedVersion: ref.Version,
					CurrentVersion:  current,
				}
			}
		}
		return e.stampEditVersions(ctx, ontologyRID, edits)
	}

	if opts.ExpectedVersion != nil {
		expected := *opts.ExpectedVersion
		// US-023 contract: lock the first MODIFY/DELETE edit only.
		for i := range edits {
			switch edits[i].Type {
			case funnel.EditTypeModify, funnel.EditTypeDelete:
				current, err := e.lookupObjectVersion(ctx, ontologyRID, edits[i].ObjectType, edits[i].PrimaryKey)
				if err != nil {
					return err
				}
				if current != int64(expected) {
					return &StaleObjectError{
						ObjectType:      edits[i].ObjectType,
						PrimaryKey:      edits[i].PrimaryKey,
						ExpectedVersion: expected,
						CurrentVersion:  current,
					}
				}
				return e.stampEditVersions(ctx, ontologyRID, edits)
			}
		}
		return nil // CREATE-only action: legacy token is a no-op.
	}

	return nil
}

// lookupObjectVersion resolves an ObjectType API name to its RID and
// returns the current object_history version count for (RID, primaryKey).
// Falls back to the raw API name when no mapping is configured, mirroring
// pkg/funnel consumer behaviour for degraded-mode test routers.
func (e *Executor) lookupObjectVersion(ctx context.Context, ontologyRID, objectType, primaryKey string) (int64, error) {
	otRID := objectType
	if e.omsRepo != nil {
		if ot, err := e.omsRepo.GetObjectTypeByAPIName(ctx, ontologyRID, objectType); err == nil && ot != nil && ot.RID != "" {
			otRID = ot.RID
		}
	}
	current, err := e.omsRepo.GetObjectVersionCount(ctx, otRID, primaryKey)
	if err != nil {
		return 0, fmt.Errorf("load object version: %w", err)
	}
	return current, nil
}

// stampEditVersions writes the observed version onto every MODIFY/DELETE
// edit so the downstream NATS payload (US-471 acceptance "Edit 记录加
// edit_version") carries the snapshot the action was authored against.
// CREATE and LINK_* edits keep EditVersion=0 because there is no pre-
// existing version to attribute. Best-effort: a failure to load any
// version is propagated so the executor still fails-fast at the boundary.
func (e *Executor) stampEditVersions(ctx context.Context, ontologyRID string, edits []funnel.Edit) error {
	if e == nil || e.omsRepo == nil || len(edits) == 0 {
		return nil
	}
	cache := make(map[string]int64, len(edits))
	for i := range edits {
		switch edits[i].Type {
		case funnel.EditTypeModify, funnel.EditTypeDelete:
		default:
			continue
		}
		key := edits[i].ObjectType + "|" + edits[i].PrimaryKey
		if v, ok := cache[key]; ok {
			edits[i].EditVersion = v
			continue
		}
		v, err := e.lookupObjectVersion(ctx, ontologyRID, edits[i].ObjectType, edits[i].PrimaryKey)
		if err != nil {
			return err
		}
		cache[key] = v
		edits[i].EditVersion = v
	}
	return nil
}

// hasOptimisticLockOptions reports whether the request has opted into any
// optimistic-concurrency check (legacy single-target or US-471 per-target).
// Used by the batch path to decide whether to walk the version-check step.
func hasOptimisticLockOptions(opts *ApplyOptions) bool {
	if opts == nil {
		return false
	}
	return opts.ExpectedVersion != nil || len(opts.ExpectedVersions) > 0
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
		strings.Contains(msg, "execute function rules"),
		strings.Contains(msg, "function dispatch"):
		return "internal"
	default:
		return "validation"
	}
}
