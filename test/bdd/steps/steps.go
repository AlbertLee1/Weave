//go:build bdd

package steps

import (
	"context"
	"testing"

	"github.com/cucumber/godog"
)

// RegisterAllSteps wires every godog step package onto the supplied scenario
// context, sharing a single *suiteState across packages so a "given an
// ontology X exists" line from one package is observable by a "given an open
// branch off X" line from another.
//
// The PG container is brought up lazily by the first `Given a fresh weave
// database` step and reused across scenarios; per-scenario state is reset
// in Before by truncating the ontology FK graph.
func RegisterAllSteps(t testing.TB, sc *godog.ScenarioContext) {
	state := newSuiteState()

	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		state.resetMaps()
		return ctx, nil
	})

	registerOntologySteps(t, sc, state)
	registerBranchMergeSteps(t, sc, state)
	registerSagaCompensationSteps(t, sc, state)
}
