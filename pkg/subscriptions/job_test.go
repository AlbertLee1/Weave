package subscriptions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// dialWelcome opens a connection and consumes the welcome frame.
func dialWelcome(t *testing.T, srv *httptest.Server, ctx context.Context) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	var welcome Message
	if err := wsjson.Read(ctx, c, &welcome); err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if welcome.Type != "welcome" {
		t.Fatalf("expected welcome, got %q", welcome.Type)
	}
	return c
}

// TestHub_SubscribeActionJob_AcceptsRequest verifies that a well-formed
// subscribeActionJob request returns a {type:"subscribed"} response with a
// non-empty subscriptionId.
func TestHub_SubscribeActionJob_AcceptsRequest(t *testing.T) {
	h := NewHub()
	defer h.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleWS(w, r)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWelcome(t, srv, ctx)
	defer c.Close(websocket.StatusNormalClosure, "")

	subData, _ := json.Marshal(ActionJobSubscribeRequest{JobID: "job-1"})
	if err := wsjson.Write(ctx, c, Message{Type: "subscribeActionJob", Data: subData}); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}
	var resp Message
	if err := wsjson.Read(ctx, c, &resp); err != nil {
		t.Fatalf("read subscribe response: %v", err)
	}
	if resp.Type != "subscribed" {
		t.Fatalf("expected subscribed, got %q (err=%q)", resp.Type, resp.Error)
	}
	if resp.SubscriptionID == "" {
		t.Fatalf("expected non-empty subscriptionId")
	}

	// Hub job-index must contain exactly one entry for the job.
	if got := h.JobSubscriptionCount("job-1"); got != 1 {
		t.Fatalf("JobSubscriptionCount(\"job-1\") = %d; want 1", got)
	}
}

// TestHub_SubscribeActionJob_RequiresJobID rejects an empty jobId with a
// typed error message.
func TestHub_SubscribeActionJob_RequiresJobID(t *testing.T) {
	h := NewHub()
	defer h.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleWS(w, r)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWelcome(t, srv, ctx)
	defer c.Close(websocket.StatusNormalClosure, "")

	subData, _ := json.Marshal(ActionJobSubscribeRequest{})
	if err := wsjson.Write(ctx, c, Message{Type: "subscribeActionJob", Data: subData}); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}
	var resp Message
	if err := wsjson.Read(ctx, c, &resp); err != nil {
		t.Fatalf("read subscribe response: %v", err)
	}
	if resp.Type != "error" {
		t.Fatalf("expected error, got %q", resp.Type)
	}
	if !strings.Contains(resp.Error, "jobId") {
		t.Fatalf("expected error mentioning jobId, got %q", resp.Error)
	}
}

// TestHub_HandleActionJobProgress_FansOutToSubscriber verifies that progress
// events emitted via Hub.HandleActionJobProgress arrive on subscribed
// connections as actionJobProgress messages.
func TestHub_HandleActionJobProgress_FansOutToSubscriber(t *testing.T) {
	h := NewHub()
	defer h.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleWS(w, r)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWelcome(t, srv, ctx)
	defer c.Close(websocket.StatusNormalClosure, "")

	subData, _ := json.Marshal(ActionJobSubscribeRequest{JobID: "job-x"})
	if err := wsjson.Write(ctx, c, Message{Type: "subscribeActionJob", Data: subData}); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}
	var sub Message
	if err := wsjson.Read(ctx, c, &sub); err != nil {
		t.Fatalf("read subscribe response: %v", err)
	}
	if sub.Type != "subscribed" {
		t.Fatalf("expected subscribed, got %q (err=%q)", sub.Type, sub.Error)
	}

	// Wait for the indexer to register the entry — the read above only
	// confirms the response was sent; the index update happens before the
	// response is enqueued so by the time the read returns the entry exists,
	// but assert defensively to avoid flakes on heavily loaded CI boxes.
	deadline := time.Now().Add(time.Second)
	for h.JobSubscriptionCount("job-x") < 1 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}

	h.HandleActionJobProgress("job-x", ActionJobProgressEvent{
		JobID:   "job-x",
		Percent: 42,
		Message: "halfway",
	})

	var got Message
	if err := wsjson.Read(ctx, c, &got); err != nil {
		t.Fatalf("read progress: %v", err)
	}
	if got.Type != "actionJobProgress" {
		t.Fatalf("expected actionJobProgress, got %q", got.Type)
	}
	if got.SubscriptionID != sub.SubscriptionID {
		t.Fatalf("subscriptionId mismatch: want %q got %q", sub.SubscriptionID, got.SubscriptionID)
	}
	var evt ActionJobProgressEvent
	if err := json.Unmarshal(got.Data, &evt); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if evt.JobID != "job-x" || evt.Percent != 42 || evt.Message != "halfway" {
		t.Fatalf("unexpected event: %+v", evt)
	}
}

// TestHub_HandleActionJobProgress_NoSubscribers_DropsSilently verifies the
// dispatch path is a no-op when no subscribers are registered.
func TestHub_HandleActionJobProgress_NoSubscribers_DropsSilently(t *testing.T) {
	h := NewHub()
	defer h.Close()
	// Should not panic / leak goroutines / write to a closed channel.
	h.HandleActionJobProgress("nobody", ActionJobProgressEvent{JobID: "nobody", Percent: 50})
	if got := h.JobSubscriptionCount("nobody"); got != 0 {
		t.Fatalf("JobSubscriptionCount unexpectedly = %d", got)
	}
}

// TestHub_HandleActionJobProgress_UnsubscribeDropsRouting verifies that an
// unsubscribe removes the entry from the jobIndex so subsequent progress
// events do not arrive on the closed-side connection.
func TestHub_Unsubscribe_DropsJobRouting(t *testing.T) {
	h := NewHub()
	defer h.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleWS(w, r)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWelcome(t, srv, ctx)
	defer c.Close(websocket.StatusNormalClosure, "")

	subData, _ := json.Marshal(ActionJobSubscribeRequest{JobID: "job-y"})
	_ = wsjson.Write(ctx, c, Message{Type: "subscribeActionJob", Data: subData})
	var sub Message
	_ = wsjson.Read(ctx, c, &sub)
	if h.JobSubscriptionCount("job-y") != 1 {
		t.Fatalf("expected 1 subscriber, got %d", h.JobSubscriptionCount("job-y"))
	}

	unsubData, _ := json.Marshal(map[string]string{"subscriptionId": sub.SubscriptionID})
	_ = wsjson.Write(ctx, c, Message{Type: "unsubscribe", Data: unsubData})
	var unsub Message
	_ = wsjson.Read(ctx, c, &unsub)
	if unsub.Type != "unsubscribed" {
		t.Fatalf("expected unsubscribed, got %q (err=%q)", unsub.Type, unsub.Error)
	}
	if got := h.JobSubscriptionCount("job-y"); got != 0 {
		t.Fatalf("expected 0 subscribers after unsubscribe, got %d", got)
	}
}
