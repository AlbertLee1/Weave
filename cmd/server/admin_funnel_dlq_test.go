package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// fakeDLQReaderForAdmin is a chi-side fake that drives the US-470 admin
// handlers through a real router without needing a NATS server.
type fakeDLQReaderForAdmin struct {
	mu      sync.Mutex
	entries map[string]funnel.DLQEntry
	order   []string
}

func newFakeDLQReaderForAdmin() *fakeDLQReaderForAdmin {
	return &fakeDLQReaderForAdmin{entries: map[string]funnel.DLQEntry{}}
}

func (f *fakeDLQReaderForAdmin) seed(id, originalSubject string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	seq, _ := strconv.ParseUint(id, 10, 64)
	f.entries[id] = funnel.DLQEntry{
		ID:      id,
		Subject: funnel.BuildDLQSubject(strings.TrimPrefix(originalSubject, funnel.SubjectPrefix+".")),
		Message: funnel.DLQMessage{
			OriginalSubject: originalSubject,
			OriginalData:    data,
			Reason:          "exceeded max deliveries (6/5)",
			NumDelivered:    6,
			MaxDeliveries:   5,
			StreamSequence:  seq,
		},
	}
	f.order = append(f.order, id)
}

func (f *fakeDLQReaderForAdmin) ListPending(ctx context.Context, limit int) ([]funnel.DLQEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if limit <= 0 || limit > len(f.order) {
		limit = len(f.order)
	}
	out := make([]funnel.DLQEntry, 0, limit)
	for _, id := range f.order[:limit] {
		out = append(out, f.entries[id])
	}
	return out, nil
}

func (f *fakeDLQReaderForAdmin) GetByID(ctx context.Context, id string) (funnel.DLQEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[id]
	if !ok {
		return funnel.DLQEntry{}, funnel.ErrDLQEntryNotFound
	}
	return e, nil
}

func (f *fakeDLQReaderForAdmin) DeleteByID(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.entries[id]; !ok {
		return funnel.ErrDLQEntryNotFound
	}
	delete(f.entries, id)
	for i, existing := range f.order {
		if existing == id {
			f.order = append(f.order[:i], f.order[i+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeDLQReaderForAdmin) Size(ctx context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(len(f.order)), nil
}

type capturePublisher struct {
	mu        sync.Mutex
	published []struct {
		subject string
		data    []byte
	}
	failNext bool
}

func (c *capturePublisher) Publish(subj string, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failNext {
		c.failNext = false
		return errors.New("simulated nats publish failure")
	}
	c.published = append(c.published, struct {
		subject string
		data    []byte
	}{subject: subj, data: append([]byte(nil), data...)})
	return nil
}

// mountAdminFunnelDLQRoutes wires the three US-470 admin routes onto a chi
// router so each test exercises the same routing surface the production
// bootstrap registers.
func mountAdminFunnelDLQRoutes(reader funnel.DLQReader, pub funnel.DLQPublishFunc) http.Handler {
	r := chi.NewRouter()
	deps := AdminFunnelDLQDeps{Reader: reader, Publish: pub}
	r.Method(http.MethodGet, "/api/admin/funnel/dlq", NewAdminFunnelDLQListHandler(deps))
	r.Method(http.MethodPost, "/api/admin/funnel/dlq/{id}/replay", NewAdminFunnelDLQReplayHandler(deps))
	r.Method(http.MethodPost, "/api/admin/funnel/dlq/{id}/discard", NewAdminFunnelDLQDiscardHandler(deps))
	return r
}

// TestBDD_US470_DLQReplay_Given_FailedMessage_When_Replay_Then_Republished is
// the PRD-mandated end-to-end scenario: a manufactured DLQ entry is listed
// via GET, then a POST to /replay republishes the original payload back to
// the live `edits.<objectType>` subject and the entry is removed.
func TestBDD_US470_DLQReplay_Given_FailedMessage_When_Replay_Then_Republished(t *testing.T) {
	reader := newFakeDLQReaderForAdmin()
	pub := &capturePublisher{}

	// Given: an EditBatch payload that previously failed and was dead-lettered.
	payload, _ := json.Marshal(map[string]any{
		"id":        "batch-replay-1",
		"timestamp": "2026-05-16T12:00:00Z",
		"edits": []map[string]any{{
			"type":       "CREATE",
			"objectType": "employee",
			"primaryKey": "emp-fail",
			"properties": map[string]any{"name": "Replay-Me"},
		}},
	})
	reader.seed("1", "edits.employee", payload)
	reader.seed("2", "edits.project", []byte(`{"id":"batch-replay-2"}`))

	handler := mountAdminFunnelDLQRoutes(reader, pub.Publish)

	// When: operator lists pending DLQ.
	req := httptest.NewRequest(http.MethodGet, "/api/admin/funnel/dlq", nil)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rw.Code, rw.Body.String())
	}
	var listResp struct {
		Entries []funnel.DLQEntry `json:"entries"`
		Size    int64             `json:"size"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Entries) != 2 {
		t.Fatalf("entries len = %d, want 2", len(listResp.Entries))
	}
	if listResp.Size != 2 {
		t.Fatalf("size = %d, want 2", listResp.Size)
	}
	if listResp.Entries[0].ID != "1" {
		t.Errorf("entries[0].id = %q, want %q", listResp.Entries[0].ID, "1")
	}
	if listResp.Entries[0].Message.OriginalSubject != "edits.employee" {
		t.Errorf("originalSubject = %q", listResp.Entries[0].Message.OriginalSubject)
	}

	// When: operator replays entry id=1.
	req = httptest.NewRequest(http.MethodPost, "/api/admin/funnel/dlq/1/replay", nil)
	rw = httptest.NewRecorder()
	handler.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("replay status = %d body=%s", rw.Code, rw.Body.String())
	}
	var replayResp struct {
		ID              string `json:"id"`
		OriginalSubject string `json:"originalSubject"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &replayResp); err != nil {
		t.Fatalf("decode replay: %v", err)
	}
	if replayResp.ID != "1" {
		t.Errorf("id = %q, want 1", replayResp.ID)
	}
	if replayResp.OriginalSubject != "edits.employee" {
		t.Errorf("originalSubject = %q, want edits.employee", replayResp.OriginalSubject)
	}

	// Then: the original payload is republished on the live subject, and the
	// DLQ row has been removed so it cannot be replayed twice.
	pub.mu.Lock()
	if len(pub.published) != 1 {
		t.Fatalf("expected 1 republish, got %d", len(pub.published))
	}
	if pub.published[0].subject != "edits.employee" {
		t.Errorf("subject = %q, want edits.employee", pub.published[0].subject)
	}
	if string(pub.published[0].data) != string(payload) {
		t.Errorf("data mismatch: got %s, want %s", pub.published[0].data, payload)
	}
	pub.mu.Unlock()

	// And: a subsequent list reflects the deletion.
	req = httptest.NewRequest(http.MethodGet, "/api/admin/funnel/dlq", nil)
	rw = httptest.NewRecorder()
	handler.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("list-after status = %d body=%s", rw.Code, rw.Body.String())
	}
	listResp.Entries = nil
	if err := json.Unmarshal(rw.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list-after: %v", err)
	}
	if len(listResp.Entries) != 1 {
		t.Fatalf("post-replay entries len = %d, want 1", len(listResp.Entries))
	}
	if listResp.Entries[0].ID != "2" {
		t.Errorf("remaining entry id = %q, want 2", listResp.Entries[0].ID)
	}
}

// TestAdminFunnelDLQ_Replay_NotFound asserts a 404 for an unknown id.
func TestAdminFunnelDLQ_Replay_NotFound(t *testing.T) {
	reader := newFakeDLQReaderForAdmin()
	pub := &capturePublisher{}
	handler := mountAdminFunnelDLQRoutes(reader, pub.Publish)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/funnel/dlq/999/replay", nil)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)
	if rw.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rw.Code)
	}
}

// TestAdminFunnelDLQ_Replay_PublishFailureKeepsEntry pins the safety
// invariant: a failed publish must NOT delete the DLQ row, so operators can
// retry once the downstream issue clears.
func TestAdminFunnelDLQ_Replay_PublishFailureKeepsEntry(t *testing.T) {
	reader := newFakeDLQReaderForAdmin()
	reader.seed("5", "edits.employee", []byte(`{"id":"batch-fail"}`))
	pub := &capturePublisher{failNext: true}
	handler := mountAdminFunnelDLQRoutes(reader, pub.Publish)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/funnel/dlq/5/replay", nil)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)
	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rw.Code)
	}
	// Entry must still exist.
	if _, err := reader.GetByID(context.Background(), "5"); err != nil {
		t.Fatalf("entry was deleted after failed replay: %v", err)
	}
}

// TestAdminFunnelDLQ_Replay_NoPublisher returns 503 when NATS is unavailable.
func TestAdminFunnelDLQ_Replay_NoPublisher(t *testing.T) {
	reader := newFakeDLQReaderForAdmin()
	reader.seed("9", "edits.employee", []byte(`{}`))
	handler := mountAdminFunnelDLQRoutes(reader, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/funnel/dlq/9/replay", nil)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)
	if rw.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rw.Code)
	}
}

// TestAdminFunnelDLQ_List_NoReader returns 503 in degraded mode.
func TestAdminFunnelDLQ_List_NoReader(t *testing.T) {
	handler := mountAdminFunnelDLQRoutes(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/funnel/dlq", nil)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)
	if rw.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rw.Code)
	}
}

// TestAdminFunnelDLQ_Discard_Success deletes a DLQ entry without republishing.
func TestAdminFunnelDLQ_Discard_Success(t *testing.T) {
	reader := newFakeDLQReaderForAdmin()
	reader.seed("11", "edits.employee", []byte(`{}`))
	pub := &capturePublisher{}
	handler := mountAdminFunnelDLQRoutes(reader, pub.Publish)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/funnel/dlq/11/discard", nil)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rw.Code, rw.Body.String())
	}
	if _, err := reader.GetByID(context.Background(), "11"); err == nil {
		t.Fatal("entry should be deleted after discard")
	}
	if len(pub.published) != 0 {
		t.Fatalf("discard must not publish; got %d publishes", len(pub.published))
	}
}

// TestAdminFunnelDLQ_List_UpdatesPrometheusGauge pins the wiring between the
// list endpoint and the weave_funnel_dlq_size gauge so the operator UI and
// /metrics agree on the pending count.
func TestAdminFunnelDLQ_List_UpdatesPrometheusGauge(t *testing.T) {
	metrics.SetFunnelDLQSize(0)
	t.Cleanup(func() { metrics.SetFunnelDLQSize(0) })

	reader := newFakeDLQReaderForAdmin()
	reader.seed("21", "edits.employee", []byte(`{}`))
	reader.seed("22", "edits.project", []byte(`{}`))
	reader.seed("23", "edits.employee", []byte(`{}`))
	handler := mountAdminFunnelDLQRoutes(reader, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/funnel/dlq", nil)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d", rw.Code)
	}
	if got := testutil.ToFloat64(metrics.FunnelDLQSizeGauge()); got != 3 {
		t.Fatalf("gauge = %v, want 3", got)
	}
}
