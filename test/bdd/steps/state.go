//go:build bdd

package steps

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/oms"
)

// suiteState is the shared per-scenario state that step definitions read and
// write. Ontology, branch, and proposal steps all bind onto a single instance
// allocated in RegisterAllSteps so a Background "given an ontology X exists"
// can be observed by branch/proposal steps later in the same scenario.
type suiteState struct {
	pg      *testutil.PGContainer
	repo    *oms.PGRepository
	handler *oms.OMSHandler

	mu             sync.Mutex
	apiNameToRID   map[string]string // ontology apiName → RID
	objectTypeRIDs map[string]string // "<ontologyApiName>/<otApiName>" → ObjectType RID
	branchIDs      map[string]string // branch name → branch ID
	proposalIDs    map[string]string // proposal alias → proposal ID
}

func newSuiteState() *suiteState {
	return &suiteState{
		apiNameToRID:   map[string]string{},
		objectTypeRIDs: map[string]string{},
		branchIDs:      map[string]string{},
		proposalIDs:    map[string]string{},
	}
}

func (s *suiteState) rememberOntologyRID(apiName, rid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apiNameToRID[apiName] = rid
}

func (s *suiteState) ontologyRIDFor(apiName string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.apiNameToRID[apiName]
	return r, ok
}

func (s *suiteState) rememberObjectTypeRID(ontologyAPIName, otAPIName, rid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objectTypeRIDs[ontologyAPIName+"/"+otAPIName] = rid
}

func (s *suiteState) objectTypeRIDFor(ontologyAPIName, otAPIName string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.objectTypeRIDs[ontologyAPIName+"/"+otAPIName]
	return r, ok
}

func (s *suiteState) rememberBranch(name, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.branchIDs[name] = id
}

func (s *suiteState) branchIDFor(name string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.branchIDs[name]
	return id, ok
}

func (s *suiteState) rememberProposal(alias, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.proposalIDs[alias] = id
}

func (s *suiteState) proposalIDFor(alias string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.proposalIDs[alias]
	return id, ok
}

// resetMaps clears every per-scenario lookup map without touching the PG
// container itself. The container survives across scenarios in one TestBDD
// run so docker startup cost amortises.
func (s *suiteState) resetMaps() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apiNameToRID = map[string]string{}
	s.objectTypeRIDs = map[string]string{}
	s.branchIDs = map[string]string{}
	s.proposalIDs = map[string]string{}
}

// ensureContainer brings up postgres + applies migrations on first use, then
// truncates ontology-rooted tables so subsequent scenarios observe a clean
// schema. The FK cascade from ontologies wipes object_types, ontology_branches,
// ontology_branch_changes, ontology_proposals, and proposal_reviews in one
// TRUNCATE — see migrations 000001, 000024, 000025.
func (s *suiteState) ensureContainer(t testing.TB) error {
	if s.pg != nil {
		if err := s.truncateOntologyGraph(); err != nil {
			return err
		}
		s.resetMaps()
		return nil
	}
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	s.pg = pg
	s.repo = oms.NewPGRepository(pg.Pool)
	s.handler = oms.NewOMSHandler(s.repo)
	return nil
}

// truncateOntologyGraph wipes every row reachable from ontologies via FK
// cascade. CASCADE on TRUNCATE walks dependents (object_types,
// ontology_branches → ontology_branch_changes, ontology_proposals →
// proposal_reviews), so one statement is enough.
func (s *suiteState) truncateOntologyGraph() error {
	_, err := s.pg.Pool.Exec(context.Background(),
		`TRUNCATE TABLE ontologies RESTART IDENTITY CASCADE`)
	if err != nil {
		return fmt.Errorf("truncate ontologies: %w", err)
	}
	return nil
}

// trimQuoted is a tiny convenience: feature step regex captures often leave
// the quote characters off but operator-supplied test data sometimes carries
// them inline (e.g. "feature/x"). Stripping defensively keeps step regexes
// readable.
func trimQuoted(s string) string {
	return strings.Trim(s, `"`)
}
