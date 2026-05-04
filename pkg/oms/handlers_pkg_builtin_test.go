package oms_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// stubBuiltinProvider is an in-memory oms.BuiltinPackageProvider used by
// the US-414 list/install handler tests. It mirrors the per-slug map the
// real cmd/server provider builds at boot.
type stubBuiltinProvider struct {
	rows map[string]builtinRow
}

type builtinRow struct {
	metadata oms.BuiltinPackageMetadata
	request  oms.PackageInstallRequest
}

func newStubBuiltinProvider(rows ...builtinRow) *stubBuiltinProvider {
	m := make(map[string]builtinRow, len(rows))
	for _, r := range rows {
		m[r.metadata.Slug] = r
	}
	return &stubBuiltinProvider{rows: m}
}

func (s *stubBuiltinProvider) List(_ context.Context) []oms.BuiltinPackageMetadata {
	out := make([]oms.BuiltinPackageMetadata, 0, len(s.rows))
	for _, r := range s.rows {
		out = append(out, r.metadata)
	}
	return out
}

func (s *stubBuiltinProvider) Get(_ context.Context, slug string) (*oms.PackageInstallRequest, *oms.BuiltinPackageMetadata, bool) {
	r, ok := s.rows[slug]
	if !ok {
		return nil, nil, false
	}
	cp := r.request
	cp.Ontology = json.RawMessage(append([]byte(nil), r.request.Ontology...))
	return &cp, &r.metadata, true
}

func newBuiltinRouter(handler *oms.OMSHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/api/v2/pkg/builtin", handler.ListBuiltinPackages)
	r.Post("/api/v2/pkg/builtin/{slug}/install", handler.InstallBuiltinPackage)
	r.Post("/api/v2/pkg/install", handler.PackageInstall)
	r.Get("/api/v2/pkg", handler.ListInstalledPackages)
	return r
}

const builtinSampleOntology = `{
	"ontology": {"apiName": "demo", "displayName": "Demo"},
	"objectTypes": [{"apiName": "Widget", "displayName": "Widget", "primaryKey": "id", "status": "ACTIVE", "visibility": "NORMAL"}],
	"linkTypes": [],
	"actionTypes": [],
	"interfaces": [],
	"sharedProperties": [],
	"valueTypes": [],
	"typeGroups": [],
	"functions": [],
	"queryTypes": []
}`

func sampleBuiltinRow(slug string) builtinRow {
	return builtinRow{
		metadata: oms.BuiltinPackageMetadata{
			Slug:            slug,
			Name:            slug,
			Version:         "1.0.0",
			OntologyAPIName: "demo",
			Author:          "Weave Examples",
			License:         "MIT",
			Description:     "Tiny demo package for tests.",
			MinWeaveVersion: "0.42.0",
			ObjectTypeCount: 1,
		},
		request: oms.PackageInstallRequest{
			Manifest: oms.PackageManifest{
				Name:            slug,
				Version:         "1.0.0",
				Author:          "Weave Examples",
				License:         "MIT",
				Description:     "Tiny demo package for tests.",
				MinWeaveVersion: "0.42.0",
			},
			Ontology: json.RawMessage(builtinSampleOntology),
		},
	}
}

func TestListBuiltinPackages_NoProvider_ReturnsEmpty(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	router := newBuiltinRouter(handler)

	req := httptest.NewRequest("GET", "/api/v2/pkg/builtin", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp oms.BuiltinPackageListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 0 {
		t.Fatalf("expected empty data, got %+v", resp.Data)
	}
}

func TestListBuiltinPackages_WithProvider_ReturnsRows(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	handler.SetBuiltinPackageProvider(newStubBuiltinProvider(
		sampleBuiltinRow("northwind"),
		sampleBuiltinRow("chinook"),
		sampleBuiltinRow("iot-demo"),
	))
	router := newBuiltinRouter(handler)

	req := httptest.NewRequest("GET", "/api/v2/pkg/builtin", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp oms.BuiltinPackageListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(resp.Data))
	}
	slugs := make(map[string]bool, 3)
	for _, r := range resp.Data {
		slugs[r.Slug] = true
	}
	for _, want := range []string{"northwind", "chinook", "iot-demo"} {
		if !slugs[want] {
			t.Errorf("missing slug %q in %+v", want, slugs)
		}
	}
}

func TestInstallBuiltinPackage_Success(t *testing.T) {
	repo := &mockRepo{}
	store := newStubInstalledPackageStore()
	handler := oms.NewOMSHandler(repo)
	handler.SetInstalledPackageStore(store)
	handler.SetBuiltinPackageProvider(newStubBuiltinProvider(sampleBuiltinRow("northwind")))
	router := newBuiltinRouter(handler)

	req := httptest.NewRequest("POST", "/api/v2/pkg/builtin/northwind/install", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["name"] != "northwind" || resp["version"] != "1.0.0" {
		t.Fatalf("response identity drift: %+v", resp)
	}
	if resp["ontology"] != "demo" {
		t.Fatalf("expected ontology 'demo', got %v", resp["ontology"])
	}
	if len(store.rows) != 1 {
		t.Fatalf("install not recorded: %+v", store.rows)
	}
	row := store.rows["northwind"]
	if row == nil || !row.Enabled {
		t.Fatalf("row drift: %+v", row)
	}
	if len(repo.objectTypes) != 1 || repo.objectTypes[0].APIName != "Widget" {
		t.Fatalf("objectType not imported: %+v", repo.objectTypes)
	}
}

func TestInstallBuiltinPackage_UnknownSlug(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	handler.SetBuiltinPackageProvider(newStubBuiltinProvider(sampleBuiltinRow("northwind")))
	router := newBuiltinRouter(handler)

	req := httptest.NewRequest("POST", "/api/v2/pkg/builtin/missing/install", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "BuiltinPackageNotFound") {
		t.Fatalf("expected BuiltinPackageNotFound, got: %s", w.Body.String())
	}
}

func TestInstallBuiltinPackage_NoProvider(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	router := newBuiltinRouter(handler)

	req := httptest.NewRequest("POST", "/api/v2/pkg/builtin/northwind/install", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "BuiltinPackagesNotConfigured") {
		t.Fatalf("expected BuiltinPackagesNotConfigured, got: %s", w.Body.String())
	}
}

func TestInstallBuiltinPackage_ConflictReturns409(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.demo", APIName: "demo", DisplayName: "Demo"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.objecttype.main.x.1", OntologyRID: "ri.ontology.main.ontology.demo", APIName: "Widget", DisplayName: "Existing Widget"},
		},
	}
	handler := oms.NewOMSHandler(repo)
	handler.SetBuiltinPackageProvider(newStubBuiltinProvider(sampleBuiltinRow("northwind")))
	router := newBuiltinRouter(handler)

	req := httptest.NewRequest("POST", "/api/v2/pkg/builtin/northwind/install", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "PackageConflict") {
		t.Fatalf("expected PackageConflict, got: %s", w.Body.String())
	}
}

func TestInstallBuiltinPackage_OnConflictOverwrite(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.demo", APIName: "demo", DisplayName: "Demo"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.objecttype.main.x.1", OntologyRID: "ri.ontology.main.ontology.demo", APIName: "Widget", DisplayName: "Existing Widget"},
		},
	}
	handler := oms.NewOMSHandler(repo)
	handler.SetBuiltinPackageProvider(newStubBuiltinProvider(sampleBuiltinRow("northwind")))
	router := newBuiltinRouter(handler)

	body := `{"onConflict": "overwrite"}`
	req := httptest.NewRequest("POST", "/api/v2/pkg/builtin/northwind/install", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 with overwrite, got %d: %s", w.Code, w.Body.String())
	}
}

func TestInstallBuiltinPackage_RejectsTooHighMinWeaveVersion(t *testing.T) {
	prev := oms.WeaveServerVersion
	oms.WeaveServerVersion = "0.1.0"
	defer func() { oms.WeaveServerVersion = prev }()

	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	row := sampleBuiltinRow("northwind")
	row.request.Manifest.MinWeaveVersion = "999.0.0"
	handler.SetBuiltinPackageProvider(newStubBuiltinProvider(row))
	router := newBuiltinRouter(handler)

	req := httptest.NewRequest("POST", "/api/v2/pkg/builtin/northwind/install", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "PackageMinWeaveVersionUnsatisfied") {
		t.Fatalf("expected PackageMinWeaveVersionUnsatisfied, got: %s", w.Body.String())
	}
}
