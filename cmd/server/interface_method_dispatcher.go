package main

import (
	"context"
	"encoding/json"

	"github.com/liyang/weave/pkg/actions"
)

// interfaceMethodActionDispatcher adapts *actions.Executor to the narrow
// oms.InterfaceMethodActionDispatcher interface used by the polymorphic
// invoke handler (US-214). Kept in cmd/server to avoid an import cycle —
// pkg/oms stays free of any pkg/actions dependency.
type interfaceMethodActionDispatcher struct {
	executor *actions.Executor
}

func newInterfaceMethodDispatcher(executor *actions.Executor) *interfaceMethodActionDispatcher {
	return &interfaceMethodActionDispatcher{executor: executor}
}

// Dispatch forwards the resolved ActionType + parameters to the configured
// executor. Returns a small JSON payload describing the edits so the
// invoke response carries a trace of what happened, matching Foundry's
// convention of returning `edits: {...}` counts per action call.
func (d *interfaceMethodActionDispatcher) Dispatch(ctx context.Context, ontologyRID, actionAPIName string, parameters map[string]interface{}) (json.RawMessage, error) {
	result, err := d.executor.Apply(ctx, ontologyRID, &actions.ApplyRequest{
		ActionType: actionAPIName,
		Parameters: parameters,
	})
	if err != nil {
		return nil, err
	}
	summary := map[string]interface{}{
		"actionRid": result.ActionRID,
		"editCount": len(result.Edits),
		"batchId":   result.BatchID,
	}
	return json.Marshal(summary)
}
