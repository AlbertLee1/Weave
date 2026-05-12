//go:build bdd

// Package bdd_test is the entry point for the Weave godog BDD suite. It is
// guarded by the `bdd` build tag so the suite never runs as part of
// `go test ./...` or `make test-unit` — those still operate on the
// existing Go unit and integration tests. Run the suite with
//
//	make test-bdd
//
// which expands to `go test -tags bdd ./test/bdd/...`. The suite spins up
// a real PostgreSQL container via internal/testutil.StartPGContainer, so
// Docker must be reachable on the executing host.
package bdd_test

import (
	"testing"

	"github.com/cucumber/godog"

	"github.com/liyang/weave/test/bdd/steps"
)

// TestBDD runs every *.feature file under test/bdd/features. We use the
// pretty formatter for human-readable output and forward the *testing.T so
// scenario failures surface as ordinary go test failures (and t.Cleanup
// drives container teardown).
func TestBDD(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			steps.RegisterAllSteps(t, sc)
		},
		Options: &godog.Options{
			Format:    "pretty",
			Paths:     []string{"features"},
			TestingT:  t,
			Randomize: 0, // keep deterministic ordering for now (see PRD US-001 notes)
		},
	}
	if status := suite.Run(); status != 0 {
		t.Fatalf("godog suite returned non-zero status: %d", status)
	}
}
