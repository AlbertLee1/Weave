package gdpr

import (
	"context"
)

// The standard erase steps wired by the cmd/server bootstrap. Each step
// targets a single subsystem; cmd/server composes them in priority
// order (auth-state revocation → row deletes → audit redaction).
//
// All steps satisfy the Step interface. Their dependencies are narrow
// adapters around existing services (UserRepo, SessionStore,
// RefreshService, ...) so degraded-mode test routers can compose any
// subset.

// UserDeleter removes the user identity row + role grants + any
// per-user persistence tied to the users PK.
type UserDeleter interface {
	DeleteUser(ctx context.Context, userID string) (rowsAffected int, err error)
}

// SessionRevoker invalidates every active session belonging to userID.
// Mirrors auth.SessionStore.DeleteAllForUser. Implementations should
// be idempotent: a user with zero sessions returns (0, nil).
type SessionRevoker interface {
	DeleteAllForUser(ctx context.Context, userID string) error
}

// RefreshRevoker is the same shape as auth.RefreshService.RevokeAllForUser.
type RefreshRevoker interface {
	RevokeAllForUser(ctx context.Context, userID, reason string) error
}

// AuditRedactor flags the user for audit-log redaction. Backed by
// pkg/audit.RedactionStore in production; the audit List path then
// scrubs the user's PII from every event response.
type AuditRedactor interface {
	Add(ctx context.Context, actorID, reason string) error
}

// APIKeyRevoker drops every API key the user owns.
type APIKeyRevoker interface {
	DeleteAllForUser(ctx context.Context, userID string) (rowsAffected int, err error)
}

// NewSessionStep deletes the user's active session inventory.
func NewSessionStep(rev SessionRevoker) Step {
	return StepFunc{
		StepName: "sessions",
		Fn: func(ctx context.Context, userID string) (int, error) {
			if rev == nil {
				return 0, nil
			}
			if err := rev.DeleteAllForUser(ctx, userID); err != nil {
				return 0, err
			}
			return 0, nil
		},
	}
}

// NewRefreshStep revokes the user's outstanding refresh tokens. Reason
// "gdpr_erase" surfaces in the refresh_tokens audit trail so operators
// can later tell logout-from-erase apart from logout-from-SSO.
func NewRefreshStep(rev RefreshRevoker) Step {
	return StepFunc{
		StepName: "refresh_tokens",
		Fn: func(ctx context.Context, userID string) (int, error) {
			if rev == nil {
				return 0, nil
			}
			if err := rev.RevokeAllForUser(ctx, userID, "gdpr_erase"); err != nil {
				return 0, err
			}
			return 0, nil
		},
	}
}

// NewAPIKeyStep deletes every API key owned by the user.
func NewAPIKeyStep(rev APIKeyRevoker) Step {
	return StepFunc{
		StepName: "api_keys",
		Fn: func(ctx context.Context, userID string) (int, error) {
			if rev == nil {
				return 0, nil
			}
			n, err := rev.DeleteAllForUser(ctx, userID)
			if err != nil {
				return 0, err
			}
			return n, nil
		},
	}
}

// NewUserStep removes the user identity row last (after all dependent
// stores have been cleared). Reports the number of rows actually
// deleted; idempotent so a second pass over an already-erased user
// returns (0, nil).
func NewUserStep(d UserDeleter) Step {
	return StepFunc{
		StepName: "user_identity",
		Fn: func(ctx context.Context, userID string) (int, error) {
			if d == nil {
				return 0, nil
			}
			n, err := d.DeleteUser(ctx, userID)
			if err != nil {
				return 0, err
			}
			return n, nil
		},
	}
}

// NewAuditRedactionStep flags the user for audit-log redaction. The
// audit chain itself is NOT mutated — this writes a row to
// gdpr_redactions and the audit RedactingStore decorator overlays the
// result at List time. Hash chain stays verifiable end-to-end.
func NewAuditRedactionStep(red AuditRedactor) Step {
	return StepFunc{
		StepName: "audit_redaction",
		Fn: func(ctx context.Context, userID string) (int, error) {
			if red == nil {
				return 0, nil
			}
			if err := red.Add(ctx, userID, "gdpr_erase"); err != nil {
				return 0, err
			}
			return 1, nil
		},
	}
}
