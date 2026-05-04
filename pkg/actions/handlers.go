package actions

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/functions"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/oms"
)

// withBranchScope wraps r so downstream handler code that propagates
// r.Context() through the executor sees the request's `?branch=` param
// stamped via oms.WithBranchScope. The branch scope is what drives
// US-384 routing of ActionType and Function lookups to the
// branch-specific row when the branch has published its own version.
// An empty / "main" branch input is a no-op so the legacy main-only
// path stays free of context churn.
func withBranchScope(r *http.Request) *http.Request {
	branch := r.URL.Query().Get("branch")
	if branch == "" || branch == oms.DefaultBranch {
		return r
	}
	return r.WithContext(oms.WithBranchScope(r.Context(), branch))
}

// staleObjectAPIError converts an Executor-level *StaleObjectError into the
// Palantir-wire-format 409 Conflict response used by US-023 optimistic
// concurrency. Returns nil when err is not a *StaleObjectError so the
// caller can fall through to its existing error translation path.
func staleObjectAPIError(err error) *apierror.APIError {
	var stale *StaleObjectError
	if !errors.As(err, &stale) {
		return nil
	}
	return apierror.NewConflict("StaleObject", map[string]string{
		"objectType":      stale.ObjectType,
		"primaryKey":      stale.PrimaryKey,
		"expectedVersion": strconv.Itoa(stale.ExpectedVersion),
		"currentVersion":  strconv.FormatInt(stale.CurrentVersion, 10),
	})
}

// typedAPIError unwraps a chained *apierror.APIError (e.g. WEAVE_VALIDATION_ENUM
// from US-208 ValueType constraint enforcement) so the handler surfaces the
// pre-built status code + parameters instead of collapsing it into a generic
// 400 ActionFailed. Returns nil when no typed error is present.
func typedAPIError(err error) *apierror.APIError {
	var apiErr *apierror.APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return nil
}

// Handler handles action HTTP requests.
type Handler struct {
	executor *Executor
}

// NewHandler creates a new action handler.
func NewHandler(executor *Executor) *Handler {
	return &Handler{executor: executor}
}

// Apply handles POST /api/v2/ontologies/{ontologyApiName}/actions/{action}/apply.
//
// The action API name lives in the URL (Foundry OSv2 shape). Any
// actionType field in the body is ignored — the path is the single
// source of truth. An empty {action} path segment is rejected with
// MissingActionType so malformed URLs surface a clean 400.
//
// Foundry OSv2 options:
//   - options.mode: VALIDATE_ONLY | VALIDATE_AND_EXECUTE (default)
//   - options.returnEdits: ALL | ALL_V2_WITH_DELETIONS | NONE (default ALL)
func (h *Handler) Apply(w http.ResponseWriter, r *http.Request) {
	r = withBranchScope(r)
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	action := chi.URLParam(r, "action")

	if action == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingActionType", nil))
		return
	}

	var req ApplyRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"error": err.Error()}))
		return
	}
	req.ActionType = action

	// Resolve options with defaults.
	mode := "VALIDATE_AND_EXECUTE"
	returnEdits := "ALL"
	if req.Options != nil {
		if req.Options.Mode != "" {
			mode = strings.ToUpper(req.Options.Mode)
		}
		if req.Options.ReturnEdits != "" {
			returnEdits = strings.ToUpper(req.Options.ReturnEdits)
		}
	}

	// Validate mode enum.
	if mode != "VALIDATE_ONLY" && mode != "VALIDATE_AND_EXECUTE" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidMode",
			map[string]string{"mode": mode, "allowed": "VALIDATE_ONLY, VALIDATE_AND_EXECUTE"}))
		return
	}

	// VALIDATE_ONLY: run Prepare only, return validation result.
	if mode == "VALIDATE_ONLY" {
		_, err := h.executor.Prepare(r.Context(), ontologyRID, &req)
		if err != nil {
			httputil.WriteJSON(w, http.StatusOK, &ValidateOnlyResponse{
				Validation: &ValidationResult{Result: "INVALID"},
			})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, &ValidateOnlyResponse{
			Validation: &ValidationResult{Result: "VALID"},
		})
		return
	}

	// US-240: opt-in async via ?async=true. When an ActionJobStore is wired
	// we persist a PENDING job row, return 202 {jobId}, and run the Apply in
	// a detached goroutine that updates the row as it progresses. When no
	// store is wired (e.g. degraded-mode test harness without PG) the query
	// param is silently ignored and the call falls through to the sync path
	// so the response contract stays stable for callers without a catalog.
	if r.URL.Query().Get("async") == "true" && h.executor.ActionJobStore() != nil {
		h.serveAsyncApply(w, r, ontologyRID, &req, returnEdits)
		return
	}

	// US-242: approval gate. When the target ActionType is flagged
	// RequiresApproval AND an ActionApprovalStore is wired, enqueue a
	// PENDING approval row and short-circuit with 202 {approvalId}. Degraded
	// mode (no store) ignores the flag so the sync apply contract stays
	// stable for tests / single-user deployments. The caller must already
	// have supplied valid parameters — approval snapshots the body so the
	// reviewer sees exactly what will run if they approve.
	if h.executor.ActionApprovalStore() != nil {
		at, resolveErr := h.executor.ResolveActionType(r.Context(), ontologyRID, action)
		if resolveErr == nil && at != nil && at.RequiresApproval {
			h.serveApprovalEnqueue(w, r, ontologyRID, at, &req)
			return
		}
	}

	// VALIDATE_AND_EXECUTE: normal execution.
	result, err := h.executor.Apply(r.Context(), ontologyRID, &req)
	if err != nil {
		if staleErr := staleObjectAPIError(err); staleErr != nil {
			apierror.WriteJSON(w, staleErr)
			return
		}
		if apiErr := typedAPIError(err); apiErr != nil {
			apierror.WriteJSON(w, apiErr)
			return
		}
		apierror.WriteJSON(w, apierror.NewInvalidParameter("ActionFailed", map[string]string{"error": err.Error()}))
		return
	}

	// Build SyncApplyActionResponseV2 envelope.
	resp := &SyncApplyActionResponseV2{
		OperationID: result.BatchID,
		ActionLogID: result.ActionLogID,
	}
	if returnEdits != "NONE" {
		resp.Edits = countEdits(result.Edits)
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// serveAsyncApply persists a PENDING ActionJob, kicks off a detached goroutine
// that runs the sync Apply path and updates the job row, and returns a 202
// Accepted envelope carrying {jobId}. The goroutine uses a fresh
// context.Background() (not the request context) so Apply keeps running after
// the HTTP response has been written.
func (h *Handler) serveAsyncApply(w http.ResponseWriter, r *http.Request, ontologyRID string, req *ApplyRequest, returnEdits string) {
	store := h.executor.ActionJobStore()
	job := &ActionJob{
		JobID:          uuid.New().String(),
		OntologyAPI:    ontologyRID,
		ActionTypeName: req.ActionType,
		Status:         ActionJobStatusPending,
		Progress:       0,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	if u := auth.UserFromContext(r.Context()); u != nil {
		job.CreatedBy = u.ID
	}
	if err := store.CreateActionJob(r.Context(), job); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ActionJobCreateFailed",
			map[string]string{"error": err.Error()}))
		return
	}

	// Copy request + ontology onto a detached context so the goroutine keeps
	// running once the response has been flushed. Auth user identity is
	// captured via copyAuthContext so the underlying Apply still records the
	// correct caller in ActionLog.
	bgCtx := copyAuthContext(context.Background(), r.Context())
	reqCopy := *req

	go runAsyncApply(bgCtx, h.executor, store, job.JobID, ontologyRID, &reqCopy, returnEdits)

	httputil.WriteJSON(w, http.StatusAccepted, &AsyncApplyResponse{
		JobID:  job.JobID,
		Status: ActionJobStatusPending,
	})
}

// runAsyncApply is the detached worker goroutine for the async apply path.
// It walks the job through RUNNING → SUCCEEDED/FAILED, persisting progress
// markers along the way so pollers see forward motion without having to wait
// for the terminal state. All store errors are best-effort logged — the
// goroutine MUST NOT block on a failing store write since nothing is watching.
func runAsyncApply(ctx context.Context, exec *Executor, store ActionJobStore, jobID, ontologyRID string, req *ApplyRequest, returnEdits string) {
	// Transition PENDING → RUNNING with 10% progress so SDK pollers see the
	// worker has picked the job up.
	progress10 := 10
	if err := store.UpdateActionJob(ctx, jobID, ActionJobUpdate{
		Status:   ActionJobStatusRunning,
		Progress: &progress10,
	}); err != nil {
		log.Printf("actions: async job %s: failed to mark RUNNING: %v", jobID, err)
	}

	// US-241: Install a progress reporter on ctx so Goja-backed action
	// functions can surface live updates via `weave.reportProgress(percent,
	// message)`. The reporter writes back into the action_jobs row AND
	// fans out a NATS event on actions.progress.<jobId>. Publisher may be
	// nil in degraded mode (no NATS) — the store update path still runs.
	reporter := newJobProgressReporter(store, exec.ProgressPublisher(), jobID, ontologyRID, req.ActionType)
	ctx = functions.WithProgressReporter(ctx, reporter)

	result, err := exec.Apply(ctx, ontologyRID, req)
	if err != nil {
		msg := err.Error()
		failProg := 0
		upd := ActionJobUpdate{
			Status:       ActionJobStatusFailed,
			Progress:     &failProg,
			ErrorMessage: &msg,
		}
		if updErr := store.UpdateActionJob(ctx, jobID, upd); updErr != nil {
			log.Printf("actions: async job %s: failed to mark FAILED: %v", jobID, updErr)
		}
		return
	}

	// Build the sync response envelope so pollers receive an identical shape
	// to the sync path once the job completes.
	resp := &SyncApplyActionResponseV2{OperationID: result.BatchID}
	if returnEdits != "NONE" {
		resp.Edits = countEdits(result.Edits)
	}
	resultJSON, _ := json.Marshal(resp)

	doneProg := 100
	if err := store.UpdateActionJob(ctx, jobID, ActionJobUpdate{
		Status:   ActionJobStatusSucceeded,
		Progress: &doneProg,
		Result:   resultJSON,
	}); err != nil {
		log.Printf("actions: async job %s: failed to mark SUCCEEDED: %v", jobID, err)
	}
}

// copyAuthContext copies the authenticated User (if present) from src onto
// dst. Used to hand the async goroutine a background context that still
// carries the caller's identity for ActionLog / rate-limit keys while
// detaching from the request's cancellation.
func copyAuthContext(dst, src context.Context) context.Context {
	if u := auth.UserFromContext(src); u != nil {
		return auth.WithUser(dst, u)
	}
	return dst
}

// serveAsyncApplyBatch persists a PENDING ActionJob, kicks off a detached
// goroutine that walks the batch one action at a time, and returns 202
// Accepted carrying {jobId}. Per-action progress is reported into the job
// row + the executor's ProgressPublisher so WebSocket subscribers see live
// updates. US-318.
func (h *Handler) serveAsyncApplyBatch(w http.ResponseWriter, r *http.Request, ontologyRID, action string, reqs []ApplyRequest, returnEdits string) {
	store := h.executor.ActionJobStore()
	job := &ActionJob{
		JobID:          uuid.New().String(),
		OntologyAPI:    ontologyRID,
		ActionTypeName: action,
		Status:         ActionJobStatusPending,
		Progress:       0,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	if u := auth.UserFromContext(r.Context()); u != nil {
		job.CreatedBy = u.ID
	}
	if err := store.CreateActionJob(r.Context(), job); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ActionJobCreateFailed",
			map[string]string{"error": err.Error()}))
		return
	}

	bgCtx := copyAuthContext(context.Background(), r.Context())
	cancelCtx, cancel := context.WithCancel(bgCtx)
	h.executor.RegisterJobCancel(job.JobID, cancel)

	// Snapshot reqs for the goroutine — the slice is owned by the caller's
	// request body which would otherwise be GC'd once the handler returns.
	reqsCopy := make([]ApplyRequest, len(reqs))
	copy(reqsCopy, reqs)

	go runAsyncApplyBatch(cancelCtx, h.executor, store, job.JobID, ontologyRID, action, reqsCopy, returnEdits, cancel)

	httputil.WriteJSON(w, http.StatusAccepted, &AsyncApplyResponse{
		JobID:  job.JobID,
		Status: ActionJobStatusPending,
	})
}

// runAsyncApplyBatch is the detached worker for async batch apply. It walks
// the action list one-by-one calling exec.Apply, accumulating edits, and
// stamping per-action progress into the job row + NATS/WebSocket fanout.
// On context cancellation (POST /actions/jobs/{id}/cancel) it stops the loop
// and marks the job CANCELED; already-applied actions are NOT rolled back
// (async-batch is non-atomic by construction — callers wanting strict
// rollback semantics use the saga / atomic-tx paths which run synchronously).
func runAsyncApplyBatch(ctx context.Context, exec *Executor, store ActionJobStore, jobID, ontologyRID, action string, reqs []ApplyRequest, returnEdits string, cancel context.CancelFunc) {
	defer exec.UnregisterJobCancel(jobID)
	defer cancel()

	publisher := exec.ProgressPublisher()
	emitProgress := func(percent int, message string) {
		if publisher == nil {
			return
		}
		evt := ProgressEvent{
			JobID:      jobID,
			Ontology:   ontologyRID,
			ActionType: action,
			Percent:    percent,
			Message:    message,
			ReportedAt: time.Now(),
		}
		data, err := json.Marshal(&evt)
		if err != nil {
			log.Printf("actions: async batch %s: marshal progress failed: %v", jobID, err)
			return
		}
		if err := publisher.PublishProgress(ProgressSubject(jobID), data); err != nil {
			log.Printf("actions: async batch %s: publish progress failed: %v", jobID, err)
		}
	}

	// PENDING → RUNNING with 0% to signal the worker has picked it up.
	startProg := 0
	if err := store.UpdateActionJob(ctx, jobID, ActionJobUpdate{
		Status:   ActionJobStatusRunning,
		Progress: &startProg,
	}); err != nil {
		log.Printf("actions: async batch %s: failed to mark RUNNING: %v", jobID, err)
	}
	emitProgress(0, "starting")

	total := len(reqs)
	if total == 0 {
		// Empty batch is a no-op success.
		resp := &BatchApplyActionResponseV2{}
		if returnEdits != "NONE" {
			resp.Edits = countEdits(nil)
		}
		resultJSON, _ := json.Marshal(resp)
		doneProg := 100
		_ = store.UpdateActionJob(ctx, jobID, ActionJobUpdate{
			Status:   ActionJobStatusSucceeded,
			Progress: &doneProg,
			Result:   resultJSON,
		})
		emitProgress(100, "done")
		return
	}

	for i, req := range reqs {
		// Cancellation check at every iteration boundary. Mid-Apply
		// cancellation is honoured by the underlying ctx propagating into
		// PG / NATS / Bleve calls — this gate stops us from STARTING a new
		// action after cancel was signalled.
		if err := ctx.Err(); err != nil {
			lastProg := percentForStep(i, total)
			cancelMsg := "canceled"
			_ = store.UpdateActionJob(context.Background(), jobID, ActionJobUpdate{
				Status:       ActionJobStatusCanceled,
				Progress:     &lastProg,
				ErrorMessage: &cancelMsg,
			})
			emitProgress(lastProg, "canceled")
			return
		}

		reqCopy := req
		_, err := exec.Apply(ctx, ontologyRID, &reqCopy)
		if err != nil {
			// Distinguish cancellation-mid-Apply from genuine failure: a
			// cancel during the underlying Apply surfaces as
			// context.Canceled which we treat as CANCELED, not FAILED.
			if errors.Is(err, context.Canceled) {
				lastProg := percentForStep(i, total)
				cancelMsg := "canceled"
				_ = store.UpdateActionJob(context.Background(), jobID, ActionJobUpdate{
					Status:       ActionJobStatusCanceled,
					Progress:     &lastProg,
					ErrorMessage: &cancelMsg,
				})
				emitProgress(lastProg, "canceled")
				return
			}
			msg := err.Error()
			failProg := percentForStep(i, total)
			_ = store.UpdateActionJob(context.Background(), jobID, ActionJobUpdate{
				Status:       ActionJobStatusFailed,
				Progress:     &failProg,
				ErrorMessage: &msg,
			})
			emitProgress(failProg, msg)
			return
		}

		done := i + 1
		percent := percentForStep(done, total)
		p := percent
		if err := store.UpdateActionJob(ctx, jobID, ActionJobUpdate{
			Status:   ActionJobStatusRunning,
			Progress: &p,
		}); err != nil {
			log.Printf("actions: async batch %s: failed to mark RUNNING progress=%d: %v", jobID, percent, err)
		}
		emitProgress(percent, "")
	}

	resp := &BatchApplyActionResponseV2{}
	if returnEdits != "NONE" {
		resp.Edits = &ActionResults{Type: "edits"}
	}
	resultJSON, _ := json.Marshal(resp)
	doneProg := 100
	if err := store.UpdateActionJob(ctx, jobID, ActionJobUpdate{
		Status:   ActionJobStatusSucceeded,
		Progress: &doneProg,
		Result:   resultJSON,
	}); err != nil {
		log.Printf("actions: async batch %s: failed to mark SUCCEEDED: %v", jobID, err)
	}
	emitProgress(100, "done")
}

// percentForStep maps a 0..total step count onto the 0..100 percentage range.
// Reserves 100 for "all steps complete" so an in-flight 99% never collides
// with the terminal SUCCEEDED state's 100%.
func percentForStep(done, total int) int {
	if total <= 0 {
		return 100
	}
	if done >= total {
		return 100
	}
	if done <= 0 {
		return 0
	}
	p := done * 100 / total
	if p >= 100 {
		// Reserve 100 strictly for "all done" so callers can distinguish
		// the terminal SUCCEEDED state from the last in-flight tick.
		return 99
	}
	return p
}

// CancelJob handles POST /api/v2/ontologies/{ontologyApiName}/actions/jobs/{jobId}/cancel.
// Signals the worker for jobID to stop. Returns 202 Accepted with the current
// job row when a runner was signalled, 404 when no in-flight job matches.
// Already-terminal jobs (SUCCEEDED / FAILED / CANCELED) report 409 Conflict so
// callers don't silently accept a no-op cancel. US-318.
func (h *Handler) CancelJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	if jobID == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingJobId", nil))
		return
	}
	store := h.executor.ActionJobStore()
	if store == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("ActionJobNotFound",
			map[string]string{"jobId": jobID}))
		return
	}
	job, err := store.GetActionJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ActionJobNotFound",
				map[string]string{"jobId": jobID}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ActionJobLoadFailed",
			map[string]string{"error": err.Error()}))
		return
	}
	if isTerminalJobStatus(job.Status) {
		apierror.WriteJSON(w, apierror.NewConflict("ActionJobAlreadyTerminal",
			map[string]string{"jobId": jobID, "status": job.Status}))
		return
	}
	if !h.executor.CancelJob(jobID) {
		// No registered cancel — runner has finished but status hasn't yet
		// been flushed (race), or a future multi-host setup landed the
		// runner on a different replica. Surface a 409 either way so the
		// caller knows to re-poll.
		apierror.WriteJSON(w, apierror.NewConflict("ActionJobNotCancelable",
			map[string]string{"jobId": jobID, "status": job.Status}))
		return
	}
	httputil.WriteJSON(w, http.StatusAccepted, job)
}

// isTerminalJobStatus reports whether a job status is terminal (no further
// transitions possible). SUCCEEDED, FAILED, and CANCELED are all terminal.
func isTerminalJobStatus(s string) bool {
	switch s {
	case ActionJobStatusSucceeded, ActionJobStatusFailed, ActionJobStatusCanceled:
		return true
	}
	return false
}

// DeleteJob handles DELETE /api/v2/ontologies/{ontologyApiName}/actions/jobs/{jobId}.
// REST-style alias of CancelJob: marks the job CANCELED and signals the
// in-flight worker to stop. 202 Accepted with the (now CANCELED) job row when
// a runner was signalled, 404 when the jobId is unknown, 409 when already
// terminal. US-426.
//
// Distinct from CancelJob in two ways: (1) the underlying status is flipped to
// CANCELED *here* (not deferred to the worker) so the response carries the
// post-cancel state inline — this matches the DELETE semantics ("the
// resource is now in the deleted state from the caller's perspective"); the
// worker then observes ctx.Done() and exits without re-stamping. (2) When the
// runner finished but its status flush race lost (no registered cancel)
// DeleteJob still flips the row to CANCELED and returns 202 — DELETE is
// idempotent on the durable side, the cancel signal is best-effort.
func (h *Handler) DeleteJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	if jobID == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingJobId", nil))
		return
	}
	store := h.executor.ActionJobStore()
	if store == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("ActionJobNotFound",
			map[string]string{"jobId": jobID}))
		return
	}
	job, err := store.GetActionJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ActionJobNotFound",
				map[string]string{"jobId": jobID}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ActionJobLoadFailed",
			map[string]string{"error": err.Error()}))
		return
	}
	if isTerminalJobStatus(job.Status) {
		apierror.WriteJSON(w, apierror.NewConflict("ActionJobAlreadyTerminal",
			map[string]string{"jobId": jobID, "status": job.Status}))
		return
	}
	// Signal the worker first so the next iteration boundary observes ctx
	// cancellation. CancelJob is best-effort: a finished-but-not-flushed
	// runner returns false but the durable flip below still owns the row's
	// terminal state, which is the contract DELETE callers actually care
	// about.
	_ = h.executor.CancelJob(jobID)

	// Durably flip the row to CANCELED so the response and any subsequent
	// GET observe the post-cancel state without waiting for the worker's
	// next tick. The worker's own UpdateActionJob writes that fire from
	// runAsyncApplyBatch's ctx.Err() branch are idempotent against this
	// flip — they re-stamp CANCELED with the last progress percent.
	cancelMsg := "canceled"
	if err := store.UpdateActionJob(r.Context(), jobID, ActionJobUpdate{
		Status:       ActionJobStatusCanceled,
		ErrorMessage: &cancelMsg,
	}); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ActionJobUpdateFailed",
			map[string]string{"error": err.Error()}))
		return
	}
	job.Status = ActionJobStatusCanceled
	job.ErrorMessage = cancelMsg
	httputil.WriteJSON(w, http.StatusAccepted, job)
}

// GetJob handles GET /api/v2/ontologies/{ontologyApiName}/actions/jobs/{jobId}.
// Returns the current ActionJob row as JSON. 404 if the job does not exist.
// When no ActionJobStore is wired (degraded mode) the endpoint returns 404 so
// callers that never persisted a job get a consistent shape.
func (h *Handler) GetJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	if jobID == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingJobId", nil))
		return
	}
	store := h.executor.ActionJobStore()
	if store == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("ActionJobNotFound",
			map[string]string{"jobId": jobID}))
		return
	}
	job, err := store.GetActionJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ActionJobNotFound",
				map[string]string{"jobId": jobID}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ActionJobLoadFailed",
			map[string]string{"error": err.Error()}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, job)
}

// ApplyActionOverrides is the Foundry OSv2 override envelope. In Foundry this
// carries uniqueIdentifier and currentTime knobs used to make auto-generated
// parameters deterministic. Weave does not currently auto-generate parameters,
// so the only meaningful override today is an explicit parameter override map
// which is merged into the wrapped request's parameters (overrides win).
type ApplyActionOverrides struct {
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

// applyWithOverridesEnvelope is the Foundry OSv2 request body for
// POST .../actions/{action}/applyWithOverrides.
type applyWithOverridesEnvelope struct {
	Request   *ApplyRequest         `json:"request"`
	Overrides *ApplyActionOverrides `json:"overrides,omitempty"`
}

// ApplyWithOverrides handles POST /api/v2/ontologies/{ontologyApiName}/actions/{action}/applyWithOverrides.
//
// The request body wraps an ApplyActionRequestV2 in a `request` field and an
// ApplyActionOverrides in an `overrides` field. Overrides.parameters are
// merged into request.parameters (overrides win), then the resulting request
// is routed through the same Apply code path so options.mode and
// options.returnEdits behave identically to the plain apply endpoint.
func (h *Handler) ApplyWithOverrides(w http.ResponseWriter, r *http.Request) {
	r = withBranchScope(r)
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	action := chi.URLParam(r, "action")

	if action == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingActionType", nil))
		return
	}

	var env applyWithOverridesEnvelope
	if err := httputil.ReadJSON(r, &env); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"error": err.Error()}))
		return
	}
	if env.Request == nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingRequest",
			map[string]string{"field": "request", "message": "request field is required"}))
		return
	}

	req := *env.Request
	req.ActionType = action

	// Merge overrides into parameters. Overrides win on key collision.
	if env.Overrides != nil && len(env.Overrides.Parameters) > 0 {
		if req.Parameters == nil {
			req.Parameters = make(map[string]interface{}, len(env.Overrides.Parameters))
		}
		for k, v := range env.Overrides.Parameters {
			req.Parameters[k] = v
		}
	}

	// Resolve options with defaults (same semantics as Apply).
	mode := "VALIDATE_AND_EXECUTE"
	returnEdits := "ALL"
	if req.Options != nil {
		if req.Options.Mode != "" {
			mode = strings.ToUpper(req.Options.Mode)
		}
		if req.Options.ReturnEdits != "" {
			returnEdits = strings.ToUpper(req.Options.ReturnEdits)
		}
	}

	if mode != "VALIDATE_ONLY" && mode != "VALIDATE_AND_EXECUTE" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidMode",
			map[string]string{"mode": mode, "allowed": "VALIDATE_ONLY, VALIDATE_AND_EXECUTE"}))
		return
	}

	if mode == "VALIDATE_ONLY" {
		if _, err := h.executor.Prepare(r.Context(), ontologyRID, &req); err != nil {
			httputil.WriteJSON(w, http.StatusOK, &ValidateOnlyResponse{
				Validation: &ValidationResult{Result: "INVALID"},
			})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, &ValidateOnlyResponse{
			Validation: &ValidationResult{Result: "VALID"},
		})
		return
	}

	result, err := h.executor.Apply(r.Context(), ontologyRID, &req)
	if err != nil {
		if staleErr := staleObjectAPIError(err); staleErr != nil {
			apierror.WriteJSON(w, staleErr)
			return
		}
		if apiErr := typedAPIError(err); apiErr != nil {
			apierror.WriteJSON(w, apiErr)
			return
		}
		apierror.WriteJSON(w, apierror.NewInvalidParameter("ActionFailed", map[string]string{"error": err.Error()}))
		return
	}

	resp := &SyncApplyActionResponseV2{
		OperationID: result.BatchID,
	}
	if returnEdits != "NONE" {
		resp.Edits = countEdits(result.Edits)
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// ApplyBatch handles POST /api/v2/ontologies/{ontologyApiName}/actions/{action}/applyBatch.
//
// In Foundry OSv2 a batch is one-action-many-parameter-sets: the action
// API name sits in the path and every body item is only a parameter
// payload for that same action. Weave enforces this by stamping the
// path's action onto every request in the body, ignoring any actionType
// a client may still be sending.
//
// Foundry OSv2 semantics (PR-03):
//   - Request body: { "actions": [...], "options": { "returnEdits": "ALL"|"NONE" } }
//   - Batch is always atomic (all-or-nothing).
//   - The old Weave "mode" field (atomic/bestEffort) is rejected with 400.
//   - options.returnEdits controls whether edits appear in the response (default ALL).
func (h *Handler) ApplyBatch(w http.ResponseWriter, r *http.Request) {
	r = withBranchScope(r)
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	action := chi.URLParam(r, "action")

	if action == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingActionType", nil))
		return
	}

	var reqs struct {
		Actions []ApplyRequest     `json:"actions"`
		Options *BatchApplyOptions `json:"options,omitempty"`
		Mode    string             `json:"mode"` // old field — rejected if present
	}
	if err := httputil.ReadJSON(r, &reqs); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"error": err.Error()}))
		return
	}

	// Reject the old Weave mode field.
	if reqs.Mode != "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("DeprecatedField",
			map[string]string{
				"field":   "mode",
				"message": "The 'mode' field has been removed. Use 'options.returnEdits' instead.",
			}))
		return
	}

	// Resolve returnEdits option with default.
	returnEdits := "ALL"
	if reqs.Options != nil && reqs.Options.ReturnEdits != "" {
		returnEdits = strings.ToUpper(reqs.Options.ReturnEdits)
	}
	if returnEdits != "ALL" && returnEdits != "NONE" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidReturnEdits",
			map[string]string{"returnEdits": returnEdits, "allowed": "ALL, NONE"}))
		return
	}

	// Foundry batch is same-action-many-parameter-sets. Stamp the path's
	// action onto every item so the executor resolves one action type
	// per batch regardless of what the client put in the body.
	for i := range reqs.Actions {
		reqs.Actions[i].ActionType = action
	}

	// US-318: opt-in async batch via ?async=true. When an ActionJobStore is
	// wired we persist a PENDING job row, return 202 {jobId}, and run the
	// batch in a detached goroutine that updates the row + emits per-action
	// progress events. Caller can subscribe via WebSocket subscribeActionJob
	// or poll GET /actions/jobs/{id}; cancellation routes through
	// POST /actions/jobs/{id}/cancel which signals the worker's ctx.
	// Async batch is non-atomic by construction — we apply actions one-by-one
	// so progress can advance per step; the saga / atomic-tx paths handle
	// strict-rollback semantics and are not eligible for ?async=true.
	if r.URL.Query().Get("async") == "true" && h.executor.ActionJobStore() != nil {
		h.serveAsyncApplyBatch(w, r, ontologyRID, action, reqs.Actions, returnEdits)
		return
	}

	// US-239: opt-in saga coordination via ?saga=true. Walks the batch in
	// declaration order; on the first prepare-or-commit failure every
	// previously-prepared action's compensator (if any) fires in reverse
	// order. Sits alongside ?atomic=true rather than replacing it — saga
	// is about rollback semantics, atomic-tx is about PG isolation.
	if r.URL.Query().Get("saga") == "true" {
		sagaResult, sagaErr := h.executor.ApplyBatchSaga(r.Context(), ontologyRID, reqs.Actions)
		if sagaErr != nil {
			apierror.WriteJSON(w, asBatchError(sagaErr))
			return
		}
		resp := &BatchApplyActionResponseV2{}
		if returnEdits != "NONE" {
			resp.Edits = countEdits(sagaResult.AppliedEdits)
		}
		httputil.WriteJSON(w, http.StatusOK, resp)
		return
	}

	// US-238: opt-in PG-transaction commit via ?atomic=true. The default
	// path is the existing best-effort-commit atomic batch — it prepares
	// all-or-nothing but writes action_logs outside a tx. Setting
	// atomic=true routes through the tx-wrapped commit so PG state rolls
	// back together on failure and NATS publish happens post-commit.
	var (
		result *BatchResult
		err    error
	)
	if r.URL.Query().Get("atomic") == "true" {
		result, err = h.executor.ApplyBatchAtomicTx(r.Context(), ontologyRID, reqs.Actions)
	} else {
		result, err = h.executor.ApplyBatchAtomic(r.Context(), ontologyRID, reqs.Actions)
	}
	if err != nil {
		apierror.WriteJSON(w, asBatchError(err))
		return
	}

	// Build BatchApplyActionResponseV2 envelope.
	resp := &BatchApplyActionResponseV2{}
	if returnEdits != "NONE" {
		resp.Edits = countEdits(result.AppliedEdits)
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// asBatchError converts an error returned by ApplyBatchAtomic / CommitBatch
// into a structured API error response. A *BatchError surfaces its phase,
// failedActionIndex, and actionType; everything else is treated as a generic
// ActionFailed. US-208: when the BatchError wraps a typed *apierror.APIError
// (e.g. WEAVE_VALIDATION_ENUM from constraint validation) the typed error is
// surfaced verbatim with its original status code + parameters.
func asBatchError(err error) *apierror.APIError {
	if apiErr := typedAPIError(err); apiErr != nil {
		return apiErr
	}
	var be *BatchError
	if errors.As(err, &be) {
		return apierror.NewInvalidParameter("ActionFailed", map[string]string{
			"phase":             be.Phase,
			"failedActionIndex": strconv.Itoa(be.FailedActionIndex),
			"actionType":        be.ActionType,
			"error":             be.Message,
		})
	}
	return apierror.NewInvalidParameter("ActionFailed", map[string]string{"error": err.Error()})
}

// revertRequest is the JSON body for POST .../actions/revert.
type revertRequest struct {
	ActionLogID int64 `json:"actionLogId"`
}

// Revert handles POST /api/v2/ontologies/{ontologyApiName}/actions/revert.
//
// Accepts { actionLogId } and reverses the action's edits by publishing a
// reverse EditBatch. Returns 409 Conflict if the action has already been
// reverted.
func (h *Handler) Revert(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")

	var req revertRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"error": err.Error()}))
		return
	}
	if req.ActionLogID == 0 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingActionLogId", nil))
		return
	}

	result, err := h.executor.Revert(r.Context(), ontologyRID, req.ActionLogID)
	if err != nil {
		var alreadyReverted *AlreadyRevertedError
		if errors.As(err, &alreadyReverted) {
			apierror.WriteJSON(w, apierror.NewConflict("AlreadyReverted", map[string]string{
				"actionLogId": strconv.FormatInt(alreadyReverted.ActionLogID, 10),
			}))
			return
		}
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ActionLogNotFound", map[string]string{
				"actionLogId": strconv.FormatInt(req.ActionLogID, 10),
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("RevertFailed", map[string]string{"error": err.Error()}))
		return
	}

	resp := &SyncApplyActionResponseV2{
		OperationID: result.BatchID,
		Edits:       countEdits(result.Edits),
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// approvalReviewRequest is the JSON body for the approve / reject endpoints.
// Reason is optional; when present it is recorded on the approval row for
// audit purposes.
type approvalReviewRequest struct {
	Reason string `json:"reason,omitempty"`
}

// serveApprovalEnqueue persists a PENDING ActionApproval and responds with
// 202 {approvalId, status}. Called from Apply when the resolved ActionType
// has RequiresApproval set and an ActionApprovalStore is wired. The
// parameters body is snapshotted verbatim — the reviewer sees exactly what
// the caller submitted.
func (h *Handler) serveApprovalEnqueue(w http.ResponseWriter, r *http.Request, ontologyRID string, at *oms.ActionType, req *ApplyRequest) {
	if len(at.Approvers) == 0 {
		apierror.WriteJSON(w, apierror.NewBadRequest("ApprovalNotConfigured",
			map[string]string{
				"actionType": at.APIName,
				"message":    "action is flagged requiresApproval but no approvers are configured",
			}))
		return
	}
	paramsJSON, err := json.Marshal(req.Parameters)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ApprovalEncodeFailed",
			map[string]string{"error": err.Error()}))
		return
	}
	now := time.Now()
	approval := &ActionApproval{
		ID:              uuid.New().String(),
		ActionTypeRID:   at.RID,
		OntologyAPIName: ontologyRID,
		ActionType:      at.APIName,
		Parameters:      paramsJSON,
		Approvers:       append([]string(nil), at.Approvers...),
		Status:          ActionApprovalStatusPending,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if u := auth.UserFromContext(r.Context()); u != nil {
		approval.RequestedBy = u.ID
	}
	if err := h.executor.ActionApprovalStore().CreateActionApproval(r.Context(), approval); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ApprovalCreateFailed",
			map[string]string{"error": err.Error()}))
		return
	}
	httputil.WriteJSON(w, http.StatusAccepted, &PendingApprovalResponse{
		ApprovalID: approval.ID,
		Status:     ActionApprovalStatusPending,
	})
}

// ListApprovals handles GET .../actions/approvals. Returns the approval
// queue scoped to the current ontology.
//
// Query parameters:
//   - status: PENDING (default) | APPROVED | REJECTED | "" (all)
//   - mine: "true" (default) | "false" — when true, only rows the caller
//     can review (user.ID OR user.Roles ∩ approvers) are returned. When
//     false, the approver filter is lifted so the full queue is visible
//     (admin / audit view).
//   - limit: [1, 500] — per-response cap, defaults to 100.
//
// Degraded mode: when no ActionApprovalStore is wired the endpoint returns
// an empty list rather than 500, matching the GetJob degraded contract.
func (h *Handler) ListApprovals(w http.ResponseWriter, r *http.Request) {
	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	q := r.URL.Query()

	filter := ActionApprovalListFilter{
		Status:          ActionApprovalStatusPending,
		OntologyAPIName: ontologyAPIName,
		Limit:           100,
	}
	if s := q.Get("status"); s != "" {
		switch strings.ToUpper(s) {
		case "ALL", "*":
			filter.Status = ""
		case ActionApprovalStatusPending,
			ActionApprovalStatusApproved,
			ActionApprovalStatusRejected:
			filter.Status = strings.ToUpper(s)
		default:
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidApprovalStatus",
				map[string]string{"status": s, "allowed": "PENDING, APPROVED, REJECTED, ALL"}))
			return
		}
	}
	if lim := q.Get("limit"); lim != "" {
		n, err := strconv.Atoi(lim)
		if err != nil || n < 1 || n > 500 {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidLimit",
				map[string]string{"limit": lim, "allowed": "1..500"}))
			return
		}
		filter.Limit = n
	}

	user := auth.UserFromContext(r.Context())
	mine := q.Get("mine") != "false"

	// Degraded mode: no store wired → empty list.
	store := h.executor.ActionApprovalStore()
	if store == nil {
		httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": []*ActionApproval{}})
		return
	}

	rows, err := store.ListActionApprovals(r.Context(), filter)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ApprovalListFailed",
			map[string]string{"error": err.Error()}))
		return
	}

	// Caller-scoped filter applied post-store so we don't have to teach the
	// store about the OR-of-identity set. Keeps the store interface small.
	if mine && user != nil {
		filtered := rows[:0]
		for _, a := range rows {
			if userCanApprove(user, a.Approvers) {
				filtered = append(filtered, a)
			}
		}
		rows = filtered
	}
	if rows == nil {
		rows = []*ActionApproval{}
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": rows})
}

// ApproveAction handles POST .../actions/approvals/{approvalId}/approve.
// Transitions a PENDING row to APPROVED and records the reviewer + reason.
// Authorization: caller's user.ID or user.Roles must intersect the approval's
// snapshotted approvers list. Already-terminal rows return 409 Conflict.
func (h *Handler) ApproveAction(w http.ResponseWriter, r *http.Request) {
	h.reviewApproval(w, r, ActionApprovalStatusApproved)
}

// RejectAction handles POST .../actions/approvals/{approvalId}/reject.
// Transitions a PENDING row to REJECTED. Same auth rules as ApproveAction.
func (h *Handler) RejectAction(w http.ResponseWriter, r *http.Request) {
	h.reviewApproval(w, r, ActionApprovalStatusRejected)
}

// reviewApproval is the shared body of ApproveAction / RejectAction. The
// approve and reject flows only differ in the terminal status they set, so a
// single helper keeps the auth + state-transition logic in one place.
func (h *Handler) reviewApproval(w http.ResponseWriter, r *http.Request, newStatus string) {
	approvalID := chi.URLParam(r, "approvalId")
	if approvalID == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingApprovalId", nil))
		return
	}
	store := h.executor.ActionApprovalStore()
	if store == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("ApprovalNotFound",
			map[string]string{"approvalId": approvalID}))
		return
	}

	user := auth.UserFromContext(r.Context())
	if user == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("Unauthenticated", nil))
		return
	}

	approval, err := store.GetActionApproval(r.Context(), approvalID)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ApprovalNotFound",
				map[string]string{"approvalId": approvalID}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ApprovalLoadFailed",
			map[string]string{"error": err.Error()}))
		return
	}

	if approval.Status != ActionApprovalStatusPending {
		apierror.WriteJSON(w, apierror.NewConflict("ApprovalAlreadyReviewed",
			map[string]string{
				"approvalId":    approvalID,
				"currentStatus": approval.Status,
			}))
		return
	}

	if !userCanApprove(user, approval.Approvers) {
		apierror.WriteJSON(w, apierror.NewPermissionDenied("ApprovalForbidden",
			map[string]string{
				"approvalId": approvalID,
				"userId":     user.ID,
			}))
		return
	}

	// Body is optional — callers can approve/reject without a reason.
	var body approvalReviewRequest
	if r.ContentLength > 0 {
		if err := httputil.ReadJSON(r, &body); err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody",
				map[string]string{"error": err.Error()}))
			return
		}
	}

	reviewedBy := user.ID
	upd := ActionApprovalUpdate{
		Status:     newStatus,
		ReviewedBy: &reviewedBy,
		Reason:     &body.Reason,
	}
	if err := store.UpdateActionApproval(r.Context(), approvalID, upd); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ApprovalUpdateFailed",
			map[string]string{"error": err.Error()}))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{
		"approvalId": approvalID,
		"status":     newStatus,
	})
}

// userCanApprove returns true when any entry in approvers matches the
// caller's ID or any of their role names. Treats the approvers list as an
// OR of acceptable identities so modelers can mix role-based and
// individual-named approvers without a separate flag.
func userCanApprove(user *auth.User, approvers []string) bool {
	if user == nil || len(approvers) == 0 {
		return false
	}
	for _, a := range approvers {
		if a == "" {
			continue
		}
		if a == user.ID {
			return true
		}
		for _, role := range user.Roles {
			if role == a {
				return true
			}
		}
	}
	return false
}
