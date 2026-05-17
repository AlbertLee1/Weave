package oss_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/scenarios"
)

// US-481 unit tests for the ScenarioConflictAuditor: it must (a) emit one
// audit row per (scenario, operation) call summarising the conflicts, (b)
// degrade to a no-op when wired with a nil store or called with zero
// conflicts, and (c) carry actor / client info from context.

func TestUS481_ScenarioConflictAuditor_NilStoreIsNoop(t *testing.T) {
	a := oss.NewScenarioConflictAuditor(nil)
	a.Record(context.Background(), "ri.scenario.main.s1", "getObject", []scenarios.ScenarioConflict{
		{ConflictType: scenarios.ConflictModifyAfterDelete, Op: "modifyProperty", ObjectType: "Order", ObjectID: "O-1", EditSeqs: []int64{1, 2}},
	})
	// No panic = pass.
}

func TestUS481_ScenarioConflictAuditor_ZeroConflictsIsNoop(t *testing.T) {
	store := audit.NewMemoryStore()
	a := oss.NewScenarioConflictAuditor(store)
	a.Record(context.Background(), "ri.scenario.main.s1", "getObject", nil)
	if got := len(store.Events()); got != 0 {
		t.Fatalf("expected 0 events when conflicts is nil, got %d", got)
	}
	a.Record(context.Background(), "ri.scenario.main.s1", "getObject", []scenarios.ScenarioConflict{})
	if got := len(store.Events()); got != 0 {
		t.Fatalf("expected 0 events when conflicts is empty, got %d", got)
	}
}

func TestUS481_ScenarioConflictAuditor_RecordsScenarioRIDAndActor(t *testing.T) {
	store := audit.NewMemoryStore()
	a := oss.NewScenarioConflictAuditor(store)

	ctx := auth.WithUser(context.Background(), &auth.User{ID: "user:alice"})
	ctx = audit.WithClientInfo(ctx, audit.ClientInfo{IP: "10.0.0.1", UserAgent: "weave-test"})

	conflicts := []scenarios.ScenarioConflict{
		{ConflictType: scenarios.ConflictModifyAfterDelete, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", EditSeqs: []int64{1, 2}},
		{ConflictType: scenarios.ConflictDuplicateCreate, Op: "createObject", ObjectType: "Order", ObjectID: "O-1", EditSeqs: []int64{3, 4}},
	}
	a.Record(ctx, "ri.scenario.main.s1", "listObjects", conflicts)

	events := store.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	evt := events[0]
	if evt.Action != oss.ScenarioConflictAction {
		t.Errorf("Action: got %q want %q", evt.Action, oss.ScenarioConflictAction)
	}
	if evt.ResourceType != "Scenario" {
		t.Errorf("ResourceType: got %q want Scenario", evt.ResourceType)
	}
	if evt.ResourceRID != "ri.scenario.main.s1" {
		t.Errorf("ResourceRID: got %q want ri.scenario.main.s1", evt.ResourceRID)
	}
	if evt.ActorID != "user:alice" {
		t.Errorf("ActorID: got %q want user:alice", evt.ActorID)
	}
	if evt.IP != "10.0.0.1" || evt.UserAgent != "weave-test" {
		t.Errorf("ClientInfo missing: IP=%q UA=%q", evt.IP, evt.UserAgent)
	}

	var diff struct {
		Operation     string                        `json:"operation"`
		ScenarioRID   string                        `json:"scenarioRid"`
		Conflicts     []scenarios.ScenarioConflict  `json:"conflicts"`
		ConflictCount int                           `json:"conflictCount"`
		ByType        map[string]int                `json:"byType"`
	}
	if err := json.Unmarshal(evt.DiffJSON, &diff); err != nil {
		t.Fatalf("unmarshal DiffJSON: %v", err)
	}
	if diff.Operation != "listObjects" {
		t.Errorf("operation: got %q want listObjects", diff.Operation)
	}
	if diff.ScenarioRID != "ri.scenario.main.s1" {
		t.Errorf("scenarioRid: got %q", diff.ScenarioRID)
	}
	if diff.ConflictCount != 2 {
		t.Errorf("conflictCount: got %d want 2", diff.ConflictCount)
	}
	if len(diff.Conflicts) != 2 {
		t.Errorf("conflicts len: got %d want 2", len(diff.Conflicts))
	}
	if diff.ByType[scenarios.ConflictModifyAfterDelete] != 1 || diff.ByType[scenarios.ConflictDuplicateCreate] != 1 {
		t.Errorf("byType: got %+v", diff.ByType)
	}
}
