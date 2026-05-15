package funcregistry_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/vertex/funcregistry"
)

// stubLookup implements funcregistry.FunctionLookup over an in-memory
// slice. It exercises the GetFunction / GetFunctionByName /
// ListFunctionVersionsByName / CreateFunction surface the handler needs
// without standing up a full Repository.
type stubLookup struct {
	functions []oms.Function
	err       error
	created   []oms.Function
	createErr error
}

func (s *stubLookup) GetFunction(_ context.Context, rid string) (*oms.Function, error) {
	if s.err != nil {
		return nil, s.err
	}
	for i := range s.functions {
		if s.functions[i].RID == rid {
			cp := s.functions[i]
			return &cp, nil
		}
	}
	return nil, oms.ErrNotFound
}

func (s *stubLookup) GetFunctionByName(_ context.Context, ontologyRID, name string) (*oms.Function, error) {
	if s.err != nil {
		return nil, s.err
	}
	matches := make([]oms.Function, 0)
	for _, fn := range s.functions {
		if fn.OntologyRID == ontologyRID && fn.Name == name {
			matches = append(matches, fn)
		}
	}
	if len(matches) == 0 {
		return nil, oms.ErrNotFound
	}
	oms.SortFunctionsByVersionDesc(matches)
	cp := matches[0]
	return &cp, nil
}

func (s *stubLookup) ListFunctionVersionsByName(_ context.Context, ontologyRID, name string) ([]oms.Function, error) {
	if s.err != nil {
		return nil, s.err
	}
	matches := make([]oms.Function, 0)
	for _, fn := range s.functions {
		if fn.OntologyRID == ontologyRID && fn.Name == name {
			matches = append(matches, fn)
		}
	}
	if len(matches) == 0 {
		return nil, nil
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].Version > matches[j].Version })
	return matches, nil
}

func (s *stubLookup) CreateFunction(_ context.Context, fn *oms.Function) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.created = append(s.created, *fn)
	s.functions = append(s.functions, *fn)
	return nil
}

// stubResolver maps ontologyApiName → RID for the handler. The body has
// a single ontology so the route exercises both the resolve-success and
// resolve-miss paths.
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

func newTestRouter(t *testing.T, lookup funcregistry.FunctionLookup, resolver funcregistry.OntologyResolver) chi.Router {
	t.Helper()
	h := funcregistry.NewHandler(lookup, resolver)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

// TestGetFunctionByRID_Given_RegisteredFunction_When_GETByRID_Then_200WithMetadata
// covers BDD #1: GET /api/vertex/v1/functions/{rid} returns metadata +
// I/O signature.
func TestGetFunctionByRID_Given_RegisteredFunction_When_GETByRID_Then_200WithMetadata(t *testing.T) {
	sig := `{"params":[{"name":"x","type":"integer","required":true}],"returns":{"type":"double"}}`
	lookup := &stubLookup{functions: []oms.Function{{
		RID:         "ri.functions.main.function.predict",
		OntologyRID: "ri.ontology.main.ontology.o1",
		Name:        "predict", Version: "1.0.0",
		SourceCode: "@function def predict(x): return x*1.0",
		Signature:  json.RawMessage(sig),
	}}}
	r := newTestRouter(t, lookup, &stubResolver{})

	req := httptest.NewRequest(http.MethodGet, "/api/vertex/v1/functions/ri.functions.main.function.predict", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		RID       string          `json:"rid"`
		Name      string          `json:"name"`
		Version   string          `json:"version"`
		Signature json.RawMessage `json:"signature"`
		Params    []struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			Required bool   `json:"required"`
		} `json:"params"`
		Returns *struct {
			Type string `json:"type"`
		} `json:"returns"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body: %s", err, w.Body.String())
	}
	if resp.RID != "ri.functions.main.function.predict" {
		t.Errorf("rid = %q, want predict", resp.RID)
	}
	if resp.Name != "predict" || resp.Version != "1.0.0" {
		t.Errorf("name/version mismatch: %q/%q", resp.Name, resp.Version)
	}
	if len(resp.Params) != 1 || resp.Params[0].Name != "x" || resp.Params[0].Type != "integer" {
		t.Errorf("params not surfaced: %+v", resp.Params)
	}
	if resp.Returns == nil || resp.Returns.Type != "double" {
		t.Errorf("returns not surfaced: %+v", resp.Returns)
	}
}

func TestGetFunctionByRID_Given_UnknownRID_When_GETByRID_Then_404(t *testing.T) {
	lookup := &stubLookup{}
	r := newTestRouter(t, lookup, &stubResolver{})

	req := httptest.NewRequest(http.MethodGet, "/api/vertex/v1/functions/ri.functions.main.function.missing", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestGetFunctionByRID_Given_RepoFailure_When_GETByRID_Then_500(t *testing.T) {
	lookup := &stubLookup{err: errors.New("db down")}
	r := newTestRouter(t, lookup, &stubResolver{})

	req := httptest.NewRequest(http.MethodGet, "/api/vertex/v1/functions/anything", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

// TestResolveByRange_Given_ThreeVersions_When_CaretRange_Then_ReturnsLatest1X
// covers BDD #3: range=^1.0.0 → default to the latest 1.x version.
func TestResolveByRange_Given_ThreeVersions_When_CaretRange_Then_ReturnsLatest1X(t *testing.T) {
	lookup := &stubLookup{functions: []oms.Function{
		{RID: "f1", OntologyRID: "ri.ontology.main.ontology.o1", Name: "predict", Version: "1.0.0"},
		{RID: "f2", OntologyRID: "ri.ontology.main.ontology.o1", Name: "predict", Version: "1.5.0"},
		{RID: "f3", OntologyRID: "ri.ontology.main.ontology.o1", Name: "predict", Version: "2.0.0"},
	}}
	resolver := &stubResolver{byAPIName: map[string]string{"northwind": "ri.ontology.main.ontology.o1"}}
	r := newTestRouter(t, lookup, resolver)

	req := httptest.NewRequest(http.MethodGet,
		"/api/vertex/v1/ontologies/northwind/functions/predict/resolve?range=%5E1.0.0", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var fn oms.Function
	if err := json.Unmarshal(w.Body.Bytes(), &fn); err != nil {
		t.Fatalf("decode: %v; body: %s", err, w.Body.String())
	}
	if fn.Version != "1.5.0" {
		t.Errorf("resolved version = %q, want 1.5.0 (latest 1.x)", fn.Version)
	}
	if fn.RID != "f2" {
		t.Errorf("resolved RID = %q, want f2", fn.RID)
	}
}

func TestResolveByRange_Given_NoRangeParam_When_Resolve_Then_LatestSemver(t *testing.T) {
	lookup := &stubLookup{functions: []oms.Function{
		{RID: "f1", OntologyRID: "ri.ontology.main.ontology.o1", Name: "predict", Version: "1.0.0"},
		{RID: "f2", OntologyRID: "ri.ontology.main.ontology.o1", Name: "predict", Version: "10.0.0"},
		{RID: "f3", OntologyRID: "ri.ontology.main.ontology.o1", Name: "predict", Version: "2.0.0"},
	}}
	resolver := &stubResolver{byAPIName: map[string]string{"northwind": "ri.ontology.main.ontology.o1"}}
	r := newTestRouter(t, lookup, resolver)

	req := httptest.NewRequest(http.MethodGet, "/api/vertex/v1/ontologies/northwind/functions/predict/resolve", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var fn oms.Function
	_ = json.Unmarshal(w.Body.Bytes(), &fn)
	if fn.Version != "10.0.0" {
		t.Errorf("default resolve should return latest semver, got %q", fn.Version)
	}
}

func TestResolveByRange_Given_NoMatch_When_Resolve_Then_404(t *testing.T) {
	lookup := &stubLookup{functions: []oms.Function{
		{RID: "f1", OntologyRID: "ri.ontology.main.ontology.o1", Name: "predict", Version: "2.0.0"},
	}}
	resolver := &stubResolver{byAPIName: map[string]string{"northwind": "ri.ontology.main.ontology.o1"}}
	r := newTestRouter(t, lookup, resolver)

	req := httptest.NewRequest(http.MethodGet,
		"/api/vertex/v1/ontologies/northwind/functions/predict/resolve?range=%5E1.0.0", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestResolveByRange_Given_UnknownOntology_When_Resolve_Then_404(t *testing.T) {
	lookup := &stubLookup{}
	resolver := &stubResolver{byAPIName: map[string]string{}}
	r := newTestRouter(t, lookup, resolver)

	req := httptest.NewRequest(http.MethodGet, "/api/vertex/v1/ontologies/missing/functions/predict/resolve", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestResolveByRange_Given_InvalidRange_When_Resolve_Then_400(t *testing.T) {
	lookup := &stubLookup{functions: []oms.Function{
		{RID: "f1", OntologyRID: "ri.ontology.main.ontology.o1", Name: "predict", Version: "1.0.0"},
	}}
	resolver := &stubResolver{byAPIName: map[string]string{"northwind": "ri.ontology.main.ontology.o1"}}
	r := newTestRouter(t, lookup, resolver)

	req := httptest.NewRequest(http.MethodGet,
		"/api/vertex/v1/ontologies/northwind/functions/predict/resolve?range=nonsense", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

// TestRegisterFunction_Given_AllowedTypes_When_POSTRegister_Then_201
// covers BDD #2 happy path: registering a function whose params/return use
// only primitive scalars / Collection succeeds.
func TestRegisterFunction_Given_AllowedTypes_When_POSTRegister_Then_201(t *testing.T) {
	lookup := &stubLookup{}
	resolver := &stubResolver{byAPIName: map[string]string{"northwind": "ri.ontology.main.ontology.o1"}}
	r := newTestRouter(t, lookup, resolver)

	body := `{
		"name": "predict",
		"sourceCode": "@function def predict(x): return x*1.0",
		"signature": {"params":[{"name":"x","type":"integer","required":true}],"returns":{"type":"double"}}
	}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/vertex/v1/ontologies/northwind/functions/register",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	if len(lookup.created) != 1 {
		t.Fatalf("expected 1 created row, got %d", len(lookup.created))
	}
	if lookup.created[0].Name != "predict" {
		t.Errorf("created.Name = %q, want predict", lookup.created[0].Name)
	}
}

// TestRegisterFunction_Given_AggregationType_When_POSTRegister_Then_400WithReason
// covers BDD #2: registration with Aggregation type is rejected with a
// clear reason in the response body.
func TestRegisterFunction_Given_AggregationType_When_POSTRegister_Then_400WithReason(t *testing.T) {
	lookup := &stubLookup{}
	resolver := &stubResolver{byAPIName: map[string]string{"northwind": "ri.ontology.main.ontology.o1"}}
	r := newTestRouter(t, lookup, resolver)

	body := `{
		"name": "bad",
		"sourceCode": "x = 1",
		"signature": {"params":[{"name":"agg","type":"aggregation","required":true}]}
	}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/vertex/v1/ontologies/northwind/functions/register",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	if len(lookup.created) != 0 {
		t.Errorf("expected NO created rows on rejection, got %d", len(lookup.created))
	}
	bodyStr := w.Body.String()
	for _, want := range []string{"aggregation", "agg"} {
		if !strings.Contains(bodyStr, want) {
			t.Errorf("response body missing %q; body=%s", want, bodyStr)
		}
	}
}

func TestRegisterFunction_Given_NotificationReturn_When_POSTRegister_Then_400(t *testing.T) {
	lookup := &stubLookup{}
	resolver := &stubResolver{byAPIName: map[string]string{"northwind": "ri.ontology.main.ontology.o1"}}
	r := newTestRouter(t, lookup, resolver)

	body := `{
		"name": "bad",
		"sourceCode": "x = 1",
		"signature": {"params":[{"name":"a","type":"integer"}],"returns":{"type":"notification"}}
	}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/vertex/v1/ontologies/northwind/functions/register",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "notification") {
		t.Errorf("body missing 'notification'; body=%s", w.Body.String())
	}
}

func TestRegisterFunction_Given_UnknownOntology_When_POSTRegister_Then_404(t *testing.T) {
	lookup := &stubLookup{}
	resolver := &stubResolver{byAPIName: map[string]string{}}
	r := newTestRouter(t, lookup, resolver)

	body := `{"name":"predict","sourceCode":"x=1"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/vertex/v1/ontologies/missing/functions/register",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestRegisterFunction_Given_DuplicateRow_When_POSTRegister_Then_409(t *testing.T) {
	lookup := &stubLookup{createErr: oms.ErrDuplicate}
	resolver := &stubResolver{byAPIName: map[string]string{"northwind": "ri.ontology.main.ontology.o1"}}
	r := newTestRouter(t, lookup, resolver)

	body := `{"name":"predict","sourceCode":"x=1","version":"1.0.0"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/vertex/v1/ontologies/northwind/functions/register",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body: %s", w.Code, w.Body.String())
	}
}

func TestRegisterFunction_Given_MissingFields_When_POSTRegister_Then_400(t *testing.T) {
	lookup := &stubLookup{}
	resolver := &stubResolver{byAPIName: map[string]string{"northwind": "ri.ontology.main.ontology.o1"}}
	r := newTestRouter(t, lookup, resolver)

	for _, body := range []string{
		`{"sourceCode":"x=1"}`, // no name
		`{"name":"x"}`,         // no sourceCode
	} {
		req := httptest.NewRequest(http.MethodPost,
			"/api/vertex/v1/ontologies/northwind/functions/register",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("body=%q status=%d, want 400", body, w.Code)
		}
	}
}
