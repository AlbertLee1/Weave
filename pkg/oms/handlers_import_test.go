package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

func TestImportOntologyV2_MergeMode_NewOntology(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/import", handler.ImportOntologyV2)

	body := `{
		"mode": "merge",
		"ontology": {"apiName": "imported", "displayName": "Imported Ontology"},
		"objectTypes": [
			{"rid": "old-ot-1", "apiName": "Employee", "displayName": "Employee", "primaryKey": "id", "status": "ACTIVE", "visibility": "NORMAL",
			 "properties": [
				{"rid": "old-prop-1", "apiName": "name", "displayName": "Name", "baseType": "string"}
			]}
		],
		"linkTypes": [
			{"rid": "old-lt-1", "apiName": "manages", "displayName": "Manages", "objectTypeApiName": "Employee", "linkedObjectTypeApiName": "Employee", "cardinality": "ONE_TO_MANY"}
		],
		"actionTypes": [
			{"rid": "old-at-1", "apiName": "createEmployee", "displayName": "Create Employee", "status": "ACTIVE"}
		],
		"interfaces": [],
		"sharedProperties": [],
		"valueTypes": [],
		"typeGroups": [],
		"functions": [],
		"queryTypes": []
	}`

	req := httptest.NewRequest("POST", "/api/v2/ontologies/import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Ontology created
	if len(repo.ontologies) != 1 {
		t.Fatalf("expected 1 ontology, got %d", len(repo.ontologies))
	}
	if repo.ontologies[0].APIName != "imported" {
		t.Errorf("expected apiName 'imported', got %q", repo.ontologies[0].APIName)
	}

	// ObjectType created with NEW RID
	if len(repo.objectTypes) != 1 {
		t.Fatalf("expected 1 objectType, got %d", len(repo.objectTypes))
	}
	if repo.objectTypes[0].RID == "old-ot-1" {
		t.Error("expected new RID for imported objectType, got old RID")
	}
	if repo.objectTypes[0].APIName != "Employee" {
		t.Errorf("expected apiName 'Employee', got %q", repo.objectTypes[0].APIName)
	}
	if repo.objectTypes[0].OntologyRID != repo.ontologies[0].RID {
		t.Error("objectType OntologyRID should match created ontology")
	}

	// Property created with NEW RID
	if len(repo.properties) != 1 {
		t.Fatalf("expected 1 property, got %d", len(repo.properties))
	}
	if repo.properties[0].RID == "old-prop-1" {
		t.Error("expected new RID for imported property")
	}
	if repo.properties[0].ObjectTypeRID != repo.objectTypes[0].RID {
		t.Error("property ObjectTypeRID should match created objectType")
	}

	// LinkType created
	if len(repo.linkTypes) != 1 {
		t.Fatalf("expected 1 linkType, got %d", len(repo.linkTypes))
	}
	if repo.linkTypes[0].RID == "old-lt-1" {
		t.Error("expected new RID for imported linkType")
	}
	if repo.linkTypes[0].SourceObjectType != "Employee" {
		t.Errorf("expected SourceObjectType 'Employee', got %q", repo.linkTypes[0].SourceObjectType)
	}

	// ActionType created
	if len(repo.actionTypes) != 1 {
		t.Fatalf("expected 1 actionType, got %d", len(repo.actionTypes))
	}

	// Check import counts in response
	imported := result["imported"].(map[string]interface{})
	if imported["objectTypes"].(float64) != 1 {
		t.Errorf("expected 1 objectType in import counts, got %v", imported["objectTypes"])
	}
	if imported["properties"].(float64) != 1 {
		t.Errorf("expected 1 property in import counts, got %v", imported["properties"])
	}
}

func TestImportOntologyV2_MergeMode_ExistingOntology(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.existing", APIName: "myonto", DisplayName: "My Ontology"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.objectType.existing", OntologyRID: "ri.ontology.main.ontology.existing", APIName: "Employee", DisplayName: "Employee"},
		},
		properties: []oms.Property{
			{RID: "ri.ontology.main.property.existing", ObjectTypeRID: "ri.ontology.main.objectType.existing", APIName: "name", DisplayName: "Name", BaseType: "string"},
		},
	}
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/import", handler.ImportOntologyV2)

	// Import: Employee already exists (should update), Department is new (should create)
	body := `{
		"mode": "merge",
		"ontology": {"apiName": "myonto", "displayName": "My Ontology Updated"},
		"objectTypes": [
			{"rid": "old-ot-1", "apiName": "Employee", "displayName": "Employee Updated", "primaryKey": "id", "status": "ACTIVE", "visibility": "NORMAL",
			 "properties": [
				{"rid": "old-p-1", "apiName": "name", "displayName": "Full Name", "baseType": "string"},
				{"rid": "old-p-2", "apiName": "age", "displayName": "Age", "baseType": "integer"}
			]},
			{"rid": "old-ot-2", "apiName": "Department", "displayName": "Department", "primaryKey": "deptId", "status": "ACTIVE", "visibility": "NORMAL",
			 "properties": [
				{"rid": "old-p-3", "apiName": "deptName", "displayName": "Department Name", "baseType": "string"}
			]}
		],
		"linkTypes": [],
		"actionTypes": [],
		"interfaces": [],
		"sharedProperties": [],
		"valueTypes": [],
		"typeGroups": [],
		"functions": [],
		"queryTypes": []
	}`

	req := httptest.NewRequest("POST", "/api/v2/ontologies/import", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Should not create a new ontology — use existing
	if len(repo.ontologies) != 1 {
		t.Fatalf("expected 1 ontology (existing), got %d", len(repo.ontologies))
	}
	if repo.ontologies[0].DisplayName != "My Ontology Updated" {
		t.Errorf("expected updated displayName, got %q", repo.ontologies[0].DisplayName)
	}

	// Employee updated (existing RID kept), Department created (new)
	if len(repo.objectTypes) != 2 {
		t.Fatalf("expected 2 objectTypes, got %d", len(repo.objectTypes))
	}

	var employee, department *oms.ObjectType
	for i := range repo.objectTypes {
		switch repo.objectTypes[i].APIName {
		case "Employee":
			employee = &repo.objectTypes[i]
		case "Department":
			department = &repo.objectTypes[i]
		}
	}
	if employee == nil || department == nil {
		t.Fatal("expected both Employee and Department")
	}
	// Employee should keep existing RID (was updated in place)
	if employee.RID != "ri.ontology.main.objectType.existing" {
		t.Errorf("Employee should keep existing RID, got %q", employee.RID)
	}
	if employee.DisplayName != "Employee Updated" {
		t.Errorf("Employee should be updated, got displayName %q", employee.DisplayName)
	}
}

func TestImportOntologyV2_ReplaceMode_ExistingOntology(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.existing", APIName: "myonto", DisplayName: "Old"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.objectType.old", OntologyRID: "ri.ontology.main.ontology.existing", APIName: "OldType", DisplayName: "Old Type"},
		},
		properties: []oms.Property{
			{RID: "ri.ontology.main.property.old", ObjectTypeRID: "ri.ontology.main.objectType.old", APIName: "oldProp"},
		},
		linkTypes: []oms.LinkType{
			{RID: "ri.ontology.main.linkType.old", OntologyRID: "ri.ontology.main.ontology.existing", APIName: "oldLink"},
		},
	}
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/import", handler.ImportOntologyV2)

	body := `{
		"mode": "replace",
		"ontology": {"apiName": "myonto", "displayName": "Replaced"},
		"objectTypes": [
			{"rid": "old-ot-1", "apiName": "NewType", "displayName": "New Type", "primaryKey": "id", "status": "ACTIVE", "visibility": "NORMAL",
			 "properties": [
				{"rid": "old-p-1", "apiName": "newProp", "displayName": "New Prop", "baseType": "string"}
			]}
		],
		"linkTypes": [],
		"actionTypes": [],
		"interfaces": [],
		"sharedProperties": [],
		"valueTypes": [],
		"typeGroups": [],
		"functions": [],
		"queryTypes": []
	}`

	req := httptest.NewRequest("POST", "/api/v2/ontologies/import", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Old entities should be deleted; only new ones remain
	if len(repo.objectTypes) != 1 {
		t.Fatalf("expected 1 objectType after replace, got %d", len(repo.objectTypes))
	}
	if repo.objectTypes[0].APIName != "NewType" {
		t.Errorf("expected 'NewType', got %q", repo.objectTypes[0].APIName)
	}
	// Old linkType should be deleted
	if len(repo.linkTypes) != 0 {
		t.Errorf("expected 0 linkTypes after replace (none imported), got %d", len(repo.linkTypes))
	}
	// Old property should be deleted
	if len(repo.properties) != 1 {
		t.Fatalf("expected 1 property after replace, got %d", len(repo.properties))
	}
	if repo.properties[0].APIName != "newProp" {
		t.Errorf("expected 'newProp', got %q", repo.properties[0].APIName)
	}
}

func TestImportOntologyV2_ReplaceMode_NewOntology(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/import", handler.ImportOntologyV2)

	body := `{
		"mode": "replace",
		"ontology": {"apiName": "newonto", "displayName": "New Ontology"},
		"objectTypes": [
			{"rid": "old-1", "apiName": "Thing", "displayName": "Thing", "primaryKey": "id", "status": "ACTIVE", "visibility": "NORMAL"}
		],
		"linkTypes": [],
		"actionTypes": [],
		"interfaces": [],
		"sharedProperties": [],
		"valueTypes": [],
		"typeGroups": [],
		"functions": [],
		"queryTypes": []
	}`

	req := httptest.NewRequest("POST", "/api/v2/ontologies/import", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if len(repo.ontologies) != 1 {
		t.Fatalf("expected 1 ontology, got %d", len(repo.ontologies))
	}
	if len(repo.objectTypes) != 1 {
		t.Fatalf("expected 1 objectType, got %d", len(repo.objectTypes))
	}
}

func TestImportOntologyV2_InvalidMode(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/import", handler.ImportOntologyV2)

	body := `{"mode": "invalid", "ontology": {"apiName": "test"}}`
	req := httptest.NewRequest("POST", "/api/v2/ontologies/import", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestImportOntologyV2_MissingAPIName(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/import", handler.ImportOntologyV2)

	body := `{"mode": "merge", "ontology": {"displayName": "No API Name"}}`
	req := httptest.NewRequest("POST", "/api/v2/ontologies/import", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestImportOntologyV2_InvalidJSON(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/import", handler.ImportOntologyV2)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/import", strings.NewReader("{invalid"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestImportOntologyV2_RIDRemapping_FunctionRID(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/import", handler.ImportOntologyV2)

	body := `{
		"mode": "merge",
		"ontology": {"apiName": "remap", "displayName": "RID Remap Test"},
		"objectTypes": [],
		"linkTypes": [],
		"actionTypes": [
			{"rid": "old-at-1", "apiName": "runCalc", "displayName": "Run Calc", "status": "ACTIVE",
			 "functionRid": "old-fn-1", "isFunctionBacked": true}
		],
		"interfaces": [
			{"rid": "old-iface-parent", "apiName": "Parent", "displayName": "Parent Interface"},
			{"rid": "old-iface-child", "apiName": "Child", "displayName": "Child Interface",
			 "extendsRid": "old-iface-parent"}
		],
		"sharedProperties": [],
		"valueTypes": [],
		"typeGroups": [],
		"functions": [
			{"rid": "old-fn-1", "name": "calcTotal", "version": "1.0.0", "sourceCode": "return 42;", "createdBy": "test"}
		],
		"queryTypes": [
			{"rid": "old-qt-1", "apiName": "findThings", "displayName": "Find", "functionRid": "old-fn-1"}
		]
	}`

	req := httptest.NewRequest("POST", "/api/v2/ontologies/import", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Function created with new RID
	if len(repo.functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(repo.functions))
	}
	newFnRID := repo.functions[0].RID
	if newFnRID == "old-fn-1" {
		t.Error("expected new RID for function, got old")
	}

	// ActionType.FunctionRID should be remapped to new function RID
	if len(repo.actionTypes) != 1 {
		t.Fatalf("expected 1 actionType, got %d", len(repo.actionTypes))
	}
	if repo.actionTypes[0].FunctionRID != newFnRID {
		t.Errorf("expected ActionType.FunctionRID to be remapped to %q, got %q", newFnRID, repo.actionTypes[0].FunctionRID)
	}

	// QueryType.FunctionRID should be remapped too
	if len(repo.queryTypes) != 1 {
		t.Fatalf("expected 1 queryType, got %d", len(repo.queryTypes))
	}
	if repo.queryTypes[0].FunctionRID != newFnRID {
		t.Errorf("expected QueryType.FunctionRID to be remapped to %q, got %q", newFnRID, repo.queryTypes[0].FunctionRID)
	}

	// Interface.ExtendsRID should be remapped
	if len(repo.interfaces) != 2 {
		t.Fatalf("expected 2 interfaces, got %d", len(repo.interfaces))
	}
	var child *oms.Interface
	for i := range repo.interfaces {
		if repo.interfaces[i].APIName == "Child" {
			child = &repo.interfaces[i]
		}
	}
	if child == nil {
		t.Fatal("expected Child interface")
	}
	parentRID := ""
	for i := range repo.interfaces {
		if repo.interfaces[i].APIName == "Parent" {
			parentRID = repo.interfaces[i].RID
		}
	}
	if child.ExtendsRID != parentRID {
		t.Errorf("expected Child.ExtendsRID to be remapped to %q, got %q", parentRID, child.ExtendsRID)
	}
}

func TestImportOntologyV2_ExportImportRoundtrip(t *testing.T) {
	// Set up source ontology with data
	sourceRepo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.src", APIName: "source", DisplayName: "Source Ontology"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.objectType.1", OntologyRID: "ri.ontology.main.ontology.src", APIName: "Employee", DisplayName: "Employee", PrimaryKey: "id", Status: "ACTIVE", Visibility: "NORMAL"},
			{RID: "ri.ontology.main.objectType.2", OntologyRID: "ri.ontology.main.ontology.src", APIName: "Department", DisplayName: "Department", PrimaryKey: "deptId", Status: "ACTIVE", Visibility: "NORMAL"},
		},
		properties: []oms.Property{
			{RID: "ri.ontology.main.property.1", ObjectTypeRID: "ri.ontology.main.objectType.1", APIName: "name", DisplayName: "Name", BaseType: "string"},
			{RID: "ri.ontology.main.property.2", ObjectTypeRID: "ri.ontology.main.objectType.1", APIName: "age", DisplayName: "Age", BaseType: "integer"},
			{RID: "ri.ontology.main.property.3", ObjectTypeRID: "ri.ontology.main.objectType.2", APIName: "deptName", DisplayName: "Dept Name", BaseType: "string"},
		},
		linkTypes: []oms.LinkType{
			{RID: "ri.ontology.main.linkType.1", OntologyRID: "ri.ontology.main.ontology.src", APIName: "worksIn", DisplayName: "Works In", SourceObjectType: "Employee", TargetObjectType: "Department", Cardinality: "MANY_TO_ONE"},
		},
		actionTypes: []oms.ActionType{
			{RID: "ri.ontology.main.actionType.1", OntologyRID: "ri.ontology.main.ontology.src", APIName: "createEmployee", DisplayName: "Create Employee", Status: "ACTIVE"},
		},
		interfaces: []oms.Interface{
			{RID: "ri.ontology.main.interface.1", OntologyRID: "ri.ontology.main.ontology.src", APIName: "HasName", DisplayName: "Has Name"},
		},
	}

	// Step 1: Export source ontology
	exportHandler := oms.NewOMSHandler(sourceRepo)
	exportRouter := chi.NewRouter()
	exportRouter.Get("/api/v2/ontologies/{ontologyApiName}/export", exportHandler.ExportOntologyV2)

	exportReq := httptest.NewRequest("GET", "/api/v2/ontologies/ri.ontology.main.ontology.src/export", nil)
	exportW := httptest.NewRecorder()
	exportRouter.ServeHTTP(exportW, exportReq)

	if exportW.Code != http.StatusOK {
		t.Fatalf("export: expected 200, got %d: %s", exportW.Code, exportW.Body.String())
	}

	// Step 2: Build import request from export data
	var exportData map[string]interface{}
	if err := json.Unmarshal(exportW.Body.Bytes(), &exportData); err != nil {
		t.Fatalf("unmarshal export: %v", err)
	}

	// Change ontology apiName for import to a new ontology
	ontology := exportData["ontology"].(map[string]interface{})
	ontology["apiName"] = "imported-copy"
	exportData["mode"] = "merge"

	importBody, err := json.Marshal(exportData)
	if err != nil {
		t.Fatalf("marshal import body: %v", err)
	}

	// Step 3: Import to a fresh repo
	targetRepo := &mockRepo{}
	importHandler := oms.NewOMSHandler(targetRepo)
	importRouter := chi.NewRouter()
	importRouter.Post("/api/v2/ontologies/import", importHandler.ImportOntologyV2)

	importReq := httptest.NewRequest("POST", "/api/v2/ontologies/import", strings.NewReader(string(importBody)))
	importW := httptest.NewRecorder()
	importRouter.ServeHTTP(importW, importReq)

	if importW.Code != http.StatusCreated {
		t.Fatalf("import: expected 201, got %d: %s", importW.Code, importW.Body.String())
	}

	// Step 4: Verify imported data matches original
	if len(targetRepo.ontologies) != 1 {
		t.Fatalf("expected 1 ontology, got %d", len(targetRepo.ontologies))
	}
	if targetRepo.ontologies[0].APIName != "imported-copy" {
		t.Errorf("expected apiName 'imported-copy', got %q", targetRepo.ontologies[0].APIName)
	}

	if len(targetRepo.objectTypes) != 2 {
		t.Fatalf("expected 2 objectTypes, got %d", len(targetRepo.objectTypes))
	}

	// Verify all objectTypes have correct API names
	otNames := make(map[string]bool)
	for _, ot := range targetRepo.objectTypes {
		otNames[ot.APIName] = true
	}
	if !otNames["Employee"] || !otNames["Department"] {
		t.Errorf("expected Employee and Department, got %v", otNames)
	}

	// Verify properties count matches
	if len(targetRepo.properties) != 3 {
		t.Errorf("expected 3 properties, got %d", len(targetRepo.properties))
	}

	// Verify linkTypes preserved API names
	if len(targetRepo.linkTypes) != 1 {
		t.Fatalf("expected 1 linkType, got %d", len(targetRepo.linkTypes))
	}
	if targetRepo.linkTypes[0].APIName != "worksIn" {
		t.Errorf("expected linkType 'worksIn', got %q", targetRepo.linkTypes[0].APIName)
	}
	if targetRepo.linkTypes[0].SourceObjectType != "Employee" {
		t.Errorf("expected SourceObjectType 'Employee', got %q", targetRepo.linkTypes[0].SourceObjectType)
	}

	if len(targetRepo.actionTypes) != 1 {
		t.Errorf("expected 1 actionType, got %d", len(targetRepo.actionTypes))
	}

	if len(targetRepo.interfaces) != 1 {
		t.Errorf("expected 1 interface, got %d", len(targetRepo.interfaces))
	}

	// All RIDs should be NEW (not matching source)
	for _, ot := range targetRepo.objectTypes {
		if ot.RID == "ri.ontology.main.objectType.1" || ot.RID == "ri.ontology.main.objectType.2" {
			t.Errorf("imported objectType should have new RID, got source RID %q", ot.RID)
		}
	}
}

func TestImportOntologyV2_AllEntityTypes(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/import", handler.ImportOntologyV2)

	body := `{
		"mode": "merge",
		"ontology": {"apiName": "full", "displayName": "Full Import"},
		"objectTypes": [
			{"rid": "old-ot", "apiName": "Widget", "displayName": "Widget", "primaryKey": "id", "status": "ACTIVE", "visibility": "NORMAL"}
		],
		"linkTypes": [
			{"rid": "old-lt", "apiName": "contains", "displayName": "Contains", "objectTypeApiName": "Widget", "linkedObjectTypeApiName": "Widget", "cardinality": "ONE_TO_MANY"}
		],
		"actionTypes": [
			{"rid": "old-at", "apiName": "createWidget", "displayName": "Create Widget", "status": "ACTIVE"}
		],
		"interfaces": [
			{"rid": "old-if", "apiName": "Describable", "displayName": "Describable"}
		],
		"sharedProperties": [
			{"rid": "old-sp", "apiName": "email", "displayName": "Email", "baseType": "string"}
		],
		"valueTypes": [
			{"rid": "old-vt", "apiName": "EmailAddress", "displayName": "Email Address", "baseType": "string", "version": 1}
		],
		"typeGroups": [
			{"rid": "old-tg", "apiName": "Widgets", "displayName": "Widgets"}
		],
		"functions": [
			{"rid": "old-fn", "name": "validateEmail", "version": "1.0.0", "sourceCode": "return true;", "createdBy": "test"}
		],
		"queryTypes": [
			{"rid": "old-qt", "apiName": "findWidgets", "displayName": "Find Widgets"}
		]
	}`

	req := httptest.NewRequest("POST", "/api/v2/ontologies/import", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &result)
	imported := result["imported"].(map[string]interface{})

	checks := map[string]struct {
		count    int
		expected float64
	}{
		"objectTypes":      {len(repo.objectTypes), 1},
		"linkTypes":        {len(repo.linkTypes), 1},
		"actionTypes":      {len(repo.actionTypes), 1},
		"interfaces":       {len(repo.interfaces), 1},
		"sharedProperties": {len(repo.sharedProperties), 1},
		"valueTypes":       {len(repo.valueTypes), 1},
		"typeGroups":       {len(repo.typeGroups), 1},
		"functions":        {len(repo.functions), 1},
		"queryTypes":       {len(repo.queryTypes), 1},
	}

	for name, check := range checks {
		if check.count != int(check.expected) {
			t.Errorf("expected %d %s in repo, got %d", int(check.expected), name, check.count)
		}
		if imported[name].(float64) != check.expected {
			t.Errorf("expected %v %s in import counts, got %v", check.expected, name, imported[name])
		}
	}
}
