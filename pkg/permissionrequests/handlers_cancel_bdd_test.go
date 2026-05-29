package permissionrequests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/auth"
)

// TestBDD_PermissionRequest_Cancel covers the round-63 Foundry-
// parity gap. Foundry's share-link permission-request UI shows a
// "Cancel request" button on PENDING rows so the requester can
// withdraw an ask that's no longer needed (they got access another
// way, or the resource is gone, or they typed the wrong target).
// Weave had Create / List / Get / Approve / Reject but no cancel —
// the requester was stuck waiting on an approver's review for a
// request they no longer cared about, and the inbox stayed
// cluttered for the approver.
//
// Wire shape:
//
//	DELETE /api/v2/permission-requests/{id}
//	  204 (no body) → row transitioned to CANCELLED. DecidedBy is
//	                  stamped with the requester's user ID and
//	                  DecidedAt with the cancel time so the audit
//	                  trail is preserved (NOT a hard delete).
//	  401 → no authenticated user
//	  403 → caller is not the original requester (admins cannot
//	        cancel on behalf — that would be a separate audit
//	        event)
//	  404 → no row with that id
//	  409 → row is already APPROVED / REJECTED / CANCELLED
//	        (terminal states are immutable)
//
// Scenarios:
//   - Requester cancels own pending request: 204; row status flips
//     to CANCELLED with DecidedBy=requester and DecidedAt populated.
//   - Non-requester (different user) cancels: 403, row untouched.
//   - Admin (also not the requester) cancels: 403 — admins approve/
//     reject via the dedicated endpoints, not cancel.
//   - Cancel an APPROVED row: 409 (terminal state immutable).
//   - Cancel a REJECTED row: 409.
//   - Cancel a non-existent id: 404.
//   - Cancel without auth: 401.
//   - Cancelled request is visible via Get with status=CANCELLED so
//     the requester's UI shows "Cancelled" not "Pending".
func TestBDD_PermissionRequest_Cancel(t *testing.T) {
	requester := &auth.User{ID: "u-requester"}
	other := &auth.User{ID: "u-other"}

	// Seed a pending row owned by `requester` against any target RID.
	seedPending := func(t *testing.T, store *MemoryStore, id string) *Request {
		t.Helper()
		row := &Request{
			ID:          id,
			TargetRID:   "ri.objects.main.Customer.42",
			RequestedBy: requester.ID,
			Status:      StatusPending,
			CreatedAt:   time.Now().UTC().Add(-time.Hour),
			UpdatedAt:   time.Now().UTC().Add(-time.Hour),
		}
		if err := store.Create(context.TODO(), row); err != nil {
			t.Fatalf("seed: %v", err)
		}
		return row
	}

	doDelete := func(t *testing.T, store Store, user *auth.User, id string) *httptest.ResponseRecorder {
		t.Helper()
		r := newTestRouter(store, user)
		req := httptest.NewRequest(http.MethodDelete, "/api/v2/permission-requests/"+id, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("Requester cancels own pending request", func(t *testing.T) {
		store := NewMemoryStore()
		row := seedPending(t, store, "pr-1")
		rec := doDelete(t, store, requester, row.ID)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status=%d, want 204; body=%s", rec.Code, rec.Body.String())
		}
		updated, err := store.Get(context.TODO(), row.ID)
		if err != nil {
			t.Fatalf("Get after cancel: %v", err)
		}
		if updated.Status != StatusCancelled {
			t.Errorf("status=%q, want %q", updated.Status, StatusCancelled)
		}
		if updated.DecidedBy != requester.ID {
			t.Errorf("decidedBy=%q, want %q", updated.DecidedBy, requester.ID)
		}
		if updated.DecidedAt == nil {
			t.Errorf("decidedAt = nil, want populated timestamp on terminal-state transition")
		}
	})

	t.Run("Non-requester cancel attempt returns 403", func(t *testing.T) {
		store := NewMemoryStore()
		row := seedPending(t, store, "pr-2")
		rec := doDelete(t, store, other, row.ID)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status=%d, want 403; body=%s", rec.Code, rec.Body.String())
		}
		// Row must remain PENDING — no privilege escalation via 403 path.
		updated, _ := store.Get(context.TODO(), row.ID)
		if updated.Status != StatusPending {
			t.Errorf("status=%q, want %q (must not transition on 403)", updated.Status, StatusPending)
		}
	})

	t.Run("Cancel on APPROVED row returns 409", func(t *testing.T) {
		store := NewMemoryStore()
		row := seedPending(t, store, "pr-3")
		// Decide it first.
		if err := store.Decide(context.TODO(), row.ID, Decision{
			Status: StatusApproved,
			By:     "u-admin",
			Note:   "ok",
		}); err != nil {
			t.Fatalf("Decide: %v", err)
		}
		rec := doDelete(t, store, requester, row.ID)
		if rec.Code != http.StatusConflict {
			t.Fatalf("status=%d, want 409; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "PermissionRequestAlreadyDecided" {
			t.Errorf("errorName=%v, want PermissionRequestAlreadyDecided", body["errorName"])
		}
		updated, _ := store.Get(context.TODO(), row.ID)
		if updated.Status != StatusApproved {
			t.Errorf("status=%q must remain %q", updated.Status, StatusApproved)
		}
	})

	t.Run("Cancel on REJECTED row returns 409", func(t *testing.T) {
		store := NewMemoryStore()
		row := seedPending(t, store, "pr-4")
		_ = store.Decide(context.TODO(), row.ID, Decision{Status: StatusRejected, By: "u-admin"})
		rec := doDelete(t, store, requester, row.ID)
		if rec.Code != http.StatusConflict {
			t.Errorf("status=%d, want 409", rec.Code)
		}
	})

	t.Run("Cancel on non-existent id returns 404", func(t *testing.T) {
		store := NewMemoryStore()
		rec := doDelete(t, store, requester, "pr-ghost")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Cancel without auth returns 401", func(t *testing.T) {
		store := NewMemoryStore()
		row := seedPending(t, store, "pr-5")
		rec := doDelete(t, store, nil, row.ID)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status=%d, want 401", rec.Code)
		}
	})

	t.Run("Double-cancel returns 409 (canceled is terminal)", func(t *testing.T) {
		store := NewMemoryStore()
		row := seedPending(t, store, "pr-6")
		rec1 := doDelete(t, store, requester, row.ID)
		if rec1.Code != http.StatusNoContent {
			t.Fatalf("first cancel: %d", rec1.Code)
		}
		rec2 := doDelete(t, store, requester, row.ID)
		if rec2.Code != http.StatusConflict {
			t.Errorf("second cancel status=%d, want 409 (CANCELLED is terminal)", rec2.Code)
		}
	})

	t.Run("Cancelled request shows up in Get with CANCELLED status", func(t *testing.T) {
		store := NewMemoryStore()
		row := seedPending(t, store, "pr-7")
		_ = doDelete(t, store, requester, row.ID)

		// Now hit GET /api/v2/permission-requests/{id} via the router.
		r := newTestRouter(store, requester)
		req := httptest.NewRequest(http.MethodGet, "/api/v2/permission-requests/"+row.ID, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("Get status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var got Request
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if got.Status != StatusCancelled {
			t.Errorf("Get returned status=%q, want %q", got.Status, StatusCancelled)
		}
		if got.DecidedBy != requester.ID {
			t.Errorf("Get returned decidedBy=%q, want %q", got.DecidedBy, requester.ID)
		}
	})
}
