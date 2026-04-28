package oss

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oss/where"
)

// authWithTestUser is a thin wrapper around auth.WithUser that creates a
// minimal auth.User with the supplied ID. The US-058 test uses it (via a
// chi middleware) to stand in for the real auth middleware while keeping
// the helper local to pkg/oss.
func authWithTestUser(ctx context.Context, id string) context.Context {
	return auth.WithUser(ctx, &auth.User{ID: id})
}

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

// TestSSEReplayFromLastEventID is the US-057 red-first acceptance test. It
// verifies that:
//   - the handler emits SSE "id:" lines carrying the NATS sequence number
//   - a reconnecting client that supplies the Last-Event-ID request header
//     receives only events newer than that sequence from the in-process
//     replay buffer, followed seamlessly by new live events
//   - events already consumed before the disconnect (seq <= Last-Event-ID)
//     are NOT re-delivered
//
// The scenario: publish three events with Sequence 10, 11, 12 BEFORE any
// client connects so they are recorded in the broadcast hub's replay ring
// buffer. Then connect a client with "Last-Event-ID: 10". The client must
// receive exactly 11, 12 (from the ring replay path) and then, after a
// follow-up live Publish with Sequence 13, that event too.
func TestSSEReplayFromLastEventID(t *testing.T) {
	const rid = "rid-order-replay-1"
	lookup := &stubObjectSetLookup{byRid: map[string]SubscriptionSpec{
		rid: {ObjectType: "order"},
	}}

	b := funnel.NewBroadcast()
	handler := NewSubscribeSSEHandler(lookup, b)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe", handler.ServeHTTP)
	srv := httptest.NewServer(r)
	defer srv.Close()

	// Pre-seed replay buffer with three events. Sequence mirrors the NATS
	// stream sequence number that main.go forwards via onChange.
	b.Publish(funnel.BroadcastEvent{
		Type:       "CREATE",
		ObjectType: "order",
		PrimaryKey: "o-10",
		Sequence:   10,
		Properties: map[string]interface{}{"status": "NEW"},
		EditedAt:   time.Now(),
	})
	b.Publish(funnel.BroadcastEvent{
		Type:       "CREATE",
		ObjectType: "order",
		PrimaryKey: "o-11",
		Sequence:   11,
		Properties: map[string]interface{}{"status": "NEW"},
		EditedAt:   time.Now(),
	})
	b.Publish(funnel.BroadcastEvent{
		Type:       "MODIFY",
		ObjectType: "order",
		PrimaryKey: "o-12",
		Sequence:   12,
		Properties: map[string]interface{}{"status": "SHIPPED"},
		EditedAt:   time.Now(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v2/ontologies/northwind/objectSets/"+rid+"/subscribe", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	// Client last saw sequence 10 → expects replay starting at 11.
	req.Header.Set("Last-Event-ID", "10")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	framesCh := make(chan sseIDFrame, 16)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		reader := bufio.NewReader(resp.Body)
		var currentID string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					return
				}
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(line, "id: ") {
				currentID = strings.TrimPrefix(line, "id: ")
				continue
			}
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
			case framesCh <- sseIDFrame{ID: currentID, Event: evt}:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Replay events 11 and 12 should arrive without any extra Publish.
	first := expectIDFrame(t, framesCh, 2*time.Second)
	if first.ID != "11" {
		t.Errorf("first id = %q, want 11", first.ID)
	}
	if pk, _ := first.Event.Object["__primaryKey"].(string); pk != "o-11" {
		t.Errorf("first __primaryKey = %v, want o-11", first.Event.Object["__primaryKey"])
	}
	second := expectIDFrame(t, framesCh, 2*time.Second)
	if second.ID != "12" {
		t.Errorf("second id = %q, want 12", second.ID)
	}
	if pk, _ := second.Event.Object["__primaryKey"].(string); pk != "o-12" {
		t.Errorf("second __primaryKey = %v, want o-12", second.Event.Object["__primaryKey"])
	}

	// Give the live subscription a brief moment to register before the
	// follow-up Publish, matching the pattern used by the other SSE tests.
	waitForSubscriber(t, b, 1, 500*time.Millisecond)

	b.Publish(funnel.BroadcastEvent{
		Type:       "CREATE",
		ObjectType: "order",
		PrimaryKey: "o-13",
		Sequence:   13,
		Properties: map[string]interface{}{"status": "NEW"},
		EditedAt:   time.Now(),
	})

	third := expectIDFrame(t, framesCh, 2*time.Second)
	if third.ID != "13" {
		t.Errorf("third id = %q, want 13", third.ID)
	}
	if pk, _ := third.Event.Object["__primaryKey"].(string); pk != "o-13" {
		t.Errorf("third __primaryKey = %v, want o-13", third.Event.Object["__primaryKey"])
	}

	select {
	case unexpected := <-framesCh:
		t.Errorf("unexpected extra event: %+v", unexpected)
	case <-time.After(150 * time.Millisecond):
	}

	cancel()
	wg.Wait()
}

// TestSSEReplayFromLastEventIDQueryParam is the US-307 acceptance test for
// the query-param fallback. Browser EventSource cannot set the Last-Event-ID
// HTTP header on an explicitly recreated connection (only on its own
// auto-reconnect cycle), so the canonical web client passes the cursor as
// `?lastEventId=` instead. The server MUST honour either source, and when
// both are supplied the explicit header wins so a malformed query param
// from a stale URL never overrides a fresh header value.
func TestSSEReplayFromLastEventIDQueryParam(t *testing.T) {
	const rid = "rid-order-replay-qp"
	lookup := &stubObjectSetLookup{byRid: map[string]SubscriptionSpec{
		rid: {ObjectType: "order"},
	}}

	b := funnel.NewBroadcast()
	handler := NewSubscribeSSEHandler(lookup, b)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe", handler.ServeHTTP)
	srv := httptest.NewServer(r)
	defer srv.Close()

	for seq := uint64(20); seq <= 22; seq++ {
		b.Publish(funnel.BroadcastEvent{
			Type:       "CREATE",
			ObjectType: "order",
			PrimaryKey: "o-" + strconv.FormatUint(seq, 10),
			Sequence:   seq,
			Properties: map[string]interface{}{"status": "NEW"},
			EditedAt:   time.Now(),
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v2/ontologies/northwind/objectSets/"+rid+"/subscribe?lastEventId=20", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	// NB: NO Last-Event-ID header — verifying the query-param fallback path.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	framesCh := make(chan sseIDFrame, 16)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		reader := bufio.NewReader(resp.Body)
		var currentID string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(line, "id: ") {
				currentID = strings.TrimPrefix(line, "id: ")
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimPrefix(line, "data: ")
			var evt sseEventFrame
			if err := json.Unmarshal([]byte(payload), &evt); err != nil {
				return
			}
			select {
			case framesCh <- sseIDFrame{ID: currentID, Event: evt}:
			case <-ctx.Done():
				return
			}
		}
	}()

	first := expectIDFrame(t, framesCh, 2*time.Second)
	if first.ID != "21" {
		t.Errorf("first id = %q, want 21", first.ID)
	}
	second := expectIDFrame(t, framesCh, 2*time.Second)
	if second.ID != "22" {
		t.Errorf("second id = %q, want 22", second.ID)
	}

	select {
	case unexpected := <-framesCh:
		t.Errorf("unexpected extra event: %+v", unexpected)
	case <-time.After(150 * time.Millisecond):
	}

	cancel()
	wg.Wait()
}

// TestSSEReplayHeaderOverridesQueryParam verifies the precedence rule when a
// caller supplies both the Last-Event-ID header AND the ?lastEventId= query
// param: the header wins, because the header is the SSE-protocol-canonical
// channel and EventSource always sends the freshest known cursor on its own
// auto-reconnects, while a stale URL carrying the query param can outlive a
// browser-initiated retry.
func TestSSEReplayHeaderOverridesQueryParam(t *testing.T) {
	const rid = "rid-order-replay-precedence"
	lookup := &stubObjectSetLookup{byRid: map[string]SubscriptionSpec{
		rid: {ObjectType: "order"},
	}}

	b := funnel.NewBroadcast()
	handler := NewSubscribeSSEHandler(lookup, b)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe", handler.ServeHTTP)
	srv := httptest.NewServer(r)
	defer srv.Close()

	for seq := uint64(30); seq <= 33; seq++ {
		b.Publish(funnel.BroadcastEvent{
			Type:       "CREATE",
			ObjectType: "order",
			PrimaryKey: "o-" + strconv.FormatUint(seq, 10),
			Sequence:   seq,
			Properties: map[string]interface{}{},
			EditedAt:   time.Now(),
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Header asks for replay from 32; query param (a stale URL) asks from 30.
	// Header MUST win → only seq 33 is replayed, NOT 31, 32.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v2/ontologies/northwind/objectSets/"+rid+"/subscribe?lastEventId=30", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Last-Event-ID", "32")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	framesCh := make(chan sseIDFrame, 16)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		reader := bufio.NewReader(resp.Body)
		var currentID string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(line, "id: ") {
				currentID = strings.TrimPrefix(line, "id: ")
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimPrefix(line, "data: ")
			var evt sseEventFrame
			if err := json.Unmarshal([]byte(payload), &evt); err != nil {
				return
			}
			select {
			case framesCh <- sseIDFrame{ID: currentID, Event: evt}:
			case <-ctx.Done():
				return
			}
		}
	}()

	got := expectIDFrame(t, framesCh, 2*time.Second)
	if got.ID != "33" {
		t.Errorf("only id = %q, want 33", got.ID)
	}

	select {
	case unexpected := <-framesCh:
		t.Errorf("unexpected extra event after seq 33: %+v", unexpected)
	case <-time.After(150 * time.Millisecond):
	}

	cancel()
	wg.Wait()
}

// TestSSEMalformedLastEventIDDegradesToZero verifies the documented "broken
// cursor never silently disables replay" guarantee. A client supplying a
// non-numeric Last-Event-ID header (or query param) should still receive the
// full ring buffer — fromSeq degrades to 0 rather than the request failing.
func TestSSEMalformedLastEventIDDegradesToZero(t *testing.T) {
	const rid = "rid-order-replay-malformed"
	lookup := &stubObjectSetLookup{byRid: map[string]SubscriptionSpec{
		rid: {ObjectType: "order"},
	}}

	b := funnel.NewBroadcast()
	handler := NewSubscribeSSEHandler(lookup, b)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe", handler.ServeHTTP)
	srv := httptest.NewServer(r)
	defer srv.Close()

	b.Publish(funnel.BroadcastEvent{
		Type: "CREATE", ObjectType: "order", PrimaryKey: "o-40", Sequence: 40,
		Properties: map[string]interface{}{}, EditedAt: time.Now(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v2/ontologies/northwind/objectSets/"+rid+"/subscribe?lastEventId=not-a-number", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Last-Event-ID", "also-garbage")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (malformed cursor must degrade, not fail)", resp.StatusCode)
	}

	framesCh := make(chan sseIDFrame, 16)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		reader := bufio.NewReader(resp.Body)
		var currentID string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(line, "id: ") {
				currentID = strings.TrimPrefix(line, "id: ")
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimPrefix(line, "data: ")
			var evt sseEventFrame
			if err := json.Unmarshal([]byte(payload), &evt); err != nil {
				return
			}
			select {
			case framesCh <- sseIDFrame{ID: currentID, Event: evt}:
			case <-ctx.Done():
				return
			}
		}
	}()

	got := expectIDFrame(t, framesCh, 2*time.Second)
	if got.ID != "40" {
		t.Errorf("replay id = %q, want 40 (malformed cursor → fromSeq=0)", got.ID)
	}

	cancel()
	wg.Wait()
}

// TestSSEReplayRingBufferSkipsSeenEvents verifies that a client requesting
// replay from a sequence that is already beyond the ring buffer tail
// receives no replay events (only live events after reconnect).
func TestSSEReplayRingBufferSkipsSeenEvents(t *testing.T) {
	const rid = "rid-order-replay-2"
	lookup := &stubObjectSetLookup{byRid: map[string]SubscriptionSpec{
		rid: {ObjectType: "order"},
	}}
	b := funnel.NewBroadcast()
	handler := NewSubscribeSSEHandler(lookup, b)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe", handler.ServeHTTP)
	srv := httptest.NewServer(r)
	defer srv.Close()

	b.Publish(funnel.BroadcastEvent{
		Type: "CREATE", ObjectType: "order", PrimaryKey: "o-5", Sequence: 5,
		Properties: map[string]interface{}{}, EditedAt: time.Now(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v2/ontologies/northwind/objectSets/"+rid+"/subscribe", nil)
	// Client already saw sequence 100 (> any ring entry). No replay expected.
	req.Header.Set("Last-Event-ID", "100")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	framesCh := make(chan sseIDFrame, 8)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		reader := bufio.NewReader(resp.Body)
		var currentID string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(line, "id: ") {
				currentID = strings.TrimPrefix(line, "id: ")
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimPrefix(line, "data: ")
			var evt sseEventFrame
			if err := json.Unmarshal([]byte(payload), &evt); err != nil {
				return
			}
			select {
			case framesCh <- sseIDFrame{ID: currentID, Event: evt}:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Within a short window the client must NOT receive the pre-seeded
	// sequence-5 event — it was acknowledged.
	select {
	case unexpected := <-framesCh:
		t.Errorf("unexpected replay event: %+v", unexpected)
	case <-time.After(200 * time.Millisecond):
	}

	cancel()
	wg.Wait()
}

// sseIDFrame couples an SSE "id:" line with the following "data:" payload so
// tests can assert id-event pairing.
type sseIDFrame struct {
	ID    string
	Event sseEventFrame
}

func expectIDFrame(t *testing.T, ch <-chan sseIDFrame, timeout time.Duration) sseIDFrame {
	t.Helper()
	select {
	case f := <-ch:
		return f
	case <-time.After(timeout):
		t.Fatal("timed out waiting for SSE id frame")
		return sseIDFrame{}
	}
}

// TestSSEHeartbeatAndLimit is the US-058 red-first acceptance test. It
// exercises two orthogonal behaviours of the SSE subscribe handler:
//
//  1. Heartbeat: every configured interval the server emits a `:ping` SSE
//     comment line so idle clients and intermediaries can detect a live
//     connection without having to wait for application events.
//  2. Per-user connection cap: when the caller already holds MaxConnections
//     open subscriptions for the same authenticated user, the next request
//     is rejected with HTTP 429 and a RESOURCE_EXHAUSTED error body. A
//     different user is unaffected.
//
// The test configures a 50ms heartbeat and a cap of 2 so both behaviours
// land deterministically inside normal go-test timeouts.
func TestSSEHeartbeatAndLimit(t *testing.T) {
	const rid = "rid-heartbeat-1"
	lookup := &stubObjectSetLookup{byRid: map[string]SubscriptionSpec{
		rid: {ObjectType: "order"},
	}}

	b := funnel.NewBroadcast()
	handler := NewSubscribeSSEHandler(lookup, b)
	handler.SetHeartbeatInterval(50 * time.Millisecond)
	handler.SetMaxConnectionsPerUser(2)

	r := chi.NewRouter()
	// The test uses an X-Test-User header as a lightweight stand-in for the
	// real auth middleware — the SSE handler only needs an auth.User on the
	// request context to key its connection counter.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if uid := req.Header.Get("X-Test-User"); uid != "" {
				req = req.WithContext(authWithTestUser(req.Context(), uid))
			}
			next.ServeHTTP(w, req)
		})
	})
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe", handler.ServeHTTP)

	srv := httptest.NewServer(r)
	defer srv.Close()

	url := srv.URL + "/api/v2/ontologies/northwind/objectSets/" + rid + "/subscribe"

	// --- Part 1: heartbeat ----------------------------------------------
	// Open a single subscription for "alice" and scan the raw body for a
	// `:ping` comment line. The server must emit one within a small
	// multiple of the heartbeat interval (50ms configured above) even
	// though no events are ever published.
	ctxHB, cancelHB := context.WithCancel(context.Background())
	defer cancelHB()
	reqHB, err := http.NewRequestWithContext(ctxHB, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	reqHB.Header.Set("X-Test-User", "alice")
	respHB, err := http.DefaultClient.Do(reqHB)
	if err != nil {
		t.Fatalf("do heartbeat: %v", err)
	}
	defer respHB.Body.Close()
	if respHB.StatusCode != http.StatusOK {
		t.Fatalf("heartbeat status = %d, want 200", respHB.StatusCode)
	}

	lineCh := make(chan string, 32)
	var wgHB sync.WaitGroup
	wgHB.Add(1)
	go func() {
		defer wgHB.Done()
		reader := bufio.NewReader(respHB.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			select {
			case lineCh <- strings.TrimRight(line, "\r\n"):
			case <-ctxHB.Done():
				return
			}
		}
	}()

	heartbeatDeadline := time.After(1 * time.Second)
	sawPing := false
	for !sawPing {
		select {
		case line := <-lineCh:
			if strings.HasPrefix(line, ":ping") {
				sawPing = true
			}
		case <-heartbeatDeadline:
			t.Fatalf("timed out waiting for :ping heartbeat line")
		}
	}

	// --- Part 2: connection cap -----------------------------------------
	// Alice already holds one active connection. Open one more (limit=2),
	// then assert the third is rejected with 429. Each passing subscription
	// must stay open across the cap check so the handler's counter actually
	// shows "2 in flight" when the third request arrives.
	ctxCap, cancelCap := context.WithCancel(context.Background())
	defer cancelCap()

	openAlice2, err := http.NewRequestWithContext(ctxCap, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new alice2 request: %v", err)
	}
	openAlice2.Header.Set("X-Test-User", "alice")
	respAlice2, err := http.DefaultClient.Do(openAlice2)
	if err != nil {
		t.Fatalf("do alice2: %v", err)
	}
	defer respAlice2.Body.Close()
	if respAlice2.StatusCode != http.StatusOK {
		t.Fatalf("alice2 status = %d, want 200", respAlice2.StatusCode)
	}
	// Drain alice2 body concurrently so the server-side handler stays
	// unblocked on its writes.
	go io.Copy(io.Discard, respAlice2.Body)
	// Give the handler a brief moment to register the second subscription
	// into the connection counter before we fire the capped third request.
	waitForSubscriber(t, b, 2, 200*time.Millisecond)

	alice3Req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new alice3 request: %v", err)
	}
	alice3Req.Header.Set("X-Test-User", "alice")
	respAlice3, err := http.DefaultClient.Do(alice3Req)
	if err != nil {
		t.Fatalf("do alice3: %v", err)
	}
	defer respAlice3.Body.Close()
	if respAlice3.StatusCode != http.StatusTooManyRequests {
		body, _ := io.ReadAll(respAlice3.Body)
		t.Fatalf("alice3 status = %d, want 429 (body=%s)", respAlice3.StatusCode, string(body))
	}
	body3, _ := io.ReadAll(respAlice3.Body)
	if !strings.Contains(string(body3), "SSEConnectionLimitExceeded") {
		t.Errorf("alice3 body = %s, want SSEConnectionLimitExceeded error", string(body3))
	}

	// --- Part 3: isolation across users ---------------------------------
	// A different user must NOT be affected by alice's cap. Open and
	// immediately close a carol subscription and assert 200.
	ctxCarol, cancelCarol := context.WithCancel(context.Background())
	carolReq, err := http.NewRequestWithContext(ctxCarol, http.MethodGet, url, nil)
	if err != nil {
		cancelCarol()
		t.Fatalf("new carol request: %v", err)
	}
	carolReq.Header.Set("X-Test-User", "carol")
	respCarol, err := http.DefaultClient.Do(carolReq)
	if err != nil {
		cancelCarol()
		t.Fatalf("do carol: %v", err)
	}
	if respCarol.StatusCode != http.StatusOK {
		cancelCarol()
		respCarol.Body.Close()
		t.Fatalf("carol status = %d, want 200", respCarol.StatusCode)
	}
	cancelCarol()
	respCarol.Body.Close()

	cancelCap()
	cancelHB()
	wgHB.Wait()
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
