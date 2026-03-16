package auth

import (
	"net/http"
	"os"
	"strings"

	"github.com/liyang/weave/pkg/apierror"
)

// Middleware returns an HTTP middleware that authenticates requests.
//
// The behaviour depends on the AUTH_MODE environment variable:
//   - "" or "dev": development mode, no token required. A default dev user
//     is injected. If a Bearer token IS provided, its value is used as
//     the user ID instead.
//   - "token": production mode. A valid Authorization: Bearer <token>
//     header is required.
func Middleware() func(http.Handler) http.Handler {
	mode := os.Getenv("AUTH_MODE")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if mode == "" || mode == "dev" {
				handleDev(next, w, r)
			} else {
				handleProd(next, w, r)
			}
		})
	}
}

// handleDev implements the dev-mode authentication logic.
func handleDev(next http.Handler, w http.ResponseWriter, r *http.Request) {
	u := &User{
		ID:    "dev-user",
		Roles: []string{"admin"},
	}

	if token := extractBearer(r); token != "" {
		u.ID = token
	}

	ctx := WithUser(r.Context(), u)
	next.ServeHTTP(w, r.WithContext(ctx))
}

// handleProd implements the production-mode authentication logic.
func handleProd(next http.Handler, w http.ResponseWriter, r *http.Request) {
	header := r.Header.Get("Authorization")
	if header == "" {
		apiErr := apierror.NewUnauthorized("MissingAuthorization", map[string]string{
			"reason": "Authorization header is required",
		})
		apierror.WriteJSON(w, apiErr)
		return
	}

	if !strings.HasPrefix(header, "Bearer ") {
		apiErr := apierror.NewUnauthorized("InvalidAuthorizationScheme", map[string]string{
			"reason": "Authorization header must use Bearer scheme",
		})
		apierror.WriteJSON(w, apiErr)
		return
	}

	token := strings.TrimPrefix(header, "Bearer ")
	if token == "" {
		apiErr := apierror.NewUnauthorized("EmptyBearerToken", map[string]string{
			"reason": "Bearer token must not be empty",
		})
		apierror.WriteJSON(w, apiErr)
		return
	}

	u := &User{
		ID:    token,
		Roles: []string{},
	}

	if strings.HasPrefix(token, "user:") {
		u.ID = strings.TrimPrefix(token, "user:")
	}

	if u.ID == "" {
		apiErr := apierror.NewPermissionDenied("InvalidToken", map[string]string{
			"reason": "Token validation failed",
		})
		apierror.WriteJSON(w, apiErr)
		return
	}

	ctx := WithUser(r.Context(), u)
	next.ServeHTTP(w, r.WithContext(ctx))
}

// extractBearer returns the bearer token from the Authorization header,
// or an empty string if the header is missing or uses a different scheme.
func extractBearer(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimPrefix(header, "Bearer ")
	}
	return ""
}
