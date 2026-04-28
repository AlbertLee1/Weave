package actions

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// runImpactRequest mounts the impact route on a chi router and serves one
// request. Centralised so each test stays focused on its assertion.
func runImpactRequest(t *testing.T, h *Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Get("/api/v2/actions/{rid}/impact", h.Impact)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeImpact(t *testing.T, w *httptest.ResponseRecorder) ImpactResponse {
	t.Helper()
	var resp ImpactResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v\nbody=%s", err, w.Body.String())
	}
	return resp
}

// TestImpact_HappyPath verifies the canonical case: an executor wires a
// LineageStore, an action runs producing CREATE+MODIFY edits, and the
// impact endpoint returns one ImpactObject per edit.
func TestImpact_HappyPath(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("upsert", []ParameterDef{
				{ID: "primaryKey", Type: "string", Required: true},
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{
					Type:       "modifyObject",
					ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					},
				},
			}),
		},
	}
	exec := NewExecutor(repo, nil)
	store := &fakeLineageStore{}
	exec.SetLineageStore(store)

	if _, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "upsert",
		Parameters: map[string]interface{}{"primaryKey": "EMP-001", "name": "Alice"},
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	logID := repo.insertedLogs[0].ID
	rid := oms.ActionLogLineageRID(logID)

	h := NewHandler(exec)
	w := runImpactRequest(t, h, "/api/v2/actions/"+rid+"/impact")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	resp := decodeImpact(t, w)
	if resp.ActionRID != rid {
		t.Errorf("actionRid: got %q, want %q", resp.ActionRID, rid)
	}
	if len(resp.Objects) != 1 {
		t.Fatalf("objects: got %d, want 1", len(resp.Objects))
	}
	obj := resp.Objects[0]
	if obj.RID != oms.ObjectLineageRID("Employee", "EMP-001") {
		t.Errorf("object rid: got %q", obj.RID)
	}
	if obj.ObjectType != "Employee" || obj.PrimaryKey != "EMP-001" {
		t.Errorf("decoded objectType/pk: got %q/%q", obj.ObjectType, obj.PrimaryKey)
	}
	if obj.Operation != string(funnel.EditTypeModify) {
		t.Errorf("operation: got %q, want %q", obj.Operation, funnel.EditTypeModify)
	}
	if obj.Timestamp.IsZero() {
		t.Errorf("expected non-zero timestamp on edge")
	}
	if resp.Truncated {
		t.Errorf("expected truncated=false for single-row result")
	}
}

// TestImpact_ActionLogEnrichment verifies that when GetActionLog returns a
// row, the response carries it under actionLog so callers can correlate
// without a second roundtrip.
func TestImpact_ActionLogEnrichment(t *testing.T) {
	createdAt := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	repo := &mockOmsRepo{
		actionLogByID: map[int64]*oms.ActionLog{
			42: {
				ID:            42,
				ActionTypeRID: "ri.actions.main.action-type.foo",
				UserID:        "user:alice@x",
				Status:        "succeeded",
				CreatedAt:     createdAt,
			},
		},
	}
	exec := NewExecutor(repo, nil)
	store := &fakeLineageStore{}
	store.addEdge(oms.LineageEdge{
		UpstreamRID:   oms.ActionLogLineageRID(42),
		DownstreamRID: oms.ObjectLineageRID("Employee", "EMP-001"),
		Operation:     "MODIFY",
		Timestamp:     createdAt,
	})
	exec.SetLineageStore(store)

	w := runImpactRequest(t, NewHandler(exec),
		"/api/v2/actions/"+oms.ActionLogLineageRID(42)+"/impact")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeImpact(t, w)
	if resp.ActionLog == nil {
		t.Fatalf("expected actionLog to be enriched, got nil")
	}
	if resp.ActionLog.ID != 42 || resp.ActionLog.UserID != "user:alice@x" {
		t.Errorf("unexpected actionLog payload: %+v", resp.ActionLog)
	}
}

// TestImpact_MissingActionLogStillReturnsObjects verifies the lineage view
// is the authoritative answer: even when the action log row cannot be
// loaded, the impact endpoint still returns the affected objects.
func TestImpact_MissingActionLogStillReturnsObjects(t *testing.T) {
	repo := &mockOmsRepo{} // no actionLogByID — GetActionLog returns ErrNotFound.
	exec := NewExecutor(repo, nil)
	store := &fakeLineageStore{}
	store.addEdge(oms.LineageEdge{
		UpstreamRID:   oms.ActionLogLineageRID(99),
		DownstreamRID: oms.ObjectLineageRID("Project", "PRJ-1"),
		Operation:     "CREATE",
	})
	exec.SetLineageStore(store)

	w := runImpactRequest(t, NewHandler(exec),
		"/api/v2/actions/"+oms.ActionLogLineageRID(99)+"/impact")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeImpact(t, w)
	if resp.ActionLog != nil {
		t.Errorf("expected actionLog=nil for missing log, got %+v", resp.ActionLog)
	}
	if len(resp.Objects) != 1 {
		t.Fatalf("objects: got %d, want 1", len(resp.Objects))
	}
}

// TestImpact_NoEdgesEmptyArray verifies the response wire shape always
// carries a non-nil objects array so SDKs can read len(objects) without
// nil-checks.
func TestImpact_NoEdgesEmptyArray(t *testing.T) {
	repo := &mockOmsRepo{}
	exec := NewExecutor(repo, nil)
	exec.SetLineageStore(&fakeLineageStore{})

	w := runImpactRequest(t, NewHandler(exec),
		"/api/v2/actions/"+oms.ActionLogLineageRID(7)+"/impact")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"objects":[]`) {
		t.Errorf("expected non-nil empty array on the wire, got %s", w.Body.String())
	}
}

// TestImpact_TruncatedFlag verifies truncated=true is set when
// ListDownstreamLineage returns its full window.
func TestImpact_TruncatedFlag(t *testing.T) {
	repo := &mockOmsRepo{}
	exec := NewExecutor(repo, nil)
	store := &fakeLineageStore{}
	for i := 0; i < impactPageLimit; i++ {
		store.addEdge(oms.LineageEdge{
			UpstreamRID:   oms.ActionLogLineageRID(7),
			DownstreamRID: oms.ObjectLineageRID("Employee", "EMP-"+itoaPad(i)),
			Operation:     "MODIFY",
		})
	}
	exec.SetLineageStore(store)

	w := runImpactRequest(t, NewHandler(exec),
		"/api/v2/actions/"+oms.ActionLogLineageRID(7)+"/impact")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	resp := decodeImpact(t, w)
	if !resp.Truncated {
		t.Errorf("expected truncated=true at page limit")
	}
	if len(resp.Objects) != impactPageLimit {
		t.Errorf("objects: got %d, want %d", len(resp.Objects), impactPageLimit)
	}
}

// TestImpact_InvalidActionRID rejects RIDs outside the action-log prefix.
func TestImpact_InvalidActionRID(t *testing.T) {
	exec := NewExecutor(&mockOmsRepo{}, nil)
	exec.SetLineageStore(&fakeLineageStore{})

	for _, rid := range []string{
		"ri.phonograph2-objects.main.object.Employee.EMP-001",
		"ri.actions.main.action-type.foo",
		"not-an-rid",
	} {
		t.Run(rid, func(t *testing.T) {
			w := runImpactRequest(t, NewHandler(exec), "/api/v2/actions/"+rid+"/impact")
			if w.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "InvalidActionRID") {
				t.Errorf("expected InvalidActionRID error, got %s", w.Body.String())
			}
		})
	}
}

// TestImpact_NoLineageStore — when no LineageStore is wired the route
// returns 404 ImpactNotConfigured, matching the lineage handler's degraded
// contract.
func TestImpact_NoLineageStore(t *testing.T) {
	exec := NewExecutor(&mockOmsRepo{}, nil)
	w := runImpactRequest(t, NewHandler(exec),
		"/api/v2/actions/"+oms.ActionLogLineageRID(1)+"/impact")
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "ImpactNotConfigured") {
		t.Errorf("expected ImpactNotConfigured, got %s", w.Body.String())
	}
}

// TestImpact_StoreError surfaces a store failure as 500 ImpactQueryFailed.
func TestImpact_StoreError(t *testing.T) {
	exec := NewExecutor(&mockOmsRepo{}, nil)
	store := &fakeLineageStore{}
	store.err = errors.New("pg unreachable")
	exec.SetLineageStore(store)

	w := runImpactRequest(t, NewHandler(exec),
		"/api/v2/actions/"+oms.ActionLogLineageRID(1)+"/impact")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "ImpactQueryFailed") {
		t.Errorf("expected ImpactQueryFailed, got %s", w.Body.String())
	}
}

// TestParseActionLogID checks the suffix parser used for enrichment.
func TestParseActionLogID(t *testing.T) {
	tests := []struct {
		rid    string
		wantID int64
		wantOK bool
	}{
		{oms.ActionLogLineageRID(42), 42, true},
		{"ri.actions.main.action-log.0", 0, false},
		{"ri.actions.main.action-log.-3", 0, false},
		{"ri.actions.main.action-log.abc", 0, false},
		{"ri.actions.main.action-log.", 0, false},
		{"ri.phonograph2-objects.main.object.X.1", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.rid, func(t *testing.T) {
			id, ok := parseActionLogID(tt.rid)
			if id != tt.wantID || ok != tt.wantOK {
				t.Errorf("parseActionLogID(%q) = (%d, %v), want (%d, %v)",
					tt.rid, id, ok, tt.wantID, tt.wantOK)
			}
		})
	}
}

// TestParseObjectLineageRID covers both shapes produced by
// oms.ObjectLineageRID and the composite-key edge case (PK contains dots).
func TestParseObjectLineageRID(t *testing.T) {
	tests := []struct {
		rid     string
		wantOT  string
		wantPK  string
	}{
		{oms.ObjectLineageRID("Employee", "EMP-001"), "Employee", "EMP-001"},
		{oms.ObjectLineageRID("Project", "PRJ-1"), "Project", "PRJ-1"},
		// Legacy one-segment shape: only primaryKey, no objectType.
		{"ri.phonograph2-objects.main.object.LEGACY", "", "LEGACY"},
		// Composite primary key carrying dots — everything after the first
		// segment is the PK.
		{"ri.phonograph2-objects.main.object.Order.US-001.line.42", "Order", "US-001.line.42"},
		{"ri.actions.main.action-log.42", "", ""},
		{"", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.rid, func(t *testing.T) {
			gotOT, gotPK := parseObjectLineageRID(tt.rid)
			if gotOT != tt.wantOT || gotPK != tt.wantPK {
				t.Errorf("parseObjectLineageRID(%q) = (%q, %q), want (%q, %q)",
					tt.rid, gotOT, gotPK, tt.wantOT, tt.wantPK)
			}
		})
	}
}

// itoaPad pads small integers so the per-edge primary keys are visually
// stable in test output without pulling in fmt.
func itoaPad(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "000"
	}
	var b [4]byte
	pos := len(b)
	for i > 0 && pos > 0 {
		pos--
		b[pos] = digits[i%10]
		i /= 10
	}
	return string(b[pos:])
}
