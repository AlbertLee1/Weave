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

	"github.com/liyang/weave/pkg/oss/objectset"
)

// ---------- subscriptionObjectTypes unit tests ----------

func TestSubscriptionObjectTypes_LegacyFixedType(t *testing.T) {
	sub := &Subscription{ID: "s1", ObjectType: "Employee"}
	got := subscriptionObjectTypes(sub)
	if !equalStringSlices(got, []string{"Employee"}) {
		t.Errorf("expected [Employee], got %v", got)
	}
}

func TestSubscriptionObjectTypes_AggregatorFixedType(t *testing.T) {
	sub := &Subscription{
		ID:         "s1",
		ObjectType: "Employee",
		Aggregator: newIncrementalAggregator("Employee", nil, AggMetric{Type: "count"}, ""),
	}
	got := subscriptionObjectTypes(sub)
	if !equalStringSlices(got, []string{"Employee"}) {
		t.Errorf("expected [Employee], got %v", got)
	}
}

func TestSubscriptionObjectTypes_DefinitionBase(t *testing.T) {
	sub := &Subscription{
		ID:         "s1",
		Definition: &objectset.Definition{Type: "base", ObjectType: "Employee"},
	}
	got := subscriptionObjectTypes(sub)
	if !equalStringSlices(got, []string{"Employee"}) {
		t.Errorf("expected [Employee], got %v", got)
	}
}

func TestSubscriptionObjectTypes_DefinitionUnion(t *testing.T) {
	sub := &Subscription{
		ID: "s1",
		Definition: &objectset.Definition{
			Type: "union",
			ObjectSets: []*objectset.Definition{
				{Type: "base", ObjectType: "Employee"},
				{Type: "base", ObjectType: "Department"},
			},
		},
	}
	got := subscriptionObjectTypes(sub)
	if !equalStringSlicesUnordered(got, []string{"Employee", "Department"}) {
		t.Errorf("expected [Employee Department] (any order), got %v", got)
	}
}

func TestSubscriptionObjectTypes_DefinitionFilterPassesThrough(t *testing.T) {
	sub := &Subscription{
		ID: "s1",
		Definition: &objectset.Definition{
			Type:      "filter",
			ObjectSet: &objectset.Definition{Type: "base", ObjectType: "Employee"},
			Where:     json.RawMessage(`{"type":"eq","field":"department","value":"Engineering"}`),
		},
	}
	got := subscriptionObjectTypes(sub)
	if !equalStringSlices(got, []string{"Employee"}) {
		t.Errorf("expected [Employee], got %v", got)
	}
}

func TestSubscriptionObjectTypes_DefinitionStaticAndAsType(t *testing.T) {
	cases := []struct {
		name string
		def  *objectset.Definition
	}{
		{
			name: "static",
			def:  &objectset.Definition{Type: "static", ObjectType: "Employee", PrimaryKeys: []string{"e1"}},
		},
		{
			name: "asType",
			def: &objectset.Definition{
				Type:       "asType",
				ObjectType: "Employee",
				ObjectSet:  &objectset.Definition{Type: "base", ObjectType: "Employee"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sub := &Subscription{ID: "s1", Definition: tc.def}
			got := subscriptionObjectTypes(sub)
			if !equalStringSlices(got, []string{"Employee"}) {
				t.Errorf("expected [Employee], got %v", got)
			}
		})
	}
}

func TestSubscriptionObjectTypes_DefinitionSubtractCollectsAllChildren(t *testing.T) {
	// Subtract membership only checks first child positively, but for routing
	// we accept a superset of candidate types because matchesDefinition gates
	// at evaluation time.
	sub := &Subscription{
		ID: "s1",
		Definition: &objectset.Definition{
			Type: "subtract",
			ObjectSets: []*objectset.Definition{
				{Type: "base", ObjectType: "Employee"},
				{Type: "base", ObjectType: "Contractor"},
			},
		},
	}
	got := subscriptionObjectTypes(sub)
	// At minimum the first child's type must be present so positive routing
	// works. Including the second child's type is acceptable but not required.
	if !contains(got, "Employee") {
		t.Errorf("expected first-child type Employee in routing keys, got %v", got)
	}
}

// ---------- Hub-level routing-index integration tests ----------

func TestHub_SubscribeIndexes_ByObjectType(t *testing.T) {
	h := NewHub()
	defer h.Close()
	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	wsjson.Write(ctx, c, Message{Type: "subscribe", Data: json.RawMessage(`{"objectType":"Employee"}`)})
	var resp Message
	wsjson.Read(ctx, c, &resp)
	if resp.Type != "subscribed" {
		t.Fatalf("expected subscribed, got %q", resp.Type)
	}

	// Wait for hub to register the routing entry (subscribe handler completes
	// before the response is read, but we cross a goroutine boundary).
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if h.subscriptionCountForObjectType("Employee") == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := h.subscriptionCountForObjectType("Employee"); got != 1 {
		t.Errorf("expected 1 subscription indexed under Employee, got %d", got)
	}
	if got := h.subscriptionCountForObjectType("Department"); got != 0 {
		t.Errorf("expected 0 subscriptions indexed under Department, got %d", got)
	}
}

func TestHub_UnsubscribeRemovesFromIndex(t *testing.T) {
	h := NewHub()
	defer h.Close()
	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	wsjson.Write(ctx, c, Message{Type: "subscribe", Data: json.RawMessage(`{"objectType":"Employee"}`)})
	var resp Message
	wsjson.Read(ctx, c, &resp)
	subID := resp.SubscriptionID

	wsjson.Write(ctx, c, Message{Type: "unsubscribe", Data: json.RawMessage(`{"subscriptionId":"` + subID + `"}`)})
	var unsub Message
	wsjson.Read(ctx, c, &unsub)

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if h.subscriptionCountForObjectType("Employee") == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := h.subscriptionCountForObjectType("Employee"); got != 0 {
		t.Errorf("expected index to be empty after unsubscribe, got %d", got)
	}
}

func TestHub_DisconnectCleansUpIndex(t *testing.T) {
	h := NewHub()
	defer h.Close()
	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)

	wsjson.Write(ctx, c, Message{Type: "subscribe", Data: json.RawMessage(`{"objectType":"Employee"}`)})
	var resp Message
	wsjson.Read(ctx, c, &resp)
	if resp.Type != "subscribed" {
		t.Fatalf("expected subscribed, got %q (%s)", resp.Type, resp.Error)
	}

	c.Close(websocket.StatusNormalClosure, "bye")

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if h.subscriptionCountForObjectType("Employee") == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := h.subscriptionCountForObjectType("Employee"); got != 0 {
		t.Errorf("expected index to be empty after disconnect, got %d", got)
	}
}

func TestHub_RoutingIndex_DoesNotIterateUnrelatedConnections(t *testing.T) {
	// Wire 100 connections, each subscribing to a unique objectType. Then fire
	// a change for a SINGLE objectType and verify only one connection sees the
	// event. This is the core invariant of objectType-keyed dispatch — without
	// it we'd be O(N) per event.
	h := NewHub()
	defer h.Close()
	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const N = 50
	conns := make([]*websocket.Conn, N)
	for i := 0; i < N; i++ {
		c, _ := dialAndWelcome(t, ctx, srv.URL)
		conns[i] = c
		ot := "Type" + strings.Repeat("X", i+1) // all unique
		wsjson.Write(ctx, c, Message{Type: "subscribe", Data: json.RawMessage(`{"objectType":"` + ot + `"}`)})
		var resp Message
		wsjson.Read(ctx, c, &resp)
	}
	defer func() {
		for _, c := range conns {
			c.Close(websocket.StatusNormalClosure, "")
		}
	}()

	// Wait for index to settle.
	deadline := time.Now().Add(1 * time.Second)
	target := "Type" + strings.Repeat("X", 5)
	for time.Now().Before(deadline) {
		if h.subscriptionCountForObjectType(target) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := h.subscriptionCountForObjectType(target); got != 1 {
		t.Fatalf("expected exactly 1 subscriber for %s, got %d", target, got)
	}

	// Fire a change for the targeted type. Only conns[4] should receive it.
	h.HandleObjectChange(target, "pk-1", "CREATE", map[string]interface{}{"name": "Anna"})

	// Conn 4 should receive an objectChanged.
	readCtx, readCancel := context.WithTimeout(ctx, 1*time.Second)
	var evt Message
	if err := wsjson.Read(readCtx, conns[4], &evt); err != nil {
		readCancel()
		t.Fatalf("expected event on conn 4: %v", err)
	}
	readCancel()
	if evt.Type != "objectChanged" {
		t.Errorf("expected objectChanged, got %q", evt.Type)
	}

	// Sample a handful of OTHER conns and confirm no message pending.
	for _, idx := range []int{0, 1, 10, 25, N - 1} {
		readCtx, readCancel := context.WithTimeout(ctx, 100*time.Millisecond)
		var msg Message
		err := wsjson.Read(readCtx, conns[idx], &msg)
		readCancel()
		if err == nil {
			t.Errorf("conn %d unexpectedly received %q (subId=%s)", idx, msg.Type, msg.SubscriptionID)
		}
	}
}

func TestHub_RoutingIndex_DefinitionUnionRoutesToBothTypes(t *testing.T) {
	h := NewHub()
	defer h.Close()
	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	body := `{"definition":{"type":"union","objectSets":[
		{"type":"base","objectType":"Employee"},
		{"type":"base","objectType":"Department"}
	]}}`
	wsjson.Write(ctx, c, Message{Type: "subscribeObjectSet", Data: json.RawMessage(body)})
	var resp Message
	wsjson.Read(ctx, c, &resp)
	if resp.Type != "subscribed" {
		t.Fatalf("expected subscribed, got %q (%s)", resp.Type, resp.Error)
	}

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if h.subscriptionCountForObjectType("Employee") == 1 && h.subscriptionCountForObjectType("Department") == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := h.subscriptionCountForObjectType("Employee"); got != 1 {
		t.Errorf("expected union sub indexed under Employee, got %d", got)
	}
	if got := h.subscriptionCountForObjectType("Department"); got != 1 {
		t.Errorf("expected union sub indexed under Department, got %d", got)
	}

	// Trigger a change on one of the two types — exactly one event should fire.
	h.HandleObjectChange("Employee", "e1", "CREATE", map[string]interface{}{"name": "Alice"})
	readCtx, readCancel := context.WithTimeout(ctx, 1*time.Second)
	var evt Message
	if err := wsjson.Read(readCtx, c, &evt); err != nil {
		readCancel()
		t.Fatalf("expected event from Employee change: %v", err)
	}
	readCancel()
	if evt.Type != "objectChanged" {
		t.Errorf("expected objectChanged, got %q", evt.Type)
	}

	// Trigger a change on Project (not in union) — nothing should fire.
	h.HandleObjectChange("Project", "p1", "CREATE", map[string]interface{}{"name": "P"})
	readCtx, readCancel = context.WithTimeout(ctx, 200*time.Millisecond)
	var noEvt Message
	if err := wsjson.Read(readCtx, c, &noEvt); err == nil {
		readCancel()
		t.Errorf("expected no event for non-routed type, got %q", noEvt.Type)
		return
	}
	readCancel()
}

// ---------- helpers ----------

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalStringSlicesUnordered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, v := range a {
		seen[v]++
	}
	for _, v := range b {
		seen[v]--
		if seen[v] < 0 {
			return false
		}
	}
	return true
}

func contains(s []string, target string) bool {
	for _, v := range s {
		if v == target {
			return true
		}
	}
	return false
}
