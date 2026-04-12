//go:build integration

package phase7_test

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

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/security"
)

// policySSEMsg pairs an SSE id with its decoded frame for assertion.
type policySSEMsg struct {
	id    string
	frame sseFrame
}

// markingEventFilter implements oss.SSEEventFilter using auth.EvaluateMarkings.
// It drops SSE events whose object markings are not a subset of the
// subscribing user's markings — the same AND semantics used by the
// ListObjects / SearchObjects read paths (US-052).
type markingEventFilter struct{}

func (markingEventFilter) AllowEvent(user *auth.User, evt funnel.BroadcastEvent) bool {
	// Extract user markings from Attributes["markings"].
	var userMarkings []string
	if user != nil && user.Attributes != nil {
		if raw, ok := user.Attributes["markings"]; ok {
			switch v := raw.(type) {
			case []string:
				userMarkings = v
			case []any:
				for _, item := range v {
					if s, ok := item.(string); ok {
						userMarkings = append(userMarkings, s)
					}
				}
			}
		}
	}

	// Extract object markings from event Properties.
	var objMarkings []string
	if raw, ok := evt.Properties[security.MarkingField]; ok {
		switch v := raw.(type) {
		case []string:
			objMarkings = v
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					objMarkings = append(objMarkings, s)
				}
			}
		}
	}

	return auth.EvaluateMarkings(userMarkings, objMarkings)
}

// TestSSEPolicy_DifferentUsersSeeDifferentEvents is the US-073 acceptance test.
//
// Two users subscribe to the same ObjectSet SSE stream. UserA holds marking
// "ACME", UserB holds marking "ACME2". Mixed events are published with
// different markings. Each user should only see events whose markings are a
// subset of their own markings.
//
// Event matrix:
//
//	ev1: _markings=["ACME"]        → UserA sees, UserB does not
//	ev2: _markings=["ACME2"]       → UserB sees, UserA does not
//	ev3: _markings=["ACME"]        → UserA sees, UserB does not
//	ev4: _markings=["ACME2"]       → UserB sees, UserA does not
//	ev5: _markings=[]              → both see (unmarked = public)
//	ev6: _markings=["ACME","ACME2"]→ neither sees (needs BOTH)
func TestSSEPolicy_DifferentUsersSeeDifferentEvents(t *testing.T) {
	const objectSetRid = "rid-sse-policy-p7"
	const objectType = "ticket"

	lookup := &stubSSELookup{byRid: map[string]oss.SubscriptionSpec{
		objectSetRid: {ObjectType: objectType},
	}}

	broadcast := funnel.NewBroadcast()

	handler := oss.NewSubscribeSSEHandler(lookup, broadcast)
	handler.SetHeartbeatInterval(0)
	handler.SetMaxConnectionsPerUser(10)
	handler.SetEventFilter(markingEventFilter{})

	// Two test users with different marking grants.
	users := map[string]*auth.User{
		"userA": {
			ID:    "userA",
			Roles: []string{auth.RoleViewer},
			Attributes: map[string]any{
				"markings": []string{"ACME"},
			},
		},
		"userB": {
			ID:    "userB",
			Roles: []string{auth.RoleViewer},
			Attributes: map[string]any{
				"markings": []string{"ACME2"},
			},
		},
	}

	// Middleware injects auth.User based on X-Test-User header.
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			uid := req.Header.Get("X-Test-User")
			if u, ok := users[uid]; ok {
				req = req.WithContext(auth.WithUser(req.Context(), u))
			}
			next.ServeHTTP(w, req)
		})
	})
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe", handler.ServeHTTP)

	srv := httptest.NewServer(r)
	defer srv.Close()

	subscribeURL := srv.URL + "/api/v2/ontologies/test/objectSets/" + objectSetRid + "/subscribe"

	// subscribe connects to the SSE endpoint as the given user and returns
	// a cancel function and a channel that receives decoded frames.
	subscribe := func(t *testing.T, userID string) (context.CancelFunc, <-chan policySSEMsg) {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, subscribeURL, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("X-Test-User", userID)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			t.Fatalf("do: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			cancel()
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}

		ch := make(chan policySSEMsg, 32)
		go func() {
			defer resp.Body.Close()
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
					return
				}
				select {
				case ch <- policySSEMsg{id: currentID, frame: f}:
					currentID = ""
				case <-ctx.Done():
					return
				}
			}
		}()

		return cancel, ch
	}

	// Subscribe both users.
	cancelA, chA := subscribe(t, "userA")
	defer cancelA()
	cancelB, chB := subscribe(t, "userB")
	defer cancelB()

	// Wait for subscribers to register.
	time.Sleep(100 * time.Millisecond)

	// Publish 6 events with mixed markings.
	type testEvent struct {
		pk       string
		markings []string // nil = no marking field (public)
	}
	events := []testEvent{
		{"TKT-001", []string{"ACME"}},          // A only
		{"TKT-002", []string{"ACME2"}},         // B only
		{"TKT-003", []string{"ACME"}},          // A only
		{"TKT-004", []string{"ACME2"}},         // B only
		{"TKT-005", nil},                       // both (public)
		{"TKT-006", []string{"ACME", "ACME2"}}, // neither (needs BOTH)
	}

	for i, ev := range events {
		props := map[string]interface{}{
			"status": "OPEN",
		}
		if ev.markings != nil {
			props[security.MarkingField] = ev.markings
		}
		broadcast.Publish(funnel.BroadcastEvent{
			Type:       "CREATE",
			ObjectType: objectType,
			PrimaryKey: ev.pk,
			Sequence:   uint64(200 + i),
			Properties: props,
			EditedAt:   time.Now(),
		})
	}

	// collectFrames reads up to `expected` frames from ch, then drains
	// any extras within a short window to catch unexpected deliveries.
	collectFrames := func(ch <-chan policySSEMsg, expected int) []policySSEMsg {
		var msgs []policySSEMsg
		deadline := time.After(3 * time.Second)
		for len(msgs) < expected {
			select {
			case msg := <-ch:
				msgs = append(msgs, msg)
			case <-deadline:
				return msgs
			}
		}
		drainDeadline := time.After(200 * time.Millisecond)
		for {
			select {
			case msg := <-ch:
				msgs = append(msgs, msg)
			case <-drainDeadline:
				return msgs
			}
		}
	}

	// UserA should see: TKT-001 (ACME), TKT-003 (ACME), TKT-005 (public)
	// UserB should see: TKT-002 (ACME2), TKT-004 (ACME2), TKT-005 (public)

	var wg sync.WaitGroup
	var msgsA, msgsB []policySSEMsg

	wg.Add(2)
	go func() {
		defer wg.Done()
		msgsA = collectFrames(chA, 3)
	}()
	go func() {
		defer wg.Done()
		msgsB = collectFrames(chB, 3)
	}()
	wg.Wait()

	// Assert UserA events.
	t.Run("UserA_sees_ACME_and_public_only", func(t *testing.T) {
		wantPKs := []string{"TKT-001", "TKT-003", "TKT-005"}
		if len(msgsA) != len(wantPKs) {
			gotPKs := make([]string, len(msgsA))
			for i, m := range msgsA {
				gotPKs[i], _ = m.frame.Object["__primaryKey"].(string)
			}
			t.Fatalf("UserA got %d events %v, want %d %v", len(msgsA), gotPKs, len(wantPKs), wantPKs)
		}
		for i, msg := range msgsA {
			pk, _ := msg.frame.Object["__primaryKey"].(string)
			if pk != wantPKs[i] {
				t.Errorf("UserA event %d: pk=%q, want %q", i, pk, wantPKs[i])
			}
		}
	})

	// Assert UserB events.
	t.Run("UserB_sees_ACME2_and_public_only", func(t *testing.T) {
		wantPKs := []string{"TKT-002", "TKT-004", "TKT-005"}
		if len(msgsB) != len(wantPKs) {
			gotPKs := make([]string, len(msgsB))
			for i, m := range msgsB {
				gotPKs[i], _ = m.frame.Object["__primaryKey"].(string)
			}
			t.Fatalf("UserB got %d events %v, want %d %v", len(msgsB), gotPKs, len(wantPKs), wantPKs)
		}
		for i, msg := range msgsB {
			pk, _ := msg.frame.Object["__primaryKey"].(string)
			if pk != wantPKs[i] {
				t.Errorf("UserB event %d: pk=%q, want %q", i, pk, wantPKs[i])
			}
		}
	})

	// Assert neither user saw the dual-marked event TKT-006.
	t.Run("neither_sees_dual_marked", func(t *testing.T) {
		for _, msg := range msgsA {
			pk, _ := msg.frame.Object["__primaryKey"].(string)
			if pk == "TKT-006" {
				t.Error("UserA should NOT see TKT-006 (requires ACME+ACME2)")
			}
		}
		for _, msg := range msgsB {
			pk, _ := msg.frame.Object["__primaryKey"].(string)
			if pk == "TKT-006" {
				t.Error("UserB should NOT see TKT-006 (requires ACME+ACME2)")
			}
		}
	})
}
