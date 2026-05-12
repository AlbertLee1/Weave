//go:build bdd

package steps

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/actions/sagapg"
	"github.com/liyang/weave/pkg/funnel"
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

	// US-014 saga BDD wiring. The executor + handler are constructed on
	// first use against the same PG pool and reused across scenarios; the
	// recording publisher is reset before each scenario so captured
	// batches and the failNext flag stay scenario-scoped.
	sagaStore     actions.SagaStore
	actionExec    *actions.Executor
	actionHandler *actions.Handler
	sagaPublisher *recordingPublisher
	sagaRouter    chi.Router

	// lastSagaResponse stores the response from the most recent
	// applySaga call so Then-steps can assert against status + body.
	lastSagaResponse *sagaHTTPResult

	mu              sync.Mutex
	apiNameToRID    map[string]string // ontology apiName → RID
	objectTypeRIDs  map[string]string // "<ontologyApiName>/<otApiName>" → ObjectType RID
	actionTypeRIDs  map[string]string // "<ontologyApiName>/<atApiName>" → ActionType RID
	branchIDs       map[string]string // branch name → branch ID
	proposalIDs     map[string]string // proposal alias → proposal ID
}

func newSuiteState() *suiteState {
	return &suiteState{
		apiNameToRID:   map[string]string{},
		objectTypeRIDs: map[string]string{},
		actionTypeRIDs: map[string]string{},
		branchIDs:      map[string]string{},
		proposalIDs:    map[string]string{},
	}
}

// sagaHTTPResult is the per-scenario response snapshot stashed on
// suiteState. Step definitions read it in Then-steps to assert HTTP
// status code and structured body fields without re-issuing the call.
type sagaHTTPResult struct {
	statusCode int
	body       []byte
}

// recordingPublisher is an actions.Publisher used by the BDD harness to
// capture every EditBatch the saga coordinator publishes and, when
// configured, fail a publish so the compensation path enqueues a DLQ
// row. It is safe to mutate flags between scenarios via reset().
type recordingPublisher struct {
	mu        sync.Mutex
	published []*funnel.EditBatch
	failNext  bool
	failErr   error
}

// Publish satisfies actions.Publisher. The fake offset increments by 1
// per successful publish so callers can correlate published batches.
func (p *recordingPublisher) Publish(batch *funnel.EditBatch) (uint64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failNext {
		p.failNext = false
		err := p.failErr
		if err == nil {
			err = fmt.Errorf("recordingPublisher: configured failure")
		}
		return 0, err
	}
	p.published = append(p.published, batch)
	return uint64(len(p.published)), nil
}

func (p *recordingPublisher) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.published = nil
	p.failNext = false
	p.failErr = nil
}

func (p *recordingPublisher) snapshot() []*funnel.EditBatch {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*funnel.EditBatch, len(p.published))
	copy(out, p.published)
	return out
}

func (p *recordingPublisher) setFailNext(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failNext = true
	p.failErr = err
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

func (s *suiteState) rememberActionTypeRID(ontologyAPIName, atAPIName, rid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actionTypeRIDs[ontologyAPIName+"/"+atAPIName] = rid
}

func (s *suiteState) actionTypeRIDFor(ontologyAPIName, atAPIName string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.actionTypeRIDs[ontologyAPIName+"/"+atAPIName]
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
	s.actionTypeRIDs = map[string]string{}
	s.branchIDs = map[string]string{}
	s.proposalIDs = map[string]string{}
	s.lastSagaResponse = nil
	if s.sagaPublisher != nil {
		s.sagaPublisher.reset()
	}
}

// ensureContainer brings up postgres + applies migrations on first use, then
// truncates ontology-rooted tables so subsequent scenarios observe a clean
// schema. The FK cascade from ontologies wipes object_types, ontology_branches,
// ontology_branch_changes, ontology_proposals, and proposal_reviews in one
// TRUNCATE — see migrations 000001, 000024, 000025.
//
// US-014: the saga lifecycle tables (action_sagas + action_saga_steps +
// action_saga_dlq, migration 000083) do not FK back to ontologies — they
// reference the ontology by string — so they are truncated separately. The
// saga executor + handler are also instantiated lazily on first use and
// reused across scenarios; only the recording publisher carries scenario-
// scoped state, reset by resetMaps().
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

	// Saga wiring: the recording publisher captures every EditBatch the
	// coordinator publishes; the executor is given the real PG SagaStore
	// from pkg/actions/sagapg so action_sagas / action_saga_steps /
	// action_saga_dlq rows are observable from BDD assertions.
	s.sagaPublisher = &recordingPublisher{}
	s.sagaStore = sagapg.NewStore(pg.Pool)
	s.actionExec = actions.NewExecutor(s.repo, s.sagaPublisher)
	s.actionExec.SetSagaStore(s.sagaStore)
	s.actionHandler = actions.NewHandler(s.actionExec)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/actions/applySaga", s.actionHandler.ApplySaga)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actions/saga/dlq", s.actionHandler.ListSagaDLQ)
	s.sagaRouter = r
	return nil
}

// truncateOntologyGraph wipes every row reachable from ontologies via FK
// cascade plus the saga lifecycle tables. CASCADE on TRUNCATE walks
// ontology dependents (object_types, ontology_branches →
// ontology_branch_changes, ontology_proposals → proposal_reviews, plus
// action_types) in one statement. action_sagas / action_saga_steps /
// action_saga_dlq are unrelated by FK and must be truncated explicitly.
func (s *suiteState) truncateOntologyGraph() error {
	// action_saga_steps + action_saga_dlq reference action_sagas with
	// ON DELETE CASCADE so they are wiped automatically when the saga
	// header table is truncated CASCADE.
	_, err := s.pg.Pool.Exec(context.Background(),
		`TRUNCATE TABLE ontologies, action_sagas RESTART IDENTITY CASCADE`)
	if err != nil {
		return fmt.Errorf("truncate ontologies + sagas: %w", err)
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
