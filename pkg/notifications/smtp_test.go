package notifications

import (
	"context"
	"net/smtp"
	"strings"
	"testing"
)

// TestSMTPMailer_FormatsMessage exercises Send by injecting a fake send
// hook so we can assert the wire shape (RFC 5322 envelope) without
// standing up a real SMTP listener. The sendMail field is intentionally
// package-private so tests can override it; production wires
// smtp.SendMail.
func TestSMTPMailer_FormatsMessage(t *testing.T) {
	var got struct {
		addr string
		auth smtp.Auth
		from string
		to   []string
		body string
	}
	m := &SMTPMailer{
		Host: "smtp.example.com",
		Port: 587,
		From: "weave-notify@example.com",
		Auth: smtp.PlainAuth("", "weave-notify@example.com", "secret", "smtp.example.com"),
		sendMail: func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
			got.addr = addr
			got.auth = a
			got.from = from
			got.to = append([]string(nil), to...)
			got.body = string(msg)
			return nil
		},
	}

	if err := m.Send(context.Background(), "alice@example.com", "Hello", "Body line"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got.addr != "smtp.example.com:587" {
		t.Errorf("addr = %q want smtp.example.com:587", got.addr)
	}
	if got.from != "weave-notify@example.com" {
		t.Errorf("from = %q", got.from)
	}
	if len(got.to) != 1 || got.to[0] != "alice@example.com" {
		t.Errorf("to = %v", got.to)
	}
	if got.auth == nil {
		t.Errorf("auth should be passed through")
	}

	mustContain := []string{
		"From: weave-notify@example.com",
		"To: alice@example.com",
		"Subject: Hello",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"\r\n\r\nBody line",
	}
	for _, s := range mustContain {
		if !strings.Contains(got.body, s) {
			t.Errorf("body missing %q\n--- body ---\n%s", s, got.body)
		}
	}
}

func TestSMTPMailer_DefaultPort(t *testing.T) {
	var addr string
	m := &SMTPMailer{
		Host: "mail.example.com",
		// Port intentionally zero — defaults to 25
		From: "noreply@example.com",
		sendMail: func(a string, _ smtp.Auth, _ string, _ []string, _ []byte) error {
			addr = a
			return nil
		},
	}
	if err := m.Send(context.Background(), "x@y.com", "s", "b"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if addr != "mail.example.com:25" {
		t.Errorf("default port want :25, got %q", addr)
	}
}

func TestSMTPMailer_RejectsBadRecipient(t *testing.T) {
	m := &SMTPMailer{
		Host: "smtp.example.com",
		Port: 25,
		From: "n@x.com",
		sendMail: func(string, smtp.Auth, string, []string, []byte) error {
			t.Fatalf("sendMail should not run for empty recipient")
			return nil
		},
	}
	if err := m.Send(context.Background(), "", "s", "b"); err == nil {
		t.Fatalf("Send with empty recipient should error")
	}
}

// TestSMTPMailer_Disabled exercises the zero-value path: Host=="" means
// the mailer is intentionally not configured. Send must no-op without
// calling smtp.SendMail so a misconfigured deployment never tries to
// dial a phantom server.
func TestSMTPMailer_Disabled(t *testing.T) {
	m := &SMTPMailer{
		// Host empty → disabled
		From: "n@x.com",
		sendMail: func(string, smtp.Auth, string, []string, []byte) error {
			t.Fatalf("disabled mailer should not call sendMail")
			return nil
		},
	}
	if err := m.Send(context.Background(), "alice@example.com", "s", "b"); err != nil {
		t.Fatalf("disabled Send should be a no-op error-free, got %v", err)
	}
}
