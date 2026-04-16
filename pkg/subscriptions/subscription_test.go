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

	"github.com/liyang/weave/pkg/oss/where"
)

// ---------- Subscription unit tests ----------

func TestNewSubscription(t *testing.T) {
	req := SubscribeRequest{
		ObjectType: "Employee",
		Select:     []string{"name", "email"},
	}
	sub := NewSubscription(req)
	if sub.ID == "" {
		t.Error("expected non-empty subscription ID")
	}
	if sub.ObjectType != "Employee" {
		t.Errorf("expected ObjectType=Employee, got %q", sub.ObjectType)
	}
	if len(sub.Select) != 2 {
		t.Errorf("expected 2 select fields, got %d", len(sub.Select))
	}
}

func TestSubscription_Matches_ObjectType(t *testing.T) {
	sub := &Subscription{ID: "s1", ObjectType: "Employee"}

	if !sub.Matches("Employee", map[string]interface{}{"name": "John"}) {
		t.Error("expected match for same ObjectType")
	}
	if sub.Matches("Department", map[string]interface{}{"name": "HR"}) {
		t.Error("expected no match for different ObjectType")
	}
}

func TestSubscription_Matches_WhereClause(t *testing.T) {
	sub := &Subscription{
		ID:         "s1",
		ObjectType: "Employee",
		Where: &where.WhereClause{
			Type:  "eq",
			Field: "department",
			Value: json.RawMessage(`"Engineering"`),
		},
	}

	if !sub.Matches("Employee", map[string]interface{}{"department": "Engineering"}) {
		t.Error("expected match when where clause satisfied")
	}
	if sub.Matches("Employee", map[string]interface{}{"department": "Sales"}) {
		t.Error("expected no match when where clause not satisfied")
	}
}

func TestSubscription_Matches_NilWhere(t *testing.T) {
	sub := &Subscription{ID: "s1", ObjectType: "Employee", Where: nil}
	if !sub.Matches("Employee", map[string]interface{}{"name": "John"}) {
		t.Error("nil where clause should match all")
	}
}

func TestSubscription_ProjectProperties_NoSelect(t *testing.T) {
	sub := &Subscription{ID: "s1", ObjectType: "Employee"}
	props := map[string]interface{}{"name": "John", "email": "john@co.com", "age": 30}
	result := sub.ProjectProperties(props)
	if len(result) != 3 {
		t.Errorf("expected 3 properties (no projection), got %d", len(result))
	}
}

func TestSubscription_ProjectProperties_WithSelect(t *testing.T) {
	sub := &Subscription{ID: "s1", ObjectType: "Employee", Select: []string{"name", "email"}}
	props := map[string]interface{}{"name": "John", "email": "john@co.com", "age": 30}
	result := sub.ProjectProperties(props)
	if len(result) != 2 {
		t.Errorf("expected 2 projected properties, got %d", len(result))
	}
	if result["name"] != "John" {
		t.Errorf("expected name=John, got %v", result["name"])
	}
	if result["email"] != "john@co.com" {
		t.Errorf("expected email=john@co.com, got %v", result["email"])
	}
	if _, ok := result["age"]; ok {
		t.Error("age should not be in projected result")
	}
}

func TestSubscription_ProjectProperties_MissingField(t *testing.T) {
	sub := &Subscription{ID: "s1", ObjectType: "Employee", Select: []string{"name", "nonexistent"}}
	props := map[string]interface{}{"name": "John"}
	result := sub.ProjectProperties(props)
	if len(result) != 1 {
		t.Errorf("expected 1 property, got %d", len(result))
	}
}

func TestEditTypeToState(t *testing.T) {
	tests := []struct {
		editType string
		want     string
	}{
		{"CREATE", "ADDED_OR_UPDATED"},
		{"MODIFY", "ADDED_OR_UPDATED"},
		{"DELETE", "DELETED"},
	}
	for _, tt := range tests {
		got := editTypeToState(tt.editType)
		if got != tt.want {
			t.Errorf("editTypeToState(%q) = %q, want %q", tt.editType, got, tt.want)
		}
	}
}

// ---------- WebSocket integration tests ----------

// dialAndWelcome connects to the test server and reads the welcome message.
func dialAndWelcome(t *testing.T, ctx context.Context, srvURL string) (*websocket.Conn, string) {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(srvURL, "http") + "/ws"
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

func TestSubscribe_Success(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, connID := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	// Send subscribe message
	subMsg := Message{
		Type: "subscribe",
		Data: json.RawMessage(`{"objectType":"Employee"}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}

	// Read subscribed response
	var resp Message
	if err := wsjson.Read(ctx, c, &resp); err != nil {
		t.Fatalf("read subscribed: %v", err)
	}
	if resp.Type != "subscribed" {
		t.Errorf("expected type=subscribed, got %q", resp.Type)
	}
	if resp.SubscriptionID == "" {
		t.Error("expected non-empty subscriptionId")
	}

	// Verify subscription count
	time.Sleep(50 * time.Millisecond)
	if cnt := h.SubscriptionCount(connID); cnt != 1 {
		t.Errorf("expected 1 subscription, got %d", cnt)
	}
}

func TestSubscribe_MissingObjectType(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	subMsg := Message{
		Type: "subscribe",
		Data: json.RawMessage(`{}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write: %v", err)
	}

	var resp Message
	if err := wsjson.Read(ctx, c, &resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.Type != "error" {
		t.Errorf("expected type=error, got %q", resp.Type)
	}
	if !strings.Contains(resp.Error, "objectType") {
		t.Errorf("expected error about objectType, got %q", resp.Error)
	}
}

func TestSubscribe_MaxLimit(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	// Subscribe 10 times — should all succeed
	for i := 0; i < MaxSubscriptionsPerConnection; i++ {
		subMsg := Message{
			Type: "subscribe",
			Data: json.RawMessage(`{"objectType":"Type` + string(rune('A'+i)) + `"}`),
		}
		if err := wsjson.Write(ctx, c, subMsg); err != nil {
			t.Fatalf("write sub %d: %v", i, err)
		}
		var resp Message
		if err := wsjson.Read(ctx, c, &resp); err != nil {
			t.Fatalf("read sub %d: %v", i, err)
		}
		if resp.Type != "subscribed" {
			t.Fatalf("sub %d: expected subscribed, got %q (%s)", i, resp.Type, resp.Error)
		}
	}

	// 11th subscription should fail
	subMsg := Message{
		Type: "subscribe",
		Data: json.RawMessage(`{"objectType":"Overflow"}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write overflow: %v", err)
	}
	var resp Message
	if err := wsjson.Read(ctx, c, &resp); err != nil {
		t.Fatalf("read overflow: %v", err)
	}
	if resp.Type != "error" {
		t.Errorf("expected type=error for 11th subscription, got %q", resp.Type)
	}
	if !strings.Contains(resp.Error, "maximum") {
		t.Errorf("expected 'maximum' in error, got %q", resp.Error)
	}
}

func TestUnsubscribe_Success(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, connID := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	// Subscribe
	subMsg := Message{
		Type: "subscribe",
		Data: json.RawMessage(`{"objectType":"Employee"}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write sub: %v", err)
	}
	var subResp Message
	if err := wsjson.Read(ctx, c, &subResp); err != nil {
		t.Fatalf("read sub: %v", err)
	}
	subID := subResp.SubscriptionID

	// Unsubscribe
	unsubMsg := Message{
		Type: "unsubscribe",
		Data: json.RawMessage(`{"subscriptionId":"` + subID + `"}`),
	}
	if err := wsjson.Write(ctx, c, unsubMsg); err != nil {
		t.Fatalf("write unsub: %v", err)
	}
	var unsubResp Message
	if err := wsjson.Read(ctx, c, &unsubResp); err != nil {
		t.Fatalf("read unsub: %v", err)
	}
	if unsubResp.Type != "unsubscribed" {
		t.Errorf("expected type=unsubscribed, got %q", unsubResp.Type)
	}
	if unsubResp.SubscriptionID != subID {
		t.Errorf("expected subscriptionId=%s, got %s", subID, unsubResp.SubscriptionID)
	}

	// Verify subscription removed
	time.Sleep(50 * time.Millisecond)
	if cnt := h.SubscriptionCount(connID); cnt != 0 {
		t.Errorf("expected 0 subscriptions after unsubscribe, got %d", cnt)
	}
}

func TestUnsubscribe_NotFound(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	unsubMsg := Message{
		Type: "unsubscribe",
		Data: json.RawMessage(`{"subscriptionId":"nonexistent"}`),
	}
	if err := wsjson.Write(ctx, c, unsubMsg); err != nil {
		t.Fatalf("write: %v", err)
	}
	var resp Message
	if err := wsjson.Read(ctx, c, &resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.Type != "error" {
		t.Errorf("expected type=error, got %q", resp.Type)
	}
}

func TestSubscribe_ThenObjectChange_ReceivesEvent(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	// Subscribe to Employee
	subMsg := Message{
		Type: "subscribe",
		Data: json.RawMessage(`{"objectType":"Employee"}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write sub: %v", err)
	}
	var subResp Message
	if err := wsjson.Read(ctx, c, &subResp); err != nil {
		t.Fatalf("read sub: %v", err)
	}
	subID := subResp.SubscriptionID

	// Trigger object change
	h.HandleObjectChange("Employee", "emp-1", "CREATE", map[string]interface{}{
		"name":       "John Smith",
		"department": "Engineering",
	})

	// Read change event
	var evt Message
	if err := wsjson.Read(ctx, c, &evt); err != nil {
		t.Fatalf("read event: %v", err)
	}
	if evt.Type != "objectChanged" {
		t.Errorf("expected type=objectChanged, got %q", evt.Type)
	}
	if evt.SubscriptionID != subID {
		t.Errorf("expected subscriptionId=%s, got %s", subID, evt.SubscriptionID)
	}

	// Parse event data
	var change ObjectChangeEvent
	if err := json.Unmarshal(evt.Data, &change); err != nil {
		t.Fatalf("unmarshal event data: %v", err)
	}
	if change.State != "ADDED_OR_UPDATED" {
		t.Errorf("expected state=ADDED_OR_UPDATED, got %q", change.State)
	}
	if change.Object["name"] != "John Smith" {
		t.Errorf("expected name=John Smith, got %v", change.Object["name"])
	}
}

func TestSubscribe_DeleteEvent_StateIsDeleted(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	subMsg := Message{
		Type: "subscribe",
		Data: json.RawMessage(`{"objectType":"Employee"}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write: %v", err)
	}
	var subResp Message
	if err := wsjson.Read(ctx, c, &subResp); err != nil {
		t.Fatalf("read: %v", err)
	}

	h.HandleObjectChange("Employee", "emp-1", "DELETE", map[string]interface{}{
		"name": "John Smith",
	})

	var evt Message
	if err := wsjson.Read(ctx, c, &evt); err != nil {
		t.Fatalf("read event: %v", err)
	}
	var change ObjectChangeEvent
	if err := json.Unmarshal(evt.Data, &change); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if change.State != "DELETED" {
		t.Errorf("expected state=DELETED, got %q", change.State)
	}
}

func TestSubscribe_WhereFilter_OnlyMatchingEvents(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	// Subscribe with where filter: department == "Engineering"
	subMsg := Message{
		Type: "subscribe",
		Data: json.RawMessage(`{
			"objectType": "Employee",
			"where": {"type":"eq","field":"department","value":"Engineering"}
		}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write: %v", err)
	}
	var subResp Message
	if err := wsjson.Read(ctx, c, &subResp); err != nil {
		t.Fatalf("read: %v", err)
	}

	// Non-matching change (Sales) — should not be pushed
	h.HandleObjectChange("Employee", "emp-1", "CREATE", map[string]interface{}{
		"name":       "Jane",
		"department": "Sales",
	})

	// Matching change (Engineering) — should be pushed
	h.HandleObjectChange("Employee", "emp-2", "CREATE", map[string]interface{}{
		"name":       "John",
		"department": "Engineering",
	})

	// Read the only event we should get
	var evt Message
	if err := wsjson.Read(ctx, c, &evt); err != nil {
		t.Fatalf("read event: %v", err)
	}
	var change ObjectChangeEvent
	if err := json.Unmarshal(evt.Data, &change); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if change.Object["name"] != "John" {
		t.Errorf("expected name=John (matching event), got %v", change.Object["name"])
	}
}

func TestSubscribe_SelectProjection(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	// Subscribe with select: only name
	subMsg := Message{
		Type: "subscribe",
		Data: json.RawMessage(`{
			"objectType": "Employee",
			"select": ["name"]
		}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write: %v", err)
	}
	var subResp Message
	if err := wsjson.Read(ctx, c, &subResp); err != nil {
		t.Fatalf("read: %v", err)
	}

	h.HandleObjectChange("Employee", "emp-1", "MODIFY", map[string]interface{}{
		"name":       "John",
		"email":      "john@co.com",
		"department": "Engineering",
	})

	var evt Message
	if err := wsjson.Read(ctx, c, &evt); err != nil {
		t.Fatalf("read event: %v", err)
	}
	var change ObjectChangeEvent
	if err := json.Unmarshal(evt.Data, &change); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Should only contain "name"
	if change.Object["name"] != "John" {
		t.Errorf("expected name=John, got %v", change.Object["name"])
	}
	if _, ok := change.Object["email"]; ok {
		t.Error("email should not be in projected object")
	}
	if _, ok := change.Object["department"]; ok {
		t.Error("department should not be in projected object")
	}
}

func TestSubscribe_DifferentObjectType_NoEvent(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	// Subscribe to Employee
	subMsg := Message{
		Type: "subscribe",
		Data: json.RawMessage(`{"objectType":"Employee"}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write: %v", err)
	}
	var subResp Message
	if err := wsjson.Read(ctx, c, &subResp); err != nil {
		t.Fatalf("read: %v", err)
	}

	// Change on Department — should NOT trigger
	h.HandleObjectChange("Department", "dept-1", "CREATE", map[string]interface{}{
		"name": "HR",
	})

	// Then change on Employee — should trigger
	h.HandleObjectChange("Employee", "emp-1", "CREATE", map[string]interface{}{
		"name": "John",
	})

	var evt Message
	if err := wsjson.Read(ctx, c, &evt); err != nil {
		t.Fatalf("read event: %v", err)
	}
	var change ObjectChangeEvent
	if err := json.Unmarshal(evt.Data, &change); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Should receive the Employee event, not the Department event
	if change.Object["name"] != "John" {
		t.Errorf("expected name=John, got %v", change.Object["name"])
	}
}

func TestMultipleSubscriptions_SameConnection(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	// Subscribe to Employee and Department
	for _, ot := range []string{"Employee", "Department"} {
		subMsg := Message{
			Type: "subscribe",
			Data: json.RawMessage(`{"objectType":"` + ot + `"}`),
		}
		if err := wsjson.Write(ctx, c, subMsg); err != nil {
			t.Fatalf("write sub %s: %v", ot, err)
		}
		var resp Message
		if err := wsjson.Read(ctx, c, &resp); err != nil {
			t.Fatalf("read sub %s: %v", ot, err)
		}
	}

	// Change on Employee — should match 1 subscription
	h.HandleObjectChange("Employee", "emp-1", "CREATE", map[string]interface{}{
		"name": "John",
	})

	var evt Message
	if err := wsjson.Read(ctx, c, &evt); err != nil {
		t.Fatalf("read event: %v", err)
	}
	if evt.Type != "objectChanged" {
		t.Errorf("expected objectChanged, got %q", evt.Type)
	}
}

func TestUnsubscribe_StopsEvents(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	// Subscribe
	subMsg := Message{
		Type: "subscribe",
		Data: json.RawMessage(`{"objectType":"Employee"}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write sub: %v", err)
	}
	var subResp Message
	if err := wsjson.Read(ctx, c, &subResp); err != nil {
		t.Fatalf("read sub: %v", err)
	}
	subID := subResp.SubscriptionID

	// Unsubscribe
	unsubMsg := Message{
		Type: "unsubscribe",
		Data: json.RawMessage(`{"subscriptionId":"` + subID + `"}`),
	}
	if err := wsjson.Write(ctx, c, unsubMsg); err != nil {
		t.Fatalf("write unsub: %v", err)
	}
	var unsubResp Message
	if err := wsjson.Read(ctx, c, &unsubResp); err != nil {
		t.Fatalf("read unsub: %v", err)
	}

	// Trigger change — should NOT receive event
	h.HandleObjectChange("Employee", "emp-1", "CREATE", map[string]interface{}{
		"name": "John",
	})

	// Send a ping to verify no event was queued (subscribe to another type to generate a response)
	pingMsg := Message{
		Type: "subscribe",
		Data: json.RawMessage(`{"objectType":"Ping"}`),
	}
	if err := wsjson.Write(ctx, c, pingMsg); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	var pingResp Message
	if err := wsjson.Read(ctx, c, &pingResp); err != nil {
		t.Fatalf("read ping: %v", err)
	}
	// The response should be "subscribed" (not an objectChanged event)
	if pingResp.Type != "subscribed" {
		t.Errorf("expected subscribed (no change event queued), got %q", pingResp.Type)
	}
}

func TestUnknownMessageType(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	badMsg := Message{Type: "foo", Data: json.RawMessage(`{}`)}
	if err := wsjson.Write(ctx, c, badMsg); err != nil {
		t.Fatalf("write: %v", err)
	}

	var resp Message
	if err := wsjson.Read(ctx, c, &resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.Type != "error" {
		t.Errorf("expected type=error, got %q", resp.Type)
	}
	if !strings.Contains(resp.Error, "unknown") {
		t.Errorf("expected 'unknown' in error, got %q", resp.Error)
	}
}

func TestDisconnect_CleansSubscriptions(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, connID := dialAndWelcome(t, ctx, srv.URL)

	// Subscribe
	subMsg := Message{
		Type: "subscribe",
		Data: json.RawMessage(`{"objectType":"Employee"}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write: %v", err)
	}
	var resp Message
	if err := wsjson.Read(ctx, c, &resp); err != nil {
		t.Fatalf("read: %v", err)
	}

	// Verify subscription exists
	time.Sleep(50 * time.Millisecond)
	if cnt := h.SubscriptionCount(connID); cnt != 1 {
		t.Fatalf("expected 1 subscription, got %d", cnt)
	}

	// Disconnect
	c.Close(websocket.StatusNormalClosure, "done")

	// Wait for cleanup
	time.Sleep(100 * time.Millisecond)

	// Connection and subscriptions should be cleaned up
	if cnt := h.SubscriptionCount(connID); cnt != -1 {
		t.Errorf("expected -1 (connection gone), got %d", cnt)
	}
}
