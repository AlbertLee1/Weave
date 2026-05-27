package oms_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/oms"
)

// US-214 Interface Method Signatures: CRUD + invoke handler tests. The
// narrow oms.InterfaceMethodStore lets us drive every code path without
// touching the full Repository surface.

// --- in-memory InterfaceMethodStore ---

type memInterfaceMethodStore struct {
	mu    sync.Mutex
	byRID map[string]*oms.InterfaceMethod
}

func newMemInterfaceMethodStore() *memInterfaceMethodStore {
	return &memInterfaceMethodStore{byRID: map[string]*oms.InterfaceMethod{}}
}

func (s *memInterfaceMethodStore) CreateInterfaceMethod(_ context.Context, im *oms.InterfaceMethod) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := im.Validate(); err != nil {
		return err
	}
	for _, existing := range s.byRID {
		if existing.InterfaceRID == im.InterfaceRID && existing.Name == im.Name {
			return oms.ErrDuplicate
		}
	}
	cp := *im
	s.byRID[im.RID] = &cp
	return nil
}

func (s *memInterfaceMethodStore) GetInterfaceMethod(_ context.Context, rid string) (*oms.InterfaceMethod, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	im, ok := s.byRID[rid]
	if !ok {
		return nil, oms.ErrNotFound
	}
	cp := *im
	return &cp, nil
}

func (s *memInterfaceMethodStore) ListInterfaceMethods(_ context.Context, interfaceRID string) ([]oms.InterfaceMethod, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []oms.InterfaceMethod
	for _, im := range s.byRID {
		if im.InterfaceRID == interfaceRID {
			out = append(out, *im)
		}
	}
	return out, nil
}

func (s *memInterfaceMethodStore) UpdateInterfaceMethod(_ context.Context, im *oms.InterfaceMethod) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byRID[im.RID]; !ok {
		return oms.ErrNotFound
	}
	cp := *im
	s.byRID[im.RID] = &cp
	return nil
}

func (s *memInterfaceMethodStore) DeleteInterfaceMethod(_ context.Context, rid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byRID[rid]; !ok {
		return oms.ErrNotFound
	}
	delete(s.byRID, rid)
	return nil
}

// --- helpers ---

func newInterfaceMethodRouter(handler *oms.OMSHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/interfaces/{interfaceRid}/methods", handler.CreateInterfaceMethod)
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfaces/{interfaceRid}/methods", handler.ListInterfaceMethods)
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfaces/methods/byRid/{methodRid}", handler.GetInterfaceMethod)
	r.Put("/api/v2/ontologies/{ontologyApiName}/interfaces/methods/byRid/{methodRid}", handler.UpdateInterfaceMethod)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/interfaces/methods/byRid/{methodRid}", handler.DeleteInterfaceMethod)
	r.Post("/api/v2/ontologies/{ontologyApiName}/interfaces/methods/{methodRid}/invoke", handler.InvokeInterfaceMethod)
	r.Post("/api/v2/ontologies/{ontologyApiName}/actionTypes", handler.CreateActionType)
	r.Put("/api/v2/ontologies/{ontologyApiName}/actionTypes/byRid/{actionTypeRid}", handler.UpdateActionType)
	return r
}

func seedInterfaceFixture(t *testing.T, repo *mockRepo) (ontRID, ifaceRID, objectTypeRID string) {
	t.Helper()
	ontRID = seedMockOntology(repo)
	ifaceRID = "ri.ontology.main.interface.1"
	repo.interfaces = append(repo.interfaces, oms.Interface{
		RID:         ifaceRID,
		OntologyRID: ontRID,
		APIName:     "greeter",
		DisplayName: "Greeter",
	})
	objectTypeRID = "ri.ontology.main.object-type.person"
	repo.objectTypes = append(repo.objectTypes, oms.ObjectType{
		RID:         objectTypeRID,
		OntologyRID: ontRID,
		APIName:     "person",
		DisplayName: "Person",
		PrimaryKey:  "id",
		Status:      "ACTIVE",
	})
	repo.interfaceAttachments = append(repo.interfaceAttachments, oms.ObjectTypeInterface{
		ObjectTypeRID: objectTypeRID,
		InterfaceRID:  ifaceRID,
	})
	return
}

func wireInterfaceMethodHandler(repo *mockRepo) (*oms.OMSHandler, *memInterfaceMethodStore) {
	h := oms.NewOMSHandler(repo)
	store := newMemInterfaceMethodStore()
	h.SetInterfaceMethodStore(store)
	return h, store
}

// --- CRUD ---

func TestCreateInterfaceMethod_Success(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ifaceRID, _ := seedInterfaceFixture(t, repo)
	h, store := wireInterfaceMethodHandler(repo)
	r := newInterfaceMethodRouter(h)

	body := map[string]interface{}{
		"name": "greet",
		"params": []map[string]interface{}{
			{"name": "greeting", "type": "string", "required": true},
		},
		"returns": map[string]interface{}{"type": "string"},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontRID+"/interfaces/"+ifaceRID+"/methods",
		strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if resp["name"] != "greet" {
		t.Errorf("unexpected response: %v", resp)
	}
	methodRID, _ := resp["rid"].(string)
	if methodRID == "" {
		t.Fatalf("expected non-empty rid")
	}
	if _, err := store.GetInterfaceMethod(context.Background(), methodRID); err != nil {
		t.Fatalf("row not persisted: %v", err)
	}
}

func TestCreateInterfaceMethod_DuplicateName(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ifaceRID, _ := seedInterfaceFixture(t, repo)
	h, _ := wireInterfaceMethodHandler(repo)
	r := newInterfaceMethodRouter(h)

	body := map[string]interface{}{
		"name":    "greet",
		"returns": map[string]interface{}{"type": "string"},
	}
	b, _ := json.Marshal(body)
	mk := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/"+ontRID+"/interfaces/"+ifaceRID+"/methods",
			strings.NewReader(string(b)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	if got := mk().Code; got != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d", got)
	}
	w := mk()
	if w.Code != http.StatusConflict {
		t.Fatalf("second create: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateInterfaceMethod_UnknownInterface(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	h, _ := wireInterfaceMethodHandler(repo)
	r := newInterfaceMethodRouter(h)

	body, _ := json.Marshal(map[string]interface{}{
		"name":    "greet",
		"returns": map[string]interface{}{"type": "string"},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontRID+"/interfaces/ri.missing/methods",
		strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateInterfaceMethod_ParamMissingName(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ifaceRID, _ := seedInterfaceFixture(t, repo)
	h, _ := wireInterfaceMethodHandler(repo)
	r := newInterfaceMethodRouter(h)

	body, _ := json.Marshal(map[string]interface{}{
		"name": "greet",
		"params": []map[string]interface{}{
			{"type": "string"},
		},
		"returns": map[string]interface{}{"type": "string"},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontRID+"/interfaces/"+ifaceRID+"/methods",
		strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListInterfaceMethods_Empty(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ifaceRID, _ := seedInterfaceFixture(t, repo)
	h, _ := wireInterfaceMethodHandler(repo)
	r := newInterfaceMethodRouter(h)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+ontRID+"/interfaces/"+ifaceRID+"/methods", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := parseJSON(t, w.Body.Bytes())
	data, _ := resp["data"].([]interface{})
	if len(data) != 0 {
		t.Fatalf("expected empty data, got %v", data)
	}
}

func TestUpdateInterfaceMethod_ReplacesFields(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ifaceRID, _ := seedInterfaceFixture(t, repo)
	h, store := wireInterfaceMethodHandler(repo)
	r := newInterfaceMethodRouter(h)

	// Seed.
	body, _ := json.Marshal(map[string]interface{}{
		"name":    "greet",
		"returns": map[string]interface{}{"type": "string"},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontRID+"/interfaces/"+ifaceRID+"/methods",
		strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed: %d %s", w.Code, w.Body.String())
	}
	created := parseJSON(t, w.Body.Bytes())
	methodRID, _ := created["rid"].(string)

	// Update — replace returns type, add description.
	updBody, _ := json.Marshal(map[string]interface{}{
		"name":        "greet",
		"params":      []map[string]interface{}{{"name": "n", "type": "integer"}},
		"returns":     map[string]interface{}{"type": "integer"},
		"description": "counted greeting",
	})
	req = httptest.NewRequest(http.MethodPut,
		"/api/v2/ontologies/"+ontRID+"/interfaces/methods/byRid/"+methodRID,
		strings.NewReader(string(updBody)))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	got, err := store.GetInterfaceMethod(context.Background(), methodRID)
	if err != nil {
		t.Fatalf("store get: %v", err)
	}
	if got.Returns.Type != "integer" || got.Description != "counted greeting" {
		t.Fatalf("update did not take effect: %+v", got)
	}
}

func TestDeleteInterfaceMethod_Success(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ifaceRID, _ := seedInterfaceFixture(t, repo)
	h, store := wireInterfaceMethodHandler(repo)
	r := newInterfaceMethodRouter(h)

	body, _ := json.Marshal(map[string]interface{}{
		"name":    "greet",
		"returns": map[string]interface{}{"type": "string"},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontRID+"/interfaces/"+ifaceRID+"/methods",
		strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	methodRID, _ := parseJSON(t, w.Body.Bytes())["rid"].(string)

	req = httptest.NewRequest(http.MethodDelete,
		"/api/v2/ontologies/"+ontRID+"/interfaces/methods/byRid/"+methodRID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if _, err := store.GetInterfaceMethod(context.Background(), methodRID); !errors.Is(err, oms.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

// --- ActionType implementsMethodRid support ---

func seedInterfaceMethod(t *testing.T, repo *mockRepo, store *memInterfaceMethodStore, ifaceRID, name string) string {
	t.Helper()
	im := &oms.InterfaceMethod{
		RID:          "ri.ontology.main.interface-method." + name,
		InterfaceRID: ifaceRID,
		Name:         name,
		Returns:      oms.InterfaceMethodReturns{Type: "string"},
	}
	if err := store.CreateInterfaceMethod(context.Background(), im); err != nil {
		t.Fatalf("seed method: %v", err)
	}
	return im.RID
}

func TestCreateActionType_WithImplementsMethodRid_Success(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ifaceRID, _ := seedInterfaceFixture(t, repo)
	h, store := wireInterfaceMethodHandler(repo)
	methodRID := seedInterfaceMethod(t, repo, store, ifaceRID, "greet")
	r := newInterfaceMethodRouter(h)

	body, _ := json.Marshal(map[string]interface{}{
		"apiName":             "greetPerson",
		"displayName":         "Greet Person",
		"status":              "ACTIVE",
		"rules":               []map[string]interface{}{{"type": "modifyObject", "objectType": "person"}},
		"implementsMethodRid": methodRID,
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontRID+"/actionTypes",
		strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if resp["implementsMethodRid"] != methodRID {
		t.Fatalf("response missing implementsMethodRid: %v", resp)
	}
	if len(repo.actionTypes) != 1 || repo.actionTypes[0].ImplementsMethodRID != methodRID {
		t.Fatalf("repo did not persist implementsMethodRid: %+v", repo.actionTypes)
	}
}

func TestCreateActionType_WithImplementsMethodRid_NotFound(t *testing.T) {
	repo := &mockRepo{}
	ontRID, _, _ := seedInterfaceFixture(t, repo)
	h, _ := wireInterfaceMethodHandler(repo)
	r := newInterfaceMethodRouter(h)

	body, _ := json.Marshal(map[string]interface{}{
		"apiName":             "greetPerson",
		"displayName":         "Greet Person",
		"status":              "ACTIVE",
		"rules":               []map[string]interface{}{{"type": "modifyObject", "objectType": "person"}},
		"implementsMethodRid": "ri.ontology.main.interface-method.missing",
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontRID+"/actionTypes",
		strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if len(repo.actionTypes) != 0 {
		t.Fatalf("action type should not have been persisted: %+v", repo.actionTypes)
	}
}

func TestUpdateActionType_ClearsImplementsMethodRid(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ifaceRID, _ := seedInterfaceFixture(t, repo)
	h, store := wireInterfaceMethodHandler(repo)
	methodRID := seedInterfaceMethod(t, repo, store, ifaceRID, "greet")
	r := newInterfaceMethodRouter(h)

	// Seed the ActionType with the binding.
	body, _ := json.Marshal(map[string]interface{}{
		"apiName":             "greetPerson",
		"displayName":         "Greet Person",
		"status":              "ACTIVE",
		"rules":               []map[string]interface{}{{"type": "modifyObject", "objectType": "person"}},
		"implementsMethodRid": methodRID,
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontRID+"/actionTypes",
		strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed action: %d %s", w.Code, w.Body.String())
	}
	actionRID, _ := parseJSON(t, w.Body.Bytes())["rid"].(string)

	// Update — clear the binding by sending "" (pointer semantics).
	upd, _ := json.Marshal(map[string]interface{}{
		"displayName":         "Greet Person",
		"status":              "ACTIVE",
		"implementsMethodRid": "",
	})
	req = httptest.NewRequest(http.MethodPut,
		"/api/v2/ontologies/"+ontRID+"/actionTypes/byRid/"+actionRID,
		strings.NewReader(string(upd)))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	if repo.actionTypes[0].ImplementsMethodRID != "" {
		t.Fatalf("expected binding cleared, got %q", repo.actionTypes[0].ImplementsMethodRID)
	}
}

// --- Polymorphic invoke ---

// capturingDispatcher records the single most recent invocation so tests
// can assert the dispatcher received the resolved ActionType.
type capturingDispatcher struct {
	ontologyRID string
	actionName  string
	parameters  map[string]interface{}
	result      json.RawMessage
	err         error
}

func (c *capturingDispatcher) Dispatch(_ context.Context, ontologyRID, actionAPIName string, parameters map[string]interface{}) (json.RawMessage, error) {
	c.ontologyRID = ontologyRID
	c.actionName = actionAPIName
	c.parameters = parameters
	if c.err != nil {
		return nil, c.err
	}
	if c.result == nil {
		return json.RawMessage(`{"ok":true}`), nil
	}
	return c.result, nil
}

func TestInvokeInterfaceMethod_DispatchesToImplementingAction(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ifaceRID, _ := seedInterfaceFixture(t, repo)
	h, store := wireInterfaceMethodHandler(repo)
	methodRID := seedInterfaceMethod(t, repo, store, ifaceRID, "greet")

	repo.actionTypes = append(repo.actionTypes, oms.ActionType{
		RID:                 "ri.ontology.main.action-type.greetPerson",
		OntologyRID:         ontRID,
		APIName:             "greetPerson",
		DisplayName:         "Greet Person",
		Status:              "ACTIVE",
		Rules:               json.RawMessage(`[{"type":"modifyObject","objectType":"person"}]`),
		ImplementsMethodRID: methodRID,
	})

	dispatcher := &capturingDispatcher{}
	h.SetInterfaceMethodDispatcher(dispatcher)
	r := newInterfaceMethodRouter(h)

	body, _ := json.Marshal(map[string]interface{}{
		"objectType": "person",
		"parameters": map[string]interface{}{"greeting": "hi"},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontRID+"/interfaces/methods/"+methodRID+"/invoke",
		strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if dispatcher.actionName != "greetPerson" {
		t.Fatalf("dispatcher not invoked with resolved action: got %q", dispatcher.actionName)
	}
	if dispatcher.parameters["greeting"] != "hi" {
		t.Fatalf("parameters not forwarded: %v", dispatcher.parameters)
	}
	resp := parseJSON(t, w.Body.Bytes())
	if resp["actionTypeApiName"] != "greetPerson" {
		t.Fatalf("response missing resolved actionTypeApiName: %v", resp)
	}
	if resp["objectType"] != "person" {
		t.Fatalf("response objectType wrong: %v", resp)
	}
}

func TestInvokeInterfaceMethod_NoImplementation(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ifaceRID, _ := seedInterfaceFixture(t, repo)
	h, store := wireInterfaceMethodHandler(repo)
	methodRID := seedInterfaceMethod(t, repo, store, ifaceRID, "greet")
	r := newInterfaceMethodRouter(h)

	body, _ := json.Marshal(map[string]interface{}{"objectType": "person"})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontRID+"/interfaces/methods/"+methodRID+"/invoke",
		strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when no implementation, got %d: %s", w.Code, w.Body.String())
	}
}

func TestInvokeInterfaceMethod_ObjectTypeDoesNotImplementInterface(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ifaceRID, _ := seedInterfaceFixture(t, repo)
	h, store := wireInterfaceMethodHandler(repo)
	methodRID := seedInterfaceMethod(t, repo, store, ifaceRID, "greet")

	// Pre-seed an ObjectType "robot" that does NOT implement `greeter`.
	repo.objectTypes = append(repo.objectTypes, oms.ObjectType{
		RID:         "ri.ontology.main.object-type.robot",
		OntologyRID: ontRID,
		APIName:     "robot",
		DisplayName: "Robot",
		PrimaryKey:  "id",
		Status:      "ACTIVE",
	})
	repo.actionTypes = append(repo.actionTypes, oms.ActionType{
		RID:                 "ri.ontology.main.action-type.greetRobot",
		OntologyRID:         ontRID,
		APIName:             "greetRobot",
		Status:              "ACTIVE",
		Rules:               json.RawMessage(`[{"type":"modifyObject","objectType":"robot"}]`),
		ImplementsMethodRID: methodRID,
	})
	r := newInterfaceMethodRouter(h)

	body, _ := json.Marshal(map[string]interface{}{"objectType": "robot"})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontRID+"/interfaces/methods/"+methodRID+"/invoke",
		strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (ObjectType does not implement Interface), got %d: %s", w.Code, w.Body.String())
	}
}

func TestInvokeInterfaceMethod_MissingMethod(t *testing.T) {
	repo := &mockRepo{}
	ontRID, _, _ := seedInterfaceFixture(t, repo)
	h, _ := wireInterfaceMethodHandler(repo)
	r := newInterfaceMethodRouter(h)

	body, _ := json.Marshal(map[string]interface{}{"objectType": "person"})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontRID+"/interfaces/methods/ri.missing/invoke",
		strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestInvokeInterfaceMethod_ResolvesWithoutDispatcher(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ifaceRID, _ := seedInterfaceFixture(t, repo)
	h, store := wireInterfaceMethodHandler(repo)
	methodRID := seedInterfaceMethod(t, repo, store, ifaceRID, "greet")

	repo.actionTypes = append(repo.actionTypes, oms.ActionType{
		RID:                 "ri.ontology.main.action-type.greetPerson",
		OntologyRID:         ontRID,
		APIName:             "greetPerson",
		Status:              "ACTIVE",
		Rules:               json.RawMessage(`[{"type":"modifyObject","objectType":"person"}]`),
		ImplementsMethodRID: methodRID,
	})
	r := newInterfaceMethodRouter(h)

	// No dispatcher wired — the handler should still resolve and 200 with
	// the dispatch decision so clients can confirm routing.
	body, _ := json.Marshal(map[string]interface{}{"objectType": "person"})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontRID+"/interfaces/methods/"+methodRID+"/invoke",
		strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if resp["actionTypeApiName"] != "greetPerson" {
		t.Fatalf("unexpected response: %v", resp)
	}
	if _, hasResult := resp["result"]; hasResult {
		t.Fatalf("no dispatcher wired — should not have a result field: %v", resp)
	}
}
