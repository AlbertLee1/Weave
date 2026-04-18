package main

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
)

// loadObjectSetAuditAdapter satisfies the objectset.DataAccessAuditor
// interface by looking up the target ObjectType through the OMS repo and
// delegating to oss.DataAccessAuditor.Record. The two-step lookup keeps
// pkg/oss/objectset free of any OMS / audit import: the handler passes
// down an opaque (ontologyAPIName, objectTypeAPIName) pair and the adapter
// resolves the ObjectType (so AuditDataAccess can gate the write) before
// forwarding to the shared auditor.
type loadObjectSetAuditAdapter struct {
	repo     oms.Repository
	auditor  *oss.DataAccessAuditor
	resolver func(ctx context.Context, ontologyRID, apiName string) (*oms.ObjectType, error)
}

func newLoadObjectSetAuditAdapter(repo oms.Repository, auditor *oss.DataAccessAuditor) *loadObjectSetAuditAdapter {
	return &loadObjectSetAuditAdapter{
		repo:    repo,
		auditor: auditor,
		resolver: func(ctx context.Context, ontologyRID, apiName string) (*oms.ObjectType, error) {
			return repo.GetObjectTypeByAPIName(ctx, ontologyRID, apiName)
		},
	}
}

// RecordLoadObjectSet implements objectset.DataAccessAuditor. The adapter
// resolves the target ObjectType and forwards to the shared auditor; lookup
// failures are swallowed because an audit must never turn a 200 read into a
// 500. The ObjectType's AuditDataAccess flag still gates the eventual
// audit_events insert inside DataAccessAuditor.Record.
func (a *loadObjectSetAuditAdapter) RecordLoadObjectSet(ctx context.Context, ontologyRID, objectTypeAPIName string, details map[string]any) {
	if a == nil || a.auditor == nil || a.repo == nil || objectTypeAPIName == "" {
		return
	}
	ot, err := a.resolver(ctx, ontologyRID, objectTypeAPIName)
	if err != nil || ot == nil {
		return
	}
	payload := map[string]any{"objectType": objectTypeAPIName}
	for k, v := range details {
		payload[k] = v
	}
	a.auditor.Record(ctx, ot, "loadObjectSet", payload)
}
