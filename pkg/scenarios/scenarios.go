// Package scenarios persists Vertex Case Studies, Scenarios, scenario edits,
// and Model Mesh overrides. A Scenario is an application-layer fork over a
// base ontology commit: writes go into scenario_edits (a delta log), reads
// fold those edits over the base view. Freezing a Scenario flips
// immutable=true; further AppendEdit calls return ErrScenarioImmutable. The
// immutability check lives here (not as a DB constraint) so the API surfaces
// a typed error instead of an opaque SQL CHECK violation.
package scenarios

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ErrScenarioNotFound is returned when a scenario RID does not exist.
var ErrScenarioNotFound = errors.New("scenario not found")

// ErrScenarioImmutable is returned when AppendEdit is called on a frozen
// scenario. Application-layer enforced (DB column is just a flag).
var ErrScenarioImmutable = errors.New("scenario is immutable")

// CaseStudy groups one or more Scenarios under a single ontology snapshot.
type CaseStudy struct {
	RID         string
	Name        string
	OntologyRID string
	CreatedBy   string
	CreatedAt   time.Time
}

// Scenario is a forkable view of the base ontology. Edits append to
// scenario_edits; reads fold them on top of the base. parent_ontology_commit
// pins which ontology commit this scenario was forked from.
type Scenario struct {
	RID                  string
	CaseStudyRID         string
	Name                 string
	ParentOntologyCommit string
	Status               string // draft | frozen | archived
	Immutable            bool
	CreatedBy            string
	CreatedAt            time.Time
}

// ScenarioEdit is one entry in the delta log. Op constrains what the other
// fields mean — e.g. modifyProperty uses object_type+object_id+property+new_value;
// addLink uses link_type+src_id+dst_id. The seq column is BIGSERIAL so reads
// can replay in insertion order across all writers without coordination.
type ScenarioEdit struct {
	ScenarioRID string
	Seq         int64
	Op          string          // createObject | modifyProperty | deleteObject | addLink | deleteLink
	ObjectType  string
	ObjectID    string
	Property    string
	NewValue    json.RawMessage // JSONB; nil for delete-style ops
	LinkType    string
	SrcID       string
	DstID       string
	CreatedAt   time.Time
}

// ScenarioOverride captures a Model Mesh parameter override (scalar or
// per-object). ObjectID is "" for whole-model overrides. The PK is
// (scenario_rid, model_rid, parameter, object_id) — upserts replace by key.
type ScenarioOverride struct {
	ScenarioRID string
	ModelRID    string
	Parameter   string
	ObjectID    string
	Value       json.RawMessage
	AppliedAt   time.Time
}

// Repo is the persistence boundary for Vertex scenarios. Mirrors the style
// of pkg/oms.Repository: small, ctx-first, returns sentinel errors that
// callers can errors.Is against. Implementations must be safe for concurrent
// use.
type Repo interface {
	CreateCaseStudy(ctx context.Context, name, ontologyRID, createdBy string) (*CaseStudy, error)
	GetCaseStudy(ctx context.Context, rid string) (*CaseStudy, error)

	CreateScenario(ctx context.Context, caseStudyRID, name, parentCommit, createdBy string) (*Scenario, error)
	GetScenario(ctx context.Context, rid string) (*Scenario, error)

	// AppendEdit returns ErrScenarioImmutable if the scenario is frozen and
	// ErrScenarioNotFound if the RID does not exist.
	AppendEdit(ctx context.Context, scenarioRID string, edit ScenarioEdit) error
	ListEdits(ctx context.Context, scenarioRID string) ([]ScenarioEdit, error)

	// Freeze flips immutable=true and status='frozen'. Idempotent: freezing
	// an already-frozen scenario is a no-op.
	Freeze(ctx context.Context, scenarioRID string) error

	UpsertOverride(ctx context.Context, ov ScenarioOverride) error
	ListOverrides(ctx context.Context, scenarioRID string) ([]ScenarioOverride, error)
}
