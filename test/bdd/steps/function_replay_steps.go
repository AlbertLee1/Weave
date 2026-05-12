//go:build bdd

package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/cucumber/godog"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// registerFunctionReplaySteps wires the US-019 function_replay_versioning
// feature's step regex onto the scenario context. The harness drives the
// real chi-routed OMSHandler Function endpoints (create/execute/replay)
// against the live PG repository while a per-suite in-memory executor
// dispatches on (functionName, semver) so the BDD never needs Goja or any
// external runtime — the executor returns whichever string the seed step
// declared for the resolved version, and the replay path uses the same
// executor for re-runs so byte-identical hashes are guaranteed on the
// happy path.
//
// Three layers of assertion are enforced per scenario:
//   - HTTP status (200 on happy execute / replay)
//   - Response body fields (match, originalHash == replayHash,
//     functionRid resolved to the latest published version)
//   - Persisted function_executions rows (counts, version pinning,
//     is_replay flag, replay_of pointer back at the original execution)
func registerFunctionReplaySteps(t testing.TB, sc *godog.ScenarioContext, state *suiteState) {

	// --- Given: seed two published versions of one Function -----------

	sc.Given(
		`^the function ontology "([^"]+)" has two published versions of "([^"]+)" returning "([^"]+)" at "([^"]+)" and "([^"]+)" at "([^"]+)"$`,
		func(ontologyAPIName, fnName, resultA, versionA, resultB, versionB string) error {
			if err := state.ensureContainer(t); err != nil {
				return err
			}
			return seedFunctionVersions(state, ontologyAPIName, fnName,
				[]functionVersionSeed{
					{Version: versionA, Result: resultA},
					{Version: versionB, Result: resultB},
				})
		},
	)

	// --- When: execute a Function by name@version or bare name --------

	sc.When(
		`^the operator executes function "([^"]+)" in "([^"]+)" with input '([^']*)'$`,
		func(fnRef, ontologyAPIName, inputJSON string) error {
			body := fmt.Sprintf(`{"parameters":%s}`, inputJSON)
			return postFunctionExecute(state, ontologyAPIName, fnRef, body)
		},
	)

	// --- When: publish an additional version after the initial seed ---

	sc.When(
		`^the operator publishes a new version of "([^"]+)" "([^"]+)" returning "([^"]+)" in "([^"]+)"$`,
		func(fnName, version, result, ontologyAPIName string) error {
			return appendFunctionVersion(state, ontologyAPIName, fnName, version, result)
		},
	)

	// --- When: replay the most recently recorded execution row --------

	sc.When(
		`^the operator replays the recorded execution for "([^"]+)" in "([^"]+)"$`,
		func(fnName, ontologyAPIName string) error {
			execID := state.lastFunctionExecutionID
			if execID == "" {
				return errors.New("no recorded executionId — run an execute step first")
			}
			body := fmt.Sprintf(`{"executionId":%q}`, execID)
			path := fmt.Sprintf("/api/v2/ontologies/%s/functions/%s/replay",
				ontologyAPIName, fnName)
			req := httptest.NewRequest(http.MethodPost, path,
				bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			state.functionRouter.ServeHTTP(rr, req)
			state.lastFunctionResponse = &functionHTTPResult{
				statusCode: rr.Code,
				body:       rr.Body.Bytes(),
			}
			return nil
		},
	)

	// --- Then: HTTP layer ---------------------------------------------

	sc.Then(`^the function HTTP status code is (\d+)$`, func(want int) error {
		if state.lastFunctionResponse == nil {
			return errors.New("no function response captured")
		}
		if state.lastFunctionResponse.statusCode != want {
			return fmt.Errorf("function status code = %d, want %d; body=%s",
				state.lastFunctionResponse.statusCode, want,
				state.lastFunctionResponse.body)
		}
		return nil
	})

	// --- Then: response body field assertions -------------------------

	sc.Then(`^the function response field "([^"]+)" is "([^"]+)"$`,
		func(field, want string) error {
			val, err := readFunctionResponseString(state.lastFunctionResponse, field)
			if err != nil {
				return err
			}
			if val != want {
				return fmt.Errorf("response.%s = %q, want %q", field, val, want)
			}
			return nil
		},
	)

	sc.Then(`^the function response field "([^"]+)" is (true|false)$`,
		func(field, want string) error {
			b, err := readFunctionResponseBool(state.lastFunctionResponse, field)
			if err != nil {
				return err
			}
			wantBool := want == "true"
			if b != wantBool {
				return fmt.Errorf("response.%s = %v, want %v", field, b, wantBool)
			}
			return nil
		},
	)

	sc.Then(
		`^the function response field "([^"]+)" equals the field "([^"]+)"$`,
		func(left, right string) error {
			lv, err := readFunctionResponseString(state.lastFunctionResponse, left)
			if err != nil {
				return err
			}
			rv, err := readFunctionResponseString(state.lastFunctionResponse, right)
			if err != nil {
				return err
			}
			if lv == "" || rv == "" {
				return fmt.Errorf("expected both %s and %s populated, got %q vs %q",
					left, right, lv, rv)
			}
			if lv != rv {
				return fmt.Errorf("response.%s (%q) != response.%s (%q)", left, lv, right, rv)
			}
			return nil
		},
	)

	sc.Then(
		`^the function response field "functionRid" matches the latest version of "([^"]+)" in "([^"]+)"$`,
		func(fnName, ontologyAPIName string) error {
			latest, err := state.repo.GetFunctionByName(context.Background(), ontologyAPIName, fnName)
			if err != nil {
				return fmt.Errorf("GetFunctionByName(%s/%s): %w", ontologyAPIName, fnName, err)
			}
			got, err := readFunctionResponseString(state.lastFunctionResponse, "functionRid")
			if err != nil {
				return err
			}
			if got != latest.RID {
				return fmt.Errorf("functionRid = %q, want latest %q (version %s)",
					got, latest.RID, latest.Version)
			}
			return nil
		},
	)

	// --- Then: DB row assertions on function_executions ---------------

	sc.Then(
		`^the function execution store has (\d+) rows? for "([^"]+)" version "([^"]+)"$`,
		func(want int, fnName, version string) error {
			rows := state.functionExecStore.snapshot()
			n := 0
			for _, row := range rows {
				if row.FunctionName == fnName && row.FunctionVersion == version {
					n++
				}
			}
			if n != want {
				return fmt.Errorf("function_executions rows for (%s,%s) = %d, want %d",
					fnName, version, n, want)
			}
			return nil
		},
	)

	sc.Then(
		`^the function execution store has (\d+) replay rows? pointing at the original execution$`,
		func(want int) error {
			rows := state.functionExecStore.snapshot()
			origID := state.lastFunctionExecutionID
			// lastFunctionExecutionID points at the most recent execution
			// — which for the happy replay scenario is the replay row, not
			// the original. The first row is the original execute; the
			// originalId we want is rows[0].ExecutionID.
			if len(rows) > 0 {
				origID = rows[0].ExecutionID
			}
			n := 0
			for _, row := range rows {
				if row.IsReplay && row.ReplayOf == origID {
					n++
				}
			}
			if n != want {
				return fmt.Errorf("replay rows pointing at %s = %d, want %d",
					origID, n, want)
			}
			return nil
		},
	)

	sc.Then(
		`^the function execution store row (\d+) has version "([^"]+)" and is a replay$`,
		func(idx int, version string) error {
			rows := state.functionExecStore.snapshot()
			if idx < 0 || idx >= len(rows) {
				return fmt.Errorf("row index %d out of bounds (have %d rows)", idx, len(rows))
			}
			row := rows[idx]
			if row.FunctionVersion != version {
				return fmt.Errorf("row[%d].version = %q, want %q", idx, row.FunctionVersion, version)
			}
			if !row.IsReplay {
				return fmt.Errorf("row[%d] expected to be a replay, but is_replay=false", idx)
			}
			return nil
		},
	)

	sc.Then(
		`^the function execution store row (\d+) has version "([^"]+)" and is not a replay$`,
		func(idx int, version string) error {
			rows := state.functionExecStore.snapshot()
			if idx < 0 || idx >= len(rows) {
				return fmt.Errorf("row index %d out of bounds (have %d rows)", idx, len(rows))
			}
			row := rows[idx]
			if row.FunctionVersion != version {
				return fmt.Errorf("row[%d].version = %q, want %q", idx, row.FunctionVersion, version)
			}
			if row.IsReplay {
				return fmt.Errorf("row[%d] expected NOT to be a replay, but is_replay=true", idx)
			}
			return nil
		},
	)
}

// --- helpers --------------------------------------------------------

type functionVersionSeed struct {
	Version string
	Result  string
}

// seedFunctionVersions creates the named ontology (api_name = arg) and
// publishes one Function row per (name, version) tuple. The Function's
// SourceCode encodes the version-distinct stub body so duplicate-version
// inserts still surface as 409 from the registry. After persisting, the
// in-memory executor's (name, version) → result map is updated so the
// /execute and /replay handlers receive a deterministic value.
func seedFunctionVersions(state *suiteState, ontologyAPIName, fnName string, seeds []functionVersionSeed) error {
	ctx := context.Background()
	ontologyRID, ok := state.ontologyRIDFor(ontologyAPIName)
	if !ok {
		ont := &oms.Ontology{
			RID:         rid.NewOntologyRID(),
			APIName:     ontologyAPIName,
			DisplayName: ontologyAPIName,
		}
		if err := state.repo.CreateOntology(ctx, ont); err != nil {
			return fmt.Errorf("CreateOntology(%s): %w", ontologyAPIName, err)
		}
		ontologyRID = ont.RID
		state.rememberOntologyRID(ontologyAPIName, ontologyRID)
	}
	for _, s := range seeds {
		if err := publishFunctionVersion(state, ctx, ontologyRID, fnName, s.Version, s.Result); err != nil {
			return err
		}
	}
	return nil
}

// appendFunctionVersion publishes one extra version after the initial
// seed (used by the Pin-Old-Version scenario to demonstrate that a
// newer version does not subsume the historical execution row's
// pinned version on replay).
func appendFunctionVersion(state *suiteState, ontologyAPIName, fnName, version, result string) error {
	ctx := context.Background()
	ontologyRID, ok := state.ontologyRIDFor(ontologyAPIName)
	if !ok {
		return fmt.Errorf("ontology %q not seeded", ontologyAPIName)
	}
	return publishFunctionVersion(state, ctx, ontologyRID, fnName, version, result)
}

func publishFunctionVersion(state *suiteState, ctx context.Context, ontologyRID, fnName, version, result string) error {
	fn := &oms.Function{
		RID:         rid.NewFunctionRID(),
		OntologyRID: ontologyRID,
		Name:        fnName,
		Version:     version,
		SourceCode:  fmt.Sprintf("// bdd stub: %s@%s -> %q", fnName, version, result),
		Runtime:     oms.FunctionRuntimeGoja,
		CreatedBy:   "bdd",
	}
	if err := state.repo.CreateFunction(ctx, fn); err != nil {
		return fmt.Errorf("CreateFunction(%s@%s): %w", fnName, version, err)
	}
	state.functionExec.set(fnName, version, result)
	return nil
}

// postFunctionExecute POSTs to /functions/{ref}/execute and stashes the
// response + the recorded executionId so subsequent steps (replay /
// assertions) can target the same row.
func postFunctionExecute(state *suiteState, ontologyAPIName, fnRef, body string) error {
	path := fmt.Sprintf("/api/v2/ontologies/%s/functions/%s/execute",
		ontologyAPIName, fnRef)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	state.functionRouter.ServeHTTP(rr, req)
	state.lastFunctionResponse = &functionHTTPResult{
		statusCode: rr.Code,
		body:       rr.Body.Bytes(),
	}
	// Capture the executionId of the just-recorded row so replay steps
	// can target it without scraping the body. The execute handler does
	// not echo executionId today, so we read the latest row out of the
	// in-memory store directly — this is consistent with the
	// production read-back path (FindByInputHash) operating on the same
	// data the handler just wrote.
	rows := state.functionExecStore.snapshot()
	if len(rows) > 0 {
		state.lastFunctionExecutionID = rows[len(rows)-1].ExecutionID
	}
	return nil
}

// readFunctionResponseString returns the named top-level string field
// from the stashed response body. Returns an error when the response is
// missing or the field is not a string.
func readFunctionResponseString(resp *functionHTTPResult, field string) (string, error) {
	if resp == nil {
		return "", errors.New("no function response captured")
	}
	var generic map[string]interface{}
	if err := json.Unmarshal(resp.body, &generic); err != nil {
		return "", fmt.Errorf("decode response: %w; body=%s", err, string(resp.body))
	}
	v, ok := generic[field]
	if !ok {
		return "", fmt.Errorf("response missing field %q; body=%s", field, string(resp.body))
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("response.%s = %v (%T), want string", field, v, v)
	}
	return s, nil
}

func readFunctionResponseBool(resp *functionHTTPResult, field string) (bool, error) {
	if resp == nil {
		return false, errors.New("no function response captured")
	}
	var generic map[string]interface{}
	if err := json.Unmarshal(resp.body, &generic); err != nil {
		return false, fmt.Errorf("decode response: %w; body=%s", err, string(resp.body))
	}
	v, ok := generic[field]
	if !ok {
		return false, fmt.Errorf("response missing field %q; body=%s", field, string(resp.body))
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("response.%s = %v (%T), want bool", field, v, v)
	}
	return b, nil
}

// --- BDD-local executor + execution store ---------------------------

// bddFunctionExecutor dispatches on (functionName, version) so the BDD
// can encode "this version returns alpha-v1, the other returns beta-v2"
// without a real Goja runtime. The map is populated by seed steps and
// queried by oms.OMSHandler.ExecuteFunction / ReplayFunction through the
// FunctionExecutor interface.
//
// A miss returns nil so unseeded calls don't accidentally pass; the
// handler then surfaces a 200 with result=null and the replay path will
// observe a stable null hash either way.
type bddFunctionExecutor struct {
	mu      sync.Mutex
	results map[string]string // "<name>@<version>" → result
}

func newBDDFunctionExecutor() *bddFunctionExecutor {
	return &bddFunctionExecutor{results: map[string]string{}}
}

func (e *bddFunctionExecutor) set(name, version, result string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.results[name+"@"+version] = result
}

func (e *bddFunctionExecutor) Execute(_ context.Context, fn *oms.Function, _ map[string]interface{}) (interface{}, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := fn.Name + "@" + fn.NormalisedVersion()
	if v, ok := e.results[key]; ok {
		return v, nil
	}
	return nil, nil
}

// bddFunctionExecStore is an in-memory implementation of
// oms.FunctionExecutionStore. The BDD scenarios assert on row counts and
// per-row metadata so a slice-backed store is sufficient — the live PG
// equivalent in cmd/server/function_execution_store.go is exercised by
// its own unit tests, and this BDD focuses on the handler contract.
type bddFunctionExecStore struct {
	mu   sync.Mutex
	rows []*oms.FunctionExecution
}

func newBDDFunctionExecStore() *bddFunctionExecStore {
	return &bddFunctionExecStore{}
}

func (s *bddFunctionExecStore) RecordExecution(_ context.Context, exec *oms.FunctionExecution) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	clone := *exec
	s.rows = append(s.rows, &clone)
	return nil
}

func (s *bddFunctionExecStore) GetExecution(_ context.Context, executionID string) (*oms.FunctionExecution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, row := range s.rows {
		if row.ExecutionID == executionID {
			clone := *row
			return &clone, nil
		}
	}
	return nil, oms.ErrExecutionNotFound
}

func (s *bddFunctionExecStore) FindByInputHash(_ context.Context, fnRID, version, inputHash string) (*oms.FunctionExecution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.rows) - 1; i >= 0; i-- {
		row := s.rows[i]
		if row.FunctionRID == fnRID && row.FunctionVersion == version && row.InputHash == inputHash {
			clone := *row
			return &clone, nil
		}
	}
	return nil, oms.ErrExecutionNotFound
}

func (s *bddFunctionExecStore) ListExecutions(_ context.Context, fnRID, version string, limit int) ([]*oms.FunctionExecution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*oms.FunctionExecution, 0, len(s.rows))
	for i := len(s.rows) - 1; i >= 0; i-- {
		row := s.rows[i]
		if row.FunctionRID != fnRID {
			continue
		}
		if version != "" && row.FunctionVersion != version {
			continue
		}
		clone := *row
		out = append(out, &clone)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *bddFunctionExecStore) snapshot() []*oms.FunctionExecution {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*oms.FunctionExecution, len(s.rows))
	for i, r := range s.rows {
		clone := *r
		out[i] = &clone
	}
	return out
}

func (s *bddFunctionExecStore) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = nil
}
