package actions

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// Foundry SyncApplyActionResponseV2 parity — the `validation` field.
//
// Foundry's apply endpoint ALWAYS reports validation in the response
// envelope, and the field is the full ValidateActionResponseV2 shape:
//
//	{result: "VALID"|"INVALID",
//	 submissionCriteria: [...],
//	 parameters: {<paramId>: {result, evaluatedConstraints[], required}}}
//
// Two pre-fix deviations are locked down here:
//
//  1. apply?mode=VALIDATE_ONLY returned a thin one-off envelope
//     ValidateOnlyResponse{validation:{result}} — no submissionCriteria,
//     no per-parameter attribution — even though the dedicated /validate
//     endpoint already produced the rich structure. OSDK clients reading
//     validation.parameters off a VALIDATE_ONLY apply saw undefined.
//
//  2. The VALIDATE_AND_EXECUTE success path omitted `validation`
//     entirely, so clients could not confirm the VALID verdict that
//     Prepare had already computed before executing.

// TestBDD_ApplyAction_ValidateOnlyReturnsRichValidation
//
//	Given an ontology with a createEmployee action (required "name" param)
//	When  POST .../actions/{action}/apply with options.mode=VALIDATE_ONLY
//	      and valid parameters
//	Then  HTTP 200 with the SyncApplyActionResponseV2 envelope whose
//	      validation carries result=VALID, submissionCriteria=[] and
//	      parameters.name={result:VALID, required:true,
//	      evaluatedConstraints:[]}
//	And   nothing is published and no edits/operationId appear
//
//	When  the required "name" parameter is missing
//	Then  HTTP 200 with validation.result=INVALID, a submissionCriteria
//	      entry carrying the failure message, and parameters.name flagged
//	      INVALID with a {type:"required", result:"INVALID"} constraint
func TestBDD_ApplyAction_ValidateOnlyReturnsRichValidation(t *testing.T) {
	newFixture := func() (http.Handler, *fakePublisher) {
		repo := &mockOmsRepo{
			actionTypes: []oms.ActionType{
				newTestActionType("createEmployee", []ParameterDef{
					{ID: "name", Type: "string", Required: true},
					{ID: "nickname", Type: "string", Required: false},
				}, []Rule{
					{Type: "createObject", ObjectType: "Employee",
						PropertyBindings: map[string]PropertyBinding{
							"name": {Type: "parameter", Value: "name"},
						}},
				}),
			},
		}
		pub := &fakePublisher{offset: 1}
		return setupRouter(NewHandler(NewExecutor(repo, pub))), pub
	}

	postValidateOnly := func(t *testing.T, router http.Handler, params map[string]interface{}) *httptest.ResponseRecorder {
		t.Helper()
		body := mustJSON(map[string]interface{}{
			"parameters": params,
			"options":    map[string]interface{}{"mode": "VALIDATE_ONLY"},
		})
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/ont-1/actions/createEmployee/apply", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	t.Run("valid params → VALID with submissionCriteria and per-parameter map", func(t *testing.T) {
		router, pub := newFixture()
		w := postValidateOnly(t, router, map[string]interface{}{"name": "Alice"})

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		if pub.calls != 0 {
			t.Fatalf("VALIDATE_ONLY must not publish, got %d calls", pub.calls)
		}

		env, validation := decodeValidationRaw(t, w.Body.Bytes())
		for _, key := range []string{"operationId", "actionLogId", "edits"} {
			if _, ok := env[key]; ok {
				t.Errorf("VALIDATE_ONLY must omit %q from the envelope; payload = %s", key, w.Body.String())
			}
		}
		if got := validation["result"]; got != "VALID" {
			t.Errorf("validation.result = %v, want VALID; payload = %s", got, w.Body.String())
		}

		sc, ok := validation["submissionCriteria"].([]interface{})
		if !ok {
			t.Fatalf("validation.submissionCriteria must be a JSON array; payload = %s", w.Body.String())
		}
		if len(sc) != 0 {
			t.Errorf("VALID report must carry empty submissionCriteria, got %v", sc)
		}

		params := paramsMap(t, validation, w.Body.Bytes())
		name, ok := params["name"].(map[string]interface{})
		if !ok {
			t.Fatalf("validation.parameters.name must be present; payload = %s", w.Body.String())
		}
		if got := name["result"]; got != "VALID" {
			t.Errorf("parameters.name.result = %v, want VALID", got)
		}
		if got := name["required"]; got != true {
			t.Errorf("parameters.name.required = %v, want true", got)
		}
		if _, ok := name["evaluatedConstraints"].([]interface{}); !ok {
			t.Errorf("parameters.name.evaluatedConstraints must be a JSON array; payload = %s", w.Body.String())
		}
		nickname, ok := params["nickname"].(map[string]interface{})
		if !ok {
			t.Fatalf("validation.parameters.nickname must be present; payload = %s", w.Body.String())
		}
		if got := nickname["required"]; got != false {
			t.Errorf("parameters.nickname.required = %v, want false", got)
		}
	})

	t.Run("missing required param → INVALID with attribution", func(t *testing.T) {
		router, pub := newFixture()
		w := postValidateOnly(t, router, map[string]interface{}{})

		if w.Code != http.StatusOK {
			t.Fatalf("VALIDATE_ONLY reports INVALID at 200, got %d: %s", w.Code, w.Body.String())
		}
		if pub.calls != 0 {
			t.Fatalf("VALIDATE_ONLY must not publish, got %d calls", pub.calls)
		}

		_, validation := decodeValidationRaw(t, w.Body.Bytes())
		if got := validation["result"]; got != "INVALID" {
			t.Errorf("validation.result = %v, want INVALID; payload = %s", got, w.Body.String())
		}

		sc, ok := validation["submissionCriteria"].([]interface{})
		if !ok || len(sc) == 0 {
			t.Fatalf("INVALID report must carry a submissionCriteria entry; payload = %s", w.Body.String())
		}
		first, _ := sc[0].(map[string]interface{})
		if got := first["result"]; got != "INVALID" {
			t.Errorf("submissionCriteria[0].result = %v, want INVALID", got)
		}
		if msg, _ := first["configuredFailureMessage"].(string); msg == "" {
			t.Errorf("submissionCriteria[0].configuredFailureMessage must carry the failure; payload = %s", w.Body.String())
		}

		params := paramsMap(t, validation, w.Body.Bytes())
		name, ok := params["name"].(map[string]interface{})
		if !ok {
			t.Fatalf("validation.parameters.name must be present; payload = %s", w.Body.String())
		}
		if got := name["result"]; got != "INVALID" {
			t.Errorf("parameters.name.result = %v, want INVALID", got)
		}
		ecs, _ := name["evaluatedConstraints"].([]interface{})
		if len(ecs) != 1 {
			t.Fatalf("parameters.name.evaluatedConstraints must carry one entry, got %v; payload = %s", ecs, w.Body.String())
		}
		ec, _ := ecs[0].(map[string]interface{})
		if ec["type"] != "required" || ec["result"] != "INVALID" {
			t.Errorf("evaluatedConstraints[0] = %v, want {type:required result:INVALID}", ec)
		}
	})
}

// TestBDD_ApplyAction_ExecuteBackfillsValidation
//
//	Given an ontology with a createEmployee action (required "name" param)
//	When  POST .../actions/{action}/apply with default mode
//	      (VALIDATE_AND_EXECUTE) and valid parameters
//	Then  HTTP 200 where the SyncApplyActionResponseV2 envelope carries BOTH
//	      validation (result=VALID with the Prepare-stage submissionCriteria
//	      and per-parameter map) AND the edits summary
//	And   the action really executed (one publish, operationId present)
func TestBDD_ApplyAction_ExecuteBackfillsValidation(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
			}),
		},
	}
	pub := &fakePublisher{offset: 1}
	router := setupRouter(NewHandler(NewExecutor(repo, pub)))

	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{"name": "Alice"},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/apply", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if pub.calls != 1 {
		t.Fatalf("VALIDATE_AND_EXECUTE must publish exactly once, got %d calls", pub.calls)
	}

	env, validation := decodeValidationRaw(t, w.Body.Bytes())
	if opID, _ := env["operationId"].(string); opID == "" {
		t.Errorf("executed apply must carry operationId; payload = %s", w.Body.String())
	}

	if got := validation["result"]; got != "VALID" {
		t.Errorf("validation.result = %v, want VALID; payload = %s", got, w.Body.String())
	}
	if _, ok := validation["submissionCriteria"].([]interface{}); !ok {
		t.Errorf("validation.submissionCriteria must be a JSON array; payload = %s", w.Body.String())
	}
	params := paramsMap(t, validation, w.Body.Bytes())
	name, ok := params["name"].(map[string]interface{})
	if !ok {
		t.Fatalf("validation.parameters.name must be present; payload = %s", w.Body.String())
	}
	if got := name["result"]; got != "VALID" {
		t.Errorf("parameters.name.result = %v, want VALID", got)
	}
	if got := name["required"]; got != true {
		t.Errorf("parameters.name.required = %v, want true", got)
	}

	edits := decodeEditsRaw(t, w.Body.Bytes())
	if got := edits["addedObjectCount"]; got != float64(1) {
		t.Errorf("edits.addedObjectCount = %v, want 1; payload = %s", got, w.Body.String())
	}
}

// decodeValidationRaw pulls the top-level envelope and the `validation`
// object out of a response body as raw maps so key NAMES (not just Go-side
// values) can be asserted.
func decodeValidationRaw(t *testing.T, body []byte) (env, validation map[string]interface{}) {
	t.Helper()
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	validation, ok := env["validation"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected validation object in response; body = %s", string(body))
	}
	return env, validation
}

// paramsMap pulls validation.parameters as a raw map, failing the test when
// the key is absent or not an object.
func paramsMap(t *testing.T, validation map[string]interface{}, body []byte) map[string]interface{} {
	t.Helper()
	params, ok := validation["parameters"].(map[string]interface{})
	if !ok {
		t.Fatalf("validation.parameters must be a JSON object; payload = %s", string(body))
	}
	return params
}
