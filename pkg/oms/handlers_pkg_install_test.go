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

// stubInstalledPackageStore is an in-memory oms.InstalledPackageStore used
// to assert the install handler's recording side-effect.
type stubInstalledPackageStore struct {
	mu   sync.Mutex
	rows map[string]*oms.InstalledPackage
}

func newStubInstalledPackageStore() *stubInstalledPackageStore {
	return &stubInstalledPackageStore{rows: make(map[string]*oms.InstalledPackage)}
}

func (s *stubInstalledPackageStore) UpsertInstalledPackage(_ context.Context, pkg *oms.InstalledPackage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *pkg
	s.rows[pkg.Name] = &cp
	pkg.ID = int64(len(s.rows))
	return nil
}

func (s *stubInstalledPackageStore) GetInstalledPackage(_ context.Context, name string) (*oms.InstalledPackage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.rows[name]; ok {
		cp := *r
		return &cp, nil
	}
	return nil, oms.ErrInstalledPackageNotFound
}

func (s *stubInstalledPackageStore) ListInstalledPackages(_ context.Context) ([]oms.InstalledPackage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]oms.InstalledPackage, 0, len(s.rows))
	for _, r := range s.rows {
		out = append(out, *r)
	}
	return out, nil
}

func (s *stubInstalledPackageStore) SetInstalledPackageEnabled(_ context.Context, name string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[name]
	if !ok {
		return oms.ErrInstalledPackageNotFound
	}
	r.Enabled = enabled
	return nil
}

func (s *stubInstalledPackageStore) DeleteInstalledPackage(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[name]; !ok {
		return oms.ErrInstalledPackageNotFound
	}
	delete(s.rows, name)
	return nil
}

// stubMigrationRunner records every RunPackageMigrations invocation so the
// test can assert it fired (or didn't) without doing real disk + PG work.
type stubMigrationRunner struct {
	calls []migrationCall
	err   error
}

type migrationCall struct {
	pkg   string
	files []oms.PackageMigrationEntry
}

func (s *stubMigrationRunner) RunPackageMigrations(_ context.Context, name string, files []oms.PackageMigrationEntry) (int, error) {
	s.calls = append(s.calls, migrationCall{pkg: name, files: files})
	if s.err != nil {
		return 0, s.err
	}
	return len(files), nil
}

func newPackageInstallRouter(handler *oms.OMSHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Post("/api/v2/pkg/install", handler.PackageInstall)
	r.Get("/api/v2/pkg", handler.ListInstalledPackages)
	r.Get("/api/v2/pkg/{name}", handler.GetInstalledPackage)
	r.Post("/api/v2/pkg/{name}/enabled", handler.SetInstalledPackageEnabled)
	r.Delete("/api/v2/pkg/{name}", handler.DeleteInstalledPackage)
	return r
}

const pkgInstallSampleOntology = `{
	"ontology": {"apiName": "northwind", "displayName": "Northwind"},
	"objectTypes": [{"apiName": "Customer", "displayName": "Customer", "primaryKey": "id", "status": "ACTIVE", "visibility": "NORMAL"}],
	"linkTypes": [],
	"actionTypes": [{"apiName": "createCustomer", "displayName": "Create Customer", "status": "ACTIVE"}],
	"interfaces": [],
	"sharedProperties": [],
	"valueTypes": [],
	"typeGroups": [],
	"functions": [{"name": "compute", "version": "1.0.0", "sourceCode": "// fn"}],
	"queryTypes": []
}`

func TestPackageInstall_NewOntology_Success(t *testing.T) {
	repo := &mockRepo{}
	store := newStubInstalledPackageStore()
	migrator := &stubMigrationRunner{}
	handler := oms.NewOMSHandler(repo)
	handler.SetInstalledPackageStore(store)
	handler.SetPackageMigrationRunner(migrator)
	router := newPackageInstallRouter(handler)

	body := map[string]any{
		"manifest": map[string]any{
			"name":            "northwind",
			"version":         "1.0.0",
			"minWeaveVersion": "0.42.0",
		},
		"ontology": json.RawMessage(pkgInstallSampleOntology),
		"migrations": []map[string]any{
			{"filename": "000001_init.up.sql", "content": []byte("SELECT 1;")},
		},
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v2/pkg/install", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["name"] != "northwind" || resp["version"] != "1.0.0" {
		t.Fatalf("response identity drift: %+v", resp)
	}
	if resp["migrationsTotal"].(float64) != 1 || resp["migrationsRan"].(float64) != 1 {
		t.Fatalf("migrations counts drift: %+v", resp)
	}

	if len(migrator.calls) != 1 || migrator.calls[0].pkg != "northwind" {
		t.Fatalf("migration runner not invoked: %+v", migrator.calls)
	}
	if len(store.rows) != 1 {
		t.Fatalf("install not recorded: %+v", store.rows)
	}
	row := store.rows["northwind"]
	if row.Version != "1.0.0" || !row.Enabled {
		t.Fatalf("row drift: %+v", row)
	}

	// Ontology + ObjectType created.
	if len(repo.ontologies) != 1 || repo.ontologies[0].APIName != "northwind" {
		t.Fatalf("ontology not created: %+v", repo.ontologies)
	}
	if len(repo.objectTypes) != 1 || repo.objectTypes[0].APIName != "Customer" {
		t.Fatalf("objectType not created: %+v", repo.objectTypes)
	}
}

func TestPackageInstall_RejectsMissingManifestName(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	router := newPackageInstallRouter(handler)

	body := `{"manifest": {"version": "1.0.0"}, "ontology": ` + pkgInstallSampleOntology + `}`
	req := httptest.NewRequest("POST", "/api/v2/pkg/install", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "manifest.name") {
		t.Fatalf("error should mention manifest.name: %s", w.Body.String())
	}
}

func TestPackageInstall_RejectsMissingOntology(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	router := newPackageInstallRouter(handler)

	body := `{"manifest": {"name": "x", "version": "1.0.0"}}`
	req := httptest.NewRequest("POST", "/api/v2/pkg/install", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPackageInstall_RejectsTooHighMinWeaveVersion(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	router := newPackageInstallRouter(handler)

	body := `{
		"manifest": {"name": "x", "version": "1.0.0", "minWeaveVersion": "999.0.0"},
		"ontology": ` + pkgInstallSampleOntology + `
	}`
	req := httptest.NewRequest("POST", "/api/v2/pkg/install", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "PackageMinWeaveVersionUnsatisfied") {
		t.Fatalf("error should be PackageMinWeaveVersionUnsatisfied: %s", w.Body.String())
	}
}

func TestPackageInstall_AllowsMatchingMinWeaveVersion(t *testing.T) {
	prev := oms.WeaveServerVersion
	oms.WeaveServerVersion = "1.2.3"
	defer func() { oms.WeaveServerVersion = prev }()

	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	router := newPackageInstallRouter(handler)

	body := `{
		"manifest": {"name": "x", "version": "1.0.0", "minWeaveVersion": "1.2.3"},
		"ontology": ` + pkgInstallSampleOntology + `
	}`
	req := httptest.NewRequest("POST", "/api/v2/pkg/install", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPackageInstall_ConflictDetection_FailMode(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.northwind", APIName: "northwind", DisplayName: "Northwind"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.objecttype.main.x.1", OntologyRID: "ri.ontology.main.ontology.northwind", APIName: "Customer", DisplayName: "Existing Customer"},
		},
		actionTypes: []oms.ActionType{
			{RID: "ri.actiontype.main.x.1", OntologyRID: "ri.ontology.main.ontology.northwind", APIName: "createCustomer", DisplayName: "Existing Action"},
		},
	}
	handler := oms.NewOMSHandler(repo)
	router := newPackageInstallRouter(handler)

	body := `{
		"manifest": {"name": "northwind", "version": "1.0.0"},
		"ontology": ` + pkgInstallSampleOntology + `
	}`
	req := httptest.NewRequest("POST", "/api/v2/pkg/install", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	bodyOut := w.Body.String()
	if !strings.Contains(bodyOut, "PackageConflict") {
		t.Fatalf("error should be PackageConflict: %s", bodyOut)
	}
	if !strings.Contains(bodyOut, "Customer") || !strings.Contains(bodyOut, "createCustomer") {
		t.Fatalf("conflict list should mention both colliding entities: %s", bodyOut)
	}
}

func TestPackageInstall_ConflictDetection_OverwriteMode(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.northwind", APIName: "northwind", DisplayName: "Existing"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.objecttype.main.x.1", OntologyRID: "ri.ontology.main.ontology.northwind", APIName: "Customer"},
		},
	}
	handler := oms.NewOMSHandler(repo)
	router := newPackageInstallRouter(handler)

	body := `{
		"manifest": {"name": "northwind", "version": "1.0.0"},
		"ontology": ` + pkgInstallSampleOntology + `,
		"onConflict": "overwrite"
	}`
	req := httptest.NewRequest("POST", "/api/v2/pkg/install", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 in overwrite mode, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPackageInstall_ConflictDetection_SkipMode(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.northwind", APIName: "northwind", DisplayName: "Existing"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.objecttype.main.x.1", OntologyRID: "ri.ontology.main.ontology.northwind", APIName: "Customer"},
		},
	}
	handler := oms.NewOMSHandler(repo)
	router := newPackageInstallRouter(handler)

	body := `{
		"manifest": {"name": "northwind", "version": "1.0.0"},
		"ontology": ` + pkgInstallSampleOntology + `,
		"onConflict": "skip"
	}`
	req := httptest.NewRequest("POST", "/api/v2/pkg/install", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 in skip mode, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPackageInstall_RejectsBadOnConflict(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	router := newPackageInstallRouter(handler)

	body := `{
		"manifest": {"name": "x", "version": "1.0.0"},
		"ontology": ` + pkgInstallSampleOntology + `,
		"onConflict": "explode"
	}`
	req := httptest.NewRequest("POST", "/api/v2/pkg/install", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPackageInstall_MigrationRunnerErrorPropagates(t *testing.T) {
	repo := &mockRepo{}
	migrator := &stubMigrationRunner{err: errors.New("disk full")}
	handler := oms.NewOMSHandler(repo)
	handler.SetPackageMigrationRunner(migrator)
	router := newPackageInstallRouter(handler)

	body := `{
		"manifest": {"name": "x", "version": "1.0.0"},
		"ontology": ` + pkgInstallSampleOntology + `,
		"migrations": [{"filename": "000001_init.up.sql", "content": "U0VMRUNUIDE7"}]
	}`
	req := httptest.NewRequest("POST", "/api/v2/pkg/install", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "PackageMigrationFailed") {
		t.Fatalf("error should be PackageMigrationFailed: %s", w.Body.String())
	}
}

func TestPackageInstall_NoMigrationRunner_SkipsRunButReportsTotal(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	router := newPackageInstallRouter(handler)

	body := `{
		"manifest": {"name": "x", "version": "1.0.0"},
		"ontology": ` + pkgInstallSampleOntology + `,
		"migrations": [{"filename": "000001_init.up.sql", "content": "U0VMRUNUIDE7"}]
	}`
	req := httptest.NewRequest("POST", "/api/v2/pkg/install", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["migrationsTotal"].(float64) != 1 || resp["migrationsRan"].(float64) != 0 {
		t.Fatalf("expected total=1 ran=0, got %+v", resp)
	}
}

func TestPackageInstall_RecordsInstallInRegistry(t *testing.T) {
	repo := &mockRepo{}
	store := newStubInstalledPackageStore()
	handler := oms.NewOMSHandler(repo)
	handler.SetInstalledPackageStore(store)
	router := newPackageInstallRouter(handler)

	body := `{
		"manifest": {"name": "northwind", "version": "1.0.0", "author": "Albert"},
		"ontology": ` + pkgInstallSampleOntology + `
	}`
	req := httptest.NewRequest("POST", "/api/v2/pkg/install", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if len(store.rows) != 1 {
		t.Fatalf("store should have 1 row: %+v", store.rows)
	}
	row := store.rows["northwind"]
	if row.Ontology != "northwind" || row.Version != "1.0.0" {
		t.Fatalf("row drift: %+v", row)
	}
}

func TestPackageList_NoStore_EmptyData(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	router := newPackageInstallRouter(handler)

	req := httptest.NewRequest("GET", "/api/v2/pkg", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	data, _ := resp["data"].([]any)
	if len(data) != 0 {
		t.Fatalf("expected empty data array, got %+v", resp)
	}
}

func TestPackageList_Get_Toggle_Delete_Roundtrip(t *testing.T) {
	repo := &mockRepo{}
	store := newStubInstalledPackageStore()
	handler := oms.NewOMSHandler(repo)
	handler.SetInstalledPackageStore(store)
	router := newPackageInstallRouter(handler)

	body := `{
		"manifest": {"name": "alpha", "version": "1.0.0"},
		"ontology": ` + pkgInstallSampleOntology + `
	}`
	req := httptest.NewRequest("POST", "/api/v2/pkg/install", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("install 201 expected, got %d", w.Code)
	}

	// GET /api/v2/pkg/alpha
	req = httptest.NewRequest("GET", "/api/v2/pkg/alpha", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get 200 expected, got %d: %s", w.Code, w.Body.String())
	}

	// POST /api/v2/pkg/alpha/enabled false
	req = httptest.NewRequest("POST", "/api/v2/pkg/alpha/enabled", strings.NewReader(`{"enabled":false}`))
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("toggle 200 expected, got %d: %s", w.Code, w.Body.String())
	}
	if store.rows["alpha"].Enabled {
		t.Fatalf("expected disabled after toggle")
	}

	// DELETE /api/v2/pkg/alpha
	req = httptest.NewRequest("DELETE", "/api/v2/pkg/alpha", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete 204 expected, got %d: %s", w.Code, w.Body.String())
	}
	if _, ok := store.rows["alpha"]; ok {
		t.Fatalf("row should be gone after delete")
	}

	// GET should now 404
	req = httptest.NewRequest("GET", "/api/v2/pkg/alpha", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPackageInstall_FunctionConflictDetection(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.northwind", APIName: "northwind"},
		},
		functions: []oms.Function{
			{RID: "ri.fn.main.fn.1", OntologyRID: "ri.ontology.main.ontology.northwind", Name: "compute", Version: "1.0.0"},
		},
	}
	handler := oms.NewOMSHandler(repo)
	router := newPackageInstallRouter(handler)

	body := `{
		"manifest": {"name": "northwind", "version": "1.0.0"},
		"ontology": ` + pkgInstallSampleOntology + `
	}`
	req := httptest.NewRequest("POST", "/api/v2/pkg/install", strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "function") {
		t.Fatalf("expected function conflict in response: %s", w.Body.String())
	}
}
