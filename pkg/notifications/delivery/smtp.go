package delivery

import (
	"context"
	"errors"
	"fmt"
	"net/smtp"
	"strings"
)

// SMTPDriver delivers a notification as a plain-text RFC 5322 email.
// Wire shape mirrors pkg/notifications.SMTPMailer (the legacy US-338
// mailer) — kept as a separate type to keep the delivery interface
// surface uniform across channels.
//
// Zero-value behaviour: a SMTPDriver with Host == "" silently no-ops
// on Send so a degraded deployment (no WEAVE_SMTP_HOST env var) never
// tries to dial a phantom server. The dispatcher leaves the driver
// registered in either case so the SPA's preference UI can still show
// "email available" without checking host wiring per request.
type SMTPDriver struct {
	Host string
	Port int
	From string
	Auth smtp.Auth

	// SendMail is the test-injection seam. Production wiring leaves
	// this nil so smtp.SendMail is used.
	SendMail func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

// Channel reports the registry key. Always returns ChannelEmail.
func (d *SMTPDriver) Channel() string { return ChannelEmail }

// Send dispatches a single email envelope. The recipient priority is
// envelope-supplied address first (so a user-set override in
// notification_preferences.target wins), falling back to UserID — the
// caller-side resolver translates UserID → email before invoking the
// driver, so an empty Recipient at this layer means "drop, no error".
func (d *SMTPDriver) Send(_ context.Context, env Envelope) error {
	if d == nil || d.Host == "" {
		return nil
	}
	to := strings.TrimSpace(env.Recipient)
	if to == "" {
		return nil
	}
	port := d.Port
	if port == 0 {
		port = 25
	}
	addr := fmt.Sprintf("%s:%d", d.Host, port)
	msg := buildSMTPMessage(d.From, to, env.Title, env.Body, env.Link)
	send := d.SendMail
	if send == nil {
		send = smtp.SendMail
	}
	if err := send(addr, d.Auth, d.From, []string{to}, msg); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
}

// buildSMTPMessage emits the RFC 5322 wire bytes. Mirrors
// pkg/notifications.buildRFC5322Message but appends the deep link as a
// trailing line so users can jump back to the in-app object directly
// from the email — matches the SPA's notification dropdown shape.
func buildSMTPMessage(from, to, subject, body, link string) []byte {
	var b strings.Builder
	b.Grow(256 + len(body) + len(link))
	b.WriteString("From: ")
	b.WriteString(from)
	b.WriteString("\r\n")
	b.WriteString("To: ")
	b.WriteString(to)
	b.WriteString("\r\n")
	b.WriteString("Subject: ")
	b.WriteString(subject)
	b.WriteString("\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	if strings.TrimSpace(link) != "" {
		b.WriteString("\r\n\r\n")
		b.WriteString(link)
	}
	return []byte(b.String())
}

// ErrEmptyRecipient is returned only by tests / legacy callers that
// pass an explicit empty recipient through the legacy SMTPMailer path.
// The Driver itself treats an empty Recipient as a soft-skip and
// returns nil.
var ErrEmptyRecipient = errors.New("smtp: empty recipient")

var _ Driver = (*SMTPDriver)(nil)
