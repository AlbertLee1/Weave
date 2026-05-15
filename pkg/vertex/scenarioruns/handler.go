package scenarioruns

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
)

// runStarter is the slim slice of Service the Handler needs. Splitting
// it out lets the handler tests stub the service if they want, though
// in practice they pass the real Service for the end-to-end coverage.
type runStarter interface {
	Run(ctx context.Context, scenarioRID string) (string, error)
	Cancel(ctx context.Context, runRID string) error
}

type runReader interface {
	getRun(ctx context.Context, runRID string) (Run, error)
}

// serviceRunReader adapts Service.repo access. Service does not expose
// GetRun directly because the repo is the authoritative source for
// terminal state; the handler reads through the repo via this shim.
type serviceRunReader struct{ repo Repo }

func (s serviceRunReader) getRun(ctx context.Context, rid string) (Run, error) {
	return s.repo.GetRun(ctx, rid)
}

// Handler serves the VTX-057 scenario-run endpoints:
//
//	POST /api/vertex/v1/scenarios/{scenarioRid}/runs            — create
//	POST /api/vertex/v1/scenarios/{scenarioRid}/runs/{runRid}/cancel
//	GET  /api/vertex/v1/scenarios/{scenarioRid}/runs/{runRid}
//
// The handler is a thin façade — all lifecycle logic lives in Service.
type Handler struct {
	starter runStarter
	reader  runReader
}

// NewHandler wires a Handler over a Service. The Service exposes both
// the run-starter surface and a repo reader for the GET endpoint.
func NewHandler(svc *Service) *Handler {
	return &Handler{
		starter: svc,
		reader:  serviceRunReader{repo: svc.repo},
	}
}

// RegisterRoutes mounts the run lifecycle endpoints on r.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/vertex/v1/scenarios/{scenarioRid}/runs", h.startRun)
	r.Post("/api/vertex/v1/scenarios/{scenarioRid}/runs/{runRid}/cancel", h.cancelRun)
	r.Get("/api/vertex/v1/scenarios/{scenarioRid}/runs/{runRid}", h.getRun)
}

type startRunResponse struct {
	RunRID string    `json:"runRid"`
	Status RunStatus `json:"status"`
}

func (h *Handler) startRun(w http.ResponseWriter, r *http.Request) {
	scenarioRID := chi.URLParam(r, "scenarioRid")
	runRID, err := h.starter.Run(r.Context(), scenarioRID)
	if err != nil {
		writeStartError(w, scenarioRID, err)
		return
	}
	httputil.WriteJSON(w, http.StatusAccepted, startRunResponse{
		RunRID: runRID,
		Status: RunStatusPending,
	})
}

func (h *Handler) cancelRun(w http.ResponseWriter, r *http.Request) {
	runRID := chi.URLParam(r, "runRid")
	err := h.starter.Cancel(r.Context(), runRID)
	if err != nil {
		writeCancelError(w, runRID, err)
		return
	}
	httputil.WriteJSON(w, http.StatusAccepted, map[string]string{
		"runRid": runRID,
		"status": "canceling",
	})
}

func (h *Handler) getRun(w http.ResponseWriter, r *http.Request) {
	runRID := chi.URLParam(r, "runRid")
	run, err := h.reader.getRun(r.Context(), runRID)
	if err != nil {
		if errors.Is(err, ErrRunNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ScenarioRunNotFound",
				map[string]string{"runRid": runRID}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetScenarioRunFailed",
			map[string]string{"error": err.Error()}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, run)
}

func writeStartError(w http.ResponseWriter, scenarioRID string, err error) {
	// Distinct ErrRunNotFound is unlikely on a fresh start; we map any
	// scenario-resolution error to 404 so the SDK can surface a clean
	// "scenario not found" without us depending on the scenarios package.
	apierror.WriteJSON(w, apierror.NewNotFound("ScenarioNotFound",
		map[string]string{"scenarioRid": scenarioRID, "error": err.Error()}))
}

func writeCancelError(w http.ResponseWriter, runRID string, err error) {
	if errors.Is(err, ErrRunNotFound) {
		apierror.WriteJSON(w, apierror.NewNotFound("ScenarioRunNotFound",
			map[string]string{"runRid": runRID}))
		return
	}
	if errors.Is(err, ErrAlreadyTerminal) {
		apierror.WriteJSON(w, apierror.NewConflict("ScenarioRunAlreadyTerminal",
			map[string]string{"runRid": runRID}))
		return
	}
	apierror.WriteJSON(w, apierror.NewInternal("CancelScenarioRunFailed",
		map[string]string{"error": err.Error()}))
}
