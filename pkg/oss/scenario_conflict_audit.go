package oss

import (
	"context"
	"encoding/json"

	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/scenarios"
)

// ScenarioConflictAction is the audit_events.action string emitted whenever
// scenario fold surfaces a non-empty []ScenarioConflict for one Read
// request. Stable for SIEM filters / dashboards.
const ScenarioConflictAction = "scenario.fold.conflict"

// ScenarioConflictAuditor records scenario fold conflicts surfaced by
// FoldObjectWithConflicts / FoldLinksWithConflicts to the audit store.
//
// One audit row is emitted per (scenarioRID, operation) call. The conflicts
// slice is JSON-marshalled into DiffJSON so SIEM consumers can pivot per
// conflict type. A nil receiver or nil store short-circuits all calls — this
// matches the other optional-hook conventions in cmd/server.
type ScenarioConflictAuditor struct {
	store audit.Store
}

// NewScenarioConflictAuditor returns an auditor backed by store. A nil store
// produces an always-no-op auditor so degraded-mode test routers don't have
// to wire a sink.
func NewScenarioConflictAuditor(store audit.Store) *ScenarioConflictAuditor {
	return &ScenarioConflictAuditor{store: store}
}

// Record persists one audit row summarising the fold conflicts surfaced for
// scenarioRID by operation (e.g. "getObject" / "listObjects" /
// "listLinkedObjects" / "aggregate"). No-op when:
//
//   - the auditor or its store is nil
//   - scenarioRID is empty
//   - conflicts is empty (no work product to audit)
//
// Errors from audit.Record are swallowed. The original read result is the
// authoritative response and a failed audit must never turn a 200 into 500.
func (a *ScenarioConflictAuditor) Record(ctx context.Context, scenarioRID, operation string, conflicts []scenarios.ScenarioConflict) {
	if a == nil || a.store == nil {
		return
	}
	if scenarioRID == "" || len(conflicts) == 0 {
		return
	}

	byType := map[string]int{}
	for _, c := range conflicts {
		byType[c.ConflictType]++
	}
	payload := map[string]any{
		"operation":     operation,
		"scenarioRid":   scenarioRID,
		"conflictCount": len(conflicts),
		"byType":        byType,
		"conflicts":     conflicts,
	}
	diff, err := json.Marshal(payload)
	if err != nil {
		// Fallback to a minimal shape — losing the conflict list is better
		// than dropping the audit row entirely.
		diff = json.RawMessage(`{"operation":"` + operation + `","scenarioRid":"` + scenarioRID + `","conflictCount":0,"encodeError":true}`)
	}

	actor := ""
	if u := auth.UserFromContext(ctx); u != nil {
		actor = u.ID
	}
	info := audit.ClientInfoFromContext(ctx)

	_ = audit.Record(ctx, a.store, audit.AuditEvent{
		ActorID:      actor,
		Action:       ScenarioConflictAction,
		ResourceType: "Scenario",
		ResourceRID:  scenarioRID,
		DiffJSON:     diff,
		IP:           info.IP,
		UserAgent:    info.UserAgent,
	})
}
