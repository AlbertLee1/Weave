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

// Handler implements the /api/admin/gdpr/* admin endpoints:
//
//	POST /api/admin/gdpr/erase          — right-to-be-forgotten (US-267)
//	GET  /api/admin/gdpr/erase/{jobId}  — job poll (US-267)
//	POST /api/admin/gdpr/export         — data portability (US-268)
//
// The handler is gated on PermUserManage by the surrounding router; it
// does not enforce permissions on its own. The auth.UserFromContext
// gate inside each endpoint is a defence-in-depth check — without it the
// test router (which doesn't wrap the handler in RequirePermission)
// would be accidentally permissive.
type Handler struct {
	store      JobStore
	eraser     *Eraser
	exporter   *Exporter
	auditStore audit.Store
}

// NewHandler constructs a GDPR admin handler. eraser is the shared
// orchestrator carrying the registered Steps + JobStore; auditStore is
// the canonical audit log for the gdpr_* action emits. Pass nil
// auditStore in degraded-mode test routers.
//
// The exporter is wired via SetExporter so callers that only need the
// erase path can still construct a Handler with a 3-arg call.
func NewHandler(store JobStore, eraser *Eraser, auditStore audit.Store) *Handler {
	return &Handler{store: store, eraser: eraser, auditStore: auditStore}
}

// SetExporter wires the optional data-export path. Nil exporter leaves
// the POST /export endpoint returning 500 GDPRExportUnavailable so the
// SPA / SDK can surface "not configured" to operators.
func (h *Handler) SetExporter(e *Exporter) { h.exporter = e }

// RegisterRoutes mounts every GDPR admin endpoint on r. Callers should
// wrap the call in auth.RequirePermission(auth.PermUserManage).
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/admin/gdpr/erase", h.Erase)
	r.Get("/api/admin/gdpr/erase/{jobId}", h.GetJob)
	r.Post("/api/admin/gdpr/export", h.Export)
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

// ExportRequest is the wire shape for POST /api/admin/gdpr/export.
type ExportRequest struct {
	UserID string `json:"userId"`
}

// Export handles POST /api/admin/gdpr/export.
//
// Accepts {"userId": "<id>"} and streams a ZIP archive containing a
// data.json file with the user's profile / roles / audit events plus
// every media blob the user uploaded under media/<rid>/<filename>.
//
// The response is a single large body rather than an async job row
// because the payload is typically small and SDK callers expect the
// zip inline (cf. the PRD acceptance criteria "生成 ZIP"). Large
// deployments that want async upload-to-S3 semantics can layer that on
// top of the same Exporter by wiring a different MediaBlobs source.
func (h *Handler) Export(w http.ResponseWriter, r *http.Request) {
	caller := auth.UserFromContext(r.Context())
	if caller == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	if h.exporter == nil {
		apierror.WriteJSON(w, apierror.NewInternal("GDPRExportUnavailable", map[string]string{
			"reason": "GDPR export is not configured on this deployment",
		}))
		return
	}

	var req ExportRequest
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

	// Audit the export BEFORE streaming the body — status-line-already-on-
	// the-wire means a mid-stream failure can't emit a JSON error anyway,
	// so the audit has to land synchronously up front.
	if h.auditStore != nil {
		diff, _ := json.Marshal(map[string]string{"userId": req.UserID})
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      caller.ID,
			Action:       "gdpr_export_request",
			ResourceType: "User",
			ResourceRID:  req.UserID,
			DiffJSON:     diff,
		})
	}

	filename := "gdpr-export-" + sanitiseFilename(req.UserID) + ".zip"
	if filename == "gdpr-export-.zip" {
		filename = "gdpr-export.zip"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if _, err := h.exporter.WriteZip(r.Context(), req.UserID, w); err != nil {
		// The zip writer may already have flushed bytes to the wire — no
		// way to emit a structured 500 reliably. Log and let the caller
		// observe a truncated response.
		log.Printf("gdpr: export for %s: %v", req.UserID, err)
		return
	}
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
