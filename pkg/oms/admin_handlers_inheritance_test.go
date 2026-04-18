package oms_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// US-212 Object Type Inheritance: admin handlers carry `extendsRid` through
// Create/Update with same-ontology + cycle validation, and the new
// `/resolved` endpoint surfaces parent properties + outgoing links merged
// onto the child (child entries override).

func newInheritanceRouter(handler *oms.OMSHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Post("/api/admin/ontologies/{ontologyApiName}/objectTypes", handler.CreateObjectType)
	r.Put("/api/admin/objectTypes/{objectTypeRid}", handler.UpdateObjectType)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}", handler.GetObjectType)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/resolved", handler.GetObjectTypeResolved)
	return r
}

func TestCreateObjectType_WithExtendsRID_Success(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	parent := oms.ObjectType{
		RID: "ri.ontology.main.object-type.person", OntologyRID: ontRID,
		APIName: "person", DisplayName: "Person",
		PrimaryKey: "id", PrimaryKeys: []string{"id"},
		Status: "ACTIVE", Visibility: "NORMAL",
	}
	repo.objectTypes = append(repo.objectTypes, parent)

	r := newInheritanceRouter(oms.NewOMSHandler(repo))

	body := `{"apiName":"employee","displayName":"Employee","primaryKey":"id","extendsRid":"ri.ontology.main.object-type.person","status":"ACTIVE","visibility":"NORMAL"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/objectTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if resp["extendsRid"] != "ri.ontology.main.object-type.person" {
		t.Errorf("expected extendsRid in response, got %v", resp["extendsRid"])
	}

	if len(repo.objectTypes) != 2 {
		t.Fatalf("expected 2 object types after create, got %d", len(repo.objectTypes))
	}
	if got := repo.objectTypes[1].ExtendsRID; got != "ri.ontology.main.object-type.person" {
		t.Errorf("stored ExtendsRID = %q, want ri.ontology.main.object-type.person", got)
	}
}

func TestCreateObjectType_WithUnknownExtendsRID_Returns400(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	r := newInheritanceRouter(oms.NewOMSHandler(repo))

	body := `{"apiName":"x","displayName":"X","primaryKey":"id","extendsRid":"ri.ot.does-not-exist","status":"ACTIVE","visibility":"NORMAL"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/objectTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown parent, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if resp["errorName"] != "InvalidParameter:extendsRid" {
		t.Errorf("expected errorName=InvalidParameter:extendsRid, got %v", resp["errorName"])
	}
}

func TestCreateObjectType_WithCrossOntologyExtendsRID_Returns400(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	// Add a second ontology + parent within IT.
	const otherOnt = "ri.ontology.main.ontology.2"
	repo.ontologies = append(repo.ontologies, oms.Ontology{RID: otherOnt, APIName: "other", DisplayName: "Other"})
	repo.objectTypes = append(repo.objectTypes, oms.ObjectType{
		RID: "ri.ot.cross", OntologyRID: otherOnt,
		APIName: "alien", DisplayName: "Alien",
		PrimaryKey: "id", PrimaryKeys: []string{"id"},
		Status: "ACTIVE", Visibility: "NORMAL",
	})
	r := newInheritanceRouter(oms.NewOMSHandler(repo))

	body := `{"apiName":"x","displayName":"X","primaryKey":"id","extendsRid":"ri.ot.cross","status":"ACTIVE","visibility":"NORMAL"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/objectTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cross-ontology parent, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateObjectType_SetExtendsRID_AddsParent(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	parent := oms.ObjectType{
		RID: "ri.ot.parent", OntologyRID: ontRID,
		APIName: "person", DisplayName: "Person",
		PrimaryKey: "id", PrimaryKeys: []string{"id"},
		Status: "ACTIVE", Visibility: "NORMAL",
	}
	child := oms.ObjectType{
		RID: "ri.ot.child", OntologyRID: ontRID,
		APIName: "employee", DisplayName: "Employee",
		PrimaryKey: "id", PrimaryKeys: []string{"id"},
		Status: "ACTIVE", Visibility: "NORMAL",
	}
	repo.objectTypes = []oms.ObjectType{parent, child}
	r := newInheritanceRouter(oms.NewOMSHandler(repo))

	body := `{"displayName":"Employee","status":"ACTIVE","visibility":"NORMAL","extendsRid":"ri.ot.parent"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/objectTypes/ri.ot.child", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := findObjectType(repo, "ri.ot.child").ExtendsRID; got != "ri.ot.parent" {
		t.Errorf("expected stored ExtendsRID=ri.ot.parent, got %q", got)
	}
}

func TestUpdateObjectType_ClearExtendsRID(t *testing.T) {
	// US-212: tri-state pointer — empty string clears the parent link.
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	repo.objectTypes = []oms.ObjectType{
		{RID: "ri.ot.parent", OntologyRID: ontRID, APIName: "p", DisplayName: "P",
			PrimaryKey: "id", PrimaryKeys: []string{"id"}, Status: "ACTIVE", Visibility: "NORMAL"},
		{RID: "ri.ot.child", OntologyRID: ontRID, APIName: "c", DisplayName: "C",
			PrimaryKey: "id", PrimaryKeys: []string{"id"}, Status: "ACTIVE", Visibility: "NORMAL",
			ExtendsRID: "ri.ot.parent"},
	}
	r := newInheritanceRouter(oms.NewOMSHandler(repo))

	body := `{"displayName":"C","status":"ACTIVE","visibility":"NORMAL","extendsRid":""}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/objectTypes/ri.ot.child", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := findObjectType(repo, "ri.ot.child").ExtendsRID; got != "" {
		t.Errorf("expected ExtendsRID cleared, got %q", got)
	}
}

func TestUpdateObjectType_CycleRejected(t *testing.T) {
	// A→B exists. Set B.ExtendsRID = A → cycle. Must be rejected.
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	repo.objectTypes = []oms.ObjectType{
		{RID: "ri.ot.a", OntologyRID: ontRID, APIName: "a", DisplayName: "A",
			PrimaryKey: "id", PrimaryKeys: []string{"id"}, Status: "ACTIVE", Visibility: "NORMAL",
			ExtendsRID: "ri.ot.b"},
		{RID: "ri.ot.b", OntologyRID: ontRID, APIName: "b", DisplayName: "B",
			PrimaryKey: "id", PrimaryKeys: []string{"id"}, Status: "ACTIVE", Visibility: "NORMAL"},
	}
	r := newInheritanceRouter(oms.NewOMSHandler(repo))

	body := `{"displayName":"B","status":"ACTIVE","visibility":"NORMAL","extendsRid":"ri.ot.a"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/objectTypes/ri.ot.b", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cycle, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if resp["errorName"] != "InvalidParameter:extendsRid" {
		t.Errorf("expected errorName=InvalidParameter:extendsRid, got %v", resp["errorName"])
	}
}

func TestGetObjectTypeResolved_MergesParentPropertiesAndLinks(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	repo.objectTypes = []oms.ObjectType{
		{RID: "ri.ot.parent", OntologyRID: ontRID, APIName: "person", DisplayName: "Person",
			PrimaryKey: "id", PrimaryKeys: []string{"id"}, Status: "ACTIVE", Visibility: "NORMAL"},
		{RID: "ri.ot.child", OntologyRID: ontRID, APIName: "employee", DisplayName: "Employee",
			PrimaryKey: "id", PrimaryKeys: []string{"id"}, Status: "ACTIVE", Visibility: "NORMAL",
			ExtendsRID: "ri.ot.parent"},
	}
	repo.properties = []oms.Property{
		{RID: "ri.p.parent.name", ObjectTypeRID: "ri.ot.parent", APIName: "name", BaseType: "string"},
		{RID: "ri.p.parent.age", ObjectTypeRID: "ri.ot.parent", APIName: "age", BaseType: "integer"},
		{RID: "ri.p.child.salary", ObjectTypeRID: "ri.ot.child", APIName: "salary", BaseType: "double"},
	}
	repo.linkTypes = []oms.LinkType{
		{RID: "ri.lt.friends", OntologyRID: ontRID, APIName: "friends",
			SourceObjectType: "ri.ot.parent", TargetObjectType: "ri.ot.parent", Cardinality: "MANY_TO_MANY"},
	}
	r := newInheritanceRouter(oms.NewOMSHandler(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/"+ontRID+"/objectTypes/employee/resolved", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	props, ok := resp["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties map, got %v", resp["properties"])
	}
	for _, want := range []string{"name", "age", "salary"} {
		if _, ok := props[want]; !ok {
			t.Errorf("missing inherited property %q in resolved view: %v", want, props)
		}
	}
	links, ok := resp["outgoingLinkTypes"].([]interface{})
	if !ok {
		t.Fatalf("expected outgoingLinkTypes array, got %v", resp["outgoingLinkTypes"])
	}
	foundFriends := false
	for _, item := range links {
		entry := item.(map[string]interface{})
		if entry["apiName"] == "friends" {
			foundFriends = true
		}
	}
	if !foundFriends {
		t.Errorf("expected inherited 'friends' link to surface in resolved view, got %v", links)
	}
	if resp["extendsRid"] != "ri.ot.parent" {
		t.Errorf("expected extendsRid=ri.ot.parent in resolved view, got %v", resp["extendsRid"])
	}
}

func TestGetObjectTypeResolved_ChildOverridesParentProperty(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	repo.objectTypes = []oms.ObjectType{
		{RID: "ri.ot.parent", OntologyRID: ontRID, APIName: "person", DisplayName: "Person",
			PrimaryKey: "id", PrimaryKeys: []string{"id"}, Status: "ACTIVE", Visibility: "NORMAL"},
		{RID: "ri.ot.child", OntologyRID: ontRID, APIName: "employee", DisplayName: "Employee",
			PrimaryKey: "id", PrimaryKeys: []string{"id"}, Status: "ACTIVE", Visibility: "NORMAL",
			ExtendsRID: "ri.ot.parent"},
	}
	repo.properties = []oms.Property{
		{RID: "ri.p.parent.name", ObjectTypeRID: "ri.ot.parent", APIName: "name", BaseType: "string", DisplayName: "Person Name"},
		{RID: "ri.p.child.name", ObjectTypeRID: "ri.ot.child", APIName: "name", BaseType: "string", DisplayName: "Employee Name"},
	}
	r := newInheritanceRouter(oms.NewOMSHandler(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/"+ontRID+"/objectTypes/employee/resolved", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	props := resp["properties"].(map[string]interface{})
	nameEntry, ok := props["name"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected name property entry, got %v", props["name"])
	}
	if nameEntry["displayName"] != "Employee Name" {
		t.Errorf("expected child's displayName to win override, got %v", nameEntry["displayName"])
	}
	// Child-owned entry should NOT carry inheritedFrom marker.
	if _, ok := nameEntry["inheritedFrom"]; ok {
		t.Errorf("expected no inheritedFrom marker on child-owned property, got %v", nameEntry)
	}
}

func TestGetObjectTypeResolved_NoParentReturnsOwnPropertiesOnly(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	repo.objectTypes = []oms.ObjectType{
		{RID: "ri.ot.solo", OntologyRID: ontRID, APIName: "solo", DisplayName: "Solo",
			PrimaryKey: "id", PrimaryKeys: []string{"id"}, Status: "ACTIVE", Visibility: "NORMAL"},
	}
	repo.properties = []oms.Property{
		{RID: "ri.p.solo.name", ObjectTypeRID: "ri.ot.solo", APIName: "name", BaseType: "string"},
	}
	r := newInheritanceRouter(oms.NewOMSHandler(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/"+ontRID+"/objectTypes/solo/resolved", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if _, ok := resp["extendsRid"]; ok {
		t.Errorf("expected no extendsRid for root type, got %v", resp["extendsRid"])
	}
	if _, ok := resp["extendsChain"]; ok {
		t.Errorf("expected no extendsChain for root type, got %v", resp["extendsChain"])
	}
}

func TestObjectTypeWireFormat_IncludesExtendsRID(t *testing.T) {
	repo := &mockRepo{
		objectTypes: []oms.ObjectType{
			{
				RID: "ri.ot.child", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "employee", DisplayName: "Employee",
				PrimaryKey: "id", PrimaryKeys: []string{"id"},
				Status: "ACTIVE", Visibility: "NORMAL",
				ExtendsRID: "ri.ot.parent",
			},
		},
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
	}
	r := newInheritanceRouter(oms.NewOMSHandler(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/objectTypes/employee", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if resp["extendsRid"] != "ri.ot.parent" {
		t.Errorf("expected extendsRid='ri.ot.parent' in V2 wire format, got %v", resp["extendsRid"])
	}
}

// ---- helpers ----

func findObjectType(repo *mockRepo, rid string) *oms.ObjectType {
	for i := range repo.objectTypes {
		if repo.objectTypes[i].RID == rid {
			return &repo.objectTypes[i]
		}
	}
	return nil
}

