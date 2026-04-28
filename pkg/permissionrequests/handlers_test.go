package permissionrequests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
)

func newTestRouter(store Store, user *auth.User) http.Handler {
	return newTestRouterWith(store, user, nil, nil)
}

func newTestRouterWith(
	store Store,
	user *auth.User,
	notifier Notifier,
	approverLister ApproverLister,
) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := req.Context()
			if user != nil {
				ctx = auth.WithUser(ctx, user)
			}
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	h := NewHandler(store)
	if notifier != nil {
		h.SetNotifier(notifier)
	}
	if approverLister != nil {
		h.SetApproverLister(approverLister)
	}
	h.RegisterRoutes(r)
	return r
}

func mustEncode(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func adminUser() *auth.User {
	return &auth.User{ID: "user:admin", Roles: []string{auth.RoleAdmin}}
}

func viewerUser() *auth.User {
	return &auth.User{ID: "user:bob", Roles: []string{auth.RoleViewer}}
}

func TestHandler_CreateRequiresAuth(t *testing.T) {
	store := NewMemoryStore()
	r := newTestRouter(store, nil)
	body := mustEncode(t, map[string]any{"targetRid": targetRID})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/permission-requests", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anon: want 401, got %d", w.Code)
	}
}

func TestHandler_CreateUnavailableWhenStoreNil(t *testing.T) {
	r := newTestRouter(nil, viewerUser())
	body := mustEncode(t, map[string]any{"targetRid": targetRID})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/permission-requests", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("nil store: want 500, got %d", w.Code)
	}
}

func TestHandler_CreateRoundTrip(t *testing.T) {
	store := NewMemoryStore()
	r := newTestRouter(store, viewerUser())

	body := mustEncode(t, map[string]any{
		"targetRid": targetRID,
		"reason":    "I need access for the audit",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/permission-requests", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d (%s)", w.Code, w.Body.String())
	}

	var got Request
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.RequestedBy != "user:bob" {
		t.Fatalf("requestedBy: %q", got.RequestedBy)
	}
	if got.Status != StatusPending {
		t.Fatalf("status: %q", got.Status)
	}
	if got.Reason != "I need access for the audit" {
		t.Fatalf("reason: %q", got.Reason)
	}
}

func TestHandler_CreateInvalidTarget(t *testing.T) {
	store := NewMemoryStore()
	r := newTestRouter(store, viewerUser())
	body := mustEncode(t, map[string]any{"targetRid": "not-a-rid"})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/permission-requests", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid target: want 400, got %d", w.Code)
	}
}

func TestHandler_CreateOversizeReason(t *testing.T) {
	store := NewMemoryStore()
	r := newTestRouter(store, viewerUser())
	long := strings.Repeat("x", MaxReasonLength+1)
	body := mustEncode(t, map[string]any{"targetRid": targetRID, "reason": long})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/permission-requests", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("oversize reason: want 400, got %d", w.Code)
	}
}

func TestHandler_GetScopedToRequesterOrApprover(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	row := &Request{
		ID: "p1", TargetRID: targetRID, RequestedBy: "user:bob",
		Reason: "let me in", Status: StatusPending,
	}
	if err := store.Create(ctx, row); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// requester sees their own row
	bob := newTestRouter(store, viewerUser())
	req := httptest.NewRequest(http.MethodGet, "/api/v2/permission-requests/p1", nil)
	w := httptest.NewRecorder()
	bob.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("bob get own: want 200, got %d (%s)", w.Code, w.Body.String())
	}

	// admin sees any row
	admin := newTestRouter(store, adminUser())
	req = httptest.NewRequest(http.MethodGet, "/api/v2/permission-requests/p1", nil)
	w = httptest.NewRecorder()
	admin.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin get others: want 200, got %d", w.Code)
	}

	// non-approver stranger sees 404 (existence not disclosed)
	stranger := newTestRouter(store, &auth.User{ID: "user:eve", Roles: []string{auth.RoleViewer}})
	req = httptest.NewRequest(http.MethodGet, "/api/v2/permission-requests/p1", nil)
	w = httptest.NewRecorder()
	stranger.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("stranger get: want 404, got %d", w.Code)
	}
}

func TestHandler_ListNonApproverScopedToCaller(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.Create(ctx, &Request{
		ID: "p1", TargetRID: targetRID, RequestedBy: "user:bob",
	})
	_ = store.Create(ctx, &Request{
		ID: "p2", TargetRID: targetRID, RequestedBy: "user:alice",
	})

	r := newTestRouter(store, viewerUser())
	req := httptest.NewRequest(http.MethodGet, "/api/v2/permission-requests", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: want 200, got %d", w.Code)
	}
	var resp listResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Requests) != 1 || resp.Requests[0].RequestedBy != "user:bob" {
		t.Fatalf("non-approver list: want only own rows, got %+v", resp.Requests)
	}
}

func TestHandler_ListApproverSeesAll(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.Create(ctx, &Request{
		ID: "p1", TargetRID: targetRID, RequestedBy: "user:bob",
	})
	_ = store.Create(ctx, &Request{
		ID: "p2", TargetRID: targetRID, RequestedBy: "user:alice",
	})

	r := newTestRouter(store, adminUser())
	req := httptest.NewRequest(http.MethodGet, "/api/v2/permission-requests?status=PENDING", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin list: want 200, got %d", w.Code)
	}
	var resp listResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Total != 2 || len(resp.Requests) != 2 {
		t.Fatalf("admin list: want 2 rows, got total=%d len=%d", resp.Total, len(resp.Requests))
	}
}

func TestHandler_ListInvalidStatus(t *testing.T) {
	store := NewMemoryStore()
	r := newTestRouter(store, adminUser())
	req := httptest.NewRequest(http.MethodGet, "/api/v2/permission-requests?status=BOGUS", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bogus status: want 400, got %d", w.Code)
	}
}

func TestHandler_ApproveByApprover(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.Create(ctx, &Request{
		ID: "p1", TargetRID: targetRID, RequestedBy: "user:bob",
	})

	r := newTestRouter(store, adminUser())
	body := mustEncode(t, map[string]any{"note": "ok"})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/permission-requests/p1/approve", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("approve: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var got Request
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Status != StatusApproved {
		t.Fatalf("status: %q", got.Status)
	}
	if got.DecidedBy != "user:admin" {
		t.Fatalf("decidedBy: %q", got.DecidedBy)
	}
	if got.DecisionNote != "ok" {
		t.Fatalf("decisionNote: %q", got.DecisionNote)
	}
}

func TestHandler_RejectByApprover(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.Create(ctx, &Request{
		ID: "p1", TargetRID: targetRID, RequestedBy: "user:bob",
	})

	r := newTestRouter(store, adminUser())
	body := mustEncode(t, map[string]any{"note": "no"})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/permission-requests/p1/reject", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reject: want 200, got %d", w.Code)
	}
}

func TestHandler_ApproveByNonApproverForbidden(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.Create(ctx, &Request{
		ID: "p1", TargetRID: targetRID, RequestedBy: "user:bob",
	})

	r := newTestRouter(store, viewerUser())
	req := httptest.NewRequest(http.MethodPost, "/api/v2/permission-requests/p1/approve", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-approver approve: want 403, got %d", w.Code)
	}
}

func TestHandler_ApproveAlreadyDecidedConflict(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.Create(ctx, &Request{
		ID: "p1", TargetRID: targetRID, RequestedBy: "user:bob",
		Status: StatusApproved,
	})

	r := newTestRouter(store, adminUser())
	req := httptest.NewRequest(http.MethodPost, "/api/v2/permission-requests/p1/approve", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("re-approve: want 409, got %d", w.Code)
	}
}

func TestHandler_ApproveMissing404(t *testing.T) {
	store := NewMemoryStore()
	r := newTestRouter(store, adminUser())
	req := httptest.NewRequest(http.MethodPost, "/api/v2/permission-requests/missing/approve", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("approve missing: want 404, got %d", w.Code)
	}
}

func TestHandler_NotificationFanout(t *testing.T) {
	store := NewMemoryStore()
	notifier := &recordingNotifier{}
	approvers := &fixedApproverLister{ids: []string{"user:admin", "user:bob"}}
	r := newTestRouterWith(store, viewerUser(), notifier, approvers)

	// Create — fans out to every approver except the requester.
	body := mustEncode(t, map[string]any{"targetRid": targetRID})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/permission-requests", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d", w.Code)
	}
	var created Request
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	notifier.mu.Lock()
	approverEvents := append([]NewRequestEvent(nil), notifier.newEvents...)
	notifier.mu.Unlock()
	if len(approverEvents) != 1 || approverEvents[0].ApproverID != "user:admin" {
		t.Fatalf("expected exactly admin to be notified (bob is requester), got %+v", approverEvents)
	}

	// Approve — fans out to the requester.
	adminR := newTestRouterWith(store, adminUser(), notifier, approvers)
	req = httptest.NewRequest(http.MethodPost, "/api/v2/permission-requests/"+created.ID+"/approve", nil)
	w = httptest.NewRecorder()
	adminR.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("approve: %d (%s)", w.Code, w.Body.String())
	}
	notifier.mu.Lock()
	decisionEvents := append([]DecisionEvent(nil), notifier.decisionEvents...)
	notifier.mu.Unlock()
	if len(decisionEvents) != 1 || decisionEvents[0].Request.Status != StatusApproved {
		t.Fatalf("expected exactly one APPROVED decision event, got %+v", decisionEvents)
	}
}

type recordingNotifier struct {
	mu             sync.Mutex
	newEvents      []NewRequestEvent
	decisionEvents []DecisionEvent
}

func (r *recordingNotifier) NotifyApproversNewRequest(_ context.Context, ev NewRequestEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.newEvents = append(r.newEvents, ev)
	return nil
}

func (r *recordingNotifier) NotifyRequesterDecision(_ context.Context, ev DecisionEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.decisionEvents = append(r.decisionEvents, ev)
	return nil
}

type fixedApproverLister struct {
	ids []string
}

func (f *fixedApproverLister) ListApproverUserIDs(_ context.Context) ([]string, error) {
	return f.ids, nil
}
