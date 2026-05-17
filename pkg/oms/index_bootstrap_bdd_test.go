package oms_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// recordingBootstrapper captures every EnsureObjectTypeIndex /
// DropObjectTypeIndex invocation so the BDD scenarios can assert that the
// import / admin paths bootstrap object indexes synchronously, before any
// stream ingest could race against the freshly imported ObjectType.
type recordingBootstrapper struct {
	mu      sync.Mutex
	ensured []bootstrapCall
	dropped []bootstrapCall
}

type bootstrapCall struct {
	OntologyAPIName   string
	ObjectTypeAPIName string
	Props             []oms.Property
}

func (b *recordingBootstrapper) EnsureObjectTypeIndex(ontologyAPIName, objectTypeAPIName string, props []oms.Property) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	copyProps := make([]oms.Property, len(props))
	copy(copyProps, props)
	b.ensured = append(b.ensured, bootstrapCall{
		OntologyAPIName:   ontologyAPIName,
		ObjectTypeAPIName: objectTypeAPIName,
		Props:             copyProps,
	})
	return nil
}

func (b *recordingBootstrapper) DropObjectTypeIndex(ontologyAPIName, objectTypeAPIName string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dropped = append(b.dropped, bootstrapCall{
		OntologyAPIName:   ontologyAPIName,
		ObjectTypeAPIName: objectTypeAPIName,
	})
	return nil
}

func (b *recordingBootstrapper) ensureCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.ensured)
}

func (b *recordingBootstrapper) dropCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.dropped)
}

// TestBDD_ImportOntology_BootstrapsIndexForEachNewObjectType is the DOG-003
// red→green guard for the dogfood failure where 103 AI_News CREATE edits
// returned editCount=success but the freshly imported AI_News ObjectType had
// no Bleve index, so list/search/aggregate then failed with
// "index not found for object type AI_News".
//
// Given /api/v2/ontologies/import creates an AI_News ObjectType,
// When the request succeeds,
// Then the wired IndexBootstrapper has been called once per new ObjectType
// before the handler returns (so a follow-up stream ingest cannot race).
func TestBDD_ImportOntology_BootstrapsIndexForEachNewObjectType(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	boot := &recordingBootstrapper{}
	handler.SetIndexBootstrapper(boot)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/import", handler.ImportOntologyV2)

	body := `{
		"mode": "merge",
		"ontology": {"apiName": "ainews", "displayName": "AI News"},
		"objectTypes": [
			{"rid": "old-ot-ainews", "apiName": "AI_News", "displayName": "AI News Item",
			 "primaryKey": "newsId", "status": "ACTIVE", "visibility": "NORMAL",
			 "properties": [
				{"rid": "old-prop-title", "apiName": "title", "displayName": "Title",
				 "baseType": "string", "isSearchable": true, "isSortable": true},
				{"rid": "old-prop-category", "apiName": "category", "displayName": "Category",
				 "baseType": "string", "isSearchable": true, "isSortable": true}
			]}
		],
		"linkTypes": [], "actionTypes": [], "interfaces": [],
		"sharedProperties": [], "valueTypes": [], "typeGroups": [],
		"functions": [], "queryTypes": []
	}`

	req := httptest.NewRequest("POST", "/api/v2/ontologies/import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	if got := boot.ensureCount(); got != 1 {
		t.Fatalf("expected EnsureObjectTypeIndex called once for AI_News, got %d", got)
	}
	call := boot.ensured[0]
	if call.OntologyAPIName != "ainews" {
		t.Errorf("expected ontology ainews, got %q", call.OntologyAPIName)
	}
	if call.ObjectTypeAPIName != "AI_News" {
		t.Errorf("expected objectType AI_News, got %q", call.ObjectTypeAPIName)
	}
	if len(call.Props) != 2 {
		t.Fatalf("expected 2 properties passed to bootstrapper, got %d", len(call.Props))
	}
	// The bootstrapper must receive the schema-faithful property metadata so
	// the Bleve mapping is correct on the very first ingest (not a degraded
	// "all-default" mapping that would need a rebuild to fix later).
	hasTitleSearchable := false
	for _, p := range call.Props {
		if p.APIName == "title" && p.IsSearchable {
			hasTitleSearchable = true
		}
	}
	if !hasTitleSearchable {
		t.Errorf("expected title.isSearchable=true to be passed to bootstrapper, got props %+v", call.Props)
	}
}

// TestBDD_CreateObjectType_BootstrapsIndex covers the same root cause for
// the alternative admin path that creates ObjectTypes one at a time.
//
// Given POST /api/admin/ontologies/{ontology}/objectTypes succeeds,
// When a CREATE edit is published for that ObjectType,
// Then the index already exists — so the consumer's IndexDocument call does
// not fail with "index not found".
func TestBDD_CreateObjectType_BootstrapsIndex(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.ainews", APIName: "ainews", DisplayName: "AI News"},
		},
	}
	handler := oms.NewOMSHandler(repo)
	boot := &recordingBootstrapper{}
	handler.SetIndexBootstrapper(boot)

	r := chi.NewRouter()
	r.Post("/api/admin/ontologies/{ontologyApiName}/objectTypes", handler.CreateObjectType)

	body := `{
		"apiName": "AI_News",
		"displayName": "AI News Item",
		"primaryKey": "newsId",
		"classification": "Internal"
	}`

	req := httptest.NewRequest("POST", "/api/admin/ontologies/ainews/objectTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	if got := boot.ensureCount(); got != 1 {
		t.Fatalf("expected EnsureObjectTypeIndex called once, got %d", got)
	}
	call := boot.ensured[0]
	if call.OntologyAPIName != "ainews" || call.ObjectTypeAPIName != "AI_News" {
		t.Errorf("expected ainews/AI_News bootstrap, got %s/%s",
			call.OntologyAPIName, call.ObjectTypeAPIName)
	}
}

// TestBDD_ImportOntology_ReplaceMode_DropsStaleIndex covers the second
// bdd_acceptance clause for DOG-003: replace/re-import must not leak stale
// index state for ObjectTypes that no longer exist (or are being
// re-mapped). We assert the bootstrapper sees DropObjectTypeIndex for the
// previously imported ObjectType before the new EnsureObjectTypeIndex.
func TestBDD_ImportOntology_ReplaceMode_DropsStaleIndex(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.ainews", APIName: "ainews", DisplayName: "AI News"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.object-type.old", OntologyRID: "ri.ontology.main.ontology.ainews",
				APIName: "AI_News", DisplayName: "AI News Item", PrimaryKey: "newsId", Status: "ACTIVE"},
		},
	}
	handler := oms.NewOMSHandler(repo)
	boot := &recordingBootstrapper{}
	handler.SetIndexBootstrapper(boot)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/import", handler.ImportOntologyV2)

	body := `{
		"mode": "replace",
		"ontology": {"apiName": "ainews", "displayName": "AI News"},
		"objectTypes": [
			{"rid": "new-ot", "apiName": "AI_News", "displayName": "AI News Item",
			 "primaryKey": "newsId", "status": "ACTIVE", "visibility": "NORMAL",
			 "properties": []}
		],
		"linkTypes": [], "actionTypes": [], "interfaces": [],
		"sharedProperties": [], "valueTypes": [], "typeGroups": [],
		"functions": [], "queryTypes": []
	}`

	req := httptest.NewRequest("POST", "/api/v2/ontologies/import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	if got := boot.dropCount(); got != 1 {
		t.Fatalf("expected DropObjectTypeIndex called once for stale AI_News in replace mode, got %d", got)
	}
	if boot.dropped[0].OntologyAPIName != "ainews" || boot.dropped[0].ObjectTypeAPIName != "AI_News" {
		t.Errorf("expected ainews/AI_News drop, got %s/%s",
			boot.dropped[0].OntologyAPIName, boot.dropped[0].ObjectTypeAPIName)
	}
	if got := boot.ensureCount(); got != 1 {
		t.Fatalf("expected EnsureObjectTypeIndex called once for the new AI_News, got %d", got)
	}
}
