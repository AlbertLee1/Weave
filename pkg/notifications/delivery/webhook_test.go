package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebhookDriver_PostsJSONEnvelope(t *testing.T) {
	var got struct {
		body        []byte
		contentType string
		auth        string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.contentType = r.Header.Get("Content-Type")
		got.auth = r.Header.Get("Authorization")
		got.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	d := &WebhookDriver{
		Headers: map[string]string{"Authorization": "Bearer test-token"},
	}
	err := d.Send(context.Background(), Envelope{
		Channel:   ChannelWebhook,
		UserID:    "user:alice",
		Recipient: srv.URL,
		Title:     "Employee updated",
		Body:      "Dave updated Employee 42",
		Link:      "/watches?rid=42",
		Properties: map[string]interface{}{
			"objectType": "Employee",
			"primaryKey": "42",
		},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got.contentType != "application/json" {
		t.Errorf("Content-Type = %q want application/json", got.contentType)
	}
	if got.auth != "Bearer test-token" {
		t.Errorf("Authorization header lost: %q", got.auth)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(got.body, &payload); err != nil {
		t.Fatalf("payload not JSON: %v\n--- body ---\n%s", err, string(got.body))
	}
	if payload["eventType"] != "notification" {
		t.Errorf("eventType = %v want 'notification'", payload["eventType"])
	}
	if payload["channel"] != ChannelWebhook {
		t.Errorf("channel = %v want %q", payload["channel"], ChannelWebhook)
	}
	if payload["userId"] != "user:alice" {
		t.Errorf("userId = %v", payload["userId"])
	}
	if payload["title"] != "Employee updated" {
		t.Errorf("title = %v", payload["title"])
	}
	props, ok := payload["properties"].(map[string]interface{})
	if !ok || props["objectType"] != "Employee" {
		t.Errorf("properties not round-tripped: %v", payload["properties"])
	}
}

func TestWebhookDriver_NoRecipientIsSoftSkip(t *testing.T) {
	d := &WebhookDriver{
		HTTPClient: &http.Client{Transport: roundTripperFn(func(*http.Request) (*http.Response, error) {
			t.Fatalf("HTTPClient should not be invoked when no recipient")
			return nil, nil
		})},
	}
	if err := d.Send(context.Background(), Envelope{}); err != nil {
		t.Fatalf("Send with empty recipient should soft-skip, got %v", err)
	}
}

func TestWebhookDriver_NonOKErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("oops"))
	}))
	defer srv.Close()
	d := &WebhookDriver{}
	err := d.Send(context.Background(), Envelope{Recipient: srv.URL, Title: "x"})
	if err == nil {
		t.Fatalf("expected error on 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status: %v", err)
	}
}

func TestWebhookDriver_Channel(t *testing.T) {
	d := &WebhookDriver{}
	if got := d.Channel(); got != ChannelWebhook {
		t.Errorf("Channel = %q want %q", got, ChannelWebhook)
	}
}

func TestRegistry_Channels(t *testing.T) {
	r := NewRegistry()
	r.Register(&WebhookDriver{})
	r.Register(&SMTPDriver{Host: "x"})
	r.Register(&SlackDriver{})

	channels := r.Channels()
	// Channels() returns channels in canonical order: email, slack, webhook
	want := []string{ChannelEmail, ChannelSlack, ChannelWebhook}
	if len(channels) != 3 {
		t.Fatalf("want 3 channels, got %d (%v)", len(channels), channels)
	}
	for i, w := range want {
		if channels[i] != w {
			t.Errorf("channels[%d] = %q want %q", i, channels[i], w)
		}
	}

	if r.Get(ChannelEmail) == nil {
		t.Errorf("Get(email) should return registered driver")
	}
	if r.Get("does-not-exist") != nil {
		t.Errorf("Get(unknown) should return nil")
	}
}
