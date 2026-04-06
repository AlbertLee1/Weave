package auth

import (
	"encoding/json"
	"net/http"
)

// LogoutHandler implements POST /api/auth/logout. It revokes the supplied
// refresh token (if any), clears the refresh cookie, and always returns 204.
// The handler is intentionally idempotent: an unknown or already-revoked
// token does not produce an error so the SPA can call it on logout regardless
// of session state.
type LogoutHandler struct {
	rs *RefreshService
}

// NewLogoutHandler builds a logout handler.
func NewLogoutHandler(rs *RefreshService) *LogoutHandler {
	return &LogoutHandler{rs: rs}
}

// ServeHTTP implements http.Handler.
func (h *LogoutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	plain := req.RefreshToken
	if plain == "" {
		if c, err := r.Cookie(RefreshCookieName); err == nil {
			plain = c.Value
		}
	}

	if plain != "" && h.rs != nil {
		_ = h.rs.Revoke(r.Context(), plain)
	}

	// Always clear the refresh cookie on the client.
	http.SetCookie(w, &http.Cookie{
		Name:     RefreshCookieName,
		Value:    "",
		Path:     "/api/auth",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	w.WriteHeader(http.StatusNoContent)
}

var _ http.Handler = (*LogoutHandler)(nil)
