package aip

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/liyang/weave/pkg/scenarios"
)

// memRepo is a minimal in-memory scenarios.Repo that only implements the
// surface the SimulationRunner exercises. CreateCaseStudy / GetCaseStudy /
// GetScenario / Freeze / UpsertOverride / ListOverrides return ErrUnsupported
// so we cannot accidentally rely on them.
type memRepo struct {
	mu    sync.Mutex
	cases map[string]*scenarios.CaseStudy
	scen  map[string]*scenarios.Scenario
	edits map[string][]scenarios.ScenarioEdit
	nseq  int64
	nrid  int
}

func newMemRepo() *memRepo {
	return &memRepo{
		cases: map[string]*scenarios.CaseStudy{},
		scen:  map[string]*scenarios.Scenario{},
		edits: map[string][]scenarios.ScenarioEdit{},
	}
}

var errUnsupported = errors.New("memRepo: not supported")

func (m *memRepo) CreateCaseStudy(_ context.Context, name, ontRID, createdBy string) (*scenarios.CaseStudy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nrid++
	rid := "ri.vertex.main.case-study.cs" + itoa(m.nrid)
	cs := &scenarios.CaseStudy{RID: rid, Name: name, OntologyRID: ontRID, CreatedBy: createdBy}
	m.cases[rid] = cs
	return cs, nil
}

func (m *memRepo) GetCaseStudy(_ context.Context, rid string) (*scenarios.CaseStudy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cs, ok := m.cases[rid]; ok {
		return cs, nil
	}
	return nil, scenarios.ErrScenarioNotFound
}

func (m *memRepo) CreateScenario(_ context.Context, csRID, name, parent, createdBy string) (*scenarios.Scenario, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nrid++
	rid := "ri.vertex.main.scenario.s" + itoa(m.nrid)
	s := &scenarios.Scenario{RID: rid, CaseStudyRID: csRID, Name: name, ParentOntologyCommit: parent, Status: "draft", CreatedBy: createdBy}
	m.scen[rid] = s
	m.edits[rid] = nil
	return s, nil
}

func (m *memRepo) GetScenario(_ context.Context, rid string) (*scenarios.Scenario, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.scen[rid]; ok {
		return s, nil
	}
	return nil, scenarios.ErrScenarioNotFound
}

func (m *memRepo) AppendEdit(_ context.Context, sRID string, e scenarios.ScenarioEdit) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.scen[sRID]
	if !ok {
		return scenarios.ErrScenarioNotFound
	}
	if s.Immutable {
		return scenarios.ErrScenarioImmutable
	}
	m.nseq++
	e.Seq = m.nseq
	e.ScenarioRID = sRID
	m.edits[sRID] = append(m.edits[sRID], e)
	return nil
}

func (m *memRepo) ListEdits(_ context.Context, sRID string) ([]scenarios.ScenarioEdit, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]scenarios.ScenarioEdit, len(m.edits[sRID]))
	copy(out, m.edits[sRID])
	return out, nil
}

func (m *memRepo) Freeze(_ context.Context, sRID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.scen[sRID]; ok {
		s.Immutable = true
		s.Status = "frozen"
		return nil
	}
	return scenarios.ErrScenarioNotFound
}

func (m *memRepo) UpsertOverride(_ context.Context, _ scenarios.ScenarioOverride) error {
	return errUnsupported
}

func (m *memRepo) ListOverrides(_ context.Context, _ string) ([]scenarios.ScenarioOverride, error) {
	return nil, errUnsupported
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRunSimulationCase_Given_UseOntologyTrue_When_RunsCase_Then_AppliesEditsToFork(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	cs, err := repo.CreateCaseStudy(ctx, "AIP", "ri.vertex.main.ontology.x", "alice")
	if err != nil {
		t.Fatalf("CreateCaseStudy: %v", err)
	}

	tc := SimulationCase{
		Name:                  "cap150",
		UseOntologySimulation: true,
		Edits: []scenarios.ScenarioEdit{
			{Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: json.RawMessage(`150`)},
		},
		ExpectedEdits: []scenarios.ScenarioEdit{
			{Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: json.RawMessage(`150`)},
		},
	}

	res, err := RunSimulationCase(ctx, repo, cs.RID, "commit-A", "alice", tc)
	if err != nil {
		t.Fatalf("RunSimulationCase: %v", err)
	}
	if !res.Passed {
		t.Fatalf("expected pass; diff=%s", res.Diff)
	}
	// Main is untouched: only the scenario carries the edit.
	if got := len(repo.edits[res.ScenarioRID]); got != 1 {
		t.Fatalf("scenario edits = %d, want 1", got)
	}
}

func TestRunSimulationCase_Given_UseOntologyFalse_When_Run_Then_ErrorsOut(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	cs, _ := repo.CreateCaseStudy(ctx, "AIP", "ri.vertex.main.ontology.x", "alice")
	_, err := RunSimulationCase(ctx, repo, cs.RID, "commit-A", "alice", SimulationCase{Name: "x"})
	if err == nil {
		t.Fatal("expected error when UseOntologySimulation is false")
	}
}

func TestRunSimulationCase_Given_MismatchedExpected_When_Run_Then_FailsWithDiff(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	cs, _ := repo.CreateCaseStudy(ctx, "AIP", "ri.vertex.main.ontology.x", "alice")
	tc := SimulationCase{
		Name:                  "mismatch",
		UseOntologySimulation: true,
		Edits: []scenarios.ScenarioEdit{
			{Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: json.RawMessage(`150`)},
		},
		ExpectedEdits: []scenarios.ScenarioEdit{
			{Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: json.RawMessage(`999`)},
		},
	}
	res, err := RunSimulationCase(ctx, repo, cs.RID, "commit-A", "alice", tc)
	if err != nil {
		t.Fatalf("RunSimulationCase: %v", err)
	}
	if res.Passed {
		t.Fatalf("expected failed; got pass")
	}
	if res.Diff == "" {
		t.Fatal("Diff should be non-empty for failed case")
	}
}

func TestRunSimulationSuite_Given_MixedCases_When_Run_Then_AggregatedReport(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	cs, _ := repo.CreateCaseStudy(ctx, "AIP", "ri.vertex.main.ontology.x", "alice")
	cases := []SimulationCase{
		{
			Name:                  "pass",
			UseOntologySimulation: true,
			Edits:                 []scenarios.ScenarioEdit{{Op: "deleteObject", ObjectType: "Airport", ObjectID: "JFK"}},
			ExpectedEdits:         []scenarios.ScenarioEdit{{Op: "deleteObject", ObjectType: "Airport", ObjectID: "JFK"}},
		},
		{
			Name:                  "fail",
			UseOntologySimulation: true,
			Edits:                 []scenarios.ScenarioEdit{{Op: "deleteObject", ObjectType: "Airport", ObjectID: "JFK"}},
			ExpectedEdits:         []scenarios.ScenarioEdit{{Op: "deleteObject", ObjectType: "Airport", ObjectID: "LAX"}},
		},
	}
	rep, err := RunSimulationSuite(ctx, repo, cs.RID, "commit-A", "alice", cases)
	if err != nil {
		t.Fatalf("RunSimulationSuite: %v", err)
	}
	if rep.Passed != 1 || rep.Failed != 1 {
		t.Fatalf("got passed=%d failed=%d, want 1/1", rep.Passed, rep.Failed)
	}
}
