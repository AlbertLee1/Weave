//go:build integration

package phase7_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oss"
)

// stubSSELookup is a narrow fake that satisfies oss.ObjectSetLookup for the
// SSE subscription test. The handler only needs a base ObjectType to filter
// inbound funnel events, so we skip the full ObjectSet store.
type stubSSELookup struct {
	byRid map[string]oss.SubscriptionSpec
}

func (s *stubSSELookup) ResolveSubscription(rid string) (oss.SubscriptionSpec, error) {
	if s == nil || s.byRid == nil {
		return oss.SubscriptionSpec{}, oss.ErrObjectSetNotFound
	}
	spec, ok := s.byRid[rid]
	if !ok {
		return oss.SubscriptionSpec{}, oss.ErrObjectSetNotFound
	}
	return spec, nil
}

// sseFrame is the wire shape of each SSE data payload.
type sseFrame struct {
	EventType string                 `json:"eventType"`
	Object    map[string]interface{} `json:"object"`
}

// TestSSESubscription_EndToEnd is the US-072 acceptance test.
//
// Part 1: Subscribe → apply 10 actions → assert 10 SSE events arrive in
// correct order.
//
// Part 2: Disconnect → apply 5 more actions → reconnect with Last-Event-ID
// set to the last sequence received in Part 1 → assert all 5 events are
// delivered via replay with no duplicates and no gaps.
func TestSSESubscription_EndToEnd(t *testing.T) {
	const objectSetRid = "rid-sse-phase7-e2e"
	const objectType = "ticket"

	lookup := &stubSSELookup{byRid: map[string]oss.SubscriptionSpec{
		objectSetRid: {ObjectType: objectType},
	}}

	// Broadcast with default ring buffer (1024) so replay works.
	broadcast := funnel.NewBroadcast()

	handler := oss.NewSubscribeSSEHandler(lookup, broadcast)
	handler.SetHeartbeatInterval(0) // disable heartbeat noise
	handler.SetMaxConnectionsPerUser(10)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe", handler.ServeHTTP)

	srv := httptest.NewServer(r)
	defer srv.Close()

	subscribeURL := srv.URL + "/api/v2/ontologies/test/objectSets/" + objectSetRid + "/subscribe"

	// =================================================================
	// Part 1: Subscribe → apply 10 actions → assert 10 events in order
	// =================================================================
	t.Run("subscribe_and_receive_10_events", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, subscribeURL, nil)
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

		type sseMsg struct {
			id    string // the SSE id: line value
			frame sseFrame
		}
		msgCh := make(chan sseMsg, 32)

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
				var f sseFrame
				if err := json.Unmarshal([]byte(payload), &f); err != nil {
					t.Errorf("json decode %q: %v", payload, err)
					return
				}
				select {
				case msgCh <- sseMsg{id: currentID, frame: f}:
					currentID = ""
				case <-ctx.Done():
					return
				}
			}
		}()

		// Wait for the subscriber to register before publishing.
		time.Sleep(50 * time.Millisecond)

		const actionCount = 10
		for i := 0; i < actionCount; i++ {
			broadcast.Publish(funnel.BroadcastEvent{
				Type:       "CREATE",
				ObjectType: objectType,
				PrimaryKey: fmt.Sprintf("TKT-%03d", i+1),
				Sequence:   uint64(100 + i),
				Properties: map[string]interface{}{
					"status": "OPEN",
					"index":  i,
				},
				EditedAt: time.Now(),
			})
		}

		// Collect 10 frames.
		received := make([]sseMsg, 0, actionCount)
		deadline := time.After(3 * time.Second)
		for len(received) < actionCount {
			select {
			case msg := <-msgCh:
				received = append(received, msg)
			case <-deadline:
				t.Fatalf("timed out after %d/%d SSE frames", len(received), actionCount)
			}
		}

		// Assert order and content.
		for i, msg := range received {
			wantPK := fmt.Sprintf("TKT-%03d", i+1)
			wantSeqID := fmt.Sprintf("%d", 100+i)
			pk, _ := msg.frame.Object["__primaryKey"].(string)
			if pk != wantPK {
				t.Errorf("frame %d: primaryKey = %q, want %q", i, pk, wantPK)
			}
			if msg.frame.EventType != "ADDED_OR_UPDATED" {
				t.Errorf("frame %d: eventType = %q, want ADDED_OR_UPDATED", i, msg.frame.EventType)
			}
			if msg.id != wantSeqID {
				t.Errorf("frame %d: SSE id = %q, want %q", i, msg.id, wantSeqID)
			}
		}

		// No extra frames.
		select {
		case extra := <-msgCh:
			t.Errorf("unexpected extra frame: %+v", extra)
		case <-time.After(150 * time.Millisecond):
		}

		cancel()
		wg.Wait()
	})

	// =================================================================
	// Part 2: Disconnect → apply 5 more → reconnect with Last-Event-ID
	// =================================================================
	t.Run("reconnect_with_last_event_id", func(t *testing.T) {
		// At this point, events with Sequence 100-109 are in the ring buffer
		// from Part 1. The last received event had Sequence=109. Now publish
		// 5 more events while no subscriber is connected.
		const moreCount = 5
		for i := 0; i < moreCount; i++ {
			broadcast.Publish(funnel.BroadcastEvent{
				Type:       "MODIFY",
				ObjectType: objectType,
				PrimaryKey: fmt.Sprintf("TKT-%03d", 11+i),
				Sequence:   uint64(110 + i),
				Properties: map[string]interface{}{
					"status": "UPDATED",
					"index":  10 + i,
				},
				EditedAt: time.Now(),
			})
		}

		// Small pause to ensure events are fully in the ring buffer.
		time.Sleep(20 * time.Millisecond)

		// Reconnect with Last-Event-ID = 109 (the last sequence from Part 1).
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, subscribeURL, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Last-Event-ID", "109")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}

		type sseMsg struct {
			id    string
			frame sseFrame
		}
		msgCh := make(chan sseMsg, 32)

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
				var f sseFrame
				if err := json.Unmarshal([]byte(payload), &f); err != nil {
					t.Errorf("json decode %q: %v", payload, err)
					return
				}
				select {
				case msgCh <- sseMsg{id: currentID, frame: f}:
					currentID = ""
				case <-ctx.Done():
					return
				}
			}
		}()

		// Collect the 5 replayed events.
		received := make([]sseMsg, 0, moreCount)
		deadline := time.After(3 * time.Second)
		for len(received) < moreCount {
			select {
			case msg := <-msgCh:
				received = append(received, msg)
			case <-deadline:
				t.Fatalf("timed out after %d/%d replay frames", len(received), moreCount)
			}
		}

		// Assert replay delivered exactly the 5 events published while
		// disconnected, with correct order and no duplicates.
		for i, msg := range received {
			wantPK := fmt.Sprintf("TKT-%03d", 11+i)
			wantSeqID := fmt.Sprintf("%d", 110+i)
			pk, _ := msg.frame.Object["__primaryKey"].(string)
			if pk != wantPK {
				t.Errorf("replay frame %d: primaryKey = %q, want %q", i, pk, wantPK)
			}
			if msg.frame.EventType != "ADDED_OR_UPDATED" {
				t.Errorf("replay frame %d: eventType = %q, want ADDED_OR_UPDATED", i, msg.frame.EventType)
			}
			if msg.id != wantSeqID {
				t.Errorf("replay frame %d: SSE id = %q, want %q", i, msg.id, wantSeqID)
			}
		}

		// No extra frames (no duplicates from the first 10 events).
		select {
		case extra := <-msgCh:
			t.Errorf("unexpected extra replay frame: %+v", extra)
		case <-time.After(150 * time.Millisecond):
		}

		cancel()
		wg.Wait()
	})
}
