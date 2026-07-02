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

// TestBDD_SSESubscribe_InWhereFilter is the PR #293 follow-up: the Bleve
// converter learned the Foundry `in` operator but the in-memory matcher
// (where.MatchClause) used by the SSE/WS subscription paths still treated
// it as unsupported, silently dropping every event for subscriptions whose
// stored ObjectSet filters with `in`.
//
// Scenario (Given → When → Then):
//
//	Given a stored ObjectSet over "order" with where
//	      {"type":"in","field":"status","value":["SHIPPED","DELIVERED"]}
//	  And an SSE subscriber attached to it
//	When  funnel broadcasts orders with status SHIPPED / PENDING /
//	      DELIVERED / CANCELLED
//	Then  the SSE stream delivers exactly the SHIPPED and DELIVERED
//	      events, in publish order, and drops the other two
func TestBDD_SSESubscribe_InWhereFilter(t *testing.T) {
	const rid = "rid-order-in-1"
	clauseJSON := `{"type": "in", "field": "status", "value": ["SHIPPED", "DELIVERED"]}`
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

	publish := func(pk, status string) {
		b.Publish(funnel.BroadcastEvent{
			Type:       "MODIFY",
			ObjectType: "order",
			PrimaryKey: pk,
			Properties: map[string]interface{}{"status": status},
			EditedAt:   time.Now(),
		})
	}
	publish("o-1", "SHIPPED")   // MATCH — in candidate list
	publish("o-2", "PENDING")   // DROP
	publish("o-3", "DELIVERED") // MATCH — in candidate list
	publish("o-4", "CANCELLED") // DROP

	first := expectEvent(t, eventsCh, 2*time.Second)
	if pk, _ := first.Object["__primaryKey"].(string); pk != "o-1" {
		t.Errorf("first __primaryKey = %v, want o-1", first.Object["__primaryKey"])
	}
	second := expectEvent(t, eventsCh, 2*time.Second)
	if pk, _ := second.Object["__primaryKey"].(string); pk != "o-3" {
		t.Errorf("second __primaryKey = %v, want o-3", second.Object["__primaryKey"])
	}

	select {
	case unexpected := <-eventsCh:
		t.Errorf("unexpected extra event: %+v", unexpected)
	case <-time.After(150 * time.Millisecond):
		// good — the PENDING / CANCELLED events were dropped
	}

	cancel()
	wg.Wait()
}
