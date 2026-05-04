package delivery

import (
	"context"
	"net/smtp"
	"strings"
	"testing"
)

func TestSMTPDriver_FormatsMessage(t *testing.T) {
	var got struct {
		addr string
		from string
		to   []string
		body string
	}
	d := &SMTPDriver{
		Host: "smtp.example.com",
		Port: 587,
		From: "weave-notify@example.com",
		Auth: smtp.PlainAuth("", "weave-notify@example.com", "secret", "smtp.example.com"),
		SendMail: func(addr string, _ smtp.Auth, from string, to []string, msg []byte) error {
			got.addr = addr
			got.from = from
			got.to = append([]string(nil), to...)
			got.body = string(msg)
			return nil
		},
	}

	err := d.Send(context.Background(), Envelope{
		Channel:   ChannelEmail,
		UserID:    "user:alice",
		Recipient: "alice@example.com",
		Title:     "Employee updated",
		Body:      "Dave updated Employee 42",
		Link:      "/watches?rid=ri.phonograph2-objects.main.object.42",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got.addr != "smtp.example.com:587" {
		t.Errorf("addr = %q", got.addr)
	}
	if got.from != "weave-notify@example.com" {
		t.Errorf("from = %q", got.from)
	}
	if len(got.to) != 1 || got.to[0] != "alice@example.com" {
		t.Errorf("to = %v", got.to)
	}

	mustContain := []string{
		"From: weave-notify@example.com",
		"To: alice@example.com",
		"Subject: Employee updated",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"\r\n\r\nDave updated Employee 42",
		"/watches?rid=ri.phonograph2-objects.main.object.42",
	}
	for _, s := range mustContain {
		if !strings.Contains(got.body, s) {
			t.Errorf("body missing %q\n--- body ---\n%s", s, got.body)
		}
	}
}

func TestSMTPDriver_DefaultPort(t *testing.T) {
	var addr string
	d := &SMTPDriver{
		Host: "mail.example.com",
		From: "n@x.com",
		SendMail: func(a string, _ smtp.Auth, _ string, _ []string, _ []byte) error {
			addr = a
			return nil
		},
	}
	if err := d.Send(context.Background(), Envelope{Recipient: "x@y.com", Title: "s"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if addr != "mail.example.com:25" {
		t.Errorf("default port want :25, got %q", addr)
	}
}

func TestSMTPDriver_EmptyRecipientIsSoftSkip(t *testing.T) {
	d := &SMTPDriver{
		Host: "smtp.example.com",
		From: "n@x.com",
		SendMail: func(string, smtp.Auth, string, []string, []byte) error {
			t.Fatalf("SendMail should not run for empty recipient")
			return nil
		},
	}
	if err := d.Send(context.Background(), Envelope{Recipient: ""}); err != nil {
		t.Fatalf("Send with empty recipient should soft-skip, got %v", err)
	}
}

func TestSMTPDriver_DisabledHost(t *testing.T) {
	d := &SMTPDriver{
		// Host empty → disabled
		From: "n@x.com",
		SendMail: func(string, smtp.Auth, string, []string, []byte) error {
			t.Fatalf("disabled mailer should not call SendMail")
			return nil
		},
	}
	if err := d.Send(context.Background(), Envelope{Recipient: "alice@example.com"}); err != nil {
		t.Fatalf("disabled Send should be a no-op, got %v", err)
	}
}

func TestSMTPDriver_Channel(t *testing.T) {
	d := &SMTPDriver{}
	if got := d.Channel(); got != ChannelEmail {
		t.Errorf("Channel = %q want %q", got, ChannelEmail)
	}
}
