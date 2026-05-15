package modelfunctions_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/vertex/modelfunctions"
)

// stubDeploymentRepo records the deployments the handler tries to persist
// so tests can assert the wire body round-trips into the persistence
// layer correctly.
type stubDeploymentRepo struct {
	mu        sync.Mutex
	created   []modelfunctions.Deployment
	createErr error
}

func (s *stubDeploymentRepo) Create(_ context.Context, dep *modelfunctions.Deployment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createErr != nil {
		return s.createErr
	}
	s.created = append(s.created, *dep)
	return nil
}

// stubFunctionCreator records the generated wrapper Function so tests can
// confirm the signature/runtime are wired up before the row hits the OMS
// repository.
type stubFunctionCreator struct {
	mu        sync.Mutex
	created   []oms.Function
	createErr error
}

func (s *stubFunctionCreator) CreateFunction(_ context.Context, fn *oms.Function) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createErr != nil {
		return s.createErr
	}
	s.created = append(s.created, *fn)
	return nil
}

// stubResolver mirrors the funcregistry test stub: maps API name → RID
// and synthesises oms.ErrNotFound for unknown names.
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

func newTestRouter(t *testing.T, depRepo modelfunctions.DeploymentRepo, fnCreator modelfunctions.FunctionCreator, resolver modelfunctions.OntologyResolver) chi.Router {
	t.Helper()
	h := modelfunctions.NewHandler(depRepo, fnCreator, resolver)
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
		"/api/vertex/v1/ontologies/"+apiName+"/model-functions/register",
		bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

// TestRegisterModelFunction_Given_ValidPayload_When_POST_Then_201WithDeploymentAndFunction
// covers VTX-050 BDD #1: a valid registration body produces 201 + a
// deployment record + an auto-generated wrapper Function.
func TestRegisterModelFunction_Given_ValidPayload_When_POST_Then_201WithDeploymentAndFunction(t *testing.T) {
	depRepo := &stubDeploymentRepo{}
	fnCreator := &stubFunctionCreator{}
	resolver := &stubResolver{byAPIName: map[string]string{"o1": "ri.ontology.main.ontology.o1"}}
	router := newTestRouter(t, depRepo, fnCreator, resolver)

	body := map[string]interface{}{
		"name":         "flight-delay-predictor",
		"endpointUrl":  "https://models.example.com/flight-delay/predict",
		"modelVersion": "v1.2",
		"inputs": []map[string]interface{}{
			{"name": "distance_km", "type": "double", "required": true},
			{"name": "departure_hour", "type": "integer", "required": true},
		},
		"output":    map[string]interface{}{"type": "double"},
		"createdBy": "operator@example.com",
	}

	rr := doRegister(t, router, "o1", body)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body=%s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Deployment modelfunctions.Deployment `json:"deployment"`
		Function   oms.Function              `json:"function"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rr.Body.String())
	}
	if resp.Deployment.Name != "flight-delay-predictor" {
		t.Errorf("Deployment.Name: got %q", resp.Deployment.Name)
	}
	if resp.Deployment.OntologyRID != "ri.ontology.main.ontology.o1" {
		t.Errorf("Deployment.OntologyRID: got %q", resp.Deployment.OntologyRID)
	}
	if resp.Deployment.RID == "" {
		t.Error("Deployment.RID is empty")
	}
	if resp.Deployment.ModelVersion != "v1.2" {
		t.Errorf("Deployment.ModelVersion: got %q", resp.Deployment.ModelVersion)
	}
	if resp.Function.Name != "flight-delay-predictor" {
		t.Errorf("Function.Name: got %q", resp.Function.Name)
	}
	if resp.Function.Runtime != oms.FunctionRuntimeHTTP {
		t.Errorf("Function.Runtime: got %q", resp.Function.Runtime)
	}
	if resp.Function.SourceCode != "https://models.example.com/flight-delay/predict" {
		t.Errorf("Function.SourceCode: got %q", resp.Function.SourceCode)
	}

	if len(depRepo.created) != 1 {
		t.Fatalf("deployment created: got %d, want 1", len(depRepo.created))
	}
	if len(fnCreator.created) != 1 {
		t.Fatalf("function created: got %d, want 1", len(fnCreator.created))
	}
	parsed, _ := oms.ParseFunctionSignature(fnCreator.created[0].Signature)
	if len(parsed.Params) != 2 || parsed.Returns == nil || parsed.Returns.Type != "double" {
		t.Errorf("persisted signature wrong: params=%+v returns=%+v", parsed.Params, parsed.Returns)
	}
}

// TestRegisterModelFunction_Given_UnknownOntology_When_POST_Then_404
// guards against silently associating a wrapper with a nonexistent
// ontology — the resolver miss must surface as a 404 with the API name.
func TestRegisterModelFunction_Given_UnknownOntology_When_POST_Then_404(t *testing.T) {
	router := newTestRouter(t, &stubDeploymentRepo{}, &stubFunctionCreator{}, &stubResolver{})

	body := map[string]interface{}{
		"name":        "x",
		"endpointUrl": "https://e/x",
		"output":      map[string]interface{}{"type": "double"},
	}
	rr := doRegister(t, router, "missing", body)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "OntologyNotFound") {
		t.Errorf("body should reference OntologyNotFound: %s", rr.Body.String())
	}
}

// TestRegisterModelFunction_Given_MissingName_When_POST_Then_400
// makes sure the handler rejects empty payload fields with a structured
// 400 — same shape funcregistry's register endpoint uses.
func TestRegisterModelFunction_Given_MissingName_When_POST_Then_400(t *testing.T) {
	resolver := &stubResolver{byAPIName: map[string]string{"o1": "ri.ontology.main.ontology.o1"}}
	router := newTestRouter(t, &stubDeploymentRepo{}, &stubFunctionCreator{}, resolver)

	body := map[string]interface{}{
		"endpointUrl": "https://e/x",
		"output":      map[string]interface{}{"type": "double"},
	}
	rr := doRegister(t, router, "o1", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

// TestRegisterModelFunction_Given_MissingEndpoint_When_POST_Then_400
// guards the http-runtime invariant: SourceCode (the delegate URL) must
// be supplied — otherwise the wrapper would 404 the first time it ran.
func TestRegisterModelFunction_Given_MissingEndpoint_When_POST_Then_400(t *testing.T) {
	resolver := &stubResolver{byAPIName: map[string]string{"o1": "ri.ontology.main.ontology.o1"}}
	router := newTestRouter(t, &stubDeploymentRepo{}, &stubFunctionCreator{}, resolver)

	body := map[string]interface{}{
		"name":   "no-endpoint",
		"output": map[string]interface{}{"type": "double"},
	}
	rr := doRegister(t, router, "o1", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

// TestRegisterModelFunction_Given_MalformedJSON_When_POST_Then_400
// covers the wire-shape error path: a malformed body must fail fast
// with 400 InvalidRequestBody, never reach the deployment repo.
func TestRegisterModelFunction_Given_MalformedJSON_When_POST_Then_400(t *testing.T) {
	resolver := &stubResolver{byAPIName: map[string]string{"o1": "ri.ontology.main.ontology.o1"}}
	router := newTestRouter(t, &stubDeploymentRepo{}, &stubFunctionCreator{}, resolver)

	req := httptest.NewRequest(http.MethodPost,
		"/api/vertex/v1/ontologies/o1/model-functions/register",
		strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

// TestRegisterModelFunction_Given_UnsupportedInputType_When_POST_Then_400
// reuses the funcregistry param-type allowlist so the handler refuses
// to create wrappers whose I/O types fall outside the supported scalar
// set.
func TestRegisterModelFunction_Given_UnsupportedInputType_When_POST_Then_400(t *testing.T) {
	resolver := &stubResolver{byAPIName: map[string]string{"o1": "ri.ontology.main.ontology.o1"}}
	router := newTestRouter(t, &stubDeploymentRepo{}, &stubFunctionCreator{}, resolver)

	body := map[string]interface{}{
		"name":        "bad-type",
		"endpointUrl": "https://e/x",
		"inputs":      []map[string]interface{}{{"name": "agg", "type": "aggregation", "required": true}},
		"output":      map[string]interface{}{"type": "double"},
	}
	rr := doRegister(t, router, "o1", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

// TestRegisterModelFunction_Given_DuplicateFunctionName_When_POST_Then_409
// converts the underlying oms.ErrDuplicate (raised by CreateFunction
// when a name+version collides) into a 409 Conflict so SDK callers can
// distinguish "already registered" from "server error".
func TestRegisterModelFunction_Given_DuplicateFunctionName_When_POST_Then_409(t *testing.T) {
	depRepo := &stubDeploymentRepo{}
	fnCreator := &stubFunctionCreator{createErr: oms.ErrDuplicate}
	resolver := &stubResolver{byAPIName: map[string]string{"o1": "ri.ontology.main.ontology.o1"}}
	router := newTestRouter(t, depRepo, fnCreator, resolver)

	body := map[string]interface{}{
		"name":        "dup",
		"endpointUrl": "https://e/dup",
		"output":      map[string]interface{}{"type": "double"},
	}
	rr := doRegister(t, router, "o1", body)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
}

// TestRegisterModelFunction_Given_DeploymentRepoError_When_POST_Then_500
// surfaces an unexpected DeploymentRepo error as a 500 and — crucially
// — does NOT create a wrapper function (we'd otherwise leak an
// orphaned function row referencing a deployment that never persisted).
func TestRegisterModelFunction_Given_DeploymentRepoError_When_POST_Then_500(t *testing.T) {
	depRepo := &stubDeploymentRepo{createErr: errBoom}
	fnCreator := &stubFunctionCreator{}
	resolver := &stubResolver{byAPIName: map[string]string{"o1": "ri.ontology.main.ontology.o1"}}
	router := newTestRouter(t, depRepo, fnCreator, resolver)

	body := map[string]interface{}{
		"name":        "x",
		"endpointUrl": "https://e/x",
		"output":      map[string]interface{}{"type": "double"},
	}
	rr := doRegister(t, router, "o1", body)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", rr.Code)
	}
	if len(fnCreator.created) != 0 {
		t.Errorf("wrapper function should not be created when deployment write fails; got %d", len(fnCreator.created))
	}
}

// errBoom is a sentinel error used by the failure-path stubs above to
// distinguish "infrastructure broke" from the typed sentinels the
// happy path exercises.
var errBoom = stringError("boom")

type stringError string

func (e stringError) Error() string { return string(e) }
