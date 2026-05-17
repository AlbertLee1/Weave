package oss_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/scenarios"
)

// US-481 handler-level tests: the OSS handler emits exactly one
// scenario.fold.conflict audit row per request whose scenario fold surfaces
// ≥1 conflict, and zero rows when the fold is clean. These cover the wire
// path without booting PG, so the cheaper unit suite can guard regressions
// on every push.

func us481RouterWithAuditor(t *testing.T, svc oss.Service, reader oss.ScenarioReader) (http.Handler, *audit.MemoryStore) {
	t.Helper()
	store := audit.NewMemoryStore()
	h := oss.NewHandler(svc)
	if reader != nil {
		h.SetScenarioReader(reader)
	}
	h.SetScenarioConflictAuditor(oss.NewScenarioConflictAuditor(store))
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r, store
}

func TestUS481_GetObject_ModifyAfterDelete_EmitsConflictAudit(t *testing.T) {
	svc := &fakeService{
		getObject: func(_ context.Context, _ oss.GetObjectRequest) (*oss.WireObject, error) {
			return &oss.WireObject{
				PrimaryKey: "JFK", APIName: "Airport",
				Properties: map[string]any{"capacity": 100.0},
			}, nil
		},
	}
	reader := &fakeScenarioReader{
		scenarios: map[string]*scenarios.Scenario{
			"s1": {RID: "s1", ParentOntologyCommit: "ont1"},
		},
		edits: map[string][]scenarios.ScenarioEdit{
			"s1": {
				{Seq: 1, Op: "deleteObject", ObjectType: "Airport", ObjectID: "JFK"},
				{Seq: 2, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(150)},
			},
		},
	}
	router, store := us481RouterWithAuditor(t, svc, reader)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ont1/objects/Airport/JFK", nil)
	req.Header.Set("X-Scenario-Id", "s1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d body=%s (delete should yield 404 via overlay)", rec.Code, rec.Body.String())
	}

	events := store.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	evt := events[0]
	if evt.Action != oss.ScenarioConflictAction {
		t.Errorf("Action: got %q want %q", evt.Action, oss.ScenarioConflictAction)
	}
	if evt.ResourceRID != "s1" {
		t.Errorf("ResourceRID: got %q want s1", evt.ResourceRID)
	}
	var diff struct {
		Operation     string                       `json:"operation"`
		ConflictCount int                          `json:"conflictCount"`
		ByType        map[string]int               `json:"byType"`
		Conflicts     []scenarios.ScenarioConflict `json:"conflicts"`
	}
	if err := json.Unmarshal(evt.DiffJSON, &diff); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if diff.Operation != "getObject" {
		t.Errorf("operation: got %q want getObject", diff.Operation)
	}
	if diff.ConflictCount != 1 || diff.ByType[scenarios.ConflictModifyAfterDelete] != 1 {
		t.Errorf("byType: got %+v want {modify_after_delete:1}", diff.ByType)
	}
}

func TestUS481_ListObjects_DuplicateCreate_EmitsConflictAudit(t *testing.T) {
	svc := &fakeService{
		listObjects: func(_ context.Context, _ oss.ListObjectsRequest) (*oss.ObjectPage, error) {
			return &oss.ObjectPage{Data: []*oss.WireObject{
				{PrimaryKey: "O-1", APIName: "Order", Properties: map[string]any{"total": 10.0}},
			}}, nil
		},
	}
	reader := &fakeScenarioReader{
		scenarios: map[string]*scenarios.Scenario{
			"s2": {RID: "s2", ParentOntologyCommit: "ont1"},
		},
		edits: map[string][]scenarios.ScenarioEdit{
			"s2": {
				// Two create-objects for an id already live in base — duplicate_create x2
				// (one citing base, one citing the prior create-edit).
				{Seq: 1, Op: "createObject", ObjectType: "Order", ObjectID: "O-1", NewValue: raw(map[string]any{"total": 20})},
				{Seq: 2, Op: "createObject", ObjectType: "Order", ObjectID: "O-1", NewValue: raw(map[string]any{"total": 30})},
			},
		},
	}
	router, store := us481RouterWithAuditor(t, svc, reader)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ont1/objects/Order", nil)
	req.Header.Set("X-Scenario-Id", "s2")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}

	events := store.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	var diff struct {
		Operation     string         `json:"operation"`
		ConflictCount int            `json:"conflictCount"`
		ByType        map[string]int `json:"byType"`
	}
	if err := json.Unmarshal(events[0].DiffJSON, &diff); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if diff.Operation != "listObjects" {
		t.Errorf("operation: got %q want listObjects", diff.Operation)
	}
	if diff.ByType[scenarios.ConflictDuplicateCreate] != 2 {
		t.Errorf("byType: got %+v want {duplicate_create:2}", diff.ByType)
	}
}

func TestUS481_GetObject_NoConflicts_EmitsZeroAuditRows(t *testing.T) {
	// Negative control: a clean fold must NOT emit an audit row. Without
	// this assertion, an always-true regression in the auditor would still
	// pass the conflict tests.
	svc := &fakeService{
		getObject: func(_ context.Context, _ oss.GetObjectRequest) (*oss.WireObject, error) {
			return &oss.WireObject{
				PrimaryKey: "JFK", APIName: "Airport",
				Properties: map[string]any{"capacity": 100.0},
			}, nil
		},
	}
	reader := &fakeScenarioReader{
		scenarios: map[string]*scenarios.Scenario{
			"s3": {RID: "s3", ParentOntologyCommit: "ont1"},
		},
		edits: map[string][]scenarios.ScenarioEdit{
			"s3": {
				{Seq: 1, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(150)},
			},
		},
	}
	router, store := us481RouterWithAuditor(t, svc, reader)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ont1/objects/Airport/JFK", nil)
	req.Header.Set("X-Scenario-Id", "s3")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := len(store.Events()); got != 0 {
		t.Errorf("expected 0 audit events for clean fold, got %d", got)
	}
}

// Regression guard: degraded-mode handler (no auditor wired) must still
// serve the request and not panic when overlay surfaces conflicts.
func TestUS481_GetObject_NilAuditor_NoPanic(t *testing.T) {
	svc := &fakeService{
		getObject: func(_ context.Context, _ oss.GetObjectRequest) (*oss.WireObject, error) {
			return &oss.WireObject{
				PrimaryKey: "JFK", APIName: "Airport",
				Properties: map[string]any{"capacity": 100.0},
			}, nil
		},
	}
	reader := &fakeScenarioReader{
		scenarios: map[string]*scenarios.Scenario{
			"s4": {RID: "s4", ParentOntologyCommit: "ont1"},
		},
		edits: map[string][]scenarios.ScenarioEdit{
			"s4": {
				{Seq: 1, Op: "deleteObject", ObjectType: "Airport", ObjectID: "JFK"},
				{Seq: 2, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(150)},
			},
		},
	}
	// Note: no SetScenarioConflictAuditor — emulates degraded boot.
	h := oss.NewHandler(svc)
	h.SetScenarioReader(reader)
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ont1/objects/Airport/JFK", nil)
	req.Header.Set("X-Scenario-Id", "s4")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d (degraded mode should still serve 404 for deleted)", rec.Code)
	}
}
