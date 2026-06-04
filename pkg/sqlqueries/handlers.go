package sqlqueries

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
)

// Handler serves the Foundry OSv2 SqlQueries.execute endpoint.
type Handler struct {
	engine Engine
}

// NewHandler constructs a Handler. engine may be nil — when nil, the
// Execute endpoint returns SqlQueryEngineNotConfigured (degraded mode for
// dev/test runs without PostgreSQL wired up).
func NewHandler(engine Engine) *Handler {
	return &Handler{engine: engine}
}

// RegisterRoutes mounts the endpoint on r. The path is intentionally a
// top-level resource (not nested under /ontologies/) — Foundry's SQL
// queries API is global and unscoped.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/v2/sqlQueries/execute", h.Execute)
}

// executeRequest is the wire body for POST /api/v2/sqlQueries/execute.
// fallbackBranchIds is parsed for Foundry SDK parity but ignored — the
// single-machine build has no branch concept.
type executeRequest struct {
	Query             string   `json:"query"`
	FallbackBranchIDs []string `json:"fallbackBranchIds,omitempty"`
}

// QueryStatus is the Foundry response union for the execute endpoint.
// Weave only ever emits the terminal "succeeded" / "failed" variants
// because queries run synchronously on the request goroutine; the
// "running" / "canceled" variants are documented in the OpenAPI spec for
// SDK compatibility but never returned by this implementation.
//
// On a succeeded status the engine result is inlined: Columns carries the
// SELECT-list column names and Rows carries one []any per result row
// (positionally aligned with Columns). Weave executes synchronously, so
// returning the data on the same response is natural — there is no async
// "fetch results" round-trip. Columns / Rows are omitted on failed
// statuses and when the configured engine does not implement ResultEngine.
type QueryStatus struct {
	Type          string `json:"type"`
	QueryID       string `json:"queryId"`
	ErrorMessage  string `json:"errorMessage,omitempty"`
	FailureReason string `json:"failureReason,omitempty"`
	// Columns / Rows are intentionally NOT omitempty: a zero-row SELECT
	// still serialises columns + an empty rows array so the UI can render
	// the result header, while a result-less / failed status leaves both
	// nil → JSON null (distinguishable from an empty [] result set).
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

// Execute handles POST /api/v2/sqlQueries/execute.
func (h *Handler) Execute(w http.ResponseWriter, r *http.Request) {
	var body executeRequest
	if err := httputil.ReadJSON(r, &body); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	if body.Query == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingQuery", map[string]string{
			"reason": "query is required",
		}))
		return
	}

	if err := ValidateQuery(body.Query); err != nil {
		code, reason := classifyValidationError(err)
		apierror.WriteJSON(w, apierror.NewInvalidParameter(code, map[string]string{
			"reason": reason,
		}))
		return
	}

	if h.engine == nil {
		apierror.WriteJSON(w, apierror.NewInternal("SqlQueryEngineNotConfigured", map[string]string{
			"reason": "no SQL execution engine wired (PostgreSQL pool unavailable)",
		}))
		return
	}

	queryID := newQueryID()

	// Prefer the result-returning path when the engine supports it so the
	// succeeded status can inline columns + rows. Falls back to the
	// result-less Execute for engines that only implement the base
	// interface (e.g. test fakes / future streaming backends).
	var (
		columns []string
		rows    [][]any
		execErr error
	)
	if re, ok := h.engine.(ResultEngine); ok {
		columns, rows, execErr = re.ExecuteWithResult(r.Context(), body.Query)
	} else {
		execErr = h.engine.Execute(r.Context(), body.Query)
	}

	if execErr != nil {
		// On any failure we deliberately drop columns / rows so a failed
		// envelope never carries a partial result set.
		httputil.WriteJSON(w, http.StatusOK, QueryStatus{
			Type:          "failed",
			QueryID:       queryID,
			ErrorMessage:  execErr.Error(),
			FailureReason: classifyExecutionError(execErr),
		})
		return
	}

	httputil.WriteJSON(w, http.StatusOK, QueryStatus{
		Type:    "succeeded",
		QueryID: queryID,
		Columns: columns,
		Rows:    rows,
	})
}

// classifyExecutionError maps an engine error to the wire failureReason.
// Mirrors classifyValidationError but covers the runtime sentinels
// (timeout / row cap) in addition to the validation sentinels, since a
// ResultEngine runs ValidateQuery itself and may surface a safety
// sentinel from the engine layer.
func classifyExecutionError(err error) string {
	switch {
	case errors.Is(err, ErrQueryTimeout),
		errors.Is(err, context.DeadlineExceeded):
		return "QueryTimeout"
	case errors.Is(err, ErrMaxRowsExceeded):
		return "MaxRowsExceeded"
	case errors.Is(err, ErrStackedStatement):
		return "StackedStatement"
	case errors.Is(err, ErrSystemTableAccess):
		return "SystemTableAccess"
	case errors.Is(err, ErrNotSelect),
		errors.Is(err, ErrForbiddenStatement),
		errors.Is(err, ErrEmptyQuery):
		return "NonSelectQuery"
	default:
		return "ExecutionError"
	}
}

// newQueryID returns a 16-byte random hex string for QueryStatus.queryId.
// Foundry uses opaque UUID-shaped identifiers; this single-machine build
// only needs uniqueness within the process lifetime.
func newQueryID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// classifyValidationError maps a ValidateQuery sentinel to the
// (code, reason) pair returned by the Execute endpoint at the wire
// level. Falls back to NonSelectQuery for any unrecognised input so a
// future sentinel addition cannot leak a 500 to clients.
func classifyValidationError(err error) (string, string) {
	switch {
	case errors.Is(err, ErrEmptyQuery):
		return "MissingQuery", "query is empty"
	case errors.Is(err, ErrStackedStatement):
		return "StackedStatement", "stacked statements are not allowed"
	case errors.Is(err, ErrSystemTableAccess):
		return "SystemTableAccess", "queries against pg_* and information_schema are not allowed"
	case errors.Is(err, ErrForbiddenStatement):
		return "NonSelectQuery", "only single-statement SELECT queries are allowed"
	default:
		return "NonSelectQuery", "only single-statement SELECT queries are allowed"
	}
}
