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
	"strconv"
	"testing"

	"github.com/cucumber/godog"

	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// registerSagaCompensationSteps wires the US-014 action_saga_compensation
// feature's step regex onto the scenario context. The harness drives the
// real chi-routed actions.Handler.ApplySaga endpoint over an httptest
// server-less ResponseRecorder, and asserts both HTTP semantics
// (status code + structured body) and durable DB state via the wired PG
// SagaStore (action_sagas / action_saga_steps / action_saga_dlq).
func registerSagaCompensationSteps(t testing.TB, sc *godog.ScenarioContext, state *suiteState) {
	// --- Background / Given setup ------------------------------------------------

	sc.Given(
		`^the saga test ontology "([^"]+)" is seeded with Order/Booking object types and compensating action types$`,
		func(ontologyAPIName string) error {
			if err := state.ensureContainer(t); err != nil {
				return err
			}
			return seedSagaOntology(state, ontologyAPIName)
		},
	)

	sc.Given(`^the publisher will fail the next publish$`, func() error {
		if state.sagaPublisher == nil {
			return errors.New("saga publisher not initialised — call the seed step first")
		}
		state.sagaPublisher.setFailNext(fmt.Errorf("simulated publish failure"))
		return nil
	})

	// --- When --------------------------------------------------------------------

	sc.When(
		`^I POST applySaga to ontology "([^"]+)" with these steps:$`,
		func(ontologyAPIName string, table *godog.Table) error {
			body, err := buildApplySagaBody(table)
			if err != nil {
				return err
			}
			payload, err := json.Marshal(body)
			if err != nil {
				return fmt.Errorf("marshal applySaga body: %w", err)
			}
			req := httptest.NewRequest(http.MethodPost,
				"/api/v2/ontologies/"+ontologyAPIName+"/actions/applySaga",
				bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			state.sagaRouter.ServeHTTP(rr, req)
			state.lastSagaResponse = &sagaHTTPResult{
				statusCode: rr.Code,
				body:       rr.Body.Bytes(),
			}
			return nil
		},
	)

	// --- Then assertions on HTTP layer ------------------------------------------

	sc.Then(`^the saga HTTP status code is (\d+)$`, func(want int) error {
		if state.lastSagaResponse == nil {
			return errors.New("no applySaga response captured")
		}
		if state.lastSagaResponse.statusCode != want {
			return fmt.Errorf("status code = %d, want %d; body=%s",
				state.lastSagaResponse.statusCode, want, state.lastSagaResponse.body)
		}
		return nil
	})

	sc.Then(`^the saga response body status is "([^"]+)"$`, func(want string) error {
		sg, err := decodeSagaResultFromResponse(state.lastSagaResponse)
		if err != nil {
			return err
		}
		if sg.Status != want {
			return fmt.Errorf("saga.status = %q, want %q", sg.Status, want)
		}
		return nil
	})

	sc.Then(`^the saga response body has (\d+) applied entries$`, func(want int) error {
		sg, err := decodeSagaResultFromResponse(state.lastSagaResponse)
		if err != nil {
			return err
		}
		if got := len(sg.Applied); got != want {
			return fmt.Errorf("len(saga.applied) = %d, want %d", got, want)
		}
		return nil
	})

	sc.Then(`^the saga response body has (\d+) compensation entries$`, func(want int) error {
		sg, err := decodeSagaResultFromResponse(state.lastSagaResponse)
		if err != nil {
			return err
		}
		if got := len(sg.Compensations); got != want {
			return fmt.Errorf("len(saga.compensations) = %d, want %d", got, want)
		}
		return nil
	})

	sc.Then(`^the saga response body has (\d+) DLQ entries$`, func(want int) error {
		sg, err := decodeSagaResultFromResponse(state.lastSagaResponse)
		if err != nil {
			return err
		}
		if got := len(sg.DLQEntries); got != want {
			return fmt.Errorf("len(saga.dlqEntries) = %d, want %d (entries=%v)",
				got, want, sg.DLQEntries)
		}
		return nil
	})

	// --- Then assertions on the NATS publisher (recordingPublisher) -------------

	sc.Then(`^the publisher captured (\d+) primary batch(?:es)?$`, func(want int) error {
		got := len(state.sagaPublisher.snapshot())
		if got != want {
			return fmt.Errorf("publisher captured %d batches, want %d", got, want)
		}
		return nil
	})

	// --- Then assertions on the DB rows ----------------------------------------

	sc.Then(`^the action_sagas row has status "([^"]+)"$`, func(want string) error {
		sagaID, err := sagaIDFromResponse(state.lastSagaResponse)
		if err != nil {
			return err
		}
		got, err := state.sagaStore.GetSaga(context.Background(), sagaID)
		if err != nil {
			return fmt.Errorf("GetSaga(%s): %w", sagaID, err)
		}
		if got.Status != want {
			return fmt.Errorf("action_sagas.status = %q, want %q", got.Status, want)
		}
		return nil
	})

	sc.Then(`^the action_saga_steps row at index (\d+) has status "([^"]+)"$`,
		func(idx int, want string) error {
			sagaID, err := sagaIDFromResponse(state.lastSagaResponse)
			if err != nil {
				return err
			}
			steps, err := state.sagaStore.ListSagaSteps(context.Background(), sagaID)
			if err != nil {
				return fmt.Errorf("ListSagaSteps(%s): %w", sagaID, err)
			}
			if idx < 0 || idx >= len(steps) {
				return fmt.Errorf("step index %d out of range (len=%d)", idx, len(steps))
			}
			if steps[idx].Status != want {
				return fmt.Errorf("action_saga_steps[%d].status = %q, want %q",
					idx, steps[idx].Status, want)
			}
			return nil
		},
	)

	sc.Then(`^the action_saga_dlq table has (\d+) PENDING row(?:s)?$`, func(want int) error {
		entries, err := state.sagaStore.ListDLQ(context.Background(), actions.SagaDLQStatusPending, 0)
		if err != nil {
			return fmt.Errorf("ListDLQ: %w", err)
		}
		if got := len(entries); got != want {
			return fmt.Errorf("action_saga_dlq PENDING rows = %d, want %d", got, want)
		}
		return nil
	})
}

// seedSagaOntology stands up the canonical Order/Booking compensating
// action graph used by every scenario in action_saga_compensation.feature:
//
//   - createOrder  → compensates with deleteOrder
//   - bookResource → compensates with deleteBooking
//
// Each createX action declares (primaryKey, X-specific-field) parameters
// and a createObject rule on its object type; each deleteX action
// declares (primaryKey) and a deleteObject rule. The compensator links
// are set via UpdateActionType because CreateActionType also needs the
// compensator RID to exist first.
func seedSagaOntology(state *suiteState, ontologyAPIName string) error {
	ctx := context.Background()

	// 1. Ontology
	ontRID := rid.NewOntologyRID()
	ont := &oms.Ontology{
		RID:         ontRID,
		APIName:     ontologyAPIName,
		DisplayName: ontologyAPIName,
	}
	if err := state.repo.CreateOntology(ctx, ont); err != nil {
		return fmt.Errorf("seed CreateOntology: %w", err)
	}
	state.rememberOntologyRID(ontologyAPIName, ontRID)

	// 2. Two object types: Order, Booking
	for _, ot := range []struct {
		api, display string
	}{
		{"Order", "Order"},
		{"Booking", "Booking"},
	} {
		otRID := rid.NewObjectTypeRID()
		row := &oms.ObjectType{
			RID:         otRID,
			OntologyRID: ontRID,
			APIName:     ot.api,
			DisplayName: ot.display,
			PrimaryKey:  "primaryKey",
			PrimaryKeys: []string{"primaryKey"},
			Status:      "ACTIVE",
			Visibility:  "NORMAL",
		}
		if err := state.repo.CreateObjectType(ctx, row); err != nil {
			return fmt.Errorf("seed CreateObjectType(%s): %w", ot.api, err)
		}
		state.rememberObjectTypeRID(ontologyAPIName, ot.api, otRID)
	}

	// 3. Compensator action types (no CompensateActionRID — they ARE the
	//    compensators) so their RIDs exist before we wire them onto the
	//    primary actions.
	if err := seedActionType(state, ontologyAPIName, "deleteOrder", "Order",
		"deleteObject", []paramSpec{{"primaryKey", true}}, ""); err != nil {
		return err
	}
	if err := seedActionType(state, ontologyAPIName, "deleteBooking", "Booking",
		"deleteObject", []paramSpec{{"primaryKey", true}}, ""); err != nil {
		return err
	}

	// 4. Primary action types pointing at their compensator RIDs.
	delOrderRID, _ := state.actionTypeRIDFor(ontologyAPIName, "deleteOrder")
	delBookingRID, _ := state.actionTypeRIDFor(ontologyAPIName, "deleteBooking")
	if err := seedActionType(state, ontologyAPIName, "createOrder", "Order",
		"createObject", []paramSpec{{"primaryKey", true}, {"name", true}},
		delOrderRID); err != nil {
		return err
	}
	if err := seedActionType(state, ontologyAPIName, "bookResource", "Booking",
		"createObject", []paramSpec{{"primaryKey", true}, {"resourceId", true}},
		delBookingRID); err != nil {
		return err
	}
	return nil
}

type paramSpec struct {
	name     string
	required bool
}

// seedActionType is the seed-helper companion for seedSagaOntology — it
// persists one ActionType row with the given rule type / object type /
// parameter list, optionally pointing at a compensator RID. Parameters
// are encoded into the ActionType.Parameters JSON the executor parses at
// dispatch time.
func seedActionType(state *suiteState, ontologyAPIName, atAPIName, objectType,
	ruleType string, params []paramSpec, compensateActionRID string) error {
	ontRID, ok := state.ontologyRIDFor(ontologyAPIName)
	if !ok {
		return fmt.Errorf("seedActionType: ontology %q not seeded", ontologyAPIName)
	}
	defs := make([]actions.ParameterDef, len(params))
	for i, p := range params {
		defs[i] = actions.ParameterDef{
			ID:       p.name,
			Type:     "string",
			Required: p.required,
		}
	}
	paramsJSON, err := json.Marshal(defs)
	if err != nil {
		return fmt.Errorf("encode params: %w", err)
	}
	rule := actions.Rule{Type: ruleType, ObjectType: objectType}
	// createObject rules carry property bindings for every non-PK parameter
	// so the published Edit reflects every input value (matches the saga
	// happy-path test fixtures in pkg/actions/saga_test.go).
	if ruleType == "createObject" {
		rule.PropertyBindings = map[string]actions.PropertyBinding{}
		for _, p := range params {
			if p.name == "primaryKey" {
				continue
			}
			rule.PropertyBindings[p.name] = actions.PropertyBinding{
				Type: "parameter", Value: p.name,
			}
		}
	}
	rulesJSON, err := json.Marshal([]actions.Rule{rule})
	if err != nil {
		return fmt.Errorf("encode rules: %w", err)
	}
	atRID := rid.NewActionTypeRID()
	at := &oms.ActionType{
		RID:                 atRID,
		OntologyRID:         ontRID,
		APIName:             atAPIName,
		DisplayName:         atAPIName,
		Status:              "ACTIVE",
		Parameters:          paramsJSON,
		Rules:               rulesJSON,
		CompensateActionRID: compensateActionRID,
	}
	if err := state.repo.CreateActionType(context.Background(), at); err != nil {
		return fmt.Errorf("CreateActionType(%s): %w", atAPIName, err)
	}
	state.rememberActionTypeRID(ontologyAPIName, atAPIName, atRID)
	return nil
}

// buildApplySagaBody converts a godog DataTable into the applySaga
// request body. The first row is treated as the header. Every other
// column besides "actionType" is a parameter name; empty cells are
// omitted from the parameters map (used to model "missing required
// parameter" scenarios for the saga step-fail path).
func buildApplySagaBody(table *godog.Table) (map[string]interface{}, error) {
	if table == nil || len(table.Rows) < 2 {
		return nil, errors.New("applySaga step requires a table with a header and at least one data row")
	}
	headers := make([]string, len(table.Rows[0].Cells))
	for i, c := range table.Rows[0].Cells {
		headers[i] = c.Value
	}
	actionTypeCol := -1
	for i, h := range headers {
		if h == "actionType" {
			actionTypeCol = i
			break
		}
	}
	if actionTypeCol < 0 {
		return nil, errors.New("applySaga table must have an 'actionType' column")
	}
	steps := make([]map[string]interface{}, 0, len(table.Rows)-1)
	for r := 1; r < len(table.Rows); r++ {
		cells := table.Rows[r].Cells
		params := map[string]interface{}{}
		var actionType string
		for col, header := range headers {
			val := cells[col].Value
			if col == actionTypeCol {
				actionType = val
				continue
			}
			if val == "" {
				continue
			}
			params[header] = val
		}
		if actionType == "" {
			return nil, fmt.Errorf("row %d: empty actionType", r)
		}
		steps = append(steps, map[string]interface{}{
			"actionType": actionType,
			"parameters": params,
		})
	}
	return map[string]interface{}{"steps": steps}, nil
}

// decodeSagaResultFromResponse extracts the SagaResult JSON from the
// last applySaga response. Happy responses ship the SagaResult at the
// top level; error responses wrap it in {errorCode, errorName, ...,
// saga: {...}} via writeSagaErrorResponse in pkg/actions/saga_handler.go.
func decodeSagaResultFromResponse(resp *sagaHTTPResult) (*actions.SagaResult, error) {
	if resp == nil {
		return nil, errors.New("no applySaga response captured")
	}
	// Try error envelope first since that is the more deeply-nested shape.
	var envelope struct {
		Saga *actions.SagaResult `json:"saga,omitempty"`
	}
	if err := json.Unmarshal(resp.body, &envelope); err == nil && envelope.Saga != nil {
		return envelope.Saga, nil
	}
	var sg actions.SagaResult
	if err := json.Unmarshal(resp.body, &sg); err != nil {
		return nil, fmt.Errorf("decode saga response: %w", err)
	}
	return &sg, nil
}

// sagaIDFromResponse pulls the SagaID out of the response body. Falls
// back to a strconv error so step output stays human-readable when an
// assertion runs before the request executed.
func sagaIDFromResponse(resp *sagaHTTPResult) (string, error) {
	sg, err := decodeSagaResultFromResponse(resp)
	if err != nil {
		return "", err
	}
	if sg.SagaID == "" {
		return "", errors.New("response did not carry a sagaId — saga store may not be wired")
	}
	return sg.SagaID, nil
}

// stepCountString is a tiny convenience used by the Then-step that
// asserts the count of saga steps in a particular status — kept around
// for future scenario additions.
func stepCountString(n int) string { return strconv.Itoa(n) }
