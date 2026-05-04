package oms

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
)

// FunctionRepoCommit is the wire shape the commits / log endpoints surface.
// The fields mirror funcrepo.Commit so callers get a stable JSON payload
// regardless of the underlying VCS library.
type FunctionRepoCommit struct {
	Hash       string    `json:"hash"`
	Message    string    `json:"message"`
	Author     string    `json:"author"`
	Email      string    `json:"email"`
	AuthorDate time.Time `json:"authorDate"`
}

// FunctionRepoCommitRequest is the POST /commits payload. Either SourceCode
// (preferred — full new file body) or Patch (legacy unified-diff alias for
// SourceCode) must be supplied. Author / Email default to the actor (when
// resolvable) or the system identity when both are blank.
type FunctionRepoCommitRequest struct {
	Message    string `json:"message"`
	SourceCode string `json:"sourceCode,omitempty"`
	Patch      string `json:"patch,omitempty"`
	Author     string `json:"author,omitempty"`
	Email      string `json:"email,omitempty"`
}

// FunctionRepoCommitInput mirrors the funcrepo.CommitInput shape but is
// re-declared here so the OMS package can stay independent of the funcrepo
// type. The handler glues the wire request → this struct → store call.
type FunctionRepoCommitInput struct {
	Message    string
	SourceCode string
	Author     string
	Email      string
	When       time.Time
}

// FunctionRepoCommitWithSource pairs the commit metadata with the source
// blob attached at that revision. US-416's diff UI consumes this shape so
// it can fetch any historical source code without re-walking the log on
// the client.
type FunctionRepoCommitWithSource struct {
	FunctionRepoCommit
	SourceCode string `json:"sourceCode"`
}

// ErrFunctionRepoCommitNotFound is the typed sentinel returned by
// FunctionRepoStore.GetCommit when the supplied hash does not resolve to
// a commit object on the repo. The HTTP layer maps this to 404
// FunctionRepoCommitNotFound; missing-repo cases come through as
// ErrFunctionRepoNoCommits and map to 404 FunctionRepoNoCommits so the
// SPA can surface a friendlier "no history yet" empty state.
var (
	ErrFunctionRepoCommitNotFound = errors.New("function repo commit not found")
	ErrFunctionRepoNoCommits      = errors.New("function repo has no commits")
)

// FunctionRepoStore is the narrow interface the OMS handler depends on.
// pkg/funcrepo.Manager satisfies this via structural typing — pkg/oms
// stays free of the go-git import. When the store is nil the routes still
// register but respond with 503 FunctionRepoNotConfigured.
type FunctionRepoStore interface {
	Commit(ctx context.Context, rid string, in FunctionRepoCommitInput) (FunctionRepoCommit, error)
	Log(ctx context.Context, rid string, limit int) ([]FunctionRepoCommit, error)
	GetCommit(ctx context.Context, rid string, hash string) (FunctionRepoCommitWithSource, error)
}

// SetFunctionRepoStore wires the optional Function code-repository store
// (US-415). When unset the commits + log routes report 503 so degraded-mode
// test routers still boot cleanly.
func (h *OMSHandler) SetFunctionRepoStore(s FunctionRepoStore) {
	h.functionRepoStore = s
}

// FunctionRepoStore returns the wired store (or nil) so callers can decide
// whether to register the routes without importing the concrete type.
func (h *OMSHandler) FunctionRepoStore() FunctionRepoStore {
	return h.functionRepoStore
}

// SetCommitJobStore wires the optional commit_jobs registry (US-417). When
// unset the POST /commits handler still records the commit but skips the
// CI job row + the runner dispatch, and the GET /commits/{hash}/job
// endpoint surfaces 503 CommitJobsNotConfigured.
func (h *OMSHandler) SetCommitJobStore(s CommitJobStore) {
	h.commitJobStore = s
}

// CommitJobStore returns the wired store (or nil) so route registration
// can branch without importing the concrete PG type.
func (h *OMSHandler) CommitJobStore() CommitJobStore {
	return h.commitJobStore
}

// SetCommitJobRunner wires the optional CI runner (US-417). When unset the
// POST /commits handler records the queued row but never advances it past
// the queued state — the badge surfaces a "queued" badge until a runner
// is wired or an out-of-band sweep completes the row.
func (h *OMSHandler) SetCommitJobRunner(r CommitJobRunner) {
	h.commitJobRunner = r
}

// CommitJobRunner returns the wired runner (or nil) so route registration
// can branch.
func (h *OMSHandler) CommitJobRunner() CommitJobRunner {
	return h.commitJobRunner
}

// CreateFunctionRepoCommit handles POST
// /api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/commits.
//
// The handler resolves the function ref (rid / name / name@version) so the
// commit lands on the rid-keyed bare repo even when callers supply a
// human-friendly name. SourceCode trumps Patch when both are supplied; the
// legacy Patch field is accepted as an alias for forward-compatibility with
// SDK clients that expect git-style "patch" terminology.
func (h *OMSHandler) CreateFunctionRepoCommit(w http.ResponseWriter, r *http.Request) {
	if h.functionRepoStore == nil {
		httputil.WriteJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"errorCode": "FunctionRepoNotConfigured",
			"reason":    "no FunctionRepoStore wired",
		})
		return
	}
	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	fnIdentifier := chi.URLParam(r, "functionRid")

	fn, err := h.resolveFunctionRef(r.Context(), ontologyAPIName, fnIdentifier)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("FunctionNotFound", map[string]string{
				"functionRid": fnIdentifier,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetFunctionFailed", nil))
		return
	}

	var req FunctionRepoCommitRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}
	source := req.SourceCode
	if source == "" {
		source = req.Patch
	}
	if req.Message == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidCommitMessage", map[string]string{
			"reason": "message must not be empty",
		}))
		return
	}
	if source == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidCommitSource", map[string]string{
			"reason": "sourceCode (or patch) must not be empty",
		}))
		return
	}

	commit, err := h.functionRepoStore.Commit(r.Context(), fn.RID, FunctionRepoCommitInput{
		Message:    req.Message,
		SourceCode: source,
		Author:     req.Author,
		Email:      req.Email,
		When:       time.Now(),
	})
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("FunctionRepoCommitFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	// US-417: record a queued CI job row + fire the runner in a background
	// goroutine so the request returns promptly. The commit itself is
	// already durable in the bare repo at this point — failures from this
	// hook never roll the commit back; they surface via the badge later.
	h.dispatchCommitCIJob(fn.RID, commit.Hash, source)
	httputil.WriteJSON(w, http.StatusCreated, commit)
}

// dispatchCommitCIJob records the commit_jobs row in queued state and
// fires the runner asynchronously. The function is a no-op when no store
// is wired so degraded-mode callers (in-memory tests, demo bootstraps)
// can keep using POST /commits without paying the CI cost.
func (h *OMSHandler) dispatchCommitCIJob(functionRID, commitSha, sourceCode string) {
	if h.commitJobStore == nil {
		return
	}
	now := time.Now()
	queued := &CommitJob{
		FunctionRID: functionRID,
		CommitSha:   commitSha,
		Status:      CommitJobStatusQueued,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	// Use a fresh background context so the goroutine outlives the HTTP
	// request — the runner can take longer than the client is willing to
	// wait. Errors are swallowed: the badge will simply never advance past
	// the queued state, which is the correct degraded behaviour.
	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	if err := h.commitJobStore.UpsertCommitJob(bgCtx, queued); err != nil {
		cancel()
		return
	}
	if h.commitJobRunner == nil {
		cancel()
		return
	}
	go func(rid, sha, src string, jobID int64) {
		defer cancel()
		startedAt := time.Now()
		running := &CommitJob{
			ID:          jobID,
			FunctionRID: rid,
			CommitSha:   sha,
			Status:      CommitJobStatusRunning,
			StartedAt:   &startedAt,
			CreatedAt:   queued.CreatedAt,
		}
		_ = h.commitJobStore.UpsertCommitJob(bgCtx, running)

		result := h.commitJobRunner.RunCommitJob(bgCtx, CommitJobRunInput{
			FunctionRID: rid,
			CommitSha:   sha,
			SourceCode:  src,
		})
		finishedAt := time.Now()
		final := &CommitJob{
			ID:           jobID,
			FunctionRID:  rid,
			CommitSha:    sha,
			Status:       result.Status,
			LintOutput:   result.LintOutput,
			TestOutput:   result.TestOutput,
			ErrorMessage: result.ErrorMessage,
			StartedAt:    &startedAt,
			FinishedAt:   &finishedAt,
			CreatedAt:    queued.CreatedAt,
		}
		_ = h.commitJobStore.UpsertCommitJob(bgCtx, final)
	}(functionRID, commitSha, sourceCode, queued.ID)
}

// GetFunctionRepoCommitJob handles GET
// /api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/commits/{hash}/job.
//
// Returns the CI job record for the supplied commit so the SPA can render
// the ✅ / ❌ badge next to the hash. The endpoint surfaces 503
// CommitJobsNotConfigured when no store is wired, 404 CommitJobNotFound
// when the row doesn't exist (commit was not picked up by the hook).
func (h *OMSHandler) GetFunctionRepoCommitJob(w http.ResponseWriter, r *http.Request) {
	if h.commitJobStore == nil {
		httputil.WriteJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"errorCode": "CommitJobsNotConfigured",
			"reason":    "no CommitJobStore wired",
		})
		return
	}
	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	fnIdentifier := chi.URLParam(r, "functionRid")
	hash := chi.URLParam(r, "hash")

	fn, err := h.resolveFunctionRef(r.Context(), ontologyAPIName, fnIdentifier)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("FunctionNotFound", map[string]string{
				"functionRid": fnIdentifier,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetFunctionFailed", nil))
		return
	}

	job, err := h.commitJobStore.GetCommitJob(r.Context(), fn.RID, hash)
	if err != nil {
		if errors.Is(err, ErrCommitJobNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("CommitJobNotFound", map[string]string{
				"functionRid": fn.RID,
				"hash":        hash,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetCommitJobFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, job)
}

// ListFunctionRepoCommits handles GET
// /api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/log.
//
// The optional ?limit= query parameter caps the response (default: 100).
// When the repo has no commits yet the handler returns an empty `data`
// array with HTTP 200 — callers shouldn't have to special-case "function
// has no edit history yet" the same way they handle a true 404.
func (h *OMSHandler) ListFunctionRepoCommits(w http.ResponseWriter, r *http.Request) {
	if h.functionRepoStore == nil {
		httputil.WriteJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"errorCode": "FunctionRepoNotConfigured",
			"reason":    "no FunctionRepoStore wired",
		})
		return
	}
	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	fnIdentifier := chi.URLParam(r, "functionRid")

	fn, err := h.resolveFunctionRef(r.Context(), ontologyAPIName, fnIdentifier)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("FunctionNotFound", map[string]string{
				"functionRid": fnIdentifier,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetFunctionFailed", nil))
		return
	}

	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}

	commits, err := h.functionRepoStore.Log(r.Context(), fn.RID, limit)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("FunctionRepoLogFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if commits == nil {
		commits = []FunctionRepoCommit{}
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"data": commits})
}

// GetFunctionRepoCommit handles GET
// /api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/commits/{hash}.
//
// Returns the commit metadata and the source-code blob attached at that
// revision so US-416's diff UI can compare any two arbitrary commits
// against each other. The hash MUST match the form Log/HeadCommit return
// (full 40-char hex). Path params are validated against the existing
// resolveFunctionRef chain so the bare repo is keyed on the canonical
// RID even when callers POST against the human name.
func (h *OMSHandler) GetFunctionRepoCommit(w http.ResponseWriter, r *http.Request) {
	if h.functionRepoStore == nil {
		httputil.WriteJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"errorCode": "FunctionRepoNotConfigured",
			"reason":    "no FunctionRepoStore wired",
		})
		return
	}
	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	fnIdentifier := chi.URLParam(r, "functionRid")
	hash := chi.URLParam(r, "hash")

	fn, err := h.resolveFunctionRef(r.Context(), ontologyAPIName, fnIdentifier)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("FunctionNotFound", map[string]string{
				"functionRid": fnIdentifier,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetFunctionFailed", nil))
		return
	}

	commit, err := h.functionRepoStore.GetCommit(r.Context(), fn.RID, hash)
	if err != nil {
		switch {
		case errors.Is(err, ErrFunctionRepoCommitNotFound):
			apierror.WriteJSON(w, apierror.NewNotFound("FunctionRepoCommitNotFound", map[string]string{
				"functionRid": fn.RID,
				"hash":        hash,
			}))
		case errors.Is(err, ErrFunctionRepoNoCommits):
			apierror.WriteJSON(w, apierror.NewNotFound("FunctionRepoNoCommits", map[string]string{
				"functionRid": fn.RID,
			}))
		default:
			apierror.WriteJSON(w, apierror.NewInternal("FunctionRepoGetCommitFailed", map[string]string{
				"reason": err.Error(),
			}))
		}
		return
	}
	httputil.WriteJSON(w, http.StatusOK, commit)
}
