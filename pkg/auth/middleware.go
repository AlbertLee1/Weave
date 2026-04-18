package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

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
//     public key; claims populate the request User. In jwt mode the
//     middleware ALSO accepts non-interactive API keys (see
//     MiddlewareWithAPIKeys) when wired through the extended constructor.
//
// The variadic signer is optional so existing dev/token callers do not need
// to change their signature. When AUTH_MODE=jwt and no signer is supplied
// the middleware refuses every request with 500 InvalidAuthMode.
func Middleware(signers ...*JWTSigner) func(http.Handler) http.Handler {
	var signer *JWTSigner
	if len(signers) > 0 {
		signer = signers[0]
	}
	return MiddlewareWithAPIKeys(signer, nil, nil, nil)
}

// MiddlewareWithAPIKeys is the extended middleware constructor that also
// accepts non-interactive API key bearer tokens. It is the constructor used
// by the production wiring in cmd/server/main.go. Passing nil for any of
// apiKeys / users / resolver disables the API key path; the middleware then
// behaves identically to Middleware(signer).
//
// In jwt mode, when a Bearer token starts with the wvk_ marker the middleware
// looks the prefix up in apiKeys, constant-time compares the SHA-256 hash,
// checks the row is not expired or revoked, populates User from the owning
// row's roles via users + resolver, and (best-effort) records the use time.
func MiddlewareWithAPIKeys(signer *JWTSigner, apiKeys APIKeyRepository, users UserRepository, resolver *RoleResolver) func(http.Handler) http.Handler {
	mode := os.Getenv("AUTH_MODE")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch mode {
			case "", "dev":
				handleDev(signer, next, w, r)
			case "jwt":
				handleJWT(signer, apiKeys, users, resolver, next, w, r)
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
//
// When the bearer token starts with the wvk_ API-key marker AND the
// middleware was constructed with an APIKeyRepository, the function dispatches
// to handleAPIKey before attempting JWT verification. This is the only new
// branch added for Tier 2.4.
func handleJWT(signer *JWTSigner, apiKeys APIKeyRepository, users UserRepository, resolver *RoleResolver, next http.Handler, w http.ResponseWriter, r *http.Request) {
	tok := extractBearer(r)
	if tok == "" {
		// API keys still need a Bearer token, so we can fall through to the
		// missing-token branch below regardless of whether they are wired.
		if signer == nil {
			apierror.WriteJSON(w, apierror.NewInternal("JWTSignerNotConfigured", map[string]string{
				"reason": "AUTH_MODE=jwt requires a Signer in middleware constructor",
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthorization", map[string]string{
			"reason": "Authorization Bearer token is required",
		}))
		return
	}

	// Tier 2.4: API key path. wvk_ marked tokens are NEVER verified by the
	// JWT signer; they are looked up in the api_keys table instead.
	if apiKeys != nil && IsAPIKey(tok) {
		handleAPIKey(tok, apiKeys, users, resolver, next, w, r)
		return
	}

	if signer == nil {
		apierror.WriteJSON(w, apierror.NewInternal("JWTSignerNotConfigured", map[string]string{
			"reason": "AUTH_MODE=jwt requires a Signer in middleware constructor",
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
	// US-054: marking claim flows into User.Attributes so
	// pkg/security.RuleTypeMarkingSubset and pkg/oss marking enforcement
	// evaluate the caller's held markings uniformly regardless of whether
	// the user came in through JWT, dev mode, or an API key.
	if len(claims.Weave.Markings) > 0 {
		if u.Attributes == nil {
			u.Attributes = make(map[string]any, 1)
		}
		u.Attributes[MarkingsAttributeKey] = append([]string(nil), claims.Weave.Markings...)
	}
	ctx := WithUser(r.Context(), u)
	next.ServeHTTP(w, r.WithContext(ctx))
}

// handleAPIKey verifies a "wvk_..." bearer token against the api_keys table
// and populates the request User from the owning row's user record + roles.
// Errors always render a generic 401 to avoid leaking which step failed.
func handleAPIKey(tok string, apiKeys APIKeyRepository, users UserRepository, resolver *RoleResolver, next http.Handler, w http.ResponseWriter, r *http.Request) {
	prefix, err := ParseAPIKey(tok)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("InvalidAPIKey", map[string]string{
			"reason": "malformed api key",
		}))
		return
	}

	rec, err := apiKeys.GetByPrefix(r.Context(), prefix)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("InvalidAPIKey", map[string]string{
			"reason": "api key not found",
		}))
		return
	}

	// Constant-time hash comparison: never branch on the first mismatching
	// byte. The candidate hash is computed from the FULL raw token.
	candidate := HashAPIKey(tok)
	if subtle.ConstantTimeCompare(candidate, rec.KeyHash) != 1 {
		apierror.WriteJSON(w, apierror.NewUnauthorized("InvalidAPIKey", map[string]string{
			"reason": "api key hash mismatch",
		}))
		return
	}

	now := time.Now()
	if rec.IsExpired(now) {
		apierror.WriteJSON(w, apierror.NewUnauthorized("APIKeyExpired", map[string]string{
			"reason": "api key has expired",
		}))
		return
	}
	if rec.IsRotationExpired(now) {
		apierror.WriteJSON(w, apierror.NewUnauthorized("APIKeyRotated", map[string]string{
			"reason": "api key grace period has ended; use the rotated successor",
		}))
		return
	}

	// Build the User from the owning user record. Roles are resolved through
	// the same RoleResolver used by JWT login so dev-mode RBAC behaviour is
	// preserved. If the resolver is nil (test wiring), fall back to the empty
	// role set; the surrounding RequirePermission middleware will then deny.
	u := &User{ID: rec.UserID}
	if users != nil {
		if owner, oerr := users.GetUserByID(r.Context(), rec.UserID); oerr == nil {
			u.Email = owner.Email
			u.Name = owner.Name
		}
	}
	if resolver != nil {
		if global, scoped, rerr := resolver.Resolve(r.Context(), rec.UserID); rerr == nil {
			u.Roles = global
			u.OntologyRoles = scoped
		}
	}

	// Record the usage timestamp without blocking the request. Errors are
	// intentionally swallowed: telemetry must never reject a real request.
	go func(id string) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = apiKeys.TouchLastUsed(ctx, id, time.Now())
	}(rec.ID)

	ctx := WithUser(r.Context(), u)
	next.ServeHTTP(w, r.WithContext(ctx))
}

// handleDev implements the dev-mode authentication logic.
//
// US-081: when a JWT signer is available (auto-generated or explicitly
// configured) and the request carries a valid Bearer JWT, the user is
// populated from the token claims — including Roles and Attributes — so
// that security policy enforcement (row-level, column-level) works for
// logged-in users. Requests without a token, or with an invalid/expired
// token, fall through to the default admin dev-user as before.
func handleDev(signer *JWTSigner, next http.Handler, w http.ResponseWriter, r *http.Request) {
	tok := extractBearer(r)

	// When a signer is available and a bearer token is present, try to
	// verify it as a JWT. On success, build a full user from claims so
	// the security engine sees real identity / roles / markings.
	if tok != "" && signer != nil {
		claims, err := signer.Verify(tok)
		if err == nil {
			u := &User{
				ID:            claims.Subject,
				Email:         claims.Weave.Email,
				Name:          claims.Weave.Name,
				Roles:         claims.Weave.Roles,
				OntologyRoles: claims.Weave.OntologyRoles,
				Attributes:    make(map[string]any),
			}
			if len(claims.Weave.Markings) > 0 {
				u.Attributes[MarkingsAttributeKey] = append([]string(nil), claims.Weave.Markings...)
			}
			// Copy Roles into Attributes so the property-level policy
			// engine can check them via UserAttr: "roles".
			if len(u.Roles) > 0 {
				u.Attributes["roles"] = append([]string(nil), u.Roles...)
			}
			ctx := WithUser(r.Context(), u)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
	}

	// Fallback: default dev user (admin).
	u := &User{
		ID:    "dev-user",
		Roles: []string{"admin"},
	}
	if tok != "" {
		u.ID = tok
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
