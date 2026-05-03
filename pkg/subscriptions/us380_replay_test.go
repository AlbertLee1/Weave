package subscriptions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// US-380: WebSocket subscription cursor + replay buffer
//
// These tests cover the four PRD acceptance gates:
//   1. WebSocket envelope carries `cursor` on every event message and
//      `lastEventId` on the welcome.
//   2. ?since=<cursor> on reconnect replays events captured in the live
//      window.
//   3. ?since=<cursor> outside the window emits a connection-level
//      `onOutOfDate` instead of silently missing state.
//   4. The 30-second-disconnect simulation: events fire while the client is
//      offline; reconnect with the prior cursor recovers them all.

// ---------- EventLog unit tests ----------

func TestEventLog_AppendAssignsMonotonicCursors(t *testing.T) {
	l := NewEventLog(EventLogConfig{})
	a := l.Append(EventLogEntry{Kind: "objectChange", ObjectType: "Employee"})
	b := l.Append(EventLogEntry{Kind: "objectChange", ObjectType: "Employee"})
	c := l.Append(EventLogEntry{Kind: "actionJobProgress", JobID: "job-1"})
	if a != 1 || b != 2 || c != 3 {
		t.Fatalf("expected cursors 1,2,3 — got %d,%d,%d", a, b, c)
	}
	if l.LatestID() != 3 {
		t.Errorf("expected latest=3, got %d", l.LatestID())
	}
}

func TestEventLog_SnapshotSinceReturnsTail(t *testing.T) {
	l := NewEventLog(EventLogConfig{})
	for i := 0; i < 5; i++ {
		l.Append(EventLogEntry{Kind: "objectChange", ObjectType: "Employee"})
	}
	got, _ := l.Snapshot(2)
	if len(got) != 3 {
		t.Fatalf("expected 3 entries with id>2, got %d", len(got))
	}
	if got[0].ID != 3 || got[2].ID != 5 {
		t.Errorf("unexpected ids: %d..%d", got[0].ID, got[2].ID)
	}
}

func TestEventLog_EvictsByWindow(t *testing.T) {
	now := time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)
	l := NewEventLog(EventLogConfig{Window: time.Minute, MaxEntries: 100})
	l.nowFunc = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		l.Append(EventLogEntry{Kind: "objectChange", ObjectType: "X"})
	}
	// Advance past the window; on next Append the prior 3 entries should
	// have been evicted by the eviction sweep.
	l.nowFunc = func() time.Time { return now.Add(2 * time.Minute) }
	l.Append(EventLogEntry{Kind: "objectChange", ObjectType: "X"})
	if got := l.Len(); got != 1 {
		t.Errorf("expected 1 live entry after window expiry, got %d", got)
	}
	if got := l.EarliestID(); got != 4 {
		t.Errorf("expected earliest live id=4, got %d", got)
	}
}

func TestEventLog_EvictsByMaxEntries(t *testing.T) {
	l := NewEventLog(EventLogConfig{Window: time.Hour, MaxEntries: 3})
	for i := 0; i < 5; i++ {
		l.Append(EventLogEntry{Kind: "objectChange", ObjectType: "X"})
	}
	if got := l.Len(); got != 3 {
		t.Errorf("expected 3 entries (MaxEntries cap), got %d", got)
	}
	if got := l.EarliestID(); got != 3 {
		t.Errorf("expected earliest id=3 (1,2 evicted), got %d", got)
	}
}

func TestParseSinceCursor(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"0", 0},
		{"42", 42},
		{"-1", 0},
		{"abc", 0},
		{"9999999999", 9999999999},
	}
	for _, c := range cases {
		if got := parseSinceCursor(c.in); got != c.want {
			t.Errorf("parseSinceCursor(%q) = %d; want %d", c.in, got, c.want)
		}
	}
}

// ---------- Welcome carries cursor / event messages carry cursor ----------

func TestHub_WelcomeIncludesLastEventID(t *testing.T) {
	h := NewHub()
	defer h.Close()

	// Pre-populate the log with one event so the welcome's high-water
	// mark is non-zero.
	h.HandleObjectChange("Employee", "emp-0", "CREATE", map[string]interface{}{"name": "Eve"})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleWS(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	var welcome Message
	if err := wsjson.Read(ctx, c, &welcome); err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if welcome.Type != "welcome" {
		t.Fatalf("expected welcome, got %q", welcome.Type)
	}
	if welcome.LastEventID != 1 {
		t.Errorf("expected lastEventId=1, got %d", welcome.LastEventID)
	}
}

func TestHub_ObjectChangedCarriesCursor(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleWS(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	var welcome Message
	wsjson.Read(ctx, c, &welcome)

	// Subscribe.
	subReq := Message{Type: "subscribe", Data: json.RawMessage(`{"objectType":"Employee"}`)}
	if err := wsjson.Write(ctx, c, subReq); err != nil {
		t.Fatalf("write sub: %v", err)
	}
	var subResp Message
	wsjson.Read(ctx, c, &subResp)

	// Drive an event and read the dispatched objectChanged.
	h.HandleObjectChange("Employee", "emp-1", "CREATE", map[string]interface{}{"name": "John"})

	var evt Message
	if err := wsjson.Read(ctx, c, &evt); err != nil {
		t.Fatalf("read event: %v", err)
	}
	if evt.Type != "objectChanged" {
		t.Fatalf("expected objectChanged, got %q", evt.Type)
	}
	if evt.Cursor == 0 {
		t.Errorf("expected non-zero cursor on objectChanged, got 0")
	}
}

// ---------- ?since=<cursor> reconnect path ----------

func TestHub_ReconnectReplaysMissedEvents(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleWS(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Phase 1: dial, subscribe, observe one event, capture its cursor.
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	c1, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial1: %v", err)
	}
	var welcome1 Message
	wsjson.Read(ctx, c1, &welcome1)

	subReq := Message{Type: "subscribe", Data: json.RawMessage(`{"objectType":"Employee"}`)}
	wsjson.Write(ctx, c1, subReq)
	var subResp1 Message
	wsjson.Read(ctx, c1, &subResp1)

	h.HandleObjectChange("Employee", "emp-1", "CREATE", map[string]interface{}{"name": "A"})
	var evt1 Message
	wsjson.Read(ctx, c1, &evt1)
	cursorAtDisconnect := evt1.Cursor
	if cursorAtDisconnect == 0 {
		t.Fatalf("expected non-zero cursor on first event")
	}

	// Disconnect.
	c1.Close(websocket.StatusNormalClosure, "simulated drop")

	// Wait for unregister.
	time.Sleep(50 * time.Millisecond)

	// Phase 2: events fire while client is offline.
	h.HandleObjectChange("Employee", "emp-2", "CREATE", map[string]interface{}{"name": "B"})
	h.HandleObjectChange("Employee", "emp-3", "CREATE", map[string]interface{}{"name": "C"})

	// Phase 3: reconnect with ?since=<cursor>, re-subscribe, expect the
	// two missed events replayed in order.
	c2, _, err := websocket.Dial(ctx, wsURL+"?since="+itoa(cursorAtDisconnect), nil)
	if err != nil {
		t.Fatalf("dial2: %v", err)
	}
	defer c2.Close(websocket.StatusNormalClosure, "")

	var welcome2 Message
	wsjson.Read(ctx, c2, &welcome2)
	if welcome2.Type != "welcome" {
		t.Fatalf("expected welcome on reconnect, got %q", welcome2.Type)
	}

	wsjson.Write(ctx, c2, subReq)

	// Expect: subscribed, then two objectChanged replays for emp-2, emp-3.
	var subResp2 Message
	wsjson.Read(ctx, c2, &subResp2)
	if subResp2.Type != "subscribed" {
		t.Fatalf("expected subscribed, got %q", subResp2.Type)
	}

	gotPKs := []string{}
	cursors := []int64{}
	for i := 0; i < 2; i++ {
		readCtx, readCancel := context.WithTimeout(ctx, 3*time.Second)
		var evt Message
		err := wsjson.Read(readCtx, c2, &evt)
		readCancel()
		if err != nil {
			t.Fatalf("read replay %d: %v", i, err)
		}
		if evt.Type != "objectChanged" {
			t.Fatalf("expected objectChanged, got %q", evt.Type)
		}
		var change ObjectChangeEvent
		json.Unmarshal(evt.Data, &change)
		gotPKs = append(gotPKs, change.Object["name"].(string))
		cursors = append(cursors, evt.Cursor)
	}
	if gotPKs[0] != "B" || gotPKs[1] != "C" {
		t.Errorf("expected replay order [B,C], got %v", gotPKs)
	}
	if cursors[0] >= cursors[1] {
		t.Errorf("expected ascending cursors, got %v", cursors)
	}
	if cursors[0] <= cursorAtDisconnect {
		t.Errorf("expected first replay cursor > %d, got %d", cursorAtDisconnect, cursors[0])
	}
}

// 30-second-disconnect PRD gate — uses an injected time function so we
// don't actually sleep in the test path.
func TestHub_ReconnectAfter30SecondsRecoversAllEvents(t *testing.T) {
	h := NewHubWithConfig(HubConfig{
		EventLog: EventLogConfig{Window: 5 * time.Minute, MaxEntries: 1000},
	})
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleWS(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	c1, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	var welcome1 Message
	wsjson.Read(ctx, c1, &welcome1)

	wsjson.Write(ctx, c1, Message{Type: "subscribe", Data: json.RawMessage(`{"objectType":"Order"}`)})
	var subResp1 Message
	wsjson.Read(ctx, c1, &subResp1)

	// First event before disconnect.
	h.HandleObjectChange("Order", "ord-1", "CREATE", map[string]interface{}{"id": "ord-1"})
	var evt1 Message
	wsjson.Read(ctx, c1, &evt1)
	cursorAtDrop := evt1.Cursor

	// Simulate 30s outage by overriding the log clock and disconnecting.
	c1.Close(websocket.StatusNormalClosure, "")
	time.Sleep(50 * time.Millisecond)

	now0 := time.Now()
	h.eventLog.nowFunc = func() time.Time { return now0.Add(30 * time.Second) }

	// 50 events fire during the outage.
	for i := 2; i <= 51; i++ {
		h.HandleObjectChange("Order", fmt.Sprintf("ord-%d", i), "CREATE",
			map[string]interface{}{"id": fmt.Sprintf("ord-%d", i)})
	}

	// Confirm all are in the live window (well under 5 minutes).
	if got := h.eventLog.Len(); got != 51 {
		t.Errorf("expected 51 live entries (no eviction inside window), got %d", got)
	}

	// Reconnect — same window override stays in effect for the welcome
	// handshake's earliest/latest checks.
	c2, _, err := websocket.Dial(ctx, wsURL+"?since="+itoa(cursorAtDrop), nil)
	if err != nil {
		t.Fatalf("dial2: %v", err)
	}
	defer c2.Close(websocket.StatusNormalClosure, "")

	var welcome2 Message
	wsjson.Read(ctx, c2, &welcome2)
	if welcome2.Type != "welcome" {
		t.Fatalf("expected welcome, got %q", welcome2.Type)
	}

	// Re-subscribe to the same objectType — server replays the 50 missed
	// events for this subscription.
	wsjson.Write(ctx, c2, Message{Type: "subscribe", Data: json.RawMessage(`{"objectType":"Order"}`)})

	var subResp2 Message
	wsjson.Read(ctx, c2, &subResp2)
	if subResp2.Type != "subscribed" {
		t.Fatalf("expected subscribed, got %q", subResp2.Type)
	}

	seen := 0
	for {
		readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
		var evt Message
		err := wsjson.Read(readCtx, c2, &evt)
		readCancel()
		if err != nil {
			break
		}
		if evt.Type != "objectChanged" {
			continue
		}
		seen++
		if seen >= 50 {
			break
		}
	}
	if seen != 50 {
		t.Errorf("expected 50 replayed events after 30s outage, got %d", seen)
	}
}

// Out-of-window cursor → connection-level onOutOfDate signal.
func TestHub_ReconnectOutOfWindowSendsOnOutOfDate(t *testing.T) {
	h := NewHubWithConfig(HubConfig{
		EventLog: EventLogConfig{Window: time.Minute, MaxEntries: 5},
	})
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleWS(w, r)
	}))
	defer srv.Close()

	// Drive enough events to evict cursor=1 from the log (MaxEntries=5).
	for i := 0; i < 10; i++ {
		h.HandleObjectChange("Employee", fmt.Sprintf("emp-%d", i), "CREATE",
			map[string]interface{}{"id": i})
	}
	if got := h.eventLog.EarliestID(); got <= 1 {
		t.Fatalf("expected eviction past cursor 1, got earliest=%d", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	c, _, err := websocket.Dial(ctx, wsURL+"?since=1", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	var welcome Message
	if err := wsjson.Read(ctx, c, &welcome); err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if welcome.Type != "welcome" {
		t.Fatalf("expected welcome, got %q", welcome.Type)
	}

	var nextMsg Message
	if err := wsjson.Read(ctx, c, &nextMsg); err != nil {
		t.Fatalf("read second message: %v", err)
	}
	if nextMsg.Type != "onOutOfDate" {
		t.Fatalf("expected onOutOfDate after out-of-window cursor, got %q", nextMsg.Type)
	}
	if nextMsg.SubscriptionID != "" {
		t.Errorf("expected empty SubscriptionID on connection-level onOutOfDate, got %q", nextMsg.SubscriptionID)
	}
	if nextMsg.LastEventID == 0 {
		t.Errorf("expected non-zero LastEventID on onOutOfDate")
	}
}

// Replay must not fire when no cursor is supplied — backward-compat with
// pre-US-380 clients that connect fresh and re-subscribe without ?since=.
func TestHub_NoSinceParamSkipsReplay(t *testing.T) {
	h := NewHub()
	defer h.Close()

	// Pre-populate the log.
	h.HandleObjectChange("Employee", "emp-1", "CREATE", map[string]interface{}{"name": "A"})
	h.HandleObjectChange("Employee", "emp-2", "CREATE", map[string]interface{}{"name": "B"})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleWS(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	var welcome Message
	wsjson.Read(ctx, c, &welcome)
	if welcome.LastEventID != 2 {
		t.Errorf("expected lastEventId=2 on welcome, got %d", welcome.LastEventID)
	}

	wsjson.Write(ctx, c, Message{Type: "subscribe", Data: json.RawMessage(`{"objectType":"Employee"}`)})
	var subResp Message
	wsjson.Read(ctx, c, &subResp)
	if subResp.Type != "subscribed" {
		t.Fatalf("expected subscribed, got %q", subResp.Type)
	}

	// Confirm no replayed event arrives within a short window.
	noReplayCtx, noReplayCancel := context.WithTimeout(ctx, 200*time.Millisecond)
	var stray Message
	err = wsjson.Read(noReplayCtx, c, &stray)
	noReplayCancel()
	if err == nil {
		t.Errorf("expected no replay without ?since=, got %q", stray.Type)
	}
}

// itoa is a tiny helper to format int64 into a base-10 query string without
// pulling strconv into every test body.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
