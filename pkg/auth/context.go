package auth

import "context"

// User represents an authenticated user.
type User struct {
	ID    string
	Roles []string
}

type contextKey string

const userKey contextKey = "auth-user"

// WithUser stores a User in the context.
func WithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// UserFromContext retrieves the User from the context. Returns nil if not set.
func UserFromContext(ctx context.Context) *User {
	u, _ := ctx.Value(userKey).(*User)
	return u
}
