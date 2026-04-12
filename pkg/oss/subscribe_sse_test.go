package oss

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oss/where"
)

type sseEventFrame struct {
	EventType string                 `json:"eventType"`
	Object    map[string]interface{} `json:"object"`
}

type stubObjectSetLookup struct {
	byRid map[string]SubscriptionSpec
}

func (s *stubObjectSetLookup) ResolveSubscription(rid string) (SubscriptionSpec, error) {
	if s == nil || s.byRid == nil {
		return SubscriptionSpec{}, ErrObjectSetNotFound
	}
	spec, ok := s.byRid[rid]
	if !ok {
		return SubscriptionSpec{}, ErrObjectSetNotFound
	}
	return spec, nil
}

// TestSSESubscribeBasicStream is the US-055 red-first acceptance test for the
// SSE ObjectSet subscribe scaffold. It exercises:
//   - the handler wired behind
//     GET /api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe
//   - the required text/event-stream response headers
//   - the mapping from funnel.BroadcastEvent (CREATE / MODIFY / DELETE)
//     onto SSE data-lines carrying the canonical ADDED_OR_UPDATED / DELETED
//     event types plus the WireObject payload
//   - server-side ObjectType filtering so only events matching the stored
//     ObjectSet base ObjectType reach the client
func TestSSESubscribeBasicStream(t *testing.T) {
	const rid = "rid-order-scaffold-1"
	lookup := &stubObjectSetLookup{byRid: map[string]SubscriptionSpec{
		rid: {ObjectType: "order"},
	}}

	b := funnel.NewBroadcast()
	handler := NewSubscribeSSEHandler(lookup, b)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe", handler.ServeHTTP)

	srv := httptest.NewServer(r)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v2/ontologies/northwind/objectSets/"+rid+"/subscribe", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	if got := resp.Header.Get("Connection"); got != "keep-alive" {
		t.Errorf("Connection = %q, want keep-alive", got)
	}

	eventsCh := make(chan sseEventFrame, 8)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					return
				}
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimPrefix(line, "data: ")
			var evt sseEventFrame
			if err := json.Unmarshal([]byte(payload), &evt); err != nil {
				t.Errorf("json decode %q: %v", payload, err)
				return
			}
			select {
			case eventsCh <- evt:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Give the subscriber a moment to register with the broadcast hub. This
	// is inherent to the server-side fan-out design — if Publish fires
	// before the handler's Subscribe call we lose the event.
	waitForSubscriber(t, b, 1, 500*time.Millisecond)

	b.Publish(funnel.BroadcastEvent{
		Type:       "CREATE",
		ObjectType: "order",
		PrimaryKey: "o-1",
		Properties: map[string]interface{}{"status": "NEW"},
		EditedAt:   time.Now(),
	})
	// Non-matching ObjectType — handler must drop silently.
	b.Publish(funnel.BroadcastEvent{
		Type:       "MODIFY",
		ObjectType: "customer",
		PrimaryKey: "c-99",
		EditedAt:   time.Now(),
	})
	b.Publish(funnel.BroadcastEvent{
		Type:       "DELETE",
		ObjectType: "order",
		PrimaryKey: "o-2",
		EditedAt:   time.Now(),
	})

	first := expectEvent(t, eventsCh, 2*time.Second)
	if first.EventType != "ADDED_OR_UPDATED" {
		t.Errorf("first eventType = %q, want ADDED_OR_UPDATED", first.EventType)
	}
	if pk, _ := first.Object["__primaryKey"].(string); pk != "o-1" {
		t.Errorf("first __primaryKey = %v, want o-1", first.Object["__primaryKey"])
	}
	if apiName, _ := first.Object["__apiName"].(string); apiName != "order" {
		t.Errorf("first __apiName = %v, want order", first.Object["__apiName"])
	}
	if status, _ := first.Object["status"].(string); status != "NEW" {
		t.Errorf("first status = %v, want NEW", first.Object["status"])
	}

	second := expectEvent(t, eventsCh, 2*time.Second)
	if second.EventType != "DELETED" {
		t.Errorf("second eventType = %q, want DELETED", second.EventType)
	}
	if pk, _ := second.Object["__primaryKey"].(string); pk != "o-2" {
		t.Errorf("second __primaryKey = %v, want o-2", second.Object["__primaryKey"])
	}

	select {
	case unexpected := <-eventsCh:
		t.Errorf("unexpected extra event: %+v", unexpected)
	case <-time.After(150 * time.Millisecond):
	}

	cancel()
	wg.Wait()
}

// TestSSEWhereFilter is the red-first acceptance test for US-056. It
// exercises the server-side Where evaluation path: the stored ObjectSet
// definition declares `status = "SHIPPED" AND amount > 100` and the
// handler MUST drop events that do not satisfy the clause before writing
// them to the SSE stream.
//
//   - Event 1 {status=SHIPPED, amount=150} — MATCH, delivered
//   - Event 2 {status=PENDING, amount=500} — NO match (status wrong), dropped
//   - Event 3 {status=SHIPPED, amount=50}  — NO match (amount too low), dropped
//   - Event 4 {status=SHIPPED, amount=200} — MATCH, delivered
//
// The client reader must see exactly events 1 and 4, in order, and must
// NOT see events 2 or 3.
func TestSSEWhereFilter(t *testing.T) {
	const rid = "rid-order-where-1"
	clauseJSON := `{
        "type": "and",
        "value": [
            {"type": "eq", "field": "status", "value": "SHIPPED"},
            {"type": "gt", "field": "amount", "value": 100}
        ]
    }`
	var clause where.WhereClause
	if err := json.Unmarshal([]byte(clauseJSON), &clause); err != nil {
		t.Fatalf("unmarshal where clause: %v", err)
	}

	lookup := &stubObjectSetLookup{byRid: map[string]SubscriptionSpec{
		rid: {ObjectType: "order", Where: &clause},
	}}
	b := funnel.NewBroadcast()
	handler := NewSubscribeSSEHandler(lookup, b)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe", handler.ServeHTTP)

	srv := httptest.NewServer(r)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v2/ontologies/northwind/objectSets/"+rid+"/subscribe", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	eventsCh := make(chan sseEventFrame, 8)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					return
				}
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimPrefix(line, "data: ")
			var evt sseEventFrame
			if err := json.Unmarshal([]byte(payload), &evt); err != nil {
				t.Errorf("json decode %q: %v", payload, err)
				return
			}
			select {
			case eventsCh <- evt:
			case <-ctx.Done():
				return
			}
		}
	}()

	waitForSubscriber(t, b, 1, 500*time.Millisecond)

	// MATCH — status=SHIPPED AND amount=150 > 100
	b.Publish(funnel.BroadcastEvent{
		Type:       "CREATE",
		ObjectType: "order",
		PrimaryKey: "o-1",
		Properties: map[string]interface{}{"status": "SHIPPED", "amount": 150.0},
		EditedAt:   time.Now(),
	})
	// DROP — status != SHIPPED
	b.Publish(funnel.BroadcastEvent{
		Type:       "MODIFY",
		ObjectType: "order",
		PrimaryKey: "o-2",
		Properties: map[string]interface{}{"status": "PENDING", "amount": 500.0},
		EditedAt:   time.Now(),
	})
	// DROP — amount <= 100
	b.Publish(funnel.BroadcastEvent{
		Type:       "MODIFY",
		ObjectType: "order",
		PrimaryKey: "o-3",
		Properties: map[string]interface{}{"status": "SHIPPED", "amount": 50.0},
		EditedAt:   time.Now(),
	})
	// MATCH
	b.Publish(funnel.BroadcastEvent{
		Type:       "CREATE",
		ObjectType: "order",
		PrimaryKey: "o-4",
		Properties: map[string]interface{}{"status": "SHIPPED", "amount": 200.0},
		EditedAt:   time.Now(),
	})

	first := expectEvent(t, eventsCh, 2*time.Second)
	if pk, _ := first.Object["__primaryKey"].(string); pk != "o-1" {
		t.Errorf("first __primaryKey = %v, want o-1", first.Object["__primaryKey"])
	}
	second := expectEvent(t, eventsCh, 2*time.Second)
	if pk, _ := second.Object["__primaryKey"].(string); pk != "o-4" {
		t.Errorf("second __primaryKey = %v, want o-4", second.Object["__primaryKey"])
	}

	select {
	case unexpected := <-eventsCh:
		t.Errorf("unexpected extra event: %+v", unexpected)
	case <-time.After(150 * time.Millisecond):
	}

	cancel()
	wg.Wait()
}

func TestSSESubscribeObjectSetNotFound(t *testing.T) {
	lookup := &stubObjectSetLookup{byRid: map[string]SubscriptionSpec{}}
	b := funnel.NewBroadcast()
	handler := NewSubscribeSSEHandler(lookup, b)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe", handler.ServeHTTP)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v2/ontologies/northwind/objectSets/does-not-exist/subscribe")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func waitForSubscriber(t *testing.T, b *funnel.Broadcast, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// Probe by publishing a sentinel + checking via subscriber count if
		// available; the broadcast hub does not expose a counter, so we
		// just sleep briefly — the http.Client.Do call has already completed,
		// so the handler goroutine has started. One short sleep is enough
		// for the handler to reach broadcast.Subscribe.
		time.Sleep(20 * time.Millisecond)
		return
	}
	_ = want
}

func expectEvent(t *testing.T, ch <-chan sseEventFrame, timeout time.Duration) sseEventFrame {
	t.Helper()
	select {
	case evt := <-ch:
		return evt
	case <-time.After(timeout):
		t.Fatal("timed out waiting for SSE event")
		return sseEventFrame{}
	}
}
