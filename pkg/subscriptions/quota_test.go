package subscriptions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// ---------- US-308 per-user subscription quota ----------

// dialAuthenticatedAndWelcome dials the given handler URL with ?token=<userID>
// and reads the welcome envelope. The Handler at the other end is expected to
// echo the token back as the userID via TokenValidator.
func dialAuthenticatedAndWelcome(t *testing.T, ctx context.Context, srvURL, token string) (*websocket.Conn, string) {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(srvURL, "http") + "?token=" + token
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
	return c, welcome.ConnectionID
}

// echoTokenValidator returns the token verbatim as the userID; empty token
// rejects so anonymous-vs-authenticated paths stay distinct.
func echoTokenValidator(token string) (string, error) {
	if token == "" {
		return "", ErrInvalidToken
	}
	return token, nil
}

func TestUserQuota_BlocksAcrossConnections(t *testing.T) {
	h := NewHubWithConfig(HubConfig{MaxSubscriptionsPerUser: 3})
	defer h.Close()

	srv := httptest.NewServer(NewHandler(h, echoTokenValidator))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Two connections from the same user.
	c1, _ := dialAuthenticatedAndWelcome(t, ctx, srv.URL, "alice")
	defer c1.Close(websocket.StatusNormalClosure, "")
	c2, _ := dialAuthenticatedAndWelcome(t, ctx, srv.URL, "alice")
	defer c2.Close(websocket.StatusNormalClosure, "")

	subscribe := func(c *websocket.Conn, ot string) Message {
		t.Helper()
		if err := wsjson.Write(ctx, c, Message{
			Type: "subscribe",
			Data: json.RawMessage(`{"objectType":"` + ot + `"}`),
		}); err != nil {
			t.Fatalf("write subscribe: %v", err)
		}
		var resp Message
		if err := wsjson.Read(ctx, c, &resp); err != nil {
			t.Fatalf("read subscribe response: %v", err)
		}
		return resp
	}

	// 2 subscriptions on c1 + 1 on c2 = 3, hits the per-user cap.
	if r := subscribe(c1, "TypeA"); r.Type != "subscribed" {
		t.Fatalf("c1 sub 1: expected subscribed, got %q (%s)", r.Type, r.Error)
	}
	if r := subscribe(c1, "TypeB"); r.Type != "subscribed" {
		t.Fatalf("c1 sub 2: expected subscribed, got %q (%s)", r.Type, r.Error)
	}
	if r := subscribe(c2, "TypeC"); r.Type != "subscribed" {
		t.Fatalf("c2 sub 1: expected subscribed, got %q (%s)", r.Type, r.Error)
	}

	// User counter should now be 3.
	if got := h.UserSubscriptionCount("alice"); got != 3 {
		t.Errorf("expected user counter=3, got %d", got)
	}

	// 4th subscription on either connection must be rejected by the user cap
	// (the connection cap is still 10, so this is the per-user surface firing).
	r := subscribe(c2, "TypeD")
	if r.Type != "error" {
		t.Fatalf("expected error for over-cap subscribe, got %q", r.Type)
	}
	if !strings.Contains(strings.ToLower(r.Error), "user") {
		t.Errorf("expected error to mention 'user', got %q", r.Error)
	}
}

func TestUserQuota_DifferentUsersIndependent(t *testing.T) {
	h := NewHubWithConfig(HubConfig{MaxSubscriptionsPerUser: 2})
	defer h.Close()

	srv := httptest.NewServer(NewHandler(h, echoTokenValidator))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cAlice, _ := dialAuthenticatedAndWelcome(t, ctx, srv.URL, "alice")
	defer cAlice.Close(websocket.StatusNormalClosure, "")
	cBob, _ := dialAuthenticatedAndWelcome(t, ctx, srv.URL, "bob")
	defer cBob.Close(websocket.StatusNormalClosure, "")

	subscribe := func(c *websocket.Conn, ot string) Message {
		t.Helper()
		_ = wsjson.Write(ctx, c, Message{Type: "subscribe", Data: json.RawMessage(`{"objectType":"` + ot + `"}`)})
		var resp Message
		_ = wsjson.Read(ctx, c, &resp)
		return resp
	}

	// Alice fills her allowance.
	if r := subscribe(cAlice, "TypeA"); r.Type != "subscribed" {
		t.Fatalf("alice sub 1: %q (%s)", r.Type, r.Error)
	}
	if r := subscribe(cAlice, "TypeB"); r.Type != "subscribed" {
		t.Fatalf("alice sub 2: %q (%s)", r.Type, r.Error)
	}
	if r := subscribe(cAlice, "TypeC"); r.Type != "error" {
		t.Fatalf("alice sub 3: expected error, got %q", r.Type)
	}

	// Bob should still be able to subscribe — quotas are per-user, not global.
	if r := subscribe(cBob, "TypeX"); r.Type != "subscribed" {
		t.Fatalf("bob sub 1: %q (%s)", r.Type, r.Error)
	}
}

func TestUserQuota_ReleasedOnUnsubscribe(t *testing.T) {
	h := NewHubWithConfig(HubConfig{MaxSubscriptionsPerUser: 1})
	defer h.Close()

	srv := httptest.NewServer(NewHandler(h, echoTokenValidator))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, _ := dialAuthenticatedAndWelcome(t, ctx, srv.URL, "alice")
	defer c.Close(websocket.StatusNormalClosure, "")

	if err := wsjson.Write(ctx, c, Message{Type: "subscribe", Data: json.RawMessage(`{"objectType":"TypeA"}`)}); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}
	var resp Message
	if err := wsjson.Read(ctx, c, &resp); err != nil {
		t.Fatalf("read subscribe: %v", err)
	}
	if resp.Type != "subscribed" {
		t.Fatalf("expected subscribed, got %q (%s)", resp.Type, resp.Error)
	}
	subID := resp.SubscriptionID

	if got := h.UserSubscriptionCount("alice"); got != 1 {
		t.Errorf("after first subscribe: expected counter=1, got %d", got)
	}

	if err := wsjson.Write(ctx, c, Message{Type: "unsubscribe", Data: json.RawMessage(`{"subscriptionId":"` + subID + `"}`)}); err != nil {
		t.Fatalf("write unsubscribe: %v", err)
	}
	if err := wsjson.Read(ctx, c, &resp); err != nil {
		t.Fatalf("read unsubscribe: %v", err)
	}
	if resp.Type != "unsubscribed" {
		t.Fatalf("expected unsubscribed, got %q (%s)", resp.Type, resp.Error)
	}

	if got := h.UserSubscriptionCount("alice"); got != 0 {
		t.Errorf("after unsubscribe: expected counter=0, got %d", got)
	}

	// Slot should be reusable now.
	if err := wsjson.Write(ctx, c, Message{Type: "subscribe", Data: json.RawMessage(`{"objectType":"TypeB"}`)}); err != nil {
		t.Fatalf("write second subscribe: %v", err)
	}
	if err := wsjson.Read(ctx, c, &resp); err != nil {
		t.Fatalf("read second subscribe: %v", err)
	}
	if resp.Type != "subscribed" {
		t.Errorf("after release: expected subscribed, got %q (%s)", resp.Type, resp.Error)
	}
}

func TestUserQuota_ReleasedOnDisconnect(t *testing.T) {
	h := NewHubWithConfig(HubConfig{MaxSubscriptionsPerUser: 2})
	defer h.Close()

	srv := httptest.NewServer(NewHandler(h, echoTokenValidator))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, _ := dialAuthenticatedAndWelcome(t, ctx, srv.URL, "alice")

	for _, ot := range []string{"TypeA", "TypeB"} {
		if err := wsjson.Write(ctx, c, Message{Type: "subscribe", Data: json.RawMessage(`{"objectType":"` + ot + `"}`)}); err != nil {
			t.Fatalf("write %s: %v", ot, err)
		}
		var resp Message
		if err := wsjson.Read(ctx, c, &resp); err != nil {
			t.Fatalf("read %s: %v", ot, err)
		}
		if resp.Type != "subscribed" {
			t.Fatalf("%s: %q", ot, resp.Type)
		}
	}
	if got := h.UserSubscriptionCount("alice"); got != 2 {
		t.Errorf("after fill: expected 2, got %d", got)
	}

	c.Close(websocket.StatusNormalClosure, "")

	// Wait for the unregister path to drain the user counter.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.UserSubscriptionCount("alice") == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := h.UserSubscriptionCount("alice"); got != 0 {
		t.Errorf("after disconnect: expected counter=0, got %d", got)
	}
}

func TestUserQuota_AnonymousBypassesCap(t *testing.T) {
	// HandleWS without a userID (anonymous / dev-mode) must NOT consume the
	// per-user counter. This protects the dev-mode default and any caller
	// using HandleWS directly (e.g. internal tooling, contract tests).
	h := NewHubWithConfig(HubConfig{MaxSubscriptionsPerUser: 1})
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	for i := 0; i < 5; i++ {
		if err := wsjson.Write(ctx, c, Message{
			Type: "subscribe",
			Data: json.RawMessage(`{"objectType":"Type` + string(rune('A'+i)) + `"}`),
		}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		var resp Message
		if err := wsjson.Read(ctx, c, &resp); err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if resp.Type != "subscribed" {
			t.Fatalf("anonymous sub %d: expected subscribed, got %q (%s)", i, resp.Type, resp.Error)
		}
	}
}

func TestUserQuota_DefaultMaxSubscriptionsPerUser(t *testing.T) {
	cfg := HubConfig{}
	cfg.applyDefaults()
	if cfg.MaxSubscriptionsPerUser != 50 {
		t.Errorf("expected default MaxSubscriptionsPerUser=50, got %d", cfg.MaxSubscriptionsPerUser)
	}
}

// ---------- US-308 per-connection event rate limit ----------

func TestConnection_AllowEvent_DisabledLimiter(t *testing.T) {
	c := &Connection{} // zero-value rate limit = pass-through
	for i := 0; i < 1000; i++ {
		if !c.allowEvent() {
			t.Fatalf("disabled limiter rejected event %d", i)
		}
	}
}

func TestConnection_AllowEvent_LimitsAndEvicts(t *testing.T) {
	now := time.Unix(1000, 0)
	c := &Connection{
		rateLimit:  3,
		rateWindow: time.Second,
		nowFunc:    func() time.Time { return now },
	}

	// First 3 events fit within the window.
	for i := 0; i < 3; i++ {
		if !c.allowEvent() {
			t.Fatalf("event %d should be allowed", i)
		}
	}
	// 4th hits the cap.
	if c.allowEvent() {
		t.Errorf("4th event should be rate-limited")
	}

	// Advance past the window so the bucket evicts.
	now = now.Add(2 * time.Second)
	if !c.allowEvent() {
		t.Errorf("after window, expected admit")
	}
	// And we should still have headroom for two more.
	for i := 0; i < 2; i++ {
		if !c.allowEvent() {
			t.Errorf("post-eviction event %d should be allowed", i)
		}
	}
	if c.allowEvent() {
		t.Errorf("post-eviction 4th event should be rate-limited")
	}
}

func TestConnection_AllowEvent_ConcurrentSafe(t *testing.T) {
	c := &Connection{
		rateLimit:  100,
		rateWindow: time.Hour,
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = c.allowEvent()
			}
		}()
	}
	wg.Wait()
	// No assertion on counts — this verifies the mutex protects the slice.
}

func TestRateLimit_DropsExcessEventsAndMarksOverflow(t *testing.T) {
	// With EventRateLimit=2 in a 1h window, only the first 2 dispatched events
	// reach the connection's send buffer; the 3rd should be rate-limited and
	// surface via markOverflow → onOutOfDate.
	h := NewHubWithConfig(HubConfig{
		EventRateLimit:  2,
		EventRateWindow: time.Hour,
	})
	defer h.Close()

	conn := &Connection{
		id:            "conn-1",
		send:          make(chan Message, 64),
		done:          make(chan struct{}),
		subscriptions: make(map[string]*Subscription),
		overflowSubs:  make(map[string]bool),
		hub:           h,
		rateLimit:     h.config.EventRateLimit,
		rateWindow:    h.config.EventRateWindow,
	}
	sub := NewSubscription(SubscribeRequest{ObjectType: "Employee"})
	conn.subscriptions[sub.ID] = sub

	h.mu.Lock()
	h.conns[conn.id] = conn
	h.addToIndexLocked(conn, sub)
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.removeConnectionFromIndexLocked(conn)
		delete(h.conns, conn.id)
		h.mu.Unlock()
	}()

	// Fire 5 changes — only 2 should make it to the send channel.
	for i := 0; i < 5; i++ {
		h.HandleObjectChange("Employee", "pk", "MODIFY", map[string]interface{}{"i": i})
	}

	delivered := 0
	for {
		select {
		case msg := <-conn.send:
			if msg.Type == "objectChanged" {
				delivered++
			}
		default:
			goto done
		}
	}
done:
	if delivered != 2 {
		t.Errorf("expected 2 delivered events, got %d", delivered)
	}

	conn.overflowMu.Lock()
	overflowed := conn.overflowSubs[sub.ID]
	conn.overflowMu.Unlock()
	if !overflowed {
		t.Errorf("expected subscription to be marked overflowed after rate limit")
	}
}

func TestRateLimit_Disabled(t *testing.T) {
	// EventRateLimit=-1 (negative) treated as "disabled" via allowEvent's
	// limit<=0 short-circuit; tests that a cfg with explicit 0 falls back to
	// the default and a explicit negative disables.
	h := NewHubWithConfig(HubConfig{
		EventRateLimit:  -1,
		EventRateWindow: time.Hour,
	})
	defer h.Close()

	conn := &Connection{
		id:            "conn-1",
		send:          make(chan Message, 256),
		done:          make(chan struct{}),
		subscriptions: make(map[string]*Subscription),
		overflowSubs:  make(map[string]bool),
		hub:           h,
		rateLimit:     h.config.EventRateLimit,
		rateWindow:    h.config.EventRateWindow,
	}
	sub := NewSubscription(SubscribeRequest{ObjectType: "Employee"})
	conn.subscriptions[sub.ID] = sub

	h.mu.Lock()
	h.conns[conn.id] = conn
	h.addToIndexLocked(conn, sub)
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.removeConnectionFromIndexLocked(conn)
		delete(h.conns, conn.id)
		h.mu.Unlock()
	}()

	for i := 0; i < 50; i++ {
		h.HandleObjectChange("Employee", "pk", "MODIFY", map[string]interface{}{"i": i})
	}

	delivered := 0
	for {
		select {
		case msg := <-conn.send:
			if msg.Type == "objectChanged" {
				delivered++
			}
		default:
			goto done
		}
	}
done:
	if delivered != 50 {
		t.Errorf("expected all 50 events delivered when limiter disabled, got %d", delivered)
	}
}

func TestRateLimit_DefaultEventRateLimit(t *testing.T) {
	cfg := HubConfig{}
	cfg.applyDefaults()
	if cfg.EventRateLimit != 100 {
		t.Errorf("expected default EventRateLimit=100, got %d", cfg.EventRateLimit)
	}
	if cfg.EventRateWindow != time.Second {
		t.Errorf("expected default EventRateWindow=1s, got %v", cfg.EventRateWindow)
	}
}
