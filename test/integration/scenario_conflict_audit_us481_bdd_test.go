//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/scenarios"
)

// US-481 BDD — Scenario fold 冲突 audit.
//
// PRD acceptance:
//   - fold 链条返回 (view, conflicts []ScenarioConflict)
//   - 冲突写 pkg/audit log
//   - 测试：deleted+modify、duplicate add 各覆盖
//
// The two BDD scenarios below drive the wire path:
//   - GET /objects/{type}/{pk}   with X-Scenario-Id   → conflicts → 1 audit row
//   - POST /objects/{type}/aggregate with X-Scenario-Id → conflicts → 1 audit row
//
// Each scenario also includes the negative control "clean fold ⇒ zero audit
// rows" by sending a second request whose edits replay without conflict —
// otherwise an always-emit regression would silently pass both positive
// cases.

// us481BaseSvc is a deterministic stub satisfying oss.Service for the BDD
// fold-conflict scenarios. Mirrors the us479BaseSvc helper in the US-479
// BDD; kept separate so the two suites can evolve independently.
type us481BaseSvc struct {
	rows map[string]*oss.WireObject
	list []*oss.WireObject
}

func (s *us481BaseSvc) GetObject(_ context.Context, req oss.GetObjectRequest) (*oss.WireObject, error) {
	obj, ok := s.rows[req.PrimaryKey]
	if !ok {
		return nil, fmt.Errorf("not seeded: %s", req.PrimaryKey)
	}
	return obj, nil
}
func (s *us481BaseSvc) ListObjects(_ context.Context, _ oss.ListObjectsRequest) (*oss.ObjectPage, error) {
	return &oss.ObjectPage{Data: s.list}, nil
}
func (s *us481BaseSvc) SearchObjects(_ context.Context, _ oss.SearchObjectsRequest) (*oss.ObjectPage, error) {
	return nil, fmt.Errorf("not used")
}
func (s *us481BaseSvc) ListLinkedObjects(_ context.Context, _ oss.LinkedObjectsRequest) (*oss.ObjectPage, error) {
	return nil, fmt.Errorf("not used")
}
func (s *us481BaseSvc) GetLinkedObject(_ context.Context, _ oss.GetLinkedObjectRequest) (*oss.WireObject, error) {
	return nil, fmt.Errorf("not used")
}
func (s *us481BaseSvc) CountObjects(_ context.Context, _ oss.CountObjectsRequest) (*oss.CountObjectsResponse, error) {
	return nil, fmt.Errorf("not used")
}

// setupUS481Fixture spins testcontainers PG, runs migrations, seeds an
// ontology + case study + scenario row, wires a chi router with a real
// scenarios.PGRepo backing X-Scenario-Id resolution and an in-memory audit
// store. Returns the router, the scenarios repo (so the test can append the
// edits driving the conflict), the audit store (so the test can assert the
// emitted row), the ontology+scenario RIDs, and the seeded base svc.
func setupUS481Fixture(t *testing.T, baseSvc *us481BaseSvc) (*chi.Mux, scenarios.Repo, *audit.MemoryStore, string, string) {
	t.Helper()
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	ctx := context.Background()
	ontologyRID := "ri.ontology.main.ontology.us481"
	if _, err := pg.Pool.Exec(ctx,
		`INSERT INTO ontologies (rid, api_name, display_name) VALUES ($1, $2, $3)`,
		ontologyRID, "us481", "US-481 BDD"); err != nil {
		t.Fatalf("seed ontology: %v", err)
	}

	repo := scenarios.NewPGRepo(pg.Pool)
	cs, err := repo.CreateCaseStudy(ctx, "US-481 CS", ontologyRID, "tester")
	if err != nil {
		t.Fatalf("CreateCaseStudy: %v", err)
	}
	scen, err := repo.CreateScenario(ctx, cs.RID, "conflict-audit", ontologyRID, "tester")
	if err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}

	store := audit.NewMemoryStore()
	h := oss.NewHandler(baseSvc)
	h.SetScenarioReader(repo)
	h.SetScenarioConflictAuditor(oss.NewScenarioConflictAuditor(store))
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r, repo, store, ontologyRID, scen.RID
}

// TestBDD_US481_ModifyAfterDelete_GetObject_EmitsConflictAuditRow exercises
// the PRD-named "deleted+modify" conflict over the real GET /objects path.
// Given a base Airport JFK + scenario edits {deleteObject, modifyProperty},
// When GET .../objects/Airport/JFK with X-Scenario-Id,
// Then the response is 404 (deleted in scenario) AND exactly one audit row
//      with action=scenario.fold.conflict and byType[modify_after_delete]=1
//      lands in the audit store.
func TestBDD_US481_ModifyAfterDelete_GetObject_EmitsConflictAuditRow(t *testing.T) {
	base := &us481BaseSvc{
		rows: map[string]*oss.WireObject{
			"JFK": {PrimaryKey: "JFK", APIName: "Airport", Properties: map[string]any{"capacity": float64(100)}},
		},
	}
	router, repo, store, ontologyRID, scenarioRID := setupUS481Fixture(t, base)
	ctx := context.Background()

	if err := repo.AppendEdit(ctx, scenarioRID, scenarios.ScenarioEdit{
		Op: "deleteObject", ObjectType: "Airport", ObjectID: "JFK",
	}); err != nil {
		t.Fatalf("AppendEdit delete: %v", err)
	}
	if err := repo.AppendEdit(ctx, scenarioRID, scenarios.ScenarioEdit{
		Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(t, 999),
	}); err != nil {
		t.Fatalf("AppendEdit modify: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+ontologyRID+"/objects/Airport/JFK", nil)
	req.Header.Set("X-Scenario-Id", scenarioRID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d body=%s (delete should yield 404)", rec.Code, rec.Body.String())
	}

	events := store.Events()
	if len(events) != 1 {
		t.Fatalf("audit rows: got %d want 1 (%+v)", len(events), events)
	}
	evt := events[0]
	if evt.Action != oss.ScenarioConflictAction {
		t.Errorf("Action: got %q want %q", evt.Action, oss.ScenarioConflictAction)
	}
	if evt.ResourceType != "Scenario" || evt.ResourceRID != scenarioRID {
		t.Errorf("Resource: got (%s, %s) want (Scenario, %s)", evt.ResourceType, evt.ResourceRID, scenarioRID)
	}
	var diff struct {
		Operation     string                       `json:"operation"`
		ScenarioRID   string                       `json:"scenarioRid"`
		ConflictCount int                          `json:"conflictCount"`
		ByType        map[string]int               `json:"byType"`
		Conflicts     []scenarios.ScenarioConflict `json:"conflicts"`
	}
	if err := json.Unmarshal(evt.DiffJSON, &diff); err != nil {
		t.Fatalf("DiffJSON unmarshal: %v", err)
	}
	if diff.Operation != "getObject" {
		t.Errorf("operation: got %q want getObject", diff.Operation)
	}
	if diff.ScenarioRID != scenarioRID {
		t.Errorf("scenarioRid: got %q want %q", diff.ScenarioRID, scenarioRID)
	}
	if diff.ConflictCount != 1 || diff.ByType[scenarios.ConflictModifyAfterDelete] != 1 {
		t.Errorf("byType: got %+v want {modify_after_delete:1}", diff.ByType)
	}
	if len(diff.Conflicts) != 1 || diff.Conflicts[0].Property != "capacity" {
		t.Errorf("conflicts payload: got %+v", diff.Conflicts)
	}
	// Negative-control assertion lives in
	// TestBDD_US481_CleanFold_EmitsZeroAuditRows — running it as a separate
	// test gives a cleaner failure surface than a piggy-backed sub-check.
}

// TestBDD_US481_DuplicateCreate_Aggregate_EmitsConflictAuditRow exercises
// the PRD-named "duplicate add" conflict via the aggregate endpoint. The
// scenario synthesises a NEW O-1 not present in base (createObject #1) and
// then a second createObject for the same id — the second is the duplicate.
// Then the aggregate runs in the in-memory overlay path (no Bleve needed)
// and the audit row carries byType[duplicate_create]=1.
func TestBDD_US481_DuplicateCreate_Aggregate_EmitsConflictAuditRow(t *testing.T) {
	// Empty base: the scenario synthesises O-1 entirely via createObject.
	base := &us481BaseSvc{rows: map[string]*oss.WireObject{}, list: nil}
	router, repo, store, ontologyRID, scenarioRID := setupUS481Fixture(t, base)
	ctx := context.Background()

	if err := repo.AppendEdit(ctx, scenarioRID, scenarios.ScenarioEdit{
		Op: "createObject", ObjectType: "Order", ObjectID: "O-1", NewValue: raw(t, map[string]any{"total": 100}),
	}); err != nil {
		t.Fatalf("AppendEdit create #1: %v", err)
	}
	if err := repo.AppendEdit(ctx, scenarioRID, scenarios.ScenarioEdit{
		Op: "createObject", ObjectType: "Order", ObjectID: "O-1", NewValue: raw(t, map[string]any{"total": 200}),
	}); err != nil {
		t.Fatalf("AppendEdit create #2: %v", err)
	}

	body := []byte(`{"aggregation":[{"type":"count","name":"n"}]}`)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontologyRID+"/objects/Order/aggregate",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Scenario-Id", scenarioRID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}

	events := store.Events()
	if len(events) != 1 {
		t.Fatalf("audit rows: got %d want 1 (%+v)", len(events), events)
	}
	var diff struct {
		Operation     string         `json:"operation"`
		ConflictCount int            `json:"conflictCount"`
		ByType        map[string]int `json:"byType"`
	}
	if err := json.Unmarshal(events[0].DiffJSON, &diff); err != nil {
		t.Fatalf("DiffJSON unmarshal: %v", err)
	}
	if diff.Operation != "aggregate" {
		t.Errorf("operation: got %q want aggregate", diff.Operation)
	}
	if diff.ByType[scenarios.ConflictDuplicateCreate] != 1 {
		t.Errorf("byType: got %+v want {duplicate_create:1}", diff.ByType)
	}
}

// TestBDD_US481_CleanFold_EmitsZeroAuditRows is the negative control for the
// two positive scenarios above: when the fold has no conflicts, the audit
// store stays empty. Without this assertion, an always-emit regression in
// the auditor would still pass the conflict scenarios.
func TestBDD_US481_CleanFold_EmitsZeroAuditRows(t *testing.T) {
	base := &us481BaseSvc{
		rows: map[string]*oss.WireObject{
			"JFK": {PrimaryKey: "JFK", APIName: "Airport", Properties: map[string]any{"capacity": float64(100)}},
		},
	}
	router, repo, store, ontologyRID, scenarioRID := setupUS481Fixture(t, base)
	ctx := context.Background()

	// Single clean modifyProperty — no delete predecessor, no duplicate.
	if err := repo.AppendEdit(ctx, scenarioRID, scenarios.ScenarioEdit{
		Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(t, 150),
	}); err != nil {
		t.Fatalf("AppendEdit: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+ontologyRID+"/objects/Airport/JFK", nil)
	req.Header.Set("X-Scenario-Id", scenarioRID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := len(store.Events()); got != 0 {
		t.Errorf("expected 0 audit events for clean fold, got %d (%+v)", got, store.Events())
	}
}
