package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/httputil"
)

// MarkingGrantAdminRepository extends the request-hot-path MarkingRepository
// with the lookups the admin UI needs to render the /admin/markings page:
// listing grants by marking (who holds PII?) and listing grants for a user
// with timestamps (when was alice granted PII, and by whom?). Keeping these
// on a separate interface follows the US-251 pattern: if the admin surface
// is not wired (degraded mode / contract tests) the admin handler falls
// back to apierror.NewInternal instead of cascading the methods across
// every MarkingRepository mock.
type MarkingGrantAdminRepository interface {
	// ListGrantsByMarking returns every grant row for a marking name,
	// with the full GrantedAt / GrantedBy audit envelope. Used by the
	// admin UI to render "who holds PII?" per-marking.
	ListGrantsByMarking(ctx context.Context, markingName string) ([]MarkingGrant, error)

	// ListGrantsByUser returns every grant row held by the user, with
	// the full GrantedAt / GrantedBy audit envelope. The admin surface
	// wants the timestamps so the lightweight GetUserMarkings path
	// stays allocation-free.
	ListGrantsByUser(ctx context.Context, userID string) ([]MarkingGrant, error)
}

// MarkingRequest is the POST /api/admin/users/{userId}/markings body.
//
// ExpiresAt is an optional RFC3339 timestamp at which the grant should
// auto-expire. Omitting it (or passing an empty string) creates a
// permanent grant. ExpiresInDays is a convenience knob for the common
// "30-day temporary access" UX — when > 0 it overrides ExpiresAt with
// `now() + N days`. Negative values are rejected. When both are present
// ExpiresInDays wins.
type MarkingRequest struct {
	Marking       string `json:"marking"`
	ExpiresAt     string `json:"expiresAt,omitempty"`
	ExpiresInDays int    `json:"expiresInDays,omitempty"`
}

// MarkingResponse is the wire shape for a single marking definition.
//
// CreatedAt is the RFC3339 formatted instant the marking was defined (seed
// migration time for the built-in markings, or the operator's add-marking
// time for custom ones). It is sourced from the DB-populated Marking.CreatedAt
// so the admin UI can show "when was this marking defined?".
type MarkingResponse struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Color       string `json:"color"`
	CreatedAt   string `json:"createdAt"`
}

// MarkingListResponse is returned by GET /api/admin/markings.
type MarkingListResponse struct {
	Markings []MarkingResponse `json:"markings"`
}

// MarkingGrantResponse is the wire shape for a single (user, marking) row.
// ExpiresAt is the RFC3339 formatted auto-revocation timestamp, or the
// empty string for permanent grants.
type MarkingGrantResponse struct {
	UserID      string `json:"userId"`
	MarkingName string `json:"markingName"`
	GrantedAt   string `json:"grantedAt"`
	GrantedBy   string `json:"grantedBy"`
	ExpiresAt   string `json:"expiresAt,omitempty"`
}

// MarkingGrantsResponse is returned by both:
//   - GET /api/admin/markings/{name}/grants (all grants of one marking)
//   - GET /api/admin/users/{userId}/markings (all grants held by one user)
type MarkingGrantsResponse struct {
	Grants []MarkingGrantResponse `json:"grants"`
}

func toMarkingResponse(m Marking) MarkingResponse {
	return MarkingResponse{
		Name:        m.Name,
		DisplayName: m.DisplayName,
		Description: m.Description,
		Color:       m.Color,
		CreatedAt:   m.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func toMarkingGrantResponse(g MarkingGrant) MarkingGrantResponse {
	resp := MarkingGrantResponse{
		UserID:      g.UserID,
		MarkingName: g.MarkingName,
		GrantedAt:   g.GrantedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		GrantedBy:   g.GrantedBy,
	}
	if g.ExpiresAt != nil {
		resp.ExpiresAt = g.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return resp
}

// MarkingHandler implements the admin REST endpoints for marking grants.
//
// Endpoints:
//   - GET    /api/admin/markings                            — list every marking definition
//   - GET    /api/admin/markings/{name}/grants              — list grants of one marking (needs MarkingGrantAdminRepository)
//   - GET    /api/admin/users/{userId}/markings             — list grants held by a user (needs MarkingGrantAdminRepository)
//   - POST   /api/admin/users/{userId}/markings             — grant a marking (audit: marking_grant)
//   - DELETE /api/admin/users/{userId}/markings/{marking}   — revoke a grant (audit: marking_revoke)
//
// The Grant/Revoke writes call through the existing MarkingRepository
// (GrantMarking / RevokeMarking), keeping the request hot path untouched.
type MarkingHandler struct {
	repo       MarkingRepository
	admin      MarkingGrantAdminRepository
	users      UserRepository
	auditStore audit.Store
}

// NewMarkingHandler constructs the admin handler around the existing
// MarkingRepository (grant/revoke/list definitions) plus an optional
// MarkingGrantAdminRepository for the "list grants" endpoints. When admin
// is nil, the list-grants endpoints emit a clean 500 with errorName
// "MarkingGrantLookupUnsupported" so degraded-mode deployments are still
// discoverable via their error payload. users may be nil, in which case
// the grant path skips user existence validation.
func NewMarkingHandler(repo MarkingRepository, admin MarkingGrantAdminRepository, users UserRepository, auditStore audit.Store) *MarkingHandler {
	return &MarkingHandler{repo: repo, admin: admin, users: users, auditStore: auditStore}
}

// RegisterRoutes mounts the endpoints. Callers should wrap the registration
// in RequirePermission(PermUserManage) so only admins can read or mutate
// grants.
func (h *MarkingHandler) RegisterRoutes(r chi.Router) {
	r.Get("/api/admin/markings", h.ListMarkings)
	r.Get("/api/admin/markings/{name}/grants", h.ListGrantsByMarking)
	r.Get("/api/admin/users/{userId}/markings", h.ListGrantsByUser)
	r.Post("/api/admin/users/{userId}/markings", h.GrantMarking)
	r.Delete("/api/admin/users/{userId}/markings/{marking}", h.RevokeMarking)
}

// ListMarkings handles GET /api/admin/markings.
func (h *MarkingHandler) ListMarkings(w http.ResponseWriter, r *http.Request) {
	if u := UserFromContext(r.Context()); u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	rows, err := h.repo.ListMarkings(r.Context())
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("MarkingListFailed", map[string]string{"reason": err.Error()}))
		return
	}
	out := MarkingListResponse{Markings: make([]MarkingResponse, 0, len(rows))}
	for _, m := range rows {
		out.Markings = append(out.Markings, toMarkingResponse(m))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// ListGrantsByMarking handles GET /api/admin/markings/{name}/grants.
func (h *MarkingHandler) ListGrantsByMarking(w http.ResponseWriter, r *http.Request) {
	h.listGrantsByMarkingFor(w, r, chi.URLParam(r, "name"))
}

func (h *MarkingHandler) listGrantsByMarkingFor(w http.ResponseWriter, r *http.Request, name string) {
	if u := UserFromContext(r.Context()); u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	if name == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingMarkingName", map[string]string{
			"reason": "name path parameter is required",
		}))
		return
	}
	if h.admin == nil {
		apierror.WriteJSON(w, apierror.NewInternal("MarkingGrantLookupUnsupported", map[string]string{
			"reason": "marking grant admin repository is not wired on this deployment",
		}))
		return
	}
	grants, err := h.admin.ListGrantsByMarking(r.Context(), name)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("MarkingGrantListFailed", map[string]string{"reason": err.Error()}))
		return
	}
	out := MarkingGrantsResponse{Grants: make([]MarkingGrantResponse, 0, len(grants))}
	for _, g := range grants {
		out.Grants = append(out.Grants, toMarkingGrantResponse(g))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// ListGrantsByUser handles GET /api/admin/users/{userId}/markings.
func (h *MarkingHandler) ListGrantsByUser(w http.ResponseWriter, r *http.Request) {
	h.listGrantsByUserFor(w, r, chi.URLParam(r, "userId"))
}

func (h *MarkingHandler) listGrantsByUserFor(w http.ResponseWriter, r *http.Request, userID string) {
	if u := UserFromContext(r.Context()); u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	if userID == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingUserID", map[string]string{
			"reason": "userId path parameter is required",
		}))
		return
	}
	if h.admin == nil {
		apierror.WriteJSON(w, apierror.NewInternal("MarkingGrantLookupUnsupported", map[string]string{
			"reason": "marking grant admin repository is not wired on this deployment",
		}))
		return
	}
	grants, err := h.admin.ListGrantsByUser(r.Context(), userID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("MarkingGrantListFailed", map[string]string{"reason": err.Error()}))
		return
	}
	out := MarkingGrantsResponse{Grants: make([]MarkingGrantResponse, 0, len(grants))}
	for _, g := range grants {
		out.Grants = append(out.Grants, toMarkingGrantResponse(g))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// GrantMarking handles POST /api/admin/users/{userId}/markings.
func (h *MarkingHandler) GrantMarking(w http.ResponseWriter, r *http.Request) {
	h.grantMarkingFor(w, r, chi.URLParam(r, "userId"))
}

func (h *MarkingHandler) grantMarkingFor(w http.ResponseWriter, r *http.Request, userID string) {
	u := UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	if userID == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingUserID", map[string]string{
			"reason": "userId path parameter is required",
		}))
		return
	}
	var req MarkingRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidMarkingRequest", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	req.Marking = strings.TrimSpace(req.Marking)
	if req.Marking == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingMarking", map[string]string{
			"reason": "marking is required",
		}))
		return
	}
	expiresAt, apierr := resolveGrantExpiry(req)
	if apierr != nil {
		apierror.WriteJSON(w, apierr)
		return
	}
	if h.users != nil {
		if _, err := h.users.GetUserByID(r.Context(), userID); err != nil {
			if errors.Is(err, ErrUserNotFound) {
				apierror.WriteJSON(w, apierror.NewNotFound("UserNotFound", map[string]string{"userId": userID}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("UserLookupFailed", map[string]string{"reason": err.Error()}))
			return
		}
	}
	if err := h.validateMarkingExists(r.Context(), req.Marking); err != nil {
		apierror.WriteJSON(w, err)
		return
	}
	if err := h.repo.GrantMarking(r.Context(), userID, req.Marking, u.ID, expiresAt); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("GrantMarkingFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if h.auditStore != nil {
		diffMap := map[string]string{"marking": req.Marking}
		if expiresAt != nil {
			diffMap["expiresAt"] = expiresAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		diff, _ := json.Marshal(diffMap)
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "marking_grant",
			ResourceType: "User",
			ResourceRID:  userID,
			DiffJSON:     diff,
		})
	}
	// Mirror the GET /api/admin/users/{userId}/markings handler: when the
	// admin grant-listing repo is wired, return the full grant rows so the
	// freshly minted grant surfaces with its grantedAt / grantedBy /
	// expiresAt envelope. The bare-name fallback below is preserved only
	// for degraded-mode deployments where the admin interface isn't wired
	// — without it the test harness without an admin repo would 500.
	var out MarkingGrantsResponse
	if h.admin != nil {
		grants, lerr := h.admin.ListGrantsByUser(r.Context(), userID)
		if lerr != nil {
			apierror.WriteJSON(w, apierror.NewInternal("MarkingGrantListFailed", map[string]string{"reason": lerr.Error()}))
			return
		}
		out.Grants = make([]MarkingGrantResponse, 0, len(grants))
		for _, g := range grants {
			out.Grants = append(out.Grants, toMarkingGrantResponse(g))
		}
	} else {
		names, _ := h.repo.GetUserMarkings(r.Context(), userID)
		if names == nil {
			names = []string{}
		}
		out.Grants = make([]MarkingGrantResponse, 0, len(names))
		for _, n := range names {
			out.Grants = append(out.Grants, MarkingGrantResponse{UserID: userID, MarkingName: n})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// resolveGrantExpiry converts the user-facing ExpiresAt / ExpiresInDays
// knobs into a normalised *time.Time. Returns (nil, nil) for permanent
// grants, (*time.Time, nil) for time-limited grants, and (nil, *APIError)
// for invalid input. ExpiresInDays > 0 wins over ExpiresAt; ExpiresAt is
// parsed as RFC3339.
func resolveGrantExpiry(req MarkingRequest) (*time.Time, *apierror.APIError) {
	if req.ExpiresInDays < 0 {
		return nil, apierror.NewInvalidParameter("InvalidExpiresInDays", map[string]string{
			"reason": "expiresInDays must be zero or positive",
		})
	}
	if req.ExpiresInDays > 0 {
		t := time.Now().UTC().Add(time.Duration(req.ExpiresInDays) * 24 * time.Hour)
		return &t, nil
	}
	s := strings.TrimSpace(req.ExpiresAt)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, apierror.NewInvalidParameter("InvalidExpiresAt", map[string]string{
			"reason": "expiresAt must be an RFC3339 timestamp",
		})
	}
	t = t.UTC()
	return &t, nil
}

// RevokeMarking handles DELETE /api/admin/users/{userId}/markings/{marking}.
func (h *MarkingHandler) RevokeMarking(w http.ResponseWriter, r *http.Request) {
	h.revokeMarkingFor(w, r, chi.URLParam(r, "userId"), chi.URLParam(r, "marking"))
}

func (h *MarkingHandler) revokeMarkingFor(w http.ResponseWriter, r *http.Request, userID, marking string) {
	u := UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	if userID == "" || marking == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingPathParameter", map[string]string{
			"reason": "userId and marking are required",
		}))
		return
	}
	if err := h.repo.RevokeMarking(r.Context(), userID, marking); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("RevokeMarkingFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if h.auditStore != nil {
		diff, _ := json.Marshal(map[string]string{"marking": marking})
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "marking_revoke",
			ResourceType: "User",
			ResourceRID:  userID,
			DiffJSON:     diff,
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

// validateMarkingExists returns an *apierror.APIError when the marking
// name is not in the markings table; nil when it is. Emits a clean 404 so
// admin UIs can distinguish "typo'd marking name" from "grant store is
// down".
func (h *MarkingHandler) validateMarkingExists(ctx context.Context, name string) *apierror.APIError {
	rows, err := h.repo.ListMarkings(ctx)
	if err != nil {
		return apierror.NewInternal("MarkingLookupFailed", map[string]string{"reason": err.Error()})
	}
	for _, m := range rows {
		if m.Name == name {
			return nil
		}
	}
	return apierror.NewNotFound("MarkingNotFound", map[string]string{"marking": name})
}
