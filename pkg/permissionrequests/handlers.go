package permissionrequests

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/httputil"
)

// Handler implements the /api/v2/permission-requests/* endpoints (US-339).
//
//	POST   /api/v2/permission-requests              — body {targetRid, reason}
//	GET    /api/v2/permission-requests              — list (mine= or status=)
//	GET    /api/v2/permission-requests/{id}         — get one
//	POST   /api/v2/permission-requests/{id}/approve — body {note}
//	POST   /api/v2/permission-requests/{id}/reject  — body {note}
//
// Read endpoints scope by caller identity:
//   - `mine=true` (or absent + non-approver caller): rows requested by the
//     caller.
//   - approver caller (HasApprovePermission true): may pass status=PENDING
//     to inspect the inbox; absent filters return their own rows by default.
//
// Decision endpoints are gated on HasApprovePermission — admin /
// ontology-owner roles by default. The store transitions PENDING → terminal
// once and only once; re-decide attempts surface 409.
type Handler struct {
	store          Store
	notifier       Notifier
	approverLister ApproverLister
	approveCheck   ApproveAuthorizer
}

// Notifier optionally fans created / decided requests out to the existing
// notification system. nil disables fan-out — the request workflow still
// works, callers just have to poll the list endpoint to discover new rows.
type Notifier interface {
	// NotifyApproversNewRequest is called once per approver after a
	// PermissionRequest is created. Implementation should write one
	// notification per approver to whichever channel the deployment
	// uses (in-app + optional email).
	NotifyApproversNewRequest(ctx context.Context, ev NewRequestEvent) error
	// NotifyRequesterDecision is called once after the row is decided,
	// dispatching to the requester so they learn whether their request
	// was approved or rejected.
	NotifyRequesterDecision(ctx context.Context, ev DecisionEvent) error
}

// ApproverLister returns every userID that should be notified when a new
// permission request lands. Production wires this to the auth user repo's
// "users with role X" query; tests pass a fixed slice.
type ApproverLister interface {
	ListApproverUserIDs(ctx context.Context) ([]string, error)
}

// ApproveAuthorizer reports whether the caller may decide a request. The
// concrete check is "any role grants PermSecurityPolicyManage" but
// keeping the contract narrow lets future deployments scope approval to
// per-ontology owners or a custom role.
type ApproveAuthorizer interface {
	CanApprove(user *auth.User) bool
}

// ApproveAuthorizerFunc is a func adapter for ApproveAuthorizer.
type ApproveAuthorizerFunc func(*auth.User) bool

// CanApprove implements ApproveAuthorizer.
func (f ApproveAuthorizerFunc) CanApprove(u *auth.User) bool { return f(u) }

// NewRequestEvent is the payload passed to Notifier.NotifyApproversNewRequest.
type NewRequestEvent struct {
	Request    *Request
	ApproverID string
}

// DecisionEvent is the payload passed to Notifier.NotifyRequesterDecision.
type DecisionEvent struct {
	Request *Request
}

// NewHandler constructs a Handler. nil store leaves every endpoint
// reporting PermissionRequestsUnavailable so degraded-mode test routers
// (no PG) can keep their /api/v2 prefix mounted without 500s.
func NewHandler(store Store) *Handler {
	return &Handler{
		store: store,
		approveCheck: ApproveAuthorizerFunc(func(u *auth.User) bool {
			if u == nil {
				return false
			}
			return auth.HasPermission(u.Roles, auth.PermSecurityPolicyManage)
		}),
	}
}

// SetNotifier wires the optional notification fan-out. nil keeps the
// no-op default.
func (h *Handler) SetNotifier(n Notifier) { h.notifier = n }

// SetApproverLister wires the optional approver-discovery hook. Without
// it the handler can still service decisions (the gate is per-caller),
// but new requests do not produce notifications.
func (h *Handler) SetApproverLister(a ApproverLister) { h.approverLister = a }

// SetApproveAuthorizer overrides the default "PermSecurityPolicyManage"
// gate with a deployment-specific predicate.
func (h *Handler) SetApproveAuthorizer(a ApproveAuthorizer) {
	if a != nil {
		h.approveCheck = a
	}
}

// RegisterRoutes mounts every endpoint on r. The router is expected to
// already enforce auth presence; the requireAuth check below is
// defence-in-depth so test routers that skip middleware still refuse
// unauthenticated callers.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/v2/permission-requests", h.Create)
	r.Get("/api/v2/permission-requests", h.List)
	r.Get("/api/v2/permission-requests/{id}", h.Get)
	r.Post("/api/v2/permission-requests/{id}/approve", h.Approve)
	r.Post("/api/v2/permission-requests/{id}/reject", h.Reject)
	r.Delete("/api/v2/permission-requests/{id}", h.Cancel)
}

func (h *Handler) requireAuth(w http.ResponseWriter, r *http.Request) *auth.User {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return nil
	}
	return user
}

func (h *Handler) requireStore(w http.ResponseWriter) bool {
	if h.store == nil {
		apierror.WriteJSON(w, apierror.NewInternal("PermissionRequestsUnavailable", map[string]string{
			"reason": "permission requests are not configured on this deployment",
		}))
		return false
	}
	return true
}

type createRequest struct {
	TargetRID string `json:"targetRid"`
	Reason    string `json:"reason,omitempty"`
}

type decisionRequest struct {
	Note string `json:"note,omitempty"`
}

func readOptionalJSON(r *http.Request, v interface{}) error {
	if r.Body == nil {
		return nil
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(nil, r.Body, httputil.MaxBodySize)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		if err == io.EOF {
			return nil
		}
		return err
	}

	var extra interface{}
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body must contain a single JSON value")
		}
		return fmt.Errorf("request body must contain a single JSON value: %w", err)
	}
	return nil
}

type listResponse struct {
	Requests []*Request `json:"requests"`
	Total    int        `json:"total"`
	Limit    int        `json:"limit"`
	Offset   int        `json:"offset"`
}

// Create POST /api/v2/permission-requests.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	var req createRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	req.TargetRID = strings.TrimSpace(req.TargetRID)
	if err := ValidateTargetRID(req.TargetRID); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidPermissionRequestTarget", map[string]string{
			"reason":    err.Error(),
			"targetRid": req.TargetRID,
		}))
		return
	}
	if err := ValidateReason(req.Reason); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidPermissionRequestReason", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	now := time.Now().UTC()
	row := &Request{
		ID:          newRequestID(),
		TargetRID:   req.TargetRID,
		RequestedBy: user.ID,
		Reason:      req.Reason,
		Status:      StatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := h.store.Create(r.Context(), row); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("PermissionRequestCreateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	h.fanoutNewRequest(r.Context(), row)
	httputil.WriteJSON(w, http.StatusCreated, row)
}

// List GET /api/v2/permission-requests.
//
// Query params:
//   - mine=true (default for non-approvers): scope to caller-as-requester.
//   - status=PENDING|APPROVED|REJECTED: filter by status.
//   - targetRid=ri.…: narrow to a single resource.
//   - limit, offset: paging (clamped to MaxPageLimit).
//
// Approvers without `mine=true` see every row matching status / targetRid;
// non-approvers always have mine=true forced so they cannot enumerate
// other users' requests.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	q := r.URL.Query()
	mine := q.Get("mine") == "true"
	canApprove := h.approveCheck.CanApprove(user)
	if !mine && !canApprove {
		// Non-approvers are silently scoped to their own rows so they
		// can never enumerate someone else's pending requests.
		mine = true
	}
	limit := DefaultPageLimit
	if raw := q.Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > MaxPageLimit {
		limit = MaxPageLimit
	}
	offset := 0
	if raw := q.Get("offset"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			offset = v
		}
	}
	lq := ListQuery{
		Status:    strings.ToUpper(strings.TrimSpace(q.Get("status"))),
		TargetRID: q.Get("targetRid"),
		Limit:     limit,
		Offset:    offset,
	}
	if mine {
		lq.RequestedBy = user.ID
	}
	if lq.Status != "" && lq.Status != StatusPending && lq.Status != StatusApproved && lq.Status != StatusRejected {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidPermissionRequestStatus", map[string]string{
			"status": lq.Status,
		}))
		return
	}
	rows, total, err := h.store.List(r.Context(), lq)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("PermissionRequestListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if rows == nil {
		rows = []*Request{}
	}
	httputil.WriteJSON(w, http.StatusOK, listResponse{
		Requests: rows,
		Total:    total,
		Limit:    limit,
		Offset:   offset,
	})
}

// Get GET /api/v2/permission-requests/{id}. Visible to the requester
// AND to any approver — others see 404 (avoid disclosing existence to
// strangers).
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id := chi.URLParam(r, "id")
	row, err := h.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("PermissionRequestNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("PermissionRequestLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if row.RequestedBy != user.ID && !h.approveCheck.CanApprove(user) {
		apierror.WriteJSON(w, apierror.NewNotFound("PermissionRequestNotFound", map[string]string{"id": id}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, row)
}

// Approve POST /api/v2/permission-requests/{id}/approve.
func (h *Handler) Approve(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, StatusApproved)
}

// Reject POST /api/v2/permission-requests/{id}/reject.
func (h *Handler) Reject(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, StatusRejected)
}

func (h *Handler) decide(w http.ResponseWriter, r *http.Request, status string) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	if !h.approveCheck.CanApprove(user) {
		apierror.WriteJSON(w, apierror.NewPermissionDenied("PermissionRequestForbidden", map[string]string{
			"reason": "caller does not have approver role",
		}))
		return
	}
	id := chi.URLParam(r, "id")
	var req decisionRequest
	if err := readOptionalJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if err := ValidateReason(req.Note); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidPermissionRequestNote", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	dec := Decision{Status: status, By: user.ID, Note: req.Note}
	if err := h.store.Decide(r.Context(), id, dec); err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			apierror.WriteJSON(w, apierror.NewNotFound("PermissionRequestNotFound", map[string]string{"id": id}))
			return
		case errors.Is(err, ErrAlreadyDecided):
			apierror.WriteJSON(w, apierror.NewConflict("PermissionRequestAlreadyDecided", map[string]string{"id": id}))
			return
		case errors.Is(err, ErrInvalidStatus):
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidPermissionRequestStatus", map[string]string{
				"status": status,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("PermissionRequestDecideFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	row, err := h.store.Get(r.Context(), id)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("PermissionRequestLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	h.fanoutDecision(r.Context(), row)
	httputil.WriteJSON(w, http.StatusOK, row)
}

func (h *Handler) fanoutNewRequest(ctx context.Context, row *Request) {
	if h.notifier == nil || h.approverLister == nil {
		return
	}
	approvers, err := h.approverLister.ListApproverUserIDs(ctx)
	if err != nil || len(approvers) == 0 {
		return
	}
	for _, approverID := range approvers {
		if approverID == "" || approverID == row.RequestedBy {
			continue
		}
		_ = h.notifier.NotifyApproversNewRequest(ctx, NewRequestEvent{
			Request:    row,
			ApproverID: approverID,
		})
	}
}

func (h *Handler) fanoutDecision(ctx context.Context, row *Request) {
	if h.notifier == nil || row == nil {
		return
	}
	_ = h.notifier.NotifyRequesterDecision(ctx, DecisionEvent{Request: row})
}

// Cancel DELETE /api/v2/permission-requests/{id}. Round 63: the
// requester withdraws their own pending request. The row transitions
// to terminal CANCELLED state (DecidedBy = requester, DecidedAt =
// now) rather than being hard-deleted so the audit trail is
// preserved.
//
// Authorization:
//   - Only the original RequestedBy user may cancel — admins use
//     reject for unwanted requests; cancellation is the requester's
//     prerogative and stays distinct in the audit log.
//   - Only PENDING rows are cancellable; APPROVED / REJECTED /
//     already-CANCELLED rows return 409 (terminal states immutable).
func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id := chi.URLParam(r, "id")
	// Load to authorize: only the requester may cancel. Doing this
	// BEFORE Decide means the 403 path doesn't burn a write attempt
	// and preserves the row's UpdatedAt timestamp on rejected
	// cancels.
	row, err := h.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("PermissionRequestNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("PermissionRequestLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if row.RequestedBy != user.ID {
		apierror.WriteJSON(w, apierror.NewPermissionDenied("PermissionRequestForbidden", map[string]string{
			"reason": "only the original requester may cancel; admins reject instead",
		}))
		return
	}
	dec := Decision{Status: StatusCancelled, By: user.ID}
	if err := h.store.Decide(r.Context(), id, dec); err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			apierror.WriteJSON(w, apierror.NewNotFound("PermissionRequestNotFound", map[string]string{"id": id}))
			return
		case errors.Is(err, ErrAlreadyDecided):
			apierror.WriteJSON(w, apierror.NewConflict("PermissionRequestAlreadyDecided", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("PermissionRequestCancelFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// newRequestID returns a uuid-shaped identifier for a new row. Mirrors
// comments.newCommentID — RFC4122 v4 layout via crypto/rand.
func newRequestID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	h := hex.EncodeToString(buf[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
