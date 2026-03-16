package actions

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// ApplyRequest is the request to apply an action.
type ApplyRequest struct {
	ActionType string                 `json:"actionType"` // API name
	Parameters map[string]interface{} `json:"parameters"`
}

// ApplyResult is the result of applying an action.
type ApplyResult struct {
	ActionRID string        `json:"actionRid"`
	Edits     []funnel.Edit `json:"edits"`
	BatchID   string        `json:"batchId"`
	Offset    uint64        `json:"offset"`
}

// Executor executes actions.
type Executor struct {
	omsRepo   oms.Repository
	publisher *funnel.Publisher
}

// NewExecutor creates a new action executor.
func NewExecutor(omsRepo oms.Repository, publisher *funnel.Publisher) *Executor {
	return &Executor{
		omsRepo:   omsRepo,
		publisher: publisher,
	}
}

// Apply executes an action.
func (e *Executor) Apply(ctx context.Context, ontologyRID string, req *ApplyRequest) (*ApplyResult, error) {
	// Step 1: Look up ActionType
	actionTypes, err := e.omsRepo.ListActionTypes(ctx, ontologyRID)
	if err != nil {
		return nil, fmt.Errorf("list action types: %w", err)
	}

	var actionType *oms.ActionType
	for i := range actionTypes {
		if actionTypes[i].APIName == req.ActionType {
			actionType = &actionTypes[i]
			break
		}
	}
	if actionType == nil {
		return nil, fmt.Errorf("action type %q not found", req.ActionType)
	}

	// Step 2: Parse parameter definitions
	paramDefs, err := ParseParameterDefs(actionType.Parameters)
	if err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	// Step 3: Validate parameters
	if err := ValidateParameters(paramDefs, req.Parameters); err != nil {
		return nil, fmt.Errorf("validate params: %w", err)
	}

	// Step 4: Parse rules
	rules, err := ParseRules(actionType.Rules)
	if err != nil {
		return nil, fmt.Errorf("parse rules: %w", err)
	}

	// Step 5: Execute rules to generate edits
	edits, err := ExecuteRules(rules, req.Parameters)
	if err != nil {
		return nil, fmt.Errorf("execute rules: %w", err)
	}

	// Step 6: Collapse edits
	edits = CollapseEdits(edits)

	if len(edits) == 0 {
		return &ApplyResult{
			ActionRID: actionType.RID,
			Edits:     nil,
		}, nil
	}

	// Step 7: Create EditBatch
	batch := &funnel.EditBatch{
		ID:        uuid.New().String(),
		Edits:     edits,
		UserID:    "system", // TODO: get from context when auth is wired
		Timestamp: time.Now(),
	}

	// Step 8: Publish to funnel
	var offset uint64
	if e.publisher != nil {
		offset, err = e.publisher.Publish(batch)
		if err != nil {
			return nil, fmt.Errorf("publish edits: %w", err)
		}
	}

	// Step 9: Return result
	return &ApplyResult{
		ActionRID: actionType.RID,
		Edits:     edits,
		BatchID:   batch.ID,
		Offset:    offset,
	}, nil
}
