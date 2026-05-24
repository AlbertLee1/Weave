package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_ValidateAction_FoundryEndpoint covers the Foundry-1:1 alignment
// gap that this iteration closes: until now Weave only supported the
// "VALIDATE_ONLY" option on the /apply endpoint, but Foundry OSv2 ships a
// dedicated POST /api/v2/ontologies/{ontologyApiName}/actions/{action}/validate
// surface that SDKs hit on every form-field change to drive client-side
// validation without ever touching the Funnel. The new endpoint:
//
//   - never publishes to NATS (no Publisher.Publish call);
//   - always returns HTTP 200 with a ValidateActionResponse body, even
//     when the validation result is INVALID — the wire shape is a
//     report, not an HTTP error;
//   - carries the Foundry shape {result, submissionCriteria, parameters}
//     where parameters is a per-parameter map and submissionCriteria is
//     a list of {result, configuredFailureMessage} envelopes;
//   - attributes per-parameter INVALID when the underlying validation
//     error names a known parameter id (so the SDK can red-line a
//     specific form field rather than dump the raw failure).
func TestBDD_ValidateAction_FoundryEndpoint(t *testing.T) {
	newHandler := func(t *testing.T) (*Handler, *fakePublisher, *mockOmsRepo) {
		t.Helper()
		repo := &mockOmsRepo{
			actionTypes: []oms.ActionType{
				newTestActionType("createEmployee", []ParameterDef{
					{ID: "name", Type: "string", Required: true},
					{ID: "title", Type: "string", Required: false},
				}, []Rule{
					{
						Type:       "createObject",
						ObjectType: "Employee",
						PropertyBindings: map[string]PropertyBinding{
							"name": {Type: "parameter", Value: "name"},
						},
					},
				}),
			},
		}
		pub := &fakePublisher{}
		exec := NewExecutor(repo, pub)
		return NewHandler(exec), pub, repo
	}

	doValidate := func(t *testing.T, h *Handler, action string, body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/ont-1/actions/"+action+"/validate",
			bytes.NewReader(raw))
		// Chi URLParam reads from the route ctx, not the path, so simulate it.
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("ontologyApiName", "ont-1")
		rctx.URLParams.Add("action", action)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		h.Validate(rec, req)
		return rec
	}

	t.Run("valid parameters return result=VALID with empty submissionCriteria", func(t *testing.T) {
		h, pub, _ := newHandler(t)
		rec := doValidate(t, h, "createEmployee", map[string]any{
			"parameters": map[string]any{"name": "Alice"},
		})

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp ValidateActionResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Result != "VALID" {
			t.Errorf("result: got %q want VALID", resp.Result)
		}
		if len(resp.SubmissionCriteria) != 0 {
			t.Errorf("submissionCriteria: want empty, got %+v", resp.SubmissionCriteria)
		}
		if resp.Parameters == nil {
			t.Fatal("parameters must be a non-nil map (Foundry wire shape)")
		}
		if got := resp.Parameters["name"]; got.Result != "VALID" || !got.Required {
			t.Errorf("parameters.name: got %+v, want result=VALID required=true", got)
		}
		if got := resp.Parameters["title"]; got.Result != "VALID" || got.Required {
			t.Errorf("parameters.title: got %+v, want result=VALID required=false", got)
		}
		// The validate endpoint must NEVER publish — a Foundry SDK is
		// safe to call this on every keystroke without firing the
		// action.
		if pub.calls != 0 {
			t.Errorf("validate must not publish, got %d calls", pub.calls)
		}
	})

	t.Run("missing required parameter returns INVALID with per-parameter attribution", func(t *testing.T) {
		h, pub, _ := newHandler(t)
		rec := doValidate(t, h, "createEmployee", map[string]any{
			"parameters": map[string]any{}, // missing required "name"
		})

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 (validate reports never error), got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp ValidateActionResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Result != "INVALID" {
			t.Errorf("result: got %q want INVALID", resp.Result)
		}
		if len(resp.SubmissionCriteria) == 0 {
			t.Errorf("submissionCriteria must carry at least one entry on INVALID, got empty")
		} else if resp.SubmissionCriteria[0].Result != "INVALID" {
			t.Errorf("submissionCriteria[0].result: got %q want INVALID", resp.SubmissionCriteria[0].Result)
		}
		nameRes := resp.Parameters["name"]
		if nameRes.Result != "INVALID" {
			t.Errorf("parameters.name.result: got %q want INVALID (missing required must attribute back)", nameRes.Result)
		}
		// Non-offending parameter stays VALID — the SDK should only
		// red-line the field that actually failed.
		if got := resp.Parameters["title"].Result; got != "VALID" {
			t.Errorf("parameters.title.result: got %q want VALID", got)
		}
		if pub.calls != 0 {
			t.Errorf("validate must not publish even on INVALID, got %d calls", pub.calls)
		}
	})

	t.Run("unknown action type still returns INVALID with submissionCriteria, never 404", func(t *testing.T) {
		h, pub, _ := newHandler(t)
		rec := doValidate(t, h, "doesNotExist", map[string]any{
			"parameters": map[string]any{},
		})

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 (validate envelope is a report), got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp ValidateActionResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Result != "INVALID" {
			t.Errorf("unknown-action result: got %q want INVALID", resp.Result)
		}
		if len(resp.SubmissionCriteria) == 0 {
			t.Fatalf("unknown-action must surface a submissionCriterion")
		}
		msg := strings.ToLower(resp.SubmissionCriteria[0].ConfiguredFailureMessage)
		if !strings.Contains(msg, "doesnotexist") && !strings.Contains(msg, "not found") {
			t.Errorf("submissionCriteria message should mention the missing action, got %q",
				resp.SubmissionCriteria[0].ConfiguredFailureMessage)
		}
		if pub.calls != 0 {
			t.Errorf("validate must not publish on unknown action, got %d", pub.calls)
		}
	})

	t.Run("missing action segment returns 400 MissingActionType (path-level guard)", func(t *testing.T) {
		h, _, _ := newHandler(t)
		// Bypass doValidate so we can set the URL param to empty.
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/ont-1/actions//validate", bytes.NewReader([]byte(`{}`)))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("ontologyApiName", "ont-1")
		rctx.URLParams.Add("action", "")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		h.Validate(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for missing action segment, got %d body=%s", rec.Code, rec.Body.String())
		}
		var env struct {
			ErrorName string `json:"errorName"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&env)
		if env.ErrorName != "MissingActionType" {
			t.Errorf("errorName: got %q want MissingActionType", env.ErrorName)
		}
	})
}
