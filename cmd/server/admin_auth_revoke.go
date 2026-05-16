package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
)

// AdminAuthRevokeDeps wires the JWT revocation handler. The Store performs
// the persisted insert; the Checker (which fronts the Store with a TTL
// cache) is invalidated for the affected JTI so the same-process middleware
// observes the new state on the very next request.
//
// Either field may be nil — the handler returns 503 in that case so
// degraded boot (no PG pool, dev mode without persistence) does not crash.
type AdminAuthRevokeDeps struct {
	Store   auth.RevocationStore
	Checker *auth.CachedRevocationChecker
}

// AdminAuthRevokeRequest is the request body for POST
// /api/auth/tokens/{jti}/revoke. Both fields are optional; the handler
// records them alongside the JTI so audit consumers can see why a token
// was revoked.
type AdminAuthRevokeRequest struct {
	UserID    string `json:"userId,omitempty"`
	Reason    string `json:"reason,omitempty"`
	ExpiresAt string `json:"expiresAt,omitempty"`
}

// AdminAuthRevokeResponse is the wire shape returned by a successful revoke.
type AdminAuthRevokeResponse struct {
	JTI       string `json:"jti"`
	RevokedAt string `json:"revokedAt"`
}

// NewAdminAuthRevokeHandler constructs the handler for POST
// /api/auth/tokens/{jti}/revoke. The route is wrapped in
// auth.RequirePermission(PermUserManage) by main.go so this body performs
// no authorization on its own.
//
// On success returns 200 + {jti, revokedAt}. Returns:
//   - 400 InvalidJTI when {jti} is missing or empty after trim.
//   - 503 RevocationNotConfigured when the Store is nil (degraded boot).
//   - 500 RevokeFailed when the underlying Store returns an error.
func NewAdminAuthRevokeHandler(deps AdminAuthRevokeDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jti := strings.TrimSpace(chi.URLParam(r, "jti"))
		if jti == "" {
			apierror.WriteJSON(w, apierror.NewBadRequest("InvalidJTI", map[string]string{
				"reason": "path parameter {jti} is required",
			}))
			return
		}
		if deps.Store == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"errorCode": "SERVICE_UNAVAILABLE",
				"errorName": "RevocationNotConfigured",
			})
			return
		}

		var body AdminAuthRevokeRequest
		// An empty body is fine — the JTI alone is enough to revoke.
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}

		exp := time.Time{}
		if body.ExpiresAt != "" {
			if t, err := time.Parse(time.RFC3339, body.ExpiresAt); err == nil {
				exp = t
			} else {
				apierror.WriteJSON(w, apierror.NewBadRequest("InvalidExpiresAt", map[string]string{
					"reason": "expiresAt must be RFC3339 if provided",
				}))
				return
			}
		}

		now := time.Now().UTC()
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := deps.Store.Revoke(ctx, auth.RevocationRecord{
			JTI:       jti,
			UserID:    body.UserID,
			ExpiresAt: exp,
			RevokedAt: now,
			Reason:    body.Reason,
		}); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("RevokeFailed", map[string]string{
				"reason": err.Error(),
			}))
			return
		}

		// Same-process cache flush so the next middleware lookup sees the
		// new state without waiting for the TTL.
		if deps.Checker != nil {
			deps.Checker.Invalidate(jti)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(AdminAuthRevokeResponse{
			JTI:       jti,
			RevokedAt: now.Format(time.RFC3339Nano),
		})
	})
}
