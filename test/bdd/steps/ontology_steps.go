//go:build bdd

// Package steps holds godog step definitions shared across the Weave BDD
// suite. The steps are compiled only under the `bdd` build tag so they do
// not leak into the default `go test ./...` matrix or `make test-unit`.
package steps

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/cucumber/godog"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// suiteState carries the live PostgreSQL container plus the OMS repository
// across steps inside a single scenario.
type suiteState struct {
	pg   *testutil.PGContainer
	repo *oms.PGRepository

	// apiNameToRID maps the human-readable API name used in feature files to
	// the actual RID we minted at create time. Scenarios reference ontologies
	// by API name (e.g. "bdd_update"), so step impls translate via this map.
	mu           sync.Mutex
	apiNameToRID map[string]string
}

func newSuiteState() *suiteState {
	return &suiteState{apiNameToRID: map[string]string{}}
}

func (s *suiteState) rememberRID(apiName, rid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apiNameToRID[apiName] = rid
}

func (s *suiteState) ridFor(apiName string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.apiNameToRID[apiName]
	return r, ok
}

// RegisterOntologySteps wires the ontology CRUD step definitions onto the
// scenario context. It is exported so test runners under different build
// tags can compose multiple step packages into one suite.
//
// The container is started lazily on the first Given/When step that needs it
// so scenarios that never touch the database don't pay the docker startup
// cost. The caller (bdd_test.go) is responsible for owning the *testing.T
// passed into godog.Options.TestingT, which drives t.Cleanup() lifetimes
// for the testcontainers handle.
func RegisterOntologySteps(t testing.TB, sc *godog.ScenarioContext) {
	state := newSuiteState()

	// Reset the per-scenario map but keep the same PG container across
	// scenarios in the same testing.T run — testcontainers startup is the
	// dominant cost (~3-5s on first pull) so amortising it across the 3
	// scenarios keeps the loop fast.
	sc.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		state.mu.Lock()
		state.apiNameToRID = map[string]string{}
		state.mu.Unlock()
		return ctx, nil
	})

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
			state.rememberRID(apiName, ont.RID)
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
			state.rememberRID(apiName, ont.RID)
			return nil
		})

	sc.When(`^I update the ontology "([^"]+)" displayName to "([^"]+)"$`,
		func(apiName, newDisplay string) error {
			ridStr, ok := state.ridFor(apiName)
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

// ensureContainer brings up postgres + applies migrations exactly once per
// suite (a singleton on the per-scenario state instance). godog calls the
// same state across scenarios, so subsequent invocations are cheap no-ops.
func (s *suiteState) ensureContainer(t testing.TB) error {
	if s.pg != nil {
		// Truncate ontology-related tables so each scenario starts clean while
		// reusing the (expensive) postgres container across scenarios.
		_, err := s.pg.Pool.Exec(context.Background(),
			`TRUNCATE TABLE ontologies RESTART IDENTITY CASCADE`)
		if err != nil {
			return fmt.Errorf("truncate ontologies: %w", err)
		}
		return nil
	}
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	s.pg = pg
	s.repo = oms.NewPGRepository(pg.Pool)
	return nil
}
