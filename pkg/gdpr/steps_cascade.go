package gdpr

import "context"

// The cascade steps wired by the cmd/server bootstrap when the operator
// invokes DELETE /api/admin/gdpr/users/{id}/erase?cascade=true (US-494).
// Each adapter targets exactly one user-keyed subsystem so the
// orchestrator's per-step progress log records "rows touched in
// comments, reactions, watches, user_preferences" individually and the
// "user_id 出现次数 = 0" acceptance check can attribute any residual
// row to the specific step that missed it.
//
// All adapters share the same Go signature
//
//	DeleteAllForUser(ctx, userID) (int, error)
//
// so a single fakeCascade in tests covers every step. Each adapter
// interface is named after its target table for clarity at the call
// site in cmd/server/main.go.

// CommentsCascade removes every comment row authored by userID,
// including soft-deleted tombstones. Implemented by comments.Store.
type CommentsCascade interface {
	DeleteAllForUser(ctx context.Context, userID string) (rowsAffected int, err error)
}

// ReactionsCascade removes every (userID, target, emoji) reaction row.
// Implemented by reactions.Store.
type ReactionsCascade interface {
	DeleteAllForUser(ctx context.Context, userID string) (rowsAffected int, err error)
}

// WatchesCascade removes every (userID, target) follow row. Implemented
// by watches.Store.
type WatchesCascade interface {
	DeleteAllForUser(ctx context.Context, userID string) (rowsAffected int, err error)
}

// UserPrefsCascade removes the preferences row PK'd on userID.
// Implemented by userprefs.Store.
type UserPrefsCascade interface {
	DeleteAllForUser(ctx context.Context, userID string) (rowsAffected int, err error)
}

// NewCommentsCascadeStep returns the "comments_cascade" Step. nil
// adapter is a no-op so partially-wired test deployments can still
// compose a cascade eraser without panicking.
func NewCommentsCascadeStep(d CommentsCascade) Step {
	return StepFunc{
		StepName: "comments_cascade",
		Fn: func(ctx context.Context, userID string) (int, error) {
			if d == nil {
				return 0, nil
			}
			return d.DeleteAllForUser(ctx, userID)
		},
	}
}

// NewReactionsCascadeStep returns the "reactions_cascade" Step.
func NewReactionsCascadeStep(d ReactionsCascade) Step {
	return StepFunc{
		StepName: "reactions_cascade",
		Fn: func(ctx context.Context, userID string) (int, error) {
			if d == nil {
				return 0, nil
			}
			return d.DeleteAllForUser(ctx, userID)
		},
	}
}

// NewWatchesCascadeStep returns the "watches_cascade" Step.
func NewWatchesCascadeStep(d WatchesCascade) Step {
	return StepFunc{
		StepName: "watches_cascade",
		Fn: func(ctx context.Context, userID string) (int, error) {
			if d == nil {
				return 0, nil
			}
			return d.DeleteAllForUser(ctx, userID)
		},
	}
}

// NewUserPrefsCascadeStep returns the "user_preferences_cascade" Step.
func NewUserPrefsCascadeStep(d UserPrefsCascade) Step {
	return StepFunc{
		StepName: "user_preferences_cascade",
		Fn: func(ctx context.Context, userID string) (int, error) {
			if d == nil {
				return 0, nil
			}
			return d.DeleteAllForUser(ctx, userID)
		},
	}
}
