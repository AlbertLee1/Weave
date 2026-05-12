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
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss/objectset"
	"github.com/liyang/weave/pkg/rid"
)

// registerTimeTravelSteps wires the US-017 time_travel_query feature's
// step regex onto the scenario context. The harness drives the real
// chi-routed objectset.Handler.LoadObjects endpoint and asserts three
// layers:
//
//  1. HTTP status code (200 on a resolved historical scan / 400 on an
//     unknown tx envelope).
//  2. Response body shape — totalCount as a string for happy paths,
//     errorName/parameters for the rejected ones.
//  3. Durable DB state — the underlying object_history scan keys off the
//     valid_from/valid_to interval that the test-supplied dataset_transactions
//     row points at via committed_at, so passing through the real PG
//     SnapshotObjectsAt path means the feature exercises the actual time-
//     travel SQL rather than a hand-built fake.
//
// The handler is constructed with nil executor/indexMgr because the asOf
// branch in pkg/oss/objectset.Handler.LoadObjects short-circuits BEFORE
// touching either dependency — only the history-snapshot provider + tx
// resolver are consulted on the asOf path.
func registerTimeTravelSteps(t testing.TB, sc *godog.ScenarioContext, state *suiteState) {
	// --- Given: seed ontology + employee ObjectType + properties --------------

	sc.Given(
		`^the time-travel ontology "([^"]+)" is seeded with an employee object type$`,
		func(ontologyAPIName string) error {
			if err := state.ensureContainer(t); err != nil {
				return err
			}
			if err := seedTimeTravelOntology(state, ontologyAPIName); err != nil {
				return err
			}
			ensureTimeTravelRouter(state)
			return nil
		},
	)

	// --- Given: insert dataset_transactions rows --------------------------------

	sc.Given(
		`^dataset transaction "([^"]+)" on "([^"]+)" committed at "([^"]+)"$`,
		func(txID, ontologyAPIName, committedAtRaw string) error {
			return seedDatasetTransaction(state, txID, ontologyAPIName, committedAtRaw, "")
		},
	)

	sc.Given(
		`^dataset transaction "([^"]+)" on "([^"]+)" committed at "([^"]+)" with parent "([^"]+)"$`,
		func(txID, ontologyAPIName, committedAtRaw, parentTxID string) error {
			return seedDatasetTransaction(state, txID, ontologyAPIName, committedAtRaw, parentTxID)
		},
	)

	// --- Given: insert object_history rows --------------------------------------

	sc.Given(
		`^object history for "([^"]+)" "([^"]+)" "([^"]+)" recorded at "([^"]+)" version (\d+) with new state (\{[^\n]+\}) tx "([^"]+)"$`,
		func(ontologyAPIName, objectTypeAPIName, primaryKey, recordedAtRaw string,
			version int, newStateJSON, txID string,
		) error {
			return seedObjectHistory(state, ontologyAPIName, objectTypeAPIName, primaryKey,
				recordedAtRaw, int64(version), []byte(newStateJSON), txID)
		},
	)

	// --- When: POST loadObjects?asOf=<tx-id> ------------------------------------

	sc.When(
		`^the analyst loads objects of type "([^"]+)" from "([^"]+)" with asOf "([^"]+)"$`,
		func(objectTypeAPIName, ontologyAPIName, asOf string) error {
			ontologyRID, ok := state.ontologyRIDFor(ontologyAPIName)
			if !ok {
				return fmt.Errorf("ontology %q not seeded — call the Background step first", ontologyAPIName)
			}
			body := map[string]interface{}{
				"objectSet": map[string]interface{}{
					"type":       "base",
					"objectType": objectTypeAPIName,
				},
				"select": []string{"name", "title"},
			}
			payload, err := json.Marshal(body)
			if err != nil {
				return fmt.Errorf("marshal loadObjects body: %w", err)
			}
			req := httptest.NewRequest(http.MethodPost,
				"/api/v2/ontologies/"+ontologyRID+"/objectSets/loadObjects?asOf="+asOf,
				bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			state.timeTravelRouter.ServeHTTP(rr, req)
			state.lastTimeTravelResponse = &timeTravelHTTPResult{
				statusCode: rr.Code,
				body:       rr.Body.Bytes(),
			}
			return nil
		},
	)

	// --- Then: HTTP / body assertions -------------------------------------------

	sc.Then(`^the loadObjects HTTP status code is (\d+)$`, func(want int) error {
		if state.lastTimeTravelResponse == nil {
			return errors.New("no time-travel loadObjects response captured")
		}
		if state.lastTimeTravelResponse.statusCode != want {
			return fmt.Errorf("loadObjects status code = %d, want %d; body=%s",
				state.lastTimeTravelResponse.statusCode, want,
				state.lastTimeTravelResponse.body)
		}
		return nil
	})

	sc.Then(`^the loadObjects totalCount is "([^"]+)"$`, func(want string) error {
		if state.lastTimeTravelResponse == nil {
			return errors.New("no time-travel loadObjects response captured")
		}
		var resp struct {
			TotalCount string `json:"totalCount"`
		}
		if err := json.Unmarshal(state.lastTimeTravelResponse.body, &resp); err != nil {
			return fmt.Errorf("decode loadObjects body: %w; body=%s",
				err, string(state.lastTimeTravelResponse.body))
		}
		if resp.TotalCount != want {
			return fmt.Errorf("totalCount = %q, want %q; body=%s",
				resp.TotalCount, want, state.lastTimeTravelResponse.body)
		}
		return nil
	})

	sc.Then(`^the loadObjects data row count is (\d+)$`, func(want int) error {
		rows, err := timeTravelDataRows(state)
		if err != nil {
			return err
		}
		if len(rows) != want {
			return fmt.Errorf("loadObjects data rows = %d, want %d; body=%s",
				len(rows), want, state.lastTimeTravelResponse.body)
		}
		return nil
	})

	sc.Then(`^the loadObjects data row (\d+) property "([^"]+)" equals "([^"]+)"$`,
		func(rowIdx int, property, want string) error {
			rows, err := timeTravelDataRows(state)
			if err != nil {
				return err
			}
			if rowIdx < 0 || rowIdx >= len(rows) {
				return fmt.Errorf("row index %d out of range (have %d rows); body=%s",
					rowIdx, len(rows), state.lastTimeTravelResponse.body)
			}
			got, ok := rows[rowIdx][property]
			if !ok {
				keys := make([]string, 0, len(rows[rowIdx]))
				for k := range rows[rowIdx] {
					keys = append(keys, k)
				}
				return fmt.Errorf("row %d missing property %q; keys=%v", rowIdx, property, keys)
			}
			gotStr, _ := got.(string)
			if gotStr != want {
				return fmt.Errorf("row %d property %q = %v, want %q",
					rowIdx, property, got, want)
			}
			return nil
		})

	sc.Then(`^the loadObjects error name is "([^"]+)"$`, func(want string) error {
		if state.lastTimeTravelResponse == nil {
			return errors.New("no time-travel loadObjects response captured")
		}
		var env struct {
			ErrorName string `json:"errorName"`
		}
		if err := json.Unmarshal(state.lastTimeTravelResponse.body, &env); err != nil {
			return fmt.Errorf("decode error envelope: %w; body=%s",
				err, string(state.lastTimeTravelResponse.body))
		}
		if env.ErrorName != want {
			return fmt.Errorf("errorName = %q, want %q; body=%s",
				env.ErrorName, want, state.lastTimeTravelResponse.body)
		}
		return nil
	})

	sc.Then(`^the loadObjects error parameter "([^"]+)" equals "([^"]+)"$`,
		func(key, want string) error {
			if state.lastTimeTravelResponse == nil {
				return errors.New("no time-travel loadObjects response captured")
			}
			var env struct {
				Parameters map[string]interface{} `json:"parameters"`
			}
			if err := json.Unmarshal(state.lastTimeTravelResponse.body, &env); err != nil {
				return fmt.Errorf("decode error envelope: %w; body=%s",
					err, string(state.lastTimeTravelResponse.body))
			}
			got, ok := env.Parameters[key]
			if !ok {
				return fmt.Errorf("error parameter %q missing; body=%s",
					key, state.lastTimeTravelResponse.body)
			}
			gotStr, _ := got.(string)
			if gotStr != want {
				return fmt.Errorf("error parameter %q = %v, want %q",
					key, got, want)
			}
			return nil
		})
}

// seedTimeTravelOntology lays down one ontology, one Employee ObjectType
// with two properties (name + title), so the asOf scan has somewhere
// concrete to land. object_history rows are seeded per-scenario via a
// dedicated step so each Given can stamp distinct recorded_at /
// valid_from instants.
func seedTimeTravelOntology(state *suiteState, ontologyAPIName string) error {
	ctx := context.Background()
	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     ontologyAPIName,
		DisplayName: "BDD US-017 Time Travel",
	}
	if err := state.repo.CreateOntology(ctx, ont); err != nil {
		return fmt.Errorf("CreateOntology: %w", err)
	}
	state.rememberOntologyRID(ontologyAPIName, ont.RID)

	ot := &oms.ObjectType{
		RID:         rid.NewObjectTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "employee",
		DisplayName: "Employee",
		PrimaryKey:  "employeeId",
		PrimaryKeys: []string{"employeeId"},
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := state.repo.CreateObjectType(ctx, ot); err != nil {
		return fmt.Errorf("CreateObjectType: %w", err)
	}
	state.rememberObjectTypeRID(ontologyAPIName, ot.APIName, ot.RID)

	for _, apiName := range []string{"employeeId", "name", "title"} {
		p := &oms.Property{
			RID:           rid.NewPropertyRID(),
			ObjectTypeRID: ot.RID,
			APIName:       apiName,
			DisplayName:   apiName,
			BaseType:      "string",
			IsSearchable:  true,
			Status:        "ACTIVE",
		}
		if err := state.repo.CreateProperty(ctx, p); err != nil {
			return fmt.Errorf("CreateProperty(%s): %w", apiName, err)
		}
	}
	return nil
}

// seedDatasetTransaction writes a row directly into dataset_transactions
// so the asOf=tx-<id> resolver can read it back and the handler can
// resolve it to the committed_at instant. parentTxID may be "" for the
// chain genesis row.
func seedDatasetTransaction(state *suiteState, txID, ontologyAPIName, committedAtRaw, parentTxID string) error {
	ts, err := time.Parse(time.RFC3339, committedAtRaw)
	if err != nil {
		return fmt.Errorf("parse committedAt %q: %w", committedAtRaw, err)
	}
	tx := &oms.DatasetTransaction{
		TxID:            txID,
		ParentTxID:      parentTxID,
		OntologyAPIName: ontologyAPIName,
		CommittedAt:     ts,
	}
	if err := state.repo.RecordDatasetTransaction(context.Background(), tx); err != nil {
		return fmt.Errorf("RecordDatasetTransaction(%s): %w", txID, err)
	}
	return nil
}

// seedObjectHistory inserts one object_history row via the canonical
// InsertObjectHistory path so the valid_from/valid_to interval closes
// out the prior version automatically (see pkg/oms/pg_repository.go,
// the UPDATE-valid_to side-effect after each insert).
func seedObjectHistory(state *suiteState, ontologyAPIName, objectTypeAPIName,
	primaryKey, recordedAtRaw string, version int64, newState []byte, txID string,
) error {
	otRID, ok := state.objectTypeRIDFor(ontologyAPIName, objectTypeAPIName)
	if !ok {
		return fmt.Errorf("objectType %q not seeded under ontology %q",
			objectTypeAPIName, ontologyAPIName)
	}
	ts, err := time.Parse(time.RFC3339, recordedAtRaw)
	if err != nil {
		return fmt.Errorf("parse recordedAt %q: %w", recordedAtRaw, err)
	}
	h := &oms.ObjectHistory{
		ObjectTypeRID: otRID,
		PrimaryKey:    primaryKey,
		Version:       version,
		NewState:      append(json.RawMessage(nil), newState...),
		EditType:      "MODIFY",
		Source:        oms.EditSourceUser,
		RecordedAt:    ts,
		TxID:          txID,
	}
	if version == 1 {
		h.EditType = "CREATE"
	}
	if err := state.repo.InsertObjectHistory(context.Background(), h); err != nil {
		return fmt.Errorf("InsertObjectHistory(%s/%s v%d): %w",
			objectTypeAPIName, primaryKey, version, err)
	}
	return nil
}

// timeTravelDataRows decodes the captured loadObjects response body into
// the `data` slice (each row is a `map[string]interface{}` with the
// selected property fields plus internal __primaryKey / __apiName). It
// returns a non-nil error when no response was captured or the JSON did
// not parse.
func timeTravelDataRows(state *suiteState) ([]map[string]interface{}, error) {
	if state.lastTimeTravelResponse == nil {
		return nil, errors.New("no time-travel loadObjects response captured")
	}
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(state.lastTimeTravelResponse.body, &resp); err != nil {
		return nil, fmt.Errorf("decode loadObjects body: %w; body=%s",
			err, string(state.lastTimeTravelResponse.body))
	}
	return resp.Data, nil
}

// ensureTimeTravelRouter builds the objectset.Handler + chi route on
// first use of the feature. The handler is constructed with nil executor
// + indexMgr because the asOf branch in LoadObjects short-circuits before
// either is touched — only the wired HistorySnapshotProvider +
// TransactionResolver are consulted. Both adapters forward to the live
// PG repo so the BDD scenario exercises real validity-window SQL.
func ensureTimeTravelRouter(state *suiteState) {
	if state.timeTravelRouter != nil {
		return
	}
	store := objectset.NewStore(time.Hour)
	h := objectset.NewHandler(nil, nil, store)
	h.SetHistorySnapshotProvider(&bddHistorySnapshotProvider{repo: state.repo})
	h.SetTransactionResolver(&bddTransactionResolver{repo: state.repo})
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", h.LoadObjects)
	state.timeTravelRouter = r
}

// bddHistorySnapshotProvider satisfies objectset.HistorySnapshotProvider
// by walking the same chain cmd/server's historySnapshotAdapter walks:
// resolve the ontology api name → ontology row → ObjectType by api name
// → SnapshotObjectsAt over the [valid_from, valid_to) interval. Lives in
// the BDD steps package because cmd/server lives in package main and is
// not importable.
type bddHistorySnapshotProvider struct {
	repo *oms.PGRepository
}

func (p *bddHistorySnapshotProvider) SnapshotObjectsAt(ctx context.Context, ontologyAPIName, objectTypeAPIName string, asOf time.Time) ([]objectset.ObjectSnapshot, error) {
	ont, err := p.repo.GetOntology(ctx, ontologyAPIName)
	if err != nil {
		return nil, fmt.Errorf("lookup ontology %q: %w", ontologyAPIName, err)
	}
	if ont == nil {
		return nil, fmt.Errorf("ontology %q not found", ontologyAPIName)
	}
	ot, err := p.repo.GetObjectTypeByAPIName(ctx, ont.RID, objectTypeAPIName)
	if err != nil {
		return nil, fmt.Errorf("lookup objectType %q: %w", objectTypeAPIName, err)
	}
	if ot == nil {
		return nil, fmt.Errorf("objectType %q not found", objectTypeAPIName)
	}
	rows, err := p.repo.SnapshotObjectsAt(ctx, ot.RID, asOf)
	if err != nil {
		return nil, fmt.Errorf("snapshot object_history: %w", err)
	}
	out := make([]objectset.ObjectSnapshot, 0, len(rows))
	for _, row := range rows {
		var props map[string]interface{}
		if len(row.NewState) > 0 {
			if err := json.Unmarshal(row.NewState, &props); err != nil {
				return nil, fmt.Errorf("decode new_state for %s: %w", row.PrimaryKey, err)
			}
		}
		out = append(out, objectset.ObjectSnapshot{
			PrimaryKey: row.PrimaryKey,
			Properties: props,
		})
	}
	return out, nil
}

// bddTransactionResolver satisfies objectset.TransactionResolver by
// forwarding tx_id lookups to the live PG repository. Missing rows
// surface as objectset.ErrTransactionNotFound so the handler maps to
// a clean TransactionNotFound 400 — matching the production adapter
// in cmd/server/dataset_transaction_handler.go.
type bddTransactionResolver struct {
	repo *oms.PGRepository
}

func (r *bddTransactionResolver) ResolveTransaction(ctx context.Context, txID string) (time.Time, error) {
	tx, err := r.repo.GetDatasetTransaction(ctx, txID)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			return time.Time{}, objectset.ErrTransactionNotFound
		}
		return time.Time{}, err
	}
	return tx.CommittedAt, nil
}
