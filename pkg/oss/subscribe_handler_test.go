package oss_test

import (
	"context"
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

// newSubscribeHandler builds a chi router with the SSE route registered and
// (optionally) the broadcast hub wired. When broadcast is nil the route
// returns 503 to mirror the "history not configured" pattern used elsewhere.
func newSubscribeHandler(t *testing.T, broadcast *funnel.Broadcast) http.Handler {
	t.Helper()
	h := oss.NewHandler(nil)
	if broadcast != nil {
		h.SetBroadcast(broadcast)
	}
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

// readSSEFrame reads bytes off the response body until a full event frame
// (terminated by "\n\n") has arrived or the deadline expires. Returns the
// frame text without the trailing blank line.
func readSSEFrame(t *testing.T, body *bytesPipe, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s := body.String()
		if idx := strings.Index(s, "\n\n"); idx >= 0 {
			frame := s[:idx]
			body.Consume(idx + 2)
			// Skip SSE comment frames (lines starting with ":"), such as
			// the ": connected" keepalive sent by the handler on open.
			if strings.HasPrefix(frame, ":") {
				continue
			}
			return frame
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for SSE frame; buffered=%q", body.String())
	return ""
}

// bytesPipe is a thread-safe in-memory buffer used as the SSE response body.
// It implements http.ResponseWriter via httptest.ResponseRecorder, but the
// recorder buffers writes only after the handler returns — for streaming we
// need to read mid-flight, so the test plugs a pipe-style writer instead.
type bytesPipe struct {
	mu     sync.Mutex
	buf    []byte
	closed bool
}

func (p *bytesPipe) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0, http.ErrBodyNotAllowed
	}
	p.buf = append(p.buf, b...)
	return len(b), nil
}

func (p *bytesPipe) String() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return string(p.buf)
}

func (p *bytesPipe) Consume(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if n >= len(p.buf) {
		p.buf = p.buf[:0]
		return
	}
	p.buf = p.buf[n:]
}

// flushRecorder is an http.ResponseWriter wrapping a bytesPipe that also
// satisfies http.Flusher so the SSE handler can push events immediately.
type flushRecorder struct {
	header http.Header
	pipe   *bytesPipe
	status int
	mu     sync.Mutex
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{
		header: http.Header{},
		pipe:   &bytesPipe{},
	}
}

func (r *flushRecorder) Header() http.Header { return r.header }

func (r *flushRecorder) Write(b []byte) (int, error) {
	r.mu.Lock()
	if r.status == 0 {
		r.status = http.StatusOK
	}
	r.mu.Unlock()
	return r.pipe.Write(b)
}

func (r *flushRecorder) WriteHeader(code int) {
	r.mu.Lock()
	r.status = code
	r.mu.Unlock()
}

func (r *flushRecorder) Flush() {}

func (r *flushRecorder) Status() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

// TestSubscribe_NoBroadcast_Returns503 verifies that the SSE endpoint
// short-circuits with 503 when the broadcast hub has not been wired into
// the handler.
func TestSubscribe_NoBroadcast_Returns503(t *testing.T) {
	h := newSubscribeHandler(t, nil)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/test-ont/subscribe", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d; body=%s", rec.Code, rec.Body.String())
	}
}

// TestSubscribe_WritesSSEHeaders verifies that the handler sets
// Content-Type: text/event-stream, Cache-Control: no-cache, and
// Connection: keep-alive immediately after subscribing.
func TestSubscribe_WritesSSEHeaders(t *testing.T) {
	b := funnel.NewBroadcast()
	h := newSubscribeHandler(t, b)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/test-ont/subscribe", nil).WithContext(ctx)
	rec := newFlushRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(rec, req)
	}()

	// Wait briefly for the handler to write headers and the initial SSE
	// comment line, then cancel the request.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if rec.Status() == http.StatusOK {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	if rec.Status() != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Status())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("expected Cache-Control no-cache, got %q", got)
	}
	if got := rec.Header().Get("Connection"); got != "keep-alive" {
		t.Errorf("expected Connection keep-alive, got %q", got)
	}
}

// TestSubscribe_StreamsEvents_AsSSE verifies that an event published to the
// broadcast hub is delivered to the client wrapped as a `data: <json>\n\n`
// SSE frame.
func TestSubscribe_StreamsEvents_AsSSE(t *testing.T) {
	b := funnel.NewBroadcast()
	h := newSubscribeHandler(t, b)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/test-ont/subscribe", nil).WithContext(ctx)
	rec := newFlushRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(rec, req)
	}()

	// Wait until headers are written so we know Subscribe() has been called.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if rec.Status() == http.StatusOK {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	b.Publish(funnel.BroadcastEvent{
		Type:       "CREATE",
		ObjectType: "employee",
		PrimaryKey: "emp-1",
		Properties: map[string]interface{}{"name": "Alice"},
		EditedAt:   time.Date(2026, 4, 6, 10, 0, 0, 0, time.UTC),
	})

	frame := readSSEFrame(t, rec.pipe, 1*time.Second)
	if !strings.HasPrefix(frame, "data: ") {
		t.Errorf("expected SSE data frame, got %q", frame)
	}
	if !strings.Contains(frame, `"type":"CREATE"`) {
		t.Errorf("expected CREATE in frame, got %q", frame)
	}
	if !strings.Contains(frame, `"objectType":"employee"`) {
		t.Errorf("expected objectType employee in frame, got %q", frame)
	}
	if !strings.Contains(frame, `"primaryKey":"emp-1"`) {
		t.Errorf("expected primaryKey emp-1 in frame, got %q", frame)
	}

	cancel()
	<-done
}

// TestSubscribe_FiltersByObjectType verifies that the ?objectType=X query
// param filters the stream so subscribers only receive events for that one
// object type.
func TestSubscribe_FiltersByObjectType(t *testing.T) {
	b := funnel.NewBroadcast()
	h := newSubscribeHandler(t, b)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/test-ont/subscribe?objectType=employee", nil).WithContext(ctx)
	rec := newFlushRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(rec, req)
	}()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if rec.Status() == http.StatusOK {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Publish two events. Only the employee one should reach the client.
	b.Publish(funnel.BroadcastEvent{
		Type:       "CREATE",
		ObjectType: "project",
		PrimaryKey: "proj-1",
	})
	b.Publish(funnel.BroadcastEvent{
		Type:       "CREATE",
		ObjectType: "employee",
		PrimaryKey: "emp-99",
	})

	frame := readSSEFrame(t, rec.pipe, 1*time.Second)
	if !strings.Contains(frame, `"objectType":"employee"`) {
		t.Errorf("expected employee event, got %q", frame)
	}
	if strings.Contains(frame, "proj-1") {
		t.Errorf("project event should have been filtered out, got %q", frame)
	}

	cancel()
	<-done
}

// TestSubscribe_ClosesOnContextCancel verifies that the handler returns when
// the request context is cancelled (client disconnect), unsubscribing from
// the broadcast hub on the way out.
func TestSubscribe_ClosesOnContextCancel(t *testing.T) {
	b := funnel.NewBroadcast()
	h := newSubscribeHandler(t, b)

	ctx, cancel := context.WithCancel(context.Background())

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/test-ont/subscribe", nil).WithContext(ctx)
	rec := newFlushRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(rec, req)
	}()

	// Wait until handler has registered with the hub (status 200 implies
	// headers flushed which happens after Subscribe()).
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if rec.Status() == http.StatusOK {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("handler did not return after context cancellation")
	}
}
