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
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/funnel"
)

// us459EventFrame is the US-459 canonical SSE payload shape. The frame must
// expose seq (NATS-stream-sequence — strictly increasing), type (lower-case
// created|modified|deleted per the PRD), rid (the source object's RID,
// composed of objectType + primaryKey by the handler), and properties (the
// event's property map after policy filtering). The legacy eventType / object
// keys remain for the existing React hook and are not asserted here.
type us459EventFrame struct {
	Seq        uint64                 `json:"seq"`
	Type       string                 `json:"type"`
	RID        string                 `json:"rid"`
	Properties map[string]interface{} `json:"properties"`
}

// TestSSE_US459_SinceParamAliasReplaysAfterCursor is the red-first acceptance
// test for the US-459 "?since={eventId}" query parameter. The PRD names this
// parameter explicitly; the legacy ?lastEventId= continues to work for the
// existing React EventSource client (it is the canonical alias used by the
// browser-driven auto-reconnect path), but new SDK callers MUST be able to
// resume by setting ?since=N and receive only events with seq > N.
func TestSSE_US459_SinceParamAliasReplaysAfterCursor(t *testing.T) {
	const rid = "rid-us459-since"
	lookup := &stubObjectSetLookup{byRid: map[string]SubscriptionSpec{
		rid: {ObjectType: "order"},
	}}

	b := funnel.NewBroadcast()
	handler := NewSubscribeSSEHandler(lookup, b)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe", handler.ServeHTTP)
	srv := httptest.NewServer(r)
	defer srv.Close()

	for seq := uint64(50); seq <= 52; seq++ {
		b.Publish(funnel.BroadcastEvent{
			Type:       "CREATE",
			ObjectType: "order",
			PrimaryKey: "o-" + itoaSeq(seq),
			Sequence:   seq,
			Properties: map[string]interface{}{"status": "NEW"},
			EditedAt:   time.Now(),
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v2/ontologies/northwind/objectSets/"+rid+"/subscribe?since=50", nil)
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

	gotSeqs := drainSeqs(t, ctx, resp.Body, 2, 2*time.Second)
	if len(gotSeqs) != 2 || gotSeqs[0] != 51 || gotSeqs[1] != 52 {
		t.Errorf("replayed seqs = %v, want [51 52]", gotSeqs)
	}
}

// TestSSE_US459_CursorOutsideWindowReturns410 is the red-first acceptance test
// for the 5-minute replay window. When a client reconnects with a cursor
// older than every event currently retained in the hub (because the time
// window dropped the original event), the handler MUST emit HTTP 410 Gone
// rather than silently skipping the missing events. The body must carry an
// apierror payload so the SDK can surface a typed "resume window exceeded"
// error to the application.
func TestSSE_US459_CursorOutsideWindowReturns410(t *testing.T) {
	const rid = "rid-us459-window"
	lookup := &stubObjectSetLookup{byRid: map[string]SubscriptionSpec{
		rid: {ObjectType: "order"},
	}}

	// 1-minute window so the test can synthesise eviction without sleeping.
	b := funnel.NewBroadcastWithWindow(1 * time.Minute)
	handler := NewSubscribeSSEHandler(lookup, b)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe", handler.ServeHTTP)
	srv := httptest.NewServer(r)
	defer srv.Close()

	// Stale event: published with an EditedAt far enough in the past that the
	// window prune drops it. Note: the prune runs inside Publish AND inside
	// SubscribeWithReplayWindow, so eviction is deterministic regardless of
	// scheduling.
	b.Publish(funnel.BroadcastEvent{
		Type:       "CREATE",
		ObjectType: "order",
		PrimaryKey: "o-old",
		Sequence:   10,
		Properties: map[string]interface{}{"status": "OLD"},
		EditedAt:   time.Now().Add(-10 * time.Minute),
	})
	// Fresh event keeps the lastSeq counter non-zero and proves the hub still
	// produced events newer than the client's cursor.
	b.Publish(funnel.BroadcastEvent{
		Type:       "CREATE",
		ObjectType: "order",
		PrimaryKey: "o-new",
		Sequence:   20,
		Properties: map[string]interface{}{"status": "NEW"},
		EditedAt:   time.Now(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v2/ontologies/northwind/objectSets/"+rid+"/subscribe?since=10", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusGone {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 410 (body=%s)", resp.StatusCode, string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "SSEReplayWindowExceeded") {
		t.Errorf("body = %s, want SSEReplayWindowExceeded", string(body))
	}
}

// TestSSE_US459_ConcurrentAckReplay covers the "并发 ack" acceptance criterion.
// Three subscribers reconnect simultaneously with the same since cursor and
// each MUST receive the full replay tail exactly once, in seq-monotonic order,
// without any client interfering with another. The test then publishes a live
// event after subscription and asserts every client observes it once with the
// strictly-increasing seq.
func TestSSE_US459_ConcurrentAckReplay(t *testing.T) {
	const rid = "rid-us459-concurrent"
	lookup := &stubObjectSetLookup{byRid: map[string]SubscriptionSpec{
		rid: {ObjectType: "order"},
	}}

	b := funnel.NewBroadcast()
	handler := NewSubscribeSSEHandler(lookup, b)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe", handler.ServeHTTP)
	srv := httptest.NewServer(r)
	defer srv.Close()

	// Pre-seed three events: 100, 101, 102 → subscribers will replay 101..102
	// after providing since=100.
	for seq := uint64(100); seq <= 102; seq++ {
		b.Publish(funnel.BroadcastEvent{
			Type:       "CREATE",
			ObjectType: "order",
			PrimaryKey: "o-" + itoaSeq(seq),
			Sequence:   seq,
			Properties: map[string]interface{}{"status": "NEW"},
			EditedAt:   time.Now(),
		})
	}

	const numClients = 3
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type clientResult struct {
		seqs []uint64
		err  error
	}
	results := make(chan clientResult, numClients)
	var wg sync.WaitGroup
	var readyCount atomic.Int32

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet,
				srv.URL+"/api/v2/ontologies/northwind/objectSets/"+rid+"/subscribe?since=100", nil)
			if err != nil {
				results <- clientResult{err: err}
				return
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				results <- clientResult{err: err}
				return
			}
			defer resp.Body.Close()
			readyCount.Add(1)
			// Each client expects 3 events: replay 101, 102 + live 103.
			seqs := drainSeqs(t, ctx, resp.Body, 3, 3*time.Second)
			results <- clientResult{seqs: seqs}
		}()
	}

	// Wait until every client has its HTTP response and its goroutine is
	// reading the body before we fire the live event so all three subscribers
	// are guaranteed to be registered with the hub when seq 103 publishes.
	deadline := time.Now().Add(2 * time.Second)
	for readyCount.Load() < int32(numClients) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if readyCount.Load() < int32(numClients) {
		t.Fatalf("only %d/%d clients ready", readyCount.Load(), numClients)
	}
	// Brief settle so the server-side handlers have reached the broadcast
	// subscribe call after writing their replay frames.
	time.Sleep(80 * time.Millisecond)

	b.Publish(funnel.BroadcastEvent{
		Type:       "MODIFY",
		ObjectType: "order",
		PrimaryKey: "o-103",
		Sequence:   103,
		Properties: map[string]interface{}{"status": "SHIPPED"},
		EditedAt:   time.Now(),
	})

	wg.Wait()
	close(results)

	for res := range results {
		if res.err != nil {
			t.Errorf("client error: %v", res.err)
			continue
		}
		if len(res.seqs) != 3 {
			t.Errorf("got seqs = %v, want 3 entries", res.seqs)
			continue
		}
		if res.seqs[0] != 101 || res.seqs[1] != 102 || res.seqs[2] != 103 {
			t.Errorf("got seqs = %v, want [101 102 103]", res.seqs)
		}
	}
}

// TestSSE_US459_EventPayloadShape is the red-first acceptance test for the
// PRD's `{seq, type: created|modified|deleted, rid, properties}` event-payload
// contract. Each data frame's JSON object MUST expose those four keys (in
// addition to any backwards-compatible legacy keys) so SDK clients can parse
// the canonical shape without knowing the legacy ADDED_OR_UPDATED / DELETED
// taxonomy. Type values normalise to lower-case verbs.
func TestSSE_US459_EventPayloadShape(t *testing.T) {
	const rid = "rid-us459-shape"
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

	waitForSubscriber(t, b, 1, 500*time.Millisecond)

	b.Publish(funnel.BroadcastEvent{
		Type:       "CREATE",
		ObjectType: "order",
		PrimaryKey: "o-create",
		Sequence:   1,
		Properties: map[string]interface{}{"status": "NEW"},
		EditedAt:   time.Now(),
	})
	b.Publish(funnel.BroadcastEvent{
		Type:       "MODIFY",
		ObjectType: "order",
		PrimaryKey: "o-mod",
		Sequence:   2,
		Properties: map[string]interface{}{"status": "SHIPPED"},
		EditedAt:   time.Now(),
	})
	b.Publish(funnel.BroadcastEvent{
		Type:       "DELETE",
		ObjectType: "order",
		PrimaryKey: "o-del",
		Sequence:   3,
		Properties: nil,
		EditedAt:   time.Now(),
	})

	frames := drainUS459Frames(t, ctx, resp.Body, 3, 2*time.Second)
	if len(frames) != 3 {
		t.Fatalf("got %d frames, want 3", len(frames))
	}

	wantTypes := []string{"created", "modified", "deleted"}
	wantSeqs := []uint64{1, 2, 3}
	wantRids := []string{"order:o-create", "order:o-mod", "order:o-del"}
	for i, f := range frames {
		if f.Seq != wantSeqs[i] {
			t.Errorf("frame[%d] seq = %d, want %d", i, f.Seq, wantSeqs[i])
		}
		if f.Type != wantTypes[i] {
			t.Errorf("frame[%d] type = %q, want %q", i, f.Type, wantTypes[i])
		}
		if f.RID != wantRids[i] {
			t.Errorf("frame[%d] rid = %q, want %q", i, f.RID, wantRids[i])
		}
	}
	// Strictly increasing seq.
	for i := 1; i < len(frames); i++ {
		if frames[i].Seq <= frames[i-1].Seq {
			t.Errorf("seq not strictly increasing at %d: %d <= %d", i, frames[i].Seq, frames[i-1].Seq)
		}
	}
}

// drainSeqs reads SSE id-lines from an open response body until it has
// collected `count` distinct ids or the deadline expires. Returns the seq
// numbers in arrival order.
func drainSeqs(t *testing.T, ctx context.Context, body io.Reader, count int, timeout time.Duration) []uint64 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	reader := bufio.NewReader(body)
	seqs := make([]uint64, 0, count)
	for len(seqs) < count && time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, "id: ") {
			continue
		}
		raw := strings.TrimPrefix(line, "id: ")
		var s uint64
		if _, err := jsonScanUint64(raw, &s); err != nil {
			continue
		}
		seqs = append(seqs, s)
	}
	return seqs
}

// drainUS459Frames parses the JSON payload after each SSE data: line and
// returns the parsed US-459 canonical view.
func drainUS459Frames(t *testing.T, ctx context.Context, body io.Reader, count int, timeout time.Duration) []us459EventFrame {
	t.Helper()
	deadline := time.Now().Add(timeout)
	reader := bufio.NewReader(body)
	frames := make([]us459EventFrame, 0, count)
	for len(frames) < count && time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := strings.TrimPrefix(line, "data: ")
		var f us459EventFrame
		if err := json.Unmarshal([]byte(raw), &f); err != nil {
			continue
		}
		frames = append(frames, f)
	}
	return frames
}

func jsonScanUint64(s string, out *uint64) (int, error) {
	if s == "" {
		return 0, io.EOF
	}
	var v uint64
	for i, r := range s {
		if r < '0' || r > '9' {
			if i == 0 {
				return 0, io.EOF
			}
			break
		}
		v = v*10 + uint64(r-'0')
	}
	*out = v
	return len(s), nil
}

func itoaSeq(s uint64) string {
	// Avoid importing strconv twice in this file — keep the helper local so
	// the test file remains self-contained.
	if s == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for s > 0 {
		i--
		b[i] = byte('0' + s%10)
		s /= 10
	}
	return string(b[i:])
}
