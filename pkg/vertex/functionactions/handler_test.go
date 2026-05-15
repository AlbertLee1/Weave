package functionactions_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/vertex/functionactions"
)

// stubBindingRepo records the bindings the handler tries to persist. The
// Get path is filled lazily by tests that need it; Create is the only
// surface the register endpoint exercises, so its book-keeping carries
// the bulk of the stub.
type stubBindingRepo struct {
	mu        sync.Mutex
	created   []functionactions.FunctionActionBinding
	createErr error
	byAction  map[string]*functionactions.FunctionActionBinding
}

func (s *stubBindingRepo) Create(_ context.Context, b *functionactions.FunctionActionBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createErr != nil {
		return s.createErr
	}
	s.created = append(s.created, *b)
	return nil
}

func (s *stubBindingRepo) GetByActionType(_ context.Context, _, actionTypeRID string) (*functionactions.FunctionActionBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.byAction[actionTypeRID]
	if !ok {
		return nil, oms.ErrNotFound
	}
	return b, nil
}

// stubActionLookup serves *oms.ActionType rows by RID; unknown RIDs map
// to oms.ErrNotFound to match the canonical Repository semantics the
// handler relies on.
type stubActionLookup struct {
	byRID map[string]*oms.ActionType
}

func (s *stubActionLookup) GetActionType(_ context.Context, rid string) (*oms.ActionType, error) {
	at, ok := s.byRID[rid]
	if !ok {
		return nil, oms.ErrNotFound
	}
	return at, nil
}

type stubResolver struct {
	byAPIName map[string]string
}

func (s *stubResolver) ResolveOntologyRID(_ context.Context, apiName string) (string, error) {
	rid, ok := s.byAPIName[apiName]
	if !ok {
		return "", oms.ErrNotFound
	}
	return rid, nil
}

func newTestRouter(t *testing.T, br functionactions.BindingRepo, al functionactions.ActionTypeLookup, rs functionactions.OntologyResolver) chi.Router {
	t.Helper()
	h := functionactions.NewHandler(br, al, rs)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

func doRegister(t *testing.T, router chi.Router, apiName string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost,
		"/api/vertex/v1/ontologies/"+apiName+"/function-actions/register",
		bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

const (
	testOntologyAPI = "o1"
	testOntologyRID = "ri.ontology.main.ontology.o1"
	testActionRID   = "ri.ontology.main.action-type.a1"
	testFunctionRID = "ri.ontology.main.function.f1"
)

func validRegisterBody() map[string]interface{} {
	return map[string]interface{}{
		"actionTypeRid": testActionRID,
		"functionRid":   testFunctionRID,
		"outputMappings": []map[string]string{{
			"outputField":         "predictedDelay",
			"objectType":          "Flight",
			"primaryKeyParameter": "flightId",
			"property":            "predictedDelay",
		}},
		"createdBy": "creator@example.com",
	}
}

func functionBackedAT() *oms.ActionType {
	return &oms.ActionType{
		RID:              testActionRID,
		OntologyRID:      testOntologyRID,
		APIName:          "predictDelay",
		IsFunctionBacked: true,
		FunctionRID:      testFunctionRID,
	}
}

func TestRegister_Given_ValidBody_When_POST_Then_201WithBinding(t *testing.T) {
	repo := &stubBindingRepo{}
	actions := &stubActionLookup{byRID: map[string]*oms.ActionType{testActionRID: functionBackedAT()}}
	resolver := &stubResolver{byAPIName: map[string]string{testOntologyAPI: testOntologyRID}}
	router := newTestRouter(t, repo, actions, resolver)

	rr := doRegister(t, router, testOntologyAPI, validRegisterBody())

	if rr.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body=%s", rr.Code, rr.Body.String())
	}
	var got functionactions.FunctionActionBinding
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.RID == "" || got.OntologyRID != testOntologyRID {
		t.Errorf("RID/OntologyRID: got %+v", got)
	}
	if got.ActionTypeRID != testActionRID || got.FunctionRID != testFunctionRID {
		t.Errorf("ActionTypeRID/FunctionRID: got %+v", got)
	}
	if len(got.OutputMappings) != 1 || got.OutputMappings[0].Property != "predictedDelay" {
		t.Errorf("OutputMappings: got %+v", got.OutputMappings)
	}
	if got.CreatedBy != "creator@example.com" {
		t.Errorf("CreatedBy: got %q", got.CreatedBy)
	}
	if got.CreatedAt.IsZero() {
		t.Errorf("CreatedAt should be server-stamped, got zero")
	}
	if len(repo.created) != 1 {
		t.Fatalf("persisted: got %d, want 1", len(repo.created))
	}
}

func TestRegister_Given_UnknownOntology_When_POST_Then_404OntologyNotFound(t *testing.T) {
	repo := &stubBindingRepo{}
	actions := &stubActionLookup{byRID: map[string]*oms.ActionType{testActionRID: functionBackedAT()}}
	resolver := &stubResolver{byAPIName: map[string]string{}}
	router := newTestRouter(t, repo, actions, resolver)

	rr := doRegister(t, router, "missing", validRegisterBody())
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
}

func TestRegister_Given_UnknownActionType_When_POST_Then_404ActionTypeNotFound(t *testing.T) {
	repo := &stubBindingRepo{}
	actions := &stubActionLookup{byRID: map[string]*oms.ActionType{}}
	resolver := &stubResolver{byAPIName: map[string]string{testOntologyAPI: testOntologyRID}}
	router := newTestRouter(t, repo, actions, resolver)

	rr := doRegister(t, router, testOntologyAPI, validRegisterBody())
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
}

func TestRegister_Given_ActionTypeBelongsToDifferentOntology_When_POST_Then_400(t *testing.T) {
	repo := &stubBindingRepo{}
	at := functionBackedAT()
	at.OntologyRID = "ri.ontology.main.ontology.different"
	actions := &stubActionLookup{byRID: map[string]*oms.ActionType{testActionRID: at}}
	resolver := &stubResolver{byAPIName: map[string]string{testOntologyAPI: testOntologyRID}}
	router := newTestRouter(t, repo, actions, resolver)

	rr := doRegister(t, router, testOntologyAPI, validRegisterBody())
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestRegister_Given_ActionTypeNotFunctionBacked_When_POST_Then_400(t *testing.T) {
	repo := &stubBindingRepo{}
	at := functionBackedAT()
	at.IsFunctionBacked = false
	actions := &stubActionLookup{byRID: map[string]*oms.ActionType{testActionRID: at}}
	resolver := &stubResolver{byAPIName: map[string]string{testOntologyAPI: testOntologyRID}}
	router := newTestRouter(t, repo, actions, resolver)

	rr := doRegister(t, router, testOntologyAPI, validRegisterBody())
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestRegister_Given_FunctionRIDMismatch_When_POST_Then_400(t *testing.T) {
	repo := &stubBindingRepo{}
	at := functionBackedAT()
	at.FunctionRID = "ri.ontology.main.function.different"
	actions := &stubActionLookup{byRID: map[string]*oms.ActionType{testActionRID: at}}
	resolver := &stubResolver{byAPIName: map[string]string{testOntologyAPI: testOntologyRID}}
	router := newTestRouter(t, repo, actions, resolver)

	rr := doRegister(t, router, testOntologyAPI, validRegisterBody())
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestRegister_Given_MissingActionTypeRID_When_POST_Then_400(t *testing.T) {
	repo := &stubBindingRepo{}
	actions := &stubActionLookup{byRID: map[string]*oms.ActionType{testActionRID: functionBackedAT()}}
	resolver := &stubResolver{byAPIName: map[string]string{testOntologyAPI: testOntologyRID}}
	router := newTestRouter(t, repo, actions, resolver)

	body := validRegisterBody()
	delete(body, "actionTypeRid")
	rr := doRegister(t, router, testOntologyAPI, body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestRegister_Given_MissingFunctionRID_When_POST_Then_400(t *testing.T) {
	repo := &stubBindingRepo{}
	actions := &stubActionLookup{byRID: map[string]*oms.ActionType{testActionRID: functionBackedAT()}}
	resolver := &stubResolver{byAPIName: map[string]string{testOntologyAPI: testOntologyRID}}
	router := newTestRouter(t, repo, actions, resolver)

	body := validRegisterBody()
	delete(body, "functionRid")
	rr := doRegister(t, router, testOntologyAPI, body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestRegister_Given_InvalidOutputMapping_When_POST_Then_400(t *testing.T) {
	repo := &stubBindingRepo{}
	actions := &stubActionLookup{byRID: map[string]*oms.ActionType{testActionRID: functionBackedAT()}}
	resolver := &stubResolver{byAPIName: map[string]string{testOntologyAPI: testOntologyRID}}
	router := newTestRouter(t, repo, actions, resolver)

	body := validRegisterBody()
	body["outputMappings"] = []map[string]string{{
		// Missing Property.
		"outputField":         "predictedDelay",
		"objectType":          "Flight",
		"primaryKeyParameter": "flightId",
	}}
	rr := doRegister(t, router, testOntologyAPI, body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestRegister_Given_MalformedJSON_When_POST_Then_400(t *testing.T) {
	repo := &stubBindingRepo{}
	actions := &stubActionLookup{byRID: map[string]*oms.ActionType{testActionRID: functionBackedAT()}}
	resolver := &stubResolver{byAPIName: map[string]string{testOntologyAPI: testOntologyRID}}
	router := newTestRouter(t, repo, actions, resolver)

	req := httptest.NewRequest(http.MethodPost,
		"/api/vertex/v1/ontologies/"+testOntologyAPI+"/function-actions/register",
		bytes.NewReader([]byte("{not valid json")))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestRegister_Given_DuplicateBinding_When_POST_Then_409(t *testing.T) {
	repo := &stubBindingRepo{createErr: oms.ErrDuplicate}
	actions := &stubActionLookup{byRID: map[string]*oms.ActionType{testActionRID: functionBackedAT()}}
	resolver := &stubResolver{byAPIName: map[string]string{testOntologyAPI: testOntologyRID}}
	router := newTestRouter(t, repo, actions, resolver)

	rr := doRegister(t, router, testOntologyAPI, validRegisterBody())
	if rr.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
}
