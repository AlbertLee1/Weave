package gdpr

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/httputil"
)

// Handler implements POST /api/admin/gdpr/erase + GET .../{jobId}.
//
// The handler is gated on PermUserManage by the surrounding router; it
// does not enforce permissions on its own. The auth.UserFromContext
// gate inside Erase is a defence-in-depth check — without it the test
// router (which doesn't wrap the handler in RequirePermission) would
// be accidentally permissive.
type Handler struct {
	store      JobStore
	eraser     *Eraser
	auditStore audit.Store
}

// NewHandler constructs a GDPR erase handler. eraser is the shared
// orchestrator carrying the registered Steps + JobStore; auditStore is
// the canonical audit log for the gdpr_erase action emit. Pass nil
// auditStore in degraded-mode test routers.
func NewHandler(store JobStore, eraser *Eraser, auditStore audit.Store) *Handler {
	return &Handler{store: store, eraser: eraser, auditStore: auditStore}
}

// RegisterRoutes mounts the two endpoints on r. Callers should wrap
// the call in auth.RequirePermission(auth.PermUserManage).
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/admin/gdpr/erase", h.Erase)
	r.Get("/api/admin/gdpr/erase/{jobId}", h.GetJob)
}

// Erase handles POST /api/admin/gdpr/erase.
//
// Request body: {"userId": "<id>"}
// Response: 202 Accepted + {"jobId": ..., "status": "PENDING"}
func (h *Handler) Erase(w http.ResponseWriter, r *http.Request) {
	caller := auth.UserFromContext(r.Context())
	if caller == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	if h.store == nil || h.eraser == nil {
		apierror.WriteJSON(w, apierror.NewInternal("GDPREraseUnavailable", map[string]string{
			"reason": "GDPR erase is not configured on this deployment",
		}))
		return
	}

	var req EraseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if req.UserID == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingUserID", map[string]string{
			"reason": "userId is required",
		}))
		return
	}

	job := &ErasureJob{
		JobID:       uuid.NewString(),
		UserID:      req.UserID,
		Status:      JobStatusPending,
		Progress:    0,
		RequestedBy: caller.ID,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := h.store.CreateJob(r.Context(), job); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("GDPRJobCreateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	// Audit the request itself BEFORE the worker runs. The actual
	// erase work emits its own per-step results into the job row.
	if h.auditStore != nil {
		diff, _ := json.Marshal(map[string]string{
			"jobId":  job.JobID,
			"userId": req.UserID,
		})
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      caller.ID,
			Action:       "gdpr_erase_request",
			ResourceType: "User",
			ResourceRID:  req.UserID,
			DiffJSON:     diff,
		})
	}

	// Detached worker — drop the request context (cancelled the moment
	// the response flushes) and use a background context that still
	// carries the caller's identity. Same pattern actions.serveAsyncApply
	// uses for async action apply.
	bgCtx := copyAuthContext(context.Background(), r.Context())
	jobID := job.JobID
	userID := req.UserID
	go func() {
		if _, err := h.eraser.Run(bgCtx, jobID, userID); err != nil {
			log.Printf("gdpr: job %s: Run returned: %v", jobID, err)
		}
	}()

	httputil.WriteJSON(w, http.StatusAccepted, &EraseResponse{
		JobID:  job.JobID,
		Status: JobStatusPending,
	})
}

// GetJob handles GET /api/admin/gdpr/erase/{jobId}. Returns the
// current job state. 404 when the job is unknown.
func (h *Handler) GetJob(w http.ResponseWriter, r *http.Request) {
	if u := auth.UserFromContext(r.Context()); u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	jobID := chi.URLParam(r, "jobId")
	if jobID == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingJobID", map[string]string{
			"reason": "jobId is required",
		}))
		return
	}
	if h.store == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("GDPRJobNotFound", map[string]string{"jobId": jobID}))
		return
	}
	job, err := h.store.GetJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, ErrJobNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("GDPRJobNotFound", map[string]string{"jobId": jobID}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GDPRJobLookupFailed", map[string]string{"reason": err.Error()}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, job)
}

// copyAuthContext copies the authenticated user identity from src to
// dst. Used to detach the worker goroutine from the request's
// cancellation while preserving the caller's identity for downstream
// audit / rate-limit keys. Same shape as actions.copyAuthContext.
func copyAuthContext(dst, src context.Context) context.Context {
	if u := auth.UserFromContext(src); u != nil {
		return auth.WithUser(dst, u)
	}
	return dst
}
