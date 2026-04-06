package auth

import (
	"context"
	"errors"
	"strings"
)

// BootstrapAdmin idempotently ensures a user with the given email exists
// and has the global admin role granted. It is meant to be called once at
// server startup from main.go, gated on the WEAVE_BOOTSTRAP_ADMIN env var.
//
// Behavior:
//   - email == "" → no-op (allows the env var to be unset).
//   - user does not exist → creates the user with id == email.
//   - user exists → reuses the existing id.
//   - admin role grant uses ON CONFLICT DO NOTHING in the repo, so calling
//     this on every startup is safe.
func BootstrapAdmin(ctx context.Context, repo UserRepository, email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil
	}
	if repo == nil {
		return errors.New("BootstrapAdmin: nil repo")
	}

	existing, err := repo.GetUserByEmail(ctx, email)
	switch {
	case err == nil:
		return repo.UpsertUserRole(ctx, existing.ID, RoleAdmin)
	case errors.Is(err, ErrUserNotFound):
		// fall through to create
	default:
		return err
	}

	// Use the email itself as the user id for simplicity. The id field is
	// just a stable handle; in a future JWT phase the sub claim will replace
	// this and bootstrap can be re-run with the new id.
	if err := repo.CreateUser(ctx, &UserRecord{
		ID:    email,
		Email: email,
	}); err != nil {
		return err
	}
	return repo.UpsertUserRole(ctx, email, RoleAdmin)
}
