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

func TestSlackDriver_PostsMrkdwnPayload(t *testing.T) {
	var gotBody []byte
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	d := &SlackDriver{}
	err := d.Send(context.Background(), Envelope{
		Channel:   ChannelSlack,
		UserID:    "user:alice",
		Recipient: srv.URL,
		Title:     "Employee updated",
		Body:      "Dave updated Employee 42",
		Link:      "https://weave/watches?rid=ri.phonograph2-objects.main.object.42",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q want application/json", gotContentType)
	}

	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("payload not JSON: %v\n--- body ---\n%s", err, string(gotBody))
	}
	for _, s := range []string{
		"*Employee updated*",
		"Dave updated Employee 42",
		"<https://weave/watches?rid=ri.phonograph2-objects.main.object.42|View>",
	} {
		if !strings.Contains(payload.Text, s) {
			t.Errorf("text missing %q\n--- text ---\n%s", s, payload.Text)
		}
	}
}

func TestSlackDriver_NoRecipientIsSoftSkip(t *testing.T) {
	d := &SlackDriver{
		HTTPClient: &http.Client{Transport: roundTripperFn(func(*http.Request) (*http.Response, error) {
			t.Fatalf("HTTPClient should not be invoked when no recipient")
			return nil, nil
		})},
	}
	if err := d.Send(context.Background(), Envelope{}); err != nil {
		t.Fatalf("Send with empty recipient should soft-skip, got %v", err)
	}
}

func TestSlackDriver_DefaultURLFallback(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	d := &SlackDriver{DefaultURL: srv.URL}
	if err := d.Send(context.Background(), Envelope{Title: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if hits != 1 {
		t.Errorf("DefaultURL fallback should fire once, hits=%d", hits)
	}
}

func TestSlackDriver_NonOKErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("slack down"))
	}))
	defer srv.Close()
	d := &SlackDriver{}
	err := d.Send(context.Background(), Envelope{Recipient: srv.URL, Title: "x"})
	if err == nil {
		t.Fatalf("expected error on non-2xx response")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error should mention status, got %v", err)
	}
}

func TestSlackDriver_Channel(t *testing.T) {
	d := &SlackDriver{}
	if got := d.Channel(); got != ChannelSlack {
		t.Errorf("Channel = %q want %q", got, ChannelSlack)
	}
}

type roundTripperFn func(*http.Request) (*http.Response, error)

func (f roundTripperFn) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
