package compliance

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/httputil"
)

// Handler implements the /api/admin/compliance/* admin endpoints.
//
//	POST /api/admin/compliance/report    — generate a control-evidence report
//
// The handler is gated on PermUserManage by the surrounding router; the
// auth.UserFromContext nil-check below is a defence-in-depth guard so
// test routers (which may skip the RequirePermission wrapper) still
// refuse unauthenticated callers.
type Handler struct {
	gen        *Generator
	auditStore audit.Store
}

// NewHandler wires a compliance admin handler. gen must be non-nil.
// auditStore is used to record the "compliance_report_generated" audit
// event; pass nil in degraded-mode test routers.
func NewHandler(gen *Generator, auditStore audit.Store) *Handler {
	return &Handler{gen: gen, auditStore: auditStore}
}

// RegisterRoutes mounts every compliance admin endpoint on r. Callers
// should wrap the call in auth.RequirePermission(auth.PermUserManage).
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/admin/compliance/report", h.GenerateReport)
}

// ReportRequest is the wire shape for POST /api/admin/compliance/report.
// All fields are optional: omit Format to default to JSON, omit From/To
// for "since beginning → now".
type ReportRequest struct {
	Format string `json:"format,omitempty"`
	From   string `json:"from,omitempty"` // RFC3339
	To     string `json:"to,omitempty"`   // RFC3339
}

// GenerateReport handles POST /api/admin/compliance/report.
//
// Response is either application/json (Report marshalled directly) or
// text/html (RenderHTML). Default format is JSON.
func (h *Handler) GenerateReport(w http.ResponseWriter, r *http.Request) {
	caller := auth.UserFromContext(r.Context())
	if caller == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	if h.gen == nil {
		apierror.WriteJSON(w, apierror.NewInternal("ComplianceReportUnavailable", map[string]string{
			"reason": "compliance report generator is not configured on this deployment",
		}))
		return
	}

	// Accept empty body — `curl -X POST` with no body is the common
	// case. json.Decode on an empty body returns io.EOF; treat that
	// as "use all-defaults".
	req := ReportRequest{}
	if r.ContentLength != 0 && r.Body != nil {
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&req); err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
				"reason": err.Error(),
			}))
			return
		}
	}

	var from, to time.Time
	if req.From != "" {
		t, err := time.Parse(time.RFC3339, req.From)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidFrom", map[string]string{
				"reason": "from must be RFC3339: " + err.Error(),
			}))
			return
		}
		from = t
	}
	if req.To != "" {
		t, err := time.Parse(time.RFC3339, req.To)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidTo", map[string]string{
				"reason": "to must be RFC3339: " + err.Error(),
			}))
			return
		}
		to = t
	}
	if !from.IsZero() && !to.IsZero() && to.Before(from) {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidWindow", map[string]string{
			"reason": "to must be greater than or equal to from",
		}))
		return
	}

	format := req.Format
	if format == "" {
		format = r.URL.Query().Get("format")
	}
	if format == "" {
		format = "json"
	}
	switch format {
	case "json", "html":
	default:
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidFormat", map[string]string{
			"reason": "format must be one of: json, html",
		}))
		return
	}

	report, err := h.gen.Generate(r.Context(), from, to)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ComplianceReportFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	// Audit the generation BEFORE writing the body — once the status
	// line is on the wire a mid-stream failure can't emit a structured
	// 500, so the audit row has to land first. Same shape as the GDPR
	// export handler (US-268).
	if h.auditStore != nil {
		diff, _ := json.Marshal(map[string]string{
			"format": format,
			"from":   req.From,
			"to":     req.To,
		})
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      caller.ID,
			Action:       "compliance_report_generated",
			ResourceType: "ComplianceReport",
			DiffJSON:     diff,
		})
	}

	if format == "html" {
		body, rerr := RenderHTMLBytes(report)
		if rerr != nil {
			apierror.WriteJSON(w, apierror.NewInternal("ComplianceReportRenderFailed", map[string]string{
				"reason": rerr.Error(),
			}))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, report)
}
