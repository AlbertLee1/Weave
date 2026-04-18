package oss

import (
	"context"
	"encoding/json"

	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
)

// DataAccessAction is the audit_events.action string emitted for every
// successful OSS read on an ObjectType that has AuditDataAccess = true.
// Kept as a constant so dashboards / SIEM rules can filter on a stable key.
const DataAccessAction = "data.access"

// DataAccessAuditor records successful OSS read operations (getObject,
// listObjects, searchObjects, linkedObjects, loadObjectSet) to the audit
// store when the target ObjectType has AuditDataAccess = true.
//
// The auditor is a thin convenience wrapper over audit.Record. When store
// is nil the auditor becomes a no-op so degraded-mode test routers don't
// need to wire a sink.
type DataAccessAuditor struct {
	store audit.Store
}

// NewDataAccessAuditor returns an auditor that writes to store. A nil store
// produces an always-no-op auditor, matching the other optional-hook
// conventions in cmd/server.
func NewDataAccessAuditor(store audit.Store) *DataAccessAuditor {
	return &DataAccessAuditor{store: store}
}

// Enabled reports whether audits should be emitted for ot. Pointer-receiver
// nil-safe so call sites can unconditionally invoke the helper regardless of
// wiring.
func (a *DataAccessAuditor) Enabled(ot *oms.ObjectType) bool {
	if a == nil || a.store == nil || ot == nil {
		return false
	}
	return ot.AuditDataAccess
}

// Record persists a single data.access audit row. Caller-provided details
// are JSON-marshalled into AuditEvent.DiffJSON alongside the operation
// identifier so SIEM consumers see a consistent shape regardless of which
// read path produced the row.
//
// Record is a no-op when the auditor is nil or Enabled(ot) returns false.
// Errors from audit.Record are swallowed — the original read result is the
// authoritative response and a failed audit must not turn a 200 into a 500.
func (a *DataAccessAuditor) Record(ctx context.Context, ot *oms.ObjectType, operation string, details map[string]any) {
	if !a.Enabled(ot) {
		return
	}

	payload := map[string]any{"operation": operation}
	for k, v := range details {
		payload[k] = v
	}

	diff, err := json.Marshal(payload)
	if err != nil {
		diff = json.RawMessage(`{"operation":"` + operation + `"}`)
	}

	actor := ""
	if u := auth.UserFromContext(ctx); u != nil {
		actor = u.ID
	}
	info := audit.ClientInfoFromContext(ctx)

	_ = audit.Record(ctx, a.store, audit.AuditEvent{
		ActorID:      actor,
		Action:       DataAccessAction,
		ResourceType: "ObjectType",
		ResourceRID:  ot.RID,
		DiffJSON:     diff,
		IP:           info.IP,
		UserAgent:    info.UserAgent,
	})
}
