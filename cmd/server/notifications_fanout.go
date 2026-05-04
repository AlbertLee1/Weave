package main

import (
	"context"
	"errors"
	"os"
	"strconv"
	"time"

	"net/smtp"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/notifications"
	"github.com/liyang/weave/pkg/notifications/delivery"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// notificationCreatorAdapter satisfies notifications.NotificationCreator
// by writing rows through the existing oms.Repository.CreateNotification
// path. Same dep-direction trick as commentMentionNotifier — pkg/notifications
// stays free of any oms import; the adapter lives in cmd/server.
type notificationCreatorAdapter struct {
	repo oms.Repository
}

func newNotificationCreatorAdapter(repo oms.Repository) notifications.NotificationCreator {
	if repo == nil {
		return nil
	}
	return &notificationCreatorAdapter{repo: repo}
}

func (a *notificationCreatorAdapter) CreateNotificationForUser(ctx context.Context, userID, title, body, nType, link string) error {
	if userID == "" {
		return nil
	}
	n := &oms.Notification{
		ID:        rid.NewNotificationRID(),
		UserID:    userID,
		Title:     title,
		Body:      body,
		Type:      nType,
		Link:      link,
		Read:      false,
		CreatedAt: time.Now(),
	}
	return a.repo.CreateNotification(ctx, n)
}

// userEmailResolverAdapter looks up a user's email via the auth user
// repository for the optional SMTP-side delivery path. Mirrors
// mentionUserDirectoryAdapter — narrow translation to the contract the
// notifications package expects.
type userEmailResolverAdapter struct {
	users auth.UserRepository
}

func newUserEmailResolverAdapter(users auth.UserRepository) notifications.EmailResolver {
	if users == nil {
		return nil
	}
	return &userEmailResolverAdapter{users: users}
}

func (a *userEmailResolverAdapter) ResolveEmail(ctx context.Context, userID string) (string, error) {
	rec, err := a.users.GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			return "", notifications.ErrEmailNotFound
		}
		return "", err
	}
	if rec == nil || rec.Email == "" {
		return "", notifications.ErrEmailNotFound
	}
	return rec.Email, nil
}

// buildSMTPMailerFromEnv constructs a *notifications.SMTPMailer from
// the WEAVE_SMTP_* environment variables. Returns nil when SMTP_HOST is
// empty so the deployment runs without email delivery — the in-app
// notification path is unaffected.
//
// Env vars:
//
//	WEAVE_SMTP_HOST     SMTP server hostname (required to enable)
//	WEAVE_SMTP_PORT     SMTP port (defaults to 25)
//	WEAVE_SMTP_FROM     envelope sender (defaults to "weave@localhost")
//	WEAVE_SMTP_USER     PLAIN auth username (optional)
//	WEAVE_SMTP_PASS     PLAIN auth password (optional)
func buildSMTPMailerFromEnv() *notifications.SMTPMailer {
	host := os.Getenv("WEAVE_SMTP_HOST")
	if host == "" {
		return nil
	}
	port := 25
	if raw := os.Getenv("WEAVE_SMTP_PORT"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			port = n
		}
	}
	from := os.Getenv("WEAVE_SMTP_FROM")
	if from == "" {
		from = "weave@localhost"
	}
	var sauth smtp.Auth
	if user := os.Getenv("WEAVE_SMTP_USER"); user != "" {
		sauth = smtp.PlainAuth("", user, os.Getenv("WEAVE_SMTP_PASS"), host)
	}
	return &notifications.SMTPMailer{
		Host: host,
		Port: port,
		From: from,
		Auth: sauth,
	}
}

// buildDeliveryRegistry assembles the multi-channel Driver registry
// for US-429. SMTP is wired only when WEAVE_SMTP_HOST is non-empty;
// Slack and Webhook are always registered because their per-user
// target URLs come from notification_preferences.target — no
// deployment-wide credential is required for them to no-op safely
// when a user has no preference row.
func buildDeliveryRegistry() *delivery.Registry {
	registry := delivery.NewRegistry()
	if mailer := buildSMTPMailerFromEnv(); mailer != nil {
		registry.Register(&delivery.SMTPDriver{
			Host: mailer.Host,
			Port: mailer.Port,
			From: mailer.From,
			Auth: mailer.Auth,
		})
	}
	registry.Register(&delivery.SlackDriver{})
	registry.Register(&delivery.WebhookDriver{})
	return registry
}

// Compile-time guards.
var (
	_ notifications.NotificationCreator = (*notificationCreatorAdapter)(nil)
	_ notifications.EmailResolver       = (*userEmailResolverAdapter)(nil)
)
