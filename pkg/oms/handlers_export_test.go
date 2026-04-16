package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

func TestExportOntologyV2_Success(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test Ontology"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.objectType.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Employee", DisplayName: "Employee"},
		},
		properties: []oms.Property{
			{RID: "ri.ontology.main.property.1", ObjectTypeRID: "ri.ontology.main.objectType.1", APIName: "name", DisplayName: "Name"},
		},
		linkTypes: []oms.LinkType{
			{RID: "ri.ontology.main.linkType.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "manages"},
		},
		actionTypes: []oms.ActionType{
			{RID: "ri.ontology.main.actionType.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "createEmployee"},
		},
		interfaces: []oms.Interface{
			{RID: "ri.ontology.main.interface.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "HasName"},
		},
		sharedProperties: []oms.SharedProperty{
			{RID: "ri.ontology.main.sharedProperty.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "email"},
		},
		valueTypes: []oms.ValueType{
			{RID: "ri.ontology.main.valueType.1", APIName: "EmailAddress"},
		},
		typeGroups: []oms.TypeGroup{
			{RID: "ri.ontology.main.typeGroup.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "People"},
		},
		functions: []oms.Function{
			{RID: "ri.ontology.main.function.1", OntologyRID: "ri.ontology.main.ontology.1", Name: "calcTotal"},
		},
		queryTypes: []oms.QueryType{
			{RID: "ri.ontology.main.queryType.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "listEmployees"},
		},
	}

	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/export", handler.ExportOntologyV2)

	req := httptest.NewRequest("GET", "/api/v2/ontologies/ri.ontology.main.ontology.1/export", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var export oms.OntologyExport
	if err := json.Unmarshal(w.Body.Bytes(), &export); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if export.Ontology.APIName != "test" {
		t.Errorf("expected ontology apiName 'test', got %q", export.Ontology.APIName)
	}
	if len(export.ObjectTypes) != 1 || export.ObjectTypes[0].APIName != "Employee" {
		t.Errorf("expected 1 objectType 'Employee', got %v", export.ObjectTypes)
	}
	if len(export.ObjectTypes[0].Properties) != 1 {
		t.Errorf("expected 1 property on Employee, got %d", len(export.ObjectTypes[0].Properties))
	}
	if len(export.LinkTypes) != 1 {
		t.Errorf("expected 1 linkType, got %d", len(export.LinkTypes))
	}
	if len(export.ActionTypes) != 1 {
		t.Errorf("expected 1 actionType, got %d", len(export.ActionTypes))
	}
	if len(export.Interfaces) != 1 {
		t.Errorf("expected 1 interface, got %d", len(export.Interfaces))
	}
	if len(export.SharedProperties) != 1 {
		t.Errorf("expected 1 sharedProperty, got %d", len(export.SharedProperties))
	}
	if len(export.ValueTypes) != 1 {
		t.Errorf("expected 1 valueType, got %d", len(export.ValueTypes))
	}
	if len(export.TypeGroups) != 1 {
		t.Errorf("expected 1 typeGroup, got %d", len(export.TypeGroups))
	}
	if len(export.Functions) != 1 {
		t.Errorf("expected 1 function, got %d", len(export.Functions))
	}
	if len(export.QueryTypes) != 1 {
		t.Errorf("expected 1 queryType, got %d", len(export.QueryTypes))
	}
}

func TestExportOntologyV2_OntologyNotFound(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/export", handler.ExportOntologyV2)

	req := httptest.NewRequest("GET", "/api/v2/ontologies/nonexistent/export", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestExportOntologyV2_EmptyOntology(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.empty", APIName: "empty", DisplayName: "Empty"},
		},
	}

	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/export", handler.ExportOntologyV2)

	req := httptest.NewRequest("GET", "/api/v2/ontologies/ri.ontology.main.ontology.empty/export", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var export oms.OntologyExport
	if err := json.Unmarshal(w.Body.Bytes(), &export); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// All arrays should be empty (not null)
	if export.ObjectTypes == nil {
		t.Error("expected non-nil objectTypes array")
	}
	if export.LinkTypes == nil {
		t.Error("expected non-nil linkTypes array")
	}
	if export.ActionTypes == nil {
		t.Error("expected non-nil actionTypes array")
	}
	if export.Interfaces == nil {
		t.Error("expected non-nil interfaces array")
	}
	if export.SharedProperties == nil {
		t.Error("expected non-nil sharedProperties array")
	}
	if export.ValueTypes == nil {
		t.Error("expected non-nil valueTypes array")
	}
	if export.TypeGroups == nil {
		t.Error("expected non-nil typeGroups array")
	}
	if export.Functions == nil {
		t.Error("expected non-nil functions array")
	}
	if export.QueryTypes == nil {
		t.Error("expected non-nil queryTypes array")
	}
}

func TestExportOntologyV2_PropertiesLoadedPerObjectType(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ri.ontology.main.objectType.1", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Employee"},
			{RID: "ri.ontology.main.objectType.2", OntologyRID: "ri.ontology.main.ontology.1", APIName: "Department"},
		},
		properties: []oms.Property{
			{RID: "ri.ontology.main.property.1", ObjectTypeRID: "ri.ontology.main.objectType.1", APIName: "name"},
			{RID: "ri.ontology.main.property.2", ObjectTypeRID: "ri.ontology.main.objectType.1", APIName: "age"},
			{RID: "ri.ontology.main.property.3", ObjectTypeRID: "ri.ontology.main.objectType.2", APIName: "deptName"},
		},
	}

	handler := oms.NewOMSHandler(repo)
	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/export", handler.ExportOntologyV2)

	req := httptest.NewRequest("GET", "/api/v2/ontologies/ri.ontology.main.ontology.1/export", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var export oms.OntologyExport
	if err := json.Unmarshal(w.Body.Bytes(), &export); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(export.ObjectTypes) != 2 {
		t.Fatalf("expected 2 objectTypes, got %d", len(export.ObjectTypes))
	}

	// Find Employee and Department
	var employee, department *oms.ObjectType
	for i := range export.ObjectTypes {
		switch export.ObjectTypes[i].APIName {
		case "Employee":
			employee = &export.ObjectTypes[i]
		case "Department":
			department = &export.ObjectTypes[i]
		}
	}

	if employee == nil || department == nil {
		t.Fatal("expected both Employee and Department object types")
	}

	if len(employee.Properties) != 2 {
		t.Errorf("expected 2 properties on Employee, got %d", len(employee.Properties))
	}
	if len(department.Properties) != 1 {
		t.Errorf("expected 1 property on Department, got %d", len(department.Properties))
	}
}
