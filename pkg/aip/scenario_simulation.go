// VTX-106 — Scenario-fork test-case runner for AIP Logic.
//
// When an AIP Logic test case sets UseOntologySimulation=true the runner
// must not touch main: it forks a Scenario off the case study, applies the
// case's edits to the fork, lists the resulting edits, and compares them
// to the expected edits. Main is read-only, the scenario carries the
// experiment, and the comparison decides pass / fail.
package aip

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"

	"github.com/liyang/weave/pkg/scenarios"
)

// SimulationCase is one entry in an AIP Logic test suite that opts into
// the Scenario-fork runner. Edits are applied in order; ExpectedEdits is
// the canonical list the runner compares against after the apply pass.
type SimulationCase struct {
	Name                  string
	UseOntologySimulation bool
	Edits                 []scenarios.ScenarioEdit
	ExpectedEdits         []scenarios.ScenarioEdit
}

// SimulationResult reports the outcome of one SimulationCase.
type SimulationResult struct {
	Name        string
	ScenarioRID string
	Passed      bool
	Diff        string // human-readable mismatch summary; empty when Passed
}

// SimulationReport aggregates SimulationResults so the AIP Logic handler
// can emit "N passed / M failed" without recounting at every call site.
type SimulationReport struct {
	Passed  int
	Failed  int
	Results []SimulationResult
}

// RunSimulationCase forks a Scenario off caseStudyRID, replays
// tc.Edits into the fork via repo.AppendEdit, lists the resulting
// edits, and compares to tc.ExpectedEdits. It deliberately does not
// freeze the scenario afterwards — leaving it draft makes diagnostics
// possible from the failed-test debug surface (VTX-102).
//
// The main ontology is not touched: every write goes into the scenario
// delta log. Callers that need a stricter "main untouched" assertion
// should make their own snapshot before and after — this runner only
// promises it issues no main-side write itself.
func RunSimulationCase(
	ctx context.Context,
	repo scenarios.Repo,
	caseStudyRID string,
	parentCommit string,
	createdBy string,
	tc SimulationCase,
) (SimulationResult, error) {
	if !tc.UseOntologySimulation {
		return SimulationResult{}, errors.New("SimulationCase.UseOntologySimulation must be true")
	}
	scenario, err := repo.CreateScenario(ctx, caseStudyRID, "aip-sim-"+tc.Name, parentCommit, createdBy)
	if err != nil {
		return SimulationResult{}, fmt.Errorf("create scenario fork: %w", err)
	}
	for i := range tc.Edits {
		e := tc.Edits[i]
		e.ScenarioRID = scenario.RID
		if err := repo.AppendEdit(ctx, scenario.RID, e); err != nil {
			return SimulationResult{}, fmt.Errorf("append edit %d: %w", i, err)
		}
	}
	got, err := repo.ListEdits(ctx, scenario.RID)
	if err != nil {
		return SimulationResult{}, fmt.Errorf("list edits: %w", err)
	}
	diff := diffEdits(tc.ExpectedEdits, got)
	return SimulationResult{
		Name:        tc.Name,
		ScenarioRID: scenario.RID,
		Passed:      diff == "",
		Diff:        diff,
	}, nil
}

// RunSimulationSuite runs every case in cases, accumulating into a single
// SimulationReport. Cases that fail the runner setup itself (e.g.
// CreateScenario fails) abort the suite — there is no clean way to
// report scenario-creation failure as a per-case fail.
func RunSimulationSuite(
	ctx context.Context,
	repo scenarios.Repo,
	caseStudyRID string,
	parentCommit string,
	createdBy string,
	cases []SimulationCase,
) (SimulationReport, error) {
	rep := SimulationReport{Results: make([]SimulationResult, 0, len(cases))}
	for _, tc := range cases {
		res, err := RunSimulationCase(ctx, repo, caseStudyRID, parentCommit, createdBy, tc)
		if err != nil {
			return rep, err
		}
		rep.Results = append(rep.Results, res)
		if res.Passed {
			rep.Passed++
		} else {
			rep.Failed++
		}
	}
	return rep, nil
}

// diffEdits compares expected vs actual after normalising both sides
// (zero out fields the AppendEdit pipeline fills in: Seq, ScenarioRID,
// CreatedAt) and sorting deterministically. Returns "" when equal,
// otherwise a one-line summary suitable for SimulationResult.Diff.
func diffEdits(expected, got []scenarios.ScenarioEdit) string {
	e := normaliseEdits(expected)
	g := normaliseEdits(got)
	if reflect.DeepEqual(e, g) {
		return ""
	}
	mm := firstMismatch(e, g)
	return fmt.Sprintf("expected %d edits, got %d (first mismatch — expected=%+v got=%+v)",
		len(e), len(g), mm[0], mm[1])
}

// normaliseEdits zeroes the fields the runner does not control (Seq,
// ScenarioRID, CreatedAt) and sorts by the natural key so the comparison
// is order-independent — two semantically-equal edit sets compare equal
// regardless of insertion order at the case level.
func normaliseEdits(in []scenarios.ScenarioEdit) []scenarios.ScenarioEdit {
	out := make([]scenarios.ScenarioEdit, len(in))
	var zeroTime = scenarios.ScenarioEdit{}.CreatedAt
	for i, e := range in {
		e.Seq = 0
		e.ScenarioRID = ""
		e.CreatedAt = zeroTime
		out[i] = e
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Op != b.Op {
			return a.Op < b.Op
		}
		if a.ObjectType != b.ObjectType {
			return a.ObjectType < b.ObjectType
		}
		if a.ObjectID != b.ObjectID {
			return a.ObjectID < b.ObjectID
		}
		if a.Property != b.Property {
			return a.Property < b.Property
		}
		if a.LinkType != b.LinkType {
			return a.LinkType < b.LinkType
		}
		if a.SrcID != b.SrcID {
			return a.SrcID < b.SrcID
		}
		return a.DstID < b.DstID
	})
	return out
}

func firstMismatch(a, b []scenarios.ScenarioEdit) [2]scenarios.ScenarioEdit {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if !reflect.DeepEqual(a[i], b[i]) {
			return [2]scenarios.ScenarioEdit{a[i], b[i]}
		}
	}
	if len(a) > len(b) {
		return [2]scenarios.ScenarioEdit{a[len(b)], {}}
	}
	if len(b) > len(a) {
		return [2]scenarios.ScenarioEdit{{}, b[len(a)]}
	}
	return [2]scenarios.ScenarioEdit{}
}
