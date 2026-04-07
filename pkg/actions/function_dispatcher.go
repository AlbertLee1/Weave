package actions

import (
	"context"
	"fmt"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// FunctionDispatcher delegates action rule evaluation to an external function.
// Callers pass the parameters; the dispatcher returns the list of edits the
// function wants to apply. The executor then collapses and publishes these
// edits exactly like regular rule-derived edits.
type FunctionDispatcher interface {
	// Dispatch invokes the function identified by actionType.FunctionRID
	// with the given parameters and returns the edits to apply.
	Dispatch(ctx context.Context, actionType *oms.ActionType, parameters map[string]interface{}) ([]funnel.Edit, error)
}

// FunctionRequest is the JSON envelope sent to the function. The shape is the
// wire contract function authors program against; do not break field names
// without bumping the protocol version.
type FunctionRequest struct {
	ActionTypeRID string                 `json:"actionTypeRid"`
	ActionTypeAPI string                 `json:"actionTypeApiName"`
	FunctionRID   string                 `json:"functionRid"`
	Parameters    map[string]interface{} `json:"parameters"`
}

// FunctionResponse is what the function must return. A non-empty Error
// indicates the function rejected the action; the dispatcher converts that
// into a Go error so the executor can fail the action cleanly.
type FunctionResponse struct {
	Edits []FunctionEdit `json:"edits"`
	Error string         `json:"error,omitempty"`
}

// FunctionEdit mirrors funnel.Edit but uses JSON-friendly types so function
// authors don't need to import Weave's internal packages. Type is one of
// "CREATE", "MODIFY", "DELETE".
type FunctionEdit struct {
	Type       string                 `json:"type"`
	ObjectType string                 `json:"objectType"`
	PrimaryKey string                 `json:"primaryKey"`
	Properties map[string]interface{} `json:"properties,omitempty"`
}

// ToFunnelEdit converts a FunctionEdit into the internal funnel.Edit type used
// by the executor pipeline. Returns an error for unknown edit types so a
// misbehaving function cannot smuggle no-op or malformed edits downstream.
func (fe FunctionEdit) ToFunnelEdit() (funnel.Edit, error) {
	switch fe.Type {
	case "CREATE":
		return funnel.Edit{
			Type:       funnel.EditTypeCreate,
			ObjectType: fe.ObjectType,
			PrimaryKey: fe.PrimaryKey,
			Properties: fe.Properties,
		}, nil
	case "MODIFY":
		return funnel.Edit{
			Type:       funnel.EditTypeModify,
			ObjectType: fe.ObjectType,
			PrimaryKey: fe.PrimaryKey,
			Properties: fe.Properties,
		}, nil
	case "DELETE":
		return funnel.Edit{
			Type:       funnel.EditTypeDelete,
			ObjectType: fe.ObjectType,
			PrimaryKey: fe.PrimaryKey,
		}, nil
	default:
		return funnel.Edit{}, fmt.Errorf("function dispatcher: unknown edit type %q", fe.Type)
	}
}
