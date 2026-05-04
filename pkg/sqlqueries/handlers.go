package sqlqueries

import (
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
type QueryStatus struct {
	Type          string `json:"type"`
	QueryID       string `json:"queryId"`
	ErrorMessage  string `json:"errorMessage,omitempty"`
	FailureReason string `json:"failureReason,omitempty"`
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
	if err := h.engine.Execute(r.Context(), body.Query); err != nil {
		reason := "ExecutionError"
		switch {
		case errors.Is(err, ErrNotSelect),
			errors.Is(err, ErrForbiddenStatement),
			errors.Is(err, ErrEmptyQuery):
			reason = "NonSelectQuery"
		case errors.Is(err, ErrStackedStatement):
			reason = "StackedStatement"
		case errors.Is(err, ErrSystemTableAccess):
			reason = "SystemTableAccess"
		}
		httputil.WriteJSON(w, http.StatusOK, QueryStatus{
			Type:          "failed",
			QueryID:       queryID,
			ErrorMessage:  err.Error(),
			FailureReason: reason,
		})
		return
	}

	httputil.WriteJSON(w, http.StatusOK, QueryStatus{
		Type:    "succeeded",
		QueryID: queryID,
	})
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
