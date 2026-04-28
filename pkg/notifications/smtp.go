package notifications

import (
	"context"
	"errors"
	"fmt"
	"net/smtp"
	"strings"
)

// SMTPMailer is a thin RFC 5322 wrapper over net/smtp.SendMail. The
// envelope shape is intentionally minimal: From / To / Subject / plain
// UTF-8 body. HTML bodies, attachments, and BCC are out of scope for
// US-338 — the email is a parity copy of the in-app notification, not a
// general-purpose transactional mailer.
//
// Zero-value behaviour: a SMTPMailer with Host == "" silently no-ops on
// Send so a degraded deployment (no SMTP_HOST env var) never tries to
// dial a phantom server. The Fanout wires this Mailer unconditionally —
// the gate is moved into the mailer itself so the fan-out path stays
// branch-free.
type SMTPMailer struct {
	// Host is the SMTP server hostname. Empty means the mailer is
	// disabled (Send no-ops without error).
	Host string
	// Port is the SMTP server port. Defaults to 25 when zero.
	Port int
	// From is the envelope sender. Required when Host is set.
	From string
	// Auth is the authentication mechanism, typically smtp.PlainAuth.
	// Nil is permitted for unauthenticated relays (e.g. localhost
	// MTAs in dev / self-hosted deployments).
	Auth smtp.Auth

	// sendMail is the injection seam used by tests. Production code
	// path uses smtp.SendMail when nil.
	sendMail func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

// Send dispatches a single plain-text email. Returns an error when the
// recipient is empty (caller bug) or the underlying transport fails;
// no-ops when Host is empty.
func (m *SMTPMailer) Send(_ context.Context, to, subject, body string) error {
	if m == nil || m.Host == "" {
		return nil
	}
	if strings.TrimSpace(to) == "" {
		return errors.New("smtp: empty recipient")
	}
	port := m.Port
	if port == 0 {
		port = 25
	}
	addr := fmt.Sprintf("%s:%d", m.Host, port)
	msg := buildRFC5322Message(m.From, to, subject, body)
	send := m.sendMail
	if send == nil {
		send = smtp.SendMail
	}
	return send(addr, m.Auth, m.From, []string{to}, msg)
}

// buildRFC5322Message returns the wire bytes for a single From/To/Subject
// envelope with a UTF-8 plain-text body. Headers + body are CRLF-
// separated per RFC 5322 §2.1; the blank line after Content-Type marks
// the body start.
func buildRFC5322Message(from, to, subject, body string) []byte {
	var b strings.Builder
	b.Grow(256 + len(body))
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
	return []byte(b.String())
}

// Compile-time check.
var _ Mailer = (*SMTPMailer)(nil)
