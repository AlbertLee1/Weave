package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/audit"
)

// SessionIDAttributeKey is the User.Attributes key populated by the auth
// middleware for requests authenticated via a session-bound JWT. When set,
// the session list marks the corresponding row with current=true so the SPA
// can surface "this device" in the UI.
const SessionIDAttributeKey = "sessionID"

// SessionHandlerDeps groups collaborators for the session endpoints.
type SessionHandlerDeps struct {
	Sessions SessionStore
	// RefreshService is optional; when wired, DELETE /api/auth/sessions/{id}
	// revokes any refresh-token chain bound to the deleted session so the
	// attacker loses access as soon as the access-token TTL expires.
	RefreshService *RefreshService
	AuditStore     audit.Store
}

// SessionHandler implements GET /api/auth/sessions and
// DELETE /api/auth/sessions/{id}. Both endpoints require an authenticated
// request; the list returns ONLY the caller's own sessions, and delete
// refuses to remove sessions the caller does not own (ErrSessionForbidden
// surfaces as 403, not 404, so the SPA can distinguish "admin mistake" from
// "stale ID").
type SessionHandler struct {
	deps SessionHandlerDeps
}

// NewSessionHandler builds a handler. Panics if Sessions is nil since every
// endpoint depends on the store.
func NewSessionHandler(deps SessionHandlerDeps) *SessionHandler {
	if deps.Sessions == nil {
		panic("SessionHandler requires Sessions")
	}
	return &SessionHandler{deps: deps}
}

// RegisterRoutes mounts the two endpoints on the supplied router. The
// minimal router interface keeps this method usable from chi.Router as well
// as the contract-test stub router.
func (h *SessionHandler) RegisterRoutes(mux interface {
	Method(method, pattern string, handler http.Handler)
}) {
	mux.Method(http.MethodGet, "/api/auth/sessions", http.HandlerFunc(h.List))
	mux.Method(http.MethodDelete, "/api/auth/sessions/{sessionID}", http.HandlerFunc(h.Delete))
	mux.Method(http.MethodPost, "/api/auth/sessions/revoke-others", http.HandlerFunc(h.RevokeOthers))
}

// RevokeOthersResponse is the JSON body for POST /api/auth/sessions/revoke-others.
type RevokeOthersResponse struct {
	// Revoked is the count of sessions destroyed by this call. Zero
	// means the caller had no other sessions (the current one is
	// always preserved). Foundry-parity naming.
	Revoked int `json:"revoked"`
	// CurrentSessionID is the session ID the caller is currently
	// bound to. Empty for API-key / OAuth callers — when empty, the
	// handler revokes ALL of the caller's sessions because there's
	// no anchor to preserve.
	CurrentSessionID string `json:"currentSessionId"`
}

// SessionView is the wire representation of a session row. Deliberately
// narrower than SessionRecord — user_id is elided because /api/auth/sessions
// already scopes to the caller.
type SessionView struct {
	ID        string    `json:"id"`
	IP        string    `json:"ip,omitempty"`
	UserAgent string    `json:"user_agent,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
	Current   bool      `json:"current,omitempty"`
	// UserID is never populated on the wire. Kept only so tests can assert
	// it stays empty.
	UserID string `json:"-"`
}

// SessionListResponse is the body of GET /api/auth/sessions.
type SessionListResponse struct {
	Sessions []SessionView `json:"sessions"`
}

// List returns the caller's active sessions sorted by last_seen descending.
func (h *SessionHandler) List(w http.ResponseWriter, r *http.Request) {
	caller := UserFromContext(r.Context())
	if caller == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("Unauthorized", map[string]string{"reason": "authentication required"}))
		return
	}
	rows, err := h.deps.Sessions.ListByUser(r.Context(), caller.ID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("SessionListFailed", map[string]string{"reason": err.Error()}))
		return
	}
	currentID := callerSessionID(caller)
	views := make([]SessionView, 0, len(rows))
	for _, row := range rows {
		views = append(views, SessionView{
			ID:        row.ID,
			IP:        row.IP,
			UserAgent: row.UserAgent,
			CreatedAt: row.CreatedAt,
			LastSeen:  row.LastSeen,
			Current:   currentID != "" && row.ID == currentID,
		})
	}
	writeJSON(w, http.StatusOK, SessionListResponse{Sessions: views})
}

// Delete removes a session the caller owns. Returns 204 on success, 401 for
// unauthenticated callers, 403 when the caller does not own the row, and
// 404 when the row is unknown. When a RefreshService is wired and the row
// carries a RefreshTokenID, the matching refresh-token row is revoked so
// the session cannot be reopened from the stored cookie.
func (h *SessionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	caller := UserFromContext(r.Context())
	if caller == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("Unauthorized", map[string]string{"reason": "authentication required"}))
		return
	}
	id := chi.URLParam(r, "sessionID")
	if id == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingSessionID", map[string]string{"reason": "session id is required"}))
		return
	}
	row, err := h.deps.Sessions.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("SessionNotFound", map[string]string{"reason": err.Error()}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("SessionLookupFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if err := h.deps.Sessions.Delete(r.Context(), id, caller.ID); err != nil {
		switch {
		case errors.Is(err, ErrSessionNotFound):
			apierror.WriteJSON(w, apierror.NewNotFound("SessionNotFound", map[string]string{"reason": err.Error()}))
		case errors.Is(err, ErrSessionForbidden):
			apierror.WriteJSON(w, apierror.NewPermissionDenied("SessionForbidden", map[string]string{"reason": err.Error()}))
		default:
			apierror.WriteJSON(w, apierror.NewInternal("SessionDeleteFailed", map[string]string{"reason": err.Error()}))
		}
		return
	}
	if h.deps.RefreshService != nil && row.RefreshTokenID != "" {
		// Best-effort — a session without a live refresh token is a valid
		// state (user logged in, token rotated, row cleaned up separately).
		// Swallow the error so the SPA sees a clean 204.
		_ = h.deps.RefreshService.store.Revoke(r.Context(), row.RefreshTokenID, "session_revoked")
	}
	h.audit(r.Context(), caller.ID, row.ID, "session_revoked", r)
	w.WriteHeader(http.StatusNoContent)
}

// RevokeOthers destroys every session the caller owns EXCEPT the one
// they're currently authenticated on. Useful for "log out other
// devices" security flows; Foundry-parity sibling of List + Delete.
//
// 401 when no User in context. 200 with {revoked, currentSessionId}
// on success. Each revoked session also revokes its refresh token
// (when present) and emits a session_revoked audit event. Best-effort
// per-session: a single failing Delete is logged and skipped so the
// rest of the caller's sessions still get cleaned up.
//
// When the caller has no SessionIDAttributeKey on User.Attributes
// (typical for API-key / OAuth auth), there's no current-session
// anchor to preserve and the handler revokes ALL of the caller's
// sessions. The response's CurrentSessionID is empty in that case.
func (h *SessionHandler) RevokeOthers(w http.ResponseWriter, r *http.Request) {
	caller := UserFromContext(r.Context())
	if caller == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("Unauthorized", map[string]string{"reason": "authentication required"}))
		return
	}
	currentID := callerSessionID(caller)
	rows, err := h.deps.Sessions.ListByUser(r.Context(), caller.ID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("SessionListFailed", map[string]string{"reason": err.Error()}))
		return
	}
	revoked := 0
	for _, row := range rows {
		if currentID != "" && row.ID == currentID {
			continue
		}
		if err := h.deps.Sessions.Delete(r.Context(), row.ID, caller.ID); err != nil {
			// Best-effort: per-row failure logged via the audit
			// channel as "session_revoke_failed" but we keep going so
			// a stale row doesn't block the rest of the cleanup.
			h.audit(r.Context(), caller.ID, row.ID, "session_revoke_failed", r)
			continue
		}
		if h.deps.RefreshService != nil && row.RefreshTokenID != "" {
			_ = h.deps.RefreshService.store.Revoke(r.Context(), row.RefreshTokenID, "session_revoked")
		}
		h.audit(r.Context(), caller.ID, row.ID, "session_revoked", r)
		revoked++
	}
	writeJSON(w, http.StatusOK, RevokeOthersResponse{
		Revoked:          revoked,
		CurrentSessionID: currentID,
	})
}

func (h *SessionHandler) audit(ctx context.Context, actorID, sessionID, action string, r *http.Request) {
	if h.deps.AuditStore == nil {
		return
	}
	_ = audit.Record(ctx, h.deps.AuditStore, audit.AuditEvent{
		ActorID:      actorID,
		Action:       action,
		ResourceType: "Session",
		ResourceRID:  sessionID,
		IP:           clientIP(r),
		UserAgent:    r.UserAgent(),
	})
}

// callerSessionID pulls the session handle from User.Attributes (populated
// by the auth middleware on session-bound JWTs). Empty string when the
// caller's token carries no binding — typical for API-key / OAuth auth.
func callerSessionID(u *User) string {
	if u == nil || u.Attributes == nil {
		return ""
	}
	v, _ := u.Attributes[SessionIDAttributeKey].(string)
	return v
}
