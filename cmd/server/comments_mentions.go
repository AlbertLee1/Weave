package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/comments"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// mentionUserDirectoryAdapter wraps *auth.PGUserRepository to satisfy
// comments.MentionUserDirectory without widening auth.UserRepository
// (the cascade-stub pattern from US-251). The concrete type carries the
// SearchUsers and LookupUserByEmailForMention methods directly; this
// adapter just translates to the comments package's wire shape.
type mentionUserDirectoryAdapter struct {
	users *auth.PGUserRepository
}

func newMentionUserDirectoryAdapter(users *auth.PGUserRepository) comments.MentionUserDirectory {
	if users == nil {
		return nil
	}
	return &mentionUserDirectoryAdapter{users: users}
}

func (a *mentionUserDirectoryAdapter) LookupUserByEmail(ctx context.Context, email string) (comments.MentionUser, error) {
	row, err := a.users.LookupUserByEmailForMention(ctx, email)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			return comments.MentionUser{}, comments.ErrMentionUserNotFound
		}
		return comments.MentionUser{}, err
	}
	return comments.MentionUser{ID: row.ID, Email: row.Email, Name: row.Name}, nil
}

func (a *mentionUserDirectoryAdapter) SearchMentionUsers(ctx context.Context, query string, limit int) ([]comments.MentionUser, error) {
	rows, err := a.users.SearchUsers(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]comments.MentionUser, 0, len(rows))
	for _, r := range rows {
		out = append(out, comments.MentionUser{ID: r.ID, Email: r.Email, Name: r.Name})
	}
	return out, nil
}

// commentMentionNotifier fans a resolved @mention out to the existing
// notifications table via oms.Repository.CreateNotification. The
// `mention` type tag matches the SPA's notification taxonomy
// (mention | watch | approval | system); future Notification Center
// extensions key off it.
type commentMentionNotifier struct {
	repo oms.Repository
}

func newCommentMentionNotifier(repo oms.Repository) comments.MentionNotifier {
	if repo == nil {
		return nil
	}
	return &commentMentionNotifier{repo: repo}
}

func (n *commentMentionNotifier) NotifyMention(ctx context.Context, ev comments.MentionEvent) error {
	if ev.RecipientID == "" {
		return nil
	}
	notification := &oms.Notification{
		ID:        rid.NewNotificationRID(),
		UserID:    ev.RecipientID,
		Title:     buildMentionTitle(ev.AuthorID),
		Body:      ev.Snippet,
		Type:      "mention",
		Link:      buildMentionLink(ev.TargetRID, ev.CommentID),
		Read:      false,
		CreatedAt: time.Now(),
	}
	return n.repo.CreateNotification(ctx, notification)
}

func buildMentionTitle(authorID string) string {
	if authorID == "" {
		return "You were mentioned"
	}
	return fmt.Sprintf("%s mentioned you", authorID)
}

// buildMentionLink encodes the deep-link the notification dropdown
// follows. The SPA reads `?rid=<targetRid>&commentId=<id>` to scroll
// the right object's Comments tab into view.
func buildMentionLink(targetRID, commentID string) string {
	if targetRID == "" {
		return ""
	}
	q := url.Values{}
	q.Set("rid", targetRID)
	if commentID != "" {
		q.Set("commentId", commentID)
	}
	return "/mentions?" + q.Encode()
}

// Compile-time guards. Surfaces the contract at adapter boot so a
// future signature change on either side (auth or oms) trips at
// compile time rather than at runtime.
var (
	_ comments.MentionUserDirectory = (*mentionUserDirectoryAdapter)(nil)
	_ comments.MentionNotifier      = (*commentMentionNotifier)(nil)
)
