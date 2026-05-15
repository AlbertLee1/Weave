// Package functionactions wires OMS function-backed ActionTypes (US-058)
// into Vertex Scenarios (VTX-051). It owns three concerns kept HTTP-thin
// so the moving pieces stay unit-testable:
//
//   - the registration shape — a FunctionActionBinding row that binds an
//     OMS ActionType (whose action_types.is_function_backed=true and
//     action_types.function_rid points at an oms.Function) to a list of
//     OutputMapping rules describing how the Function's flat output
//     becomes property edits on Scenario fork objects;
//   - the routing decision — ResolveActionMode tells Vertex Scenario
//     execution whether to call the bound Function and map its output, or
//     fall back to the standard OMS ActionExecutor (whose edits Vertex
//     redirects to scenario_edits instead of the main ontology);
//   - the output-mapping pure logic — MapFunctionOutputToScenarioEdits
//     and FunnelEditsToScenarioEdits, both pure functions so the wiring
//     story in cmd/server/main.go can stub them without standing up a
//     full executor.
package functionactions

import (
	"time"

	"github.com/liyang/weave/pkg/rid"
)

// FunctionActionBinding is the row stored in vertex_function_actions. It
// pairs an OMS function-backed ActionType with the Vertex-specific
// OutputMappings that turn the bound Function's return payload into
// modifyProperty edits inside a Scenario fork.
type FunctionActionBinding struct {
	RID            string          `json:"rid"`
	OntologyRID    string          `json:"ontologyRid"`
	ActionTypeRID  string          `json:"actionTypeRid"`
	FunctionRID    string          `json:"functionRid"`
	OutputMappings []OutputMapping `json:"outputMappings"`
	CreatedBy      string          `json:"createdBy,omitempty"`
	CreatedAt      time.Time       `json:"createdAt,omitempty"`
}

// OutputMapping is one "Function output field → object property" rule.
// At invocation time, PrimaryKeyParameter names the invocation parameter
// whose value is the target object's primary key, OutputField names the
// key inside the Function's output map, and Property is the target
// property on the resulting modifyProperty edit. ObjectType is the API
// name of the target ObjectType (stamped onto the scenario_edits row so
// the reader knows which object table to fold the edit onto).
type OutputMapping struct {
	OutputField         string `json:"outputField"`
	ObjectType          string `json:"objectType"`
	PrimaryKeyParameter string `json:"primaryKeyParameter"`
	Property            string `json:"property"`
}

// ActionMode is the routing decision Vertex Scenario execution takes for
// a given ActionType. ActionModeFunctionBacked routes through the bound
// Function + OutputMapping pipeline; ActionModeStandard routes through
// the regular OMS ActionExecutor with the scenario_edits writer wired in.
type ActionMode string

// ActionMode constants. Kept narrow on purpose — only two routing
// outcomes — so future additions force callers to update their switch.
const (
	ActionModeFunctionBacked ActionMode = "function_backed"
	ActionModeStandard       ActionMode = "standard"
)

// ScenarioEdit mirrors one scenario_edits row's logical shape. Only the
// fields the Vertex routing layer needs are present; downstream writers
// fill in scenario_rid + seq + created_at at insert time.
type ScenarioEdit struct {
	Op         string      `json:"op"`
	ObjectType string      `json:"objectType,omitempty"`
	ObjectID   string      `json:"objectId,omitempty"`
	Property   string      `json:"property,omitempty"`
	NewValue   interface{} `json:"newValue,omitempty"`
	LinkType   string      `json:"linkType,omitempty"`
	SrcID      string      `json:"srcId,omitempty"`
	DstID      string      `json:"dstId,omitempty"`
}

// Scenario edit op constants — mirror the CHECK constraint on
// scenario_edits.op so the routing layer never produces a row PostgreSQL
// would reject at insert time.
const (
	OpCreateObject   = "createObject"
	OpModifyProperty = "modifyProperty"
	OpDeleteObject   = "deleteObject"
	OpAddLink        = "addLink"
	OpDeleteLink     = "deleteLink"
)

// NewFunctionActionRID generates a fresh RID for a binding row. Kept out
// of pkg/rid for the same reason NewDeploymentRID is: the resource type
// is Vertex-specific and adding it there would force every consumer of
// the rid package to re-test the deny-list.
func NewFunctionActionRID() string {
	return rid.New("vertex", "main", "function-action")
}
