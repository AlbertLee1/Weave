//go:build bdd

// Package steps holds godog step definitions shared across the Weave BDD
// suite. The steps are compiled only under the `bdd` build tag so they do
// not leak into the default `go test ./...` matrix or `make test-unit`.
package steps

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/cucumber/godog"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// registerOntologySteps binds ontology CRUD step regex onto the scenario
// context. It is private because step packages are composed through
// RegisterAllSteps — sharing the *suiteState lets Background-style ontology
// givens be observed by branch/proposal steps later in the same scenario.
func registerOntologySteps(t testing.TB, sc *godog.ScenarioContext, state *suiteState) {
	sc.Given(`^a fresh weave database with migrations applied$`, func() error {
		return state.ensureContainer(t)
	})

	sc.When(`^I create an ontology with apiName "([^"]+)" and displayName "([^"]+)"$`,
		func(apiName, displayName string) error {
			if state.repo == nil {
				return errors.New("background step did not initialise the database")
			}
			ont := &oms.Ontology{
				RID:         rid.NewOntologyRID(),
				APIName:     apiName,
				DisplayName: displayName,
			}
			if err := state.repo.CreateOntology(context.Background(), ont); err != nil {
				return fmt.Errorf("CreateOntology(%s): %w", apiName, err)
			}
			state.rememberOntologyRID(apiName, ont.RID)
			return nil
		})

	sc.Given(`^an ontology "([^"]+)" exists with displayName "([^"]+)"$`,
		func(apiName, displayName string) error {
			if err := state.ensureContainer(t); err != nil {
				return err
			}
			ont := &oms.Ontology{
				RID:         rid.NewOntologyRID(),
				APIName:     apiName,
				DisplayName: displayName,
			}
			if err := state.repo.CreateOntology(context.Background(), ont); err != nil {
				return fmt.Errorf("seed CreateOntology(%s): %w", apiName, err)
			}
			state.rememberOntologyRID(apiName, ont.RID)
			return nil
		})

	sc.When(`^I update the ontology "([^"]+)" displayName to "([^"]+)"$`,
		func(apiName, newDisplay string) error {
			ridStr, ok := state.ontologyRIDFor(apiName)
			if !ok {
				return fmt.Errorf("no RID tracked for apiName %q", apiName)
			}
			ont := &oms.Ontology{
				RID:         ridStr,
				DisplayName: newDisplay,
			}
			if err := state.repo.UpdateOntology(context.Background(), ont); err != nil {
				return fmt.Errorf("UpdateOntology(%s): %w", apiName, err)
			}
			return nil
		})

	sc.When(`^I delete the ontology "([^"]+)"$`, func(apiName string) error {
		// The repository interface intentionally omits DeleteOntology
		// (snapshots/branches reference rid via FKs and a soft archive flow is
		// preferred), so the BDD layer asserts the database-level contract
		// directly with a SQL DELETE. This keeps the scenario honest about
		// what is actually possible against a real schema.
		tag, err := state.pg.Pool.Exec(context.Background(),
			`DELETE FROM ontologies WHERE api_name = $1`, apiName)
		if err != nil {
			return fmt.Errorf("delete ontology %s: %w", apiName, err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("delete ontology %s: no rows affected", apiName)
		}
		return nil
	})

	sc.Then(`^the ontology "([^"]+)" exists in the database$`, func(apiName string) error {
		ont, err := state.repo.GetOntology(context.Background(), apiName)
		if err != nil {
			return fmt.Errorf("GetOntology(%s): %w", apiName, err)
		}
		if ont == nil {
			return fmt.Errorf("ontology %s not found", apiName)
		}
		return nil
	})

	sc.Then(`^the ontology "([^"]+)" has displayName "([^"]+)"$`,
		func(apiName, want string) error {
			ont, err := state.repo.GetOntology(context.Background(), apiName)
			if err != nil {
				return fmt.Errorf("GetOntology(%s): %w", apiName, err)
			}
			if ont.DisplayName != want {
				return fmt.Errorf("displayName mismatch: got %q, want %q", ont.DisplayName, want)
			}
			return nil
		})

	sc.Then(`^the ontology "([^"]+)" has currentVersion (\d+)$`,
		func(apiName string, want int) error {
			ont, err := state.repo.GetOntology(context.Background(), apiName)
			if err != nil {
				return fmt.Errorf("GetOntology(%s): %w", apiName, err)
			}
			if ont.CurrentVersion != want {
				return fmt.Errorf("currentVersion mismatch: got %d, want %d", ont.CurrentVersion, want)
			}
			return nil
		})

	sc.Then(`^the ontology "([^"]+)" no longer exists in the database$`,
		func(apiName string) error {
			ont, err := state.repo.GetOntology(context.Background(), apiName)
			if err == nil {
				return fmt.Errorf("expected ontology %s to be gone, but got %+v", apiName, ont)
			}
			if !errors.Is(err, oms.ErrNotFound) {
				return fmt.Errorf("GetOntology returned unexpected error: %v", err)
			}
			return nil
		})
}

// RegisterOntologySteps is kept for backward compat with any external test
// runner that wires ontology steps directly. New callers should prefer
// RegisterAllSteps which composes ontology + branch/proposal step packages
// onto a shared suite state.
func RegisterOntologySteps(t testing.TB, sc *godog.ScenarioContext) {
	state := newSuiteState()
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		state.resetMaps()
		return ctx, nil
	})
	registerOntologySteps(t, sc, state)
}
