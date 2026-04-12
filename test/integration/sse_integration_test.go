//go:build integration

package integration_test

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

// stubObjectSetLookup is a narrow fake that satisfies oss.ObjectSetLookup for
// the SSE integration test. The handler only needs a base ObjectType to
// filter inbound funnel events on, so we skip the full ObjectSet store.
type stubObjectSetLookup struct {
	byRid map[string]oss.SubscriptionSpec
}

func (s *stubObjectSetLookup) ResolveSubscription(rid string) (oss.SubscriptionSpec, error) {
	if s == nil || s.byRid == nil {
		return oss.SubscriptionSpec{}, oss.ErrObjectSetNotFound
	}
	spec, ok := s.byRid[rid]
	if !ok {
		return oss.SubscriptionSpec{}, oss.ErrObjectSetNotFound
	}
	return spec, nil
}

type sseIntegrationFrame struct {
	EventType string                 `json:"eventType"`
	Object    map[string]interface{} `json:"object"`
}

// TestSSEIntegration_SubscribeApplyTenActions is the US-058 integration
// acceptance test: it stands up the SSE subscribe handler end-to-end behind
// a real HTTP server (chi router + httptest) wired to a live
// funnel.Broadcast hub, subscribes a client, then simulates the effect of
// ten applied actions by firing ten CREATE BroadcastEvents through the
// hub. The test asserts that the subscriber receives exactly ten SSE data
// frames with the expected primary keys in order — mirroring the full
// production wiring (ActionExecutor → funnel.Consumer → broadcast.Publish)
// end-to-end at the HTTP boundary without requiring a real NATS / PG
// stack under the integration tag.
//
// The heartbeat interval is explicitly set to zero so the test reader does
// not have to tolerate `:ping` comment lines interleaved with the expected
// data frames; a heartbeat-enabled scenario is covered by the unit test
// TestSSEHeartbeatAndLimit in pkg/oss/subscribe_sse_test.go.
func TestSSEIntegration_SubscribeApplyTenActions(t *testing.T) {
	const rid = "rid-sse-integration-1"
	lookup := &stubObjectSetLookup{byRid: map[string]oss.SubscriptionSpec{
		rid: {ObjectType: "order"},
	}}

	b := funnel.NewBroadcast()
	handler := oss.NewSubscribeSSEHandler(lookup, b)
	// Disable heartbeat for this integration scenario — we assert exact
	// frame counts below and `:ping` comment lines would be noise.
	handler.SetHeartbeatInterval(0)
	handler.SetMaxConnectionsPerUser(10)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe", handler.ServeHTTP)

	srv := httptest.NewServer(r)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	url := srv.URL + "/api/v2/ontologies/northwind/objectSets/" + rid + "/subscribe"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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

	framesCh := make(chan sseIntegrationFrame, 32)
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
			var evt sseIntegrationFrame
			if err := json.Unmarshal([]byte(payload), &evt); err != nil {
				t.Errorf("json decode %q: %v", payload, err)
				return
			}
			select {
			case framesCh <- evt:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Give the handler goroutine time to reach broadcast.Subscribe before
	// the publisher fires — a short sleep is sufficient because the
	// http.Client.Do call has already returned the headers so the server
	// handler is past its headers write and into the fan-out loop.
	time.Sleep(50 * time.Millisecond)

	const actionCount = 10
	for i := 0; i < actionCount; i++ {
		b.Publish(funnel.BroadcastEvent{
			Type:       "CREATE",
			ObjectType: "order",
			PrimaryKey: fmt.Sprintf("o-%02d", i+1),
			Sequence:   uint64(100 + i),
			Properties: map[string]interface{}{
				"status": "NEW",
				"index":  i,
			},
			EditedAt: time.Now(),
		})
	}

	// Collect the ten expected frames, one per applied action, in order.
	received := make([]string, 0, actionCount)
	deadline := time.After(3 * time.Second)
	for len(received) < actionCount {
		select {
		case evt := <-framesCh:
			if evt.EventType != "ADDED_OR_UPDATED" {
				t.Errorf("frame %d eventType = %q, want ADDED_OR_UPDATED", len(received), evt.EventType)
			}
			pk, _ := evt.Object["__primaryKey"].(string)
			received = append(received, pk)
		case <-deadline:
			t.Fatalf("timed out after %d/%d SSE frames: %v", len(received), actionCount, received)
		}
	}

	// Assert primary key order matches the publish order.
	for i, pk := range received {
		want := fmt.Sprintf("o-%02d", i+1)
		if pk != want {
			t.Errorf("frame %d primaryKey = %q, want %q", i, pk, want)
		}
	}

	// Confirm no surplus frames arrive in a short window after the last
	// expected event.
	select {
	case extra := <-framesCh:
		t.Errorf("unexpected extra SSE frame: %+v", extra)
	case <-time.After(150 * time.Millisecond):
	}

	cancel()
	wg.Wait()
}
