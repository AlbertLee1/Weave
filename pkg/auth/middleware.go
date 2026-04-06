package auth

import (
	"errors"
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
//   - "token": legacy production mode. A valid Authorization: Bearer <token>
//     header is required, but the token value itself is not cryptographically
//     verified. Deprecated; emits a warning at startup.
//   - "jwt": JWT-verified production mode. Requires a non-nil signer to be
//     passed. The Authorization Bearer token must verify against the signer's
//     public key; claims populate the request User.
//
// The variadic signer is optional so existing dev/token callers do not need
// to change their signature. When AUTH_MODE=jwt and no signer is supplied
// the middleware refuses every request with 500 InvalidAuthMode.
func Middleware(signers ...*JWTSigner) func(http.Handler) http.Handler {
	mode := os.Getenv("AUTH_MODE")
	var signer *JWTSigner
	if len(signers) > 0 {
		signer = signers[0]
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch mode {
			case "", "dev":
				handleDev(next, w, r)
			case "jwt":
				handleJWT(signer, next, w, r)
			case "token":
				handleProd(next, w, r)
			default:
				apierror.WriteJSON(w, apierror.NewInternal("InvalidAuthMode", map[string]string{
					"mode": mode,
				}))
			}
		})
	}
}

// handleJWT validates an RS256 access token and populates the User from
// claims. Returns 401 on missing, malformed, expired, or wrong-signature
// tokens. Returns 500 if the middleware is in jwt mode but no signer was
// configured (a startup misconfiguration).
func handleJWT(signer *JWTSigner, next http.Handler, w http.ResponseWriter, r *http.Request) {
	if signer == nil {
		apierror.WriteJSON(w, apierror.NewInternal("JWTSignerNotConfigured", map[string]string{
			"reason": "AUTH_MODE=jwt requires a Signer in middleware constructor",
		}))
		return
	}

	tok := extractBearer(r)
	if tok == "" {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthorization", map[string]string{
			"reason": "Authorization Bearer token is required",
		}))
		return
	}

	claims, err := signer.Verify(tok)
	if err != nil {
		switch {
		case errors.Is(err, ErrTokenExpired):
			apierror.WriteJSON(w, apierror.NewUnauthorized("TokenExpired", map[string]string{"reason": err.Error()}))
		case errors.Is(err, ErrInvalidIssuer):
			apierror.WriteJSON(w, apierror.NewUnauthorized("InvalidIssuer", map[string]string{"reason": err.Error()}))
		case errors.Is(err, ErrInvalidAudience):
			apierror.WriteJSON(w, apierror.NewUnauthorized("InvalidAudience", map[string]string{"reason": err.Error()}))
		case errors.Is(err, ErrInvalidSignature):
			apierror.WriteJSON(w, apierror.NewUnauthorized("InvalidSignature", map[string]string{"reason": err.Error()}))
		default:
			apierror.WriteJSON(w, apierror.NewUnauthorized("InvalidToken", map[string]string{"reason": err.Error()}))
		}
		return
	}

	u := &User{
		ID:            claims.Subject,
		Email:         claims.Weave.Email,
		Name:          claims.Weave.Name,
		Roles:         claims.Weave.Roles,
		OntologyRoles: claims.Weave.OntologyRoles,
	}
	ctx := WithUser(r.Context(), u)
	next.ServeHTTP(w, r.WithContext(ctx))
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
