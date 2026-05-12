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
	"github.com/liyang/weave/pkg/cellsec"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
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

	// US-015 automation rule lifecycle BDD wiring. automationRouter wires
	// the same OMSHandler against the automation rule + executions
	// endpoints so step defs drive real chi-routed handlers.
	// lastAutomationResponse stashes the latest response for Then-steps.
	automationRouter       chi.Router
	lastAutomationResponse *automationHTTPResult

	// US-016 cell-masking CEL BDD wiring. indexMgr backs the Bleve
	// per-ObjectType index; cellMaskStore + cellMaskEngine drive the
	// US-258/US-376 cellsec engine; ossService is the read-path under test
	// with the cell-mask engine attached; cellMaskRouter exposes the OSS
	// GetObject endpoint so step defs assert through the real HTTP wire
	// (status code + JSON body) instead of poking the service directly.
	// lastCellMaskResponse stashes the most recent response for Then-steps.
	indexMgr             *index.Manager
	cellMaskStore        *cellsec.MemoryStore
	cellMaskEngine       *cellsec.Engine
	ossService           *oss.ServiceImpl
	cellMaskRouter       chi.Router
	lastCellMaskResponse *cellMaskHTTPResult

	// US-017 time-travel asOf BDD wiring. timeTravelRouter exposes the
	// OSS loadObjects endpoint wrapped around an objectset.Handler whose
	// HistorySnapshotProvider + TransactionResolver hooks forward to the
	// live PG repository (object_history + dataset_transactions). The
	// router is constructed lazily on the seed step and reused across
	// scenarios — only the last-response snapshot carries per-scenario
	// state, cleared by resetMaps().
	timeTravelRouter       chi.Router
	lastTimeTravelResponse *timeTravelHTTPResult

	mu                sync.Mutex
	apiNameToRID      map[string]string // ontology apiName → RID
	objectTypeRIDs    map[string]string // "<ontologyApiName>/<otApiName>" → ObjectType RID
	actionTypeRIDs    map[string]string // "<ontologyApiName>/<atApiName>" → ActionType RID
	branchIDs         map[string]string // branch name → branch ID
	proposalIDs       map[string]string // proposal alias → proposal ID
	automationRuleIDs map[string]string // automation rule name → rule ID
}

func newSuiteState() *suiteState {
	return &suiteState{
		apiNameToRID:      map[string]string{},
		objectTypeRIDs:    map[string]string{},
		actionTypeRIDs:    map[string]string{},
		branchIDs:         map[string]string{},
		proposalIDs:       map[string]string{},
		automationRuleIDs: map[string]string{},
	}
}

// automationHTTPResult is the per-scenario response snapshot stashed on
// suiteState for the US-015 automation rule lifecycle Then-steps.
type automationHTTPResult struct {
	statusCode int
	body       []byte
}

// cellMaskHTTPResult is the per-scenario response snapshot stashed on
// suiteState for the US-016 cell-masking CEL Then-steps.
type cellMaskHTTPResult struct {
	statusCode int
	body       []byte
}

// timeTravelHTTPResult is the per-scenario response snapshot stashed on
// suiteState for the US-017 asOf time-travel Then-steps.
type timeTravelHTTPResult struct {
	statusCode int
	body       []byte
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

func (s *suiteState) rememberAutomationRule(name, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.automationRuleIDs[name] = id
}

func (s *suiteState) automationRuleIDFor(name string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.automationRuleIDs[name]
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
	s.automationRuleIDs = map[string]string{}
	s.lastSagaResponse = nil
	s.lastAutomationResponse = nil
	s.lastCellMaskResponse = nil
	s.lastTimeTravelResponse = nil
	if s.sagaPublisher != nil {
		s.sagaPublisher.reset()
	}
	// Clear cell masks between scenarios so a Mask-Hit fixture from the
	// previous scenario does not bleed into the next. The MemoryStore is
	// re-created and a fresh Engine is wired into the OSS service so the
	// in-memory index reflects the empty set immediately.
	if s.cellMaskStore != nil {
		s.cellMaskStore = cellsec.NewMemoryStore()
		s.cellMaskEngine = cellsec.New(s.cellMaskStore, nil)
		_ = s.cellMaskEngine.Reload(context.Background())
		if s.ossService != nil {
			s.ossService.SetCellMaskEngine(s.cellMaskEngine)
		}
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

	// US-015 automation rule lifecycle. The OMSHandler already constructed
	// above is reused; we just expose its automation-rule + executions
	// endpoints on a dedicated chi router so BDD step defs drive real
	// HTTP semantics (status code + body schema) on top of PG persistence.
	ar := chi.NewRouter()
	ar.Post("/api/v2/ontologies/{ontologyApiName}/automationRules", s.handler.CreateAutomationRule)
	ar.Get("/api/v2/ontologies/{ontologyApiName}/automationRules", s.handler.ListAutomationRules)
	ar.Get("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}", s.handler.GetAutomationRule)
	ar.Put("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}", s.handler.UpdateAutomationRule)
	ar.Delete("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}", s.handler.DeleteAutomationRule)
	ar.Post("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}/pause", s.handler.PauseAutomationRule)
	ar.Post("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}/resume", s.handler.ResumeAutomationRule)
	ar.Get("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}/executions", s.handler.ListExecutions)
	s.automationRouter = ar

	// US-016 cell-masking CEL. Build the Bleve index manager (scoped under
	// the PG container's temp dir so each TestBDD run gets a fresh tree),
	// the cellsec memory store + engine, and the OSS service wired against
	// the same OMS PG repo so the Bleve docs share the same ObjectType
	// resolution. A dedicated chi.Router exposes only GetObject — Cell-
	// masking BDD only needs the single-object read path.
	s.indexMgr = index.NewManager(t.TempDir())
	s.cellMaskStore = cellsec.NewMemoryStore()
	s.cellMaskEngine = cellsec.New(s.cellMaskStore, nil)
	linkResolver := links.NewResolver(s.repo, s.indexMgr)
	s.ossService = oss.NewService(s.repo, s.indexMgr, linkResolver)
	s.ossService.SetCellMaskEngine(s.cellMaskEngine)
	ossHandler := oss.NewHandler(s.ossService)
	cr := chi.NewRouter()
	cr.Get(
		"/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}",
		ossHandler.GetObject,
	)
	s.cellMaskRouter = cr
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
