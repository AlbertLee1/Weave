package oms_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// --- Mock Repository ---

type mockRepo struct {
	ontologies  []oms.Ontology
	objectTypes []oms.ObjectType
	linkTypes   []oms.LinkType
	actionTypes []oms.ActionType
	properties  []oms.Property
	interfaces  []oms.Interface
	valueTypes  []oms.ValueType
	queryTypes  []oms.QueryType
	actionLogs  []oms.ActionLog

	// Error controls
	createErr error
	getErr    error
	listErr   error
	updateErr error
	deleteErr error
}

func (m *mockRepo) CreateOntology(_ context.Context, o *oms.Ontology) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.ontologies = append(m.ontologies, *o)
	return nil
}

func (m *mockRepo) GetOntology(_ context.Context, ridOrApiName string) (*oms.Ontology, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for i := range m.ontologies {
		if m.ontologies[i].RID == ridOrApiName || m.ontologies[i].APIName == ridOrApiName {
			return &m.ontologies[i], nil
		}
	}
	return nil, oms.ErrNotFound
}

func (m *mockRepo) ListOntologies(_ context.Context) ([]oms.Ontology, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.ontologies, nil
}

func (m *mockRepo) CreateObjectType(_ context.Context, ot *oms.ObjectType) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.objectTypes = append(m.objectTypes, *ot)
	return nil
}

func (m *mockRepo) GetObjectType(_ context.Context, rid string) (*oms.ObjectType, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for i := range m.objectTypes {
		if m.objectTypes[i].RID == rid {
			return &m.objectTypes[i], nil
		}
	}
	return nil, oms.ErrNotFound
}

func (m *mockRepo) GetObjectTypeByAPIName(_ context.Context, ontologyRID, apiNameOrRID string) (*oms.ObjectType, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for i := range m.objectTypes {
		ontologyMatch := m.objectTypes[i].OntologyRID == ontologyRID || m.matchOntologyByApiName(ontologyRID, m.objectTypes[i].OntologyRID)
		entityMatch := m.objectTypes[i].APIName == apiNameOrRID || m.objectTypes[i].RID == apiNameOrRID
		if ontologyMatch && entityMatch {
			return &m.objectTypes[i], nil
		}
	}
	return nil, oms.ErrNotFound
}

func (m *mockRepo) matchOntologyByApiName(identifier, ontologyRID string) bool {
	for _, o := range m.ontologies {
		if o.APIName == identifier && o.RID == ontologyRID {
			return true
		}
	}
	return false
}

func (m *mockRepo) ListObjectTypes(_ context.Context, ontologyRID string) ([]oms.ObjectType, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []oms.ObjectType
	for _, ot := range m.objectTypes {
		if ot.OntologyRID == ontologyRID {
			result = append(result, ot)
		}
	}
	return result, nil
}

func (m *mockRepo) UpdateObjectType(_ context.Context, ot *oms.ObjectType) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	for i := range m.objectTypes {
		if m.objectTypes[i].RID == ot.RID {
			m.objectTypes[i] = *ot
			return nil
		}
	}
	return oms.ErrNotFound
}

func (m *mockRepo) DeleteObjectType(_ context.Context, rid string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	for i := range m.objectTypes {
		if m.objectTypes[i].RID == rid {
			m.objectTypes = append(m.objectTypes[:i], m.objectTypes[i+1:]...)
			return nil
		}
	}
	return oms.ErrNotFound
}

func (m *mockRepo) CreateProperty(_ context.Context, p *oms.Property) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.properties = append(m.properties, *p)
	return nil
}

func (m *mockRepo) ListProperties(_ context.Context, objectTypeRID string) ([]oms.Property, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []oms.Property
	for _, p := range m.properties {
		if p.ObjectTypeRID == objectTypeRID {
			result = append(result, p)
		}
	}
	return result, nil
}

func (m *mockRepo) DeleteProperty(_ context.Context, rid string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	for i := range m.properties {
		if m.properties[i].RID == rid {
			m.properties = append(m.properties[:i], m.properties[i+1:]...)
			return nil
		}
	}
	return oms.ErrNotFound
}

func (m *mockRepo) CreateLinkType(_ context.Context, lt *oms.LinkType) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.linkTypes = append(m.linkTypes, *lt)
	return nil
}

func (m *mockRepo) GetLinkType(_ context.Context, rid string) (*oms.LinkType, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for i := range m.linkTypes {
		if m.linkTypes[i].RID == rid {
			return &m.linkTypes[i], nil
		}
	}
	return nil, oms.ErrNotFound
}

func (m *mockRepo) ListOutgoingLinkTypes(_ context.Context, objectTypeRID string) ([]oms.LinkType, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []oms.LinkType
	for _, lt := range m.linkTypes {
		if lt.SourceObjectType == objectTypeRID {
			result = append(result, lt)
		}
	}
	return result, nil
}

func (m *mockRepo) ListIncomingLinkTypes(_ context.Context, objectTypeRID string) ([]oms.LinkType, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []oms.LinkType
	for _, lt := range m.linkTypes {
		if lt.TargetObjectType == objectTypeRID {
			result = append(result, lt)
		}
	}
	return result, nil
}

func (m *mockRepo) CreateActionType(_ context.Context, at *oms.ActionType) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.actionTypes = append(m.actionTypes, *at)
	return nil
}

func (m *mockRepo) GetActionType(_ context.Context, rid string) (*oms.ActionType, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for i := range m.actionTypes {
		if m.actionTypes[i].RID == rid {
			return &m.actionTypes[i], nil
		}
	}
	return nil, oms.ErrNotFound
}

func (m *mockRepo) GetActionTypeByAPIName(_ context.Context, ontologyRID, apiNameOrRID string) (*oms.ActionType, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for i := range m.actionTypes {
		ontologyMatch := m.actionTypes[i].OntologyRID == ontologyRID || m.matchOntologyByApiName(ontologyRID, m.actionTypes[i].OntologyRID)
		entityMatch := m.actionTypes[i].APIName == apiNameOrRID || m.actionTypes[i].RID == apiNameOrRID
		if ontologyMatch && entityMatch {
			return &m.actionTypes[i], nil
		}
	}
	return nil, oms.ErrNotFound
}

func (m *mockRepo) ListActionTypes(_ context.Context, ontologyRID string) ([]oms.ActionType, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []oms.ActionType
	for _, at := range m.actionTypes {
		if at.OntologyRID == ontologyRID {
			result = append(result, at)
		}
	}
	return result, nil
}

func (m *mockRepo) UpdateActionType(_ context.Context, at *oms.ActionType) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	for i := range m.actionTypes {
		if m.actionTypes[i].RID == at.RID {
			m.actionTypes[i] = *at
			return nil
		}
	}
	return oms.ErrNotFound
}

func (m *mockRepo) CreateInterface(_ context.Context, iface *oms.Interface) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.interfaces = append(m.interfaces, *iface)
	return nil
}

func (m *mockRepo) ListInterfaces(_ context.Context, ontologyRID string) ([]oms.Interface, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []oms.Interface
	for _, i := range m.interfaces {
		if i.OntologyRID == ontologyRID {
			result = append(result, i)
		}
	}
	return result, nil
}

func (m *mockRepo) GetInterface(_ context.Context, rid string) (*oms.Interface, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for i := range m.interfaces {
		if m.interfaces[i].RID == rid {
			return &m.interfaces[i], nil
		}
	}
	return nil, oms.ErrNotFound
}

func (m *mockRepo) GetInterfaceByAPIName(_ context.Context, ontologyRID, apiNameOrRID string) (*oms.Interface, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for i := range m.interfaces {
		ontologyMatch := m.interfaces[i].OntologyRID == ontologyRID || m.matchOntologyByApiName(ontologyRID, m.interfaces[i].OntologyRID)
		entityMatch := m.interfaces[i].APIName == apiNameOrRID || m.interfaces[i].RID == apiNameOrRID
		if ontologyMatch && entityMatch {
			return &m.interfaces[i], nil
		}
	}
	return nil, oms.ErrNotFound
}

func (m *mockRepo) UpdateInterface(_ context.Context, iface *oms.Interface) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	for i := range m.interfaces {
		if m.interfaces[i].RID == iface.RID {
			m.interfaces[i] = *iface
			return nil
		}
	}
	return oms.ErrNotFound
}

func (m *mockRepo) DeleteInterface(_ context.Context, rid string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	for i := range m.interfaces {
		if m.interfaces[i].RID == rid {
			m.interfaces = append(m.interfaces[:i], m.interfaces[i+1:]...)
			return nil
		}
	}
	return oms.ErrNotFound
}

func (m *mockRepo) AttachInterface(_ context.Context, _ *oms.ObjectTypeInterface) error {
	return m.createErr
}

func (m *mockRepo) DetachInterface(_ context.Context, _, _ string) error {
	return m.deleteErr
}

func (m *mockRepo) ListInterfaceObjectTypes(_ context.Context, _ string) ([]oms.ObjectType, error) {
	return nil, nil
}

func (m *mockRepo) ListObjectTypeInterfaces(_ context.Context, _ string) ([]oms.ObjectTypeInterface, error) {
	return nil, nil
}

// SharedProperty stubs
func (m *mockRepo) CreateSharedProperty(_ context.Context, _ *oms.SharedProperty) error { return nil }
func (m *mockRepo) GetSharedProperty(_ context.Context, _ string) (*oms.SharedProperty, error) {
	return nil, oms.ErrNotFound
}
func (m *mockRepo) ListSharedProperties(_ context.Context, _ string) ([]oms.SharedProperty, error) {
	return nil, nil
}
func (m *mockRepo) UpdateSharedProperty(_ context.Context, _ *oms.SharedProperty) error { return nil }
func (m *mockRepo) DeleteSharedProperty(_ context.Context, _ string) error              { return nil }

// TypeGroup stubs
func (m *mockRepo) CreateTypeGroup(_ context.Context, _ *oms.TypeGroup) error { return nil }
func (m *mockRepo) GetTypeGroup(_ context.Context, _ string) (*oms.TypeGroup, error) {
	return nil, oms.ErrNotFound
}
func (m *mockRepo) ListTypeGroups(_ context.Context, _ string) ([]oms.TypeGroup, error) {
	return nil, nil
}
func (m *mockRepo) UpdateTypeGroup(_ context.Context, _ *oms.TypeGroup) error { return nil }
func (m *mockRepo) DeleteTypeGroup(_ context.Context, _ string) error         { return nil }
func (m *mockRepo) AssignTypeGroup(_ context.Context, _, _ string) error      { return nil }
func (m *mockRepo) RemoveTypeGroup(_ context.Context, _, _ string) error      { return nil }
func (m *mockRepo) ListTypeGroupsForObjectType(_ context.Context, _ string) ([]oms.TypeGroup, error) {
	return nil, nil
}

// ValueType methods
func (m *mockRepo) CreateValueType(_ context.Context, _ *oms.ValueType) error { return nil }
func (m *mockRepo) GetValueType(_ context.Context, rid string) (*oms.ValueType, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for i := range m.valueTypes {
		if m.valueTypes[i].RID == rid {
			return &m.valueTypes[i], nil
		}
	}
	return nil, oms.ErrNotFound
}
func (m *mockRepo) GetValueTypeByAPIName(_ context.Context, ridOrApiName string) (*oms.ValueType, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for i := range m.valueTypes {
		if m.valueTypes[i].RID == ridOrApiName || m.valueTypes[i].APIName == ridOrApiName {
			return &m.valueTypes[i], nil
		}
	}
	return nil, oms.ErrNotFound
}
func (m *mockRepo) ListValueTypes(_ context.Context) ([]oms.ValueType, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.valueTypes, nil
}
func (m *mockRepo) UpdateValueType(_ context.Context, _ *oms.ValueType) error { return nil }
func (m *mockRepo) DeleteValueType(_ context.Context, _ string) error         { return nil }

// DatasourceBinding stubs
func (m *mockRepo) CreateDatasourceBinding(_ context.Context, _ *oms.DatasourceBinding) error {
	return nil
}
func (m *mockRepo) GetDatasourceBinding(_ context.Context, _ string) (*oms.DatasourceBinding, error) {
	return nil, oms.ErrNotFound
}
func (m *mockRepo) ListDatasourceBindings(_ context.Context, _ string) ([]oms.DatasourceBinding, error) {
	return nil, nil
}
func (m *mockRepo) UpdateDatasourceBinding(_ context.Context, _ *oms.DatasourceBinding) error {
	return nil
}
func (m *mockRepo) DeleteDatasourceBinding(_ context.Context, _ string) error { return nil }

// QueryType stubs
func (m *mockRepo) CreateQueryType(_ context.Context, _ *oms.QueryType) error { return nil }
func (m *mockRepo) GetQueryType(_ context.Context, rid string) (*oms.QueryType, error) {
	for i := range m.queryTypes {
		if m.queryTypes[i].RID == rid {
			return &m.queryTypes[i], nil
		}
	}
	return nil, oms.ErrNotFound
}
func (m *mockRepo) GetQueryTypeByAPIName(_ context.Context, ontologyIdentifier, apiNameOrRID string) (*oms.QueryType, error) {
	for i := range m.queryTypes {
		ontologyMatch := m.queryTypes[i].OntologyRID == ontologyIdentifier || m.matchOntologyByApiName(ontologyIdentifier, m.queryTypes[i].OntologyRID)
		if ontologyMatch && (m.queryTypes[i].APIName == apiNameOrRID || m.queryTypes[i].RID == apiNameOrRID) {
			return &m.queryTypes[i], nil
		}
	}
	return nil, oms.ErrNotFound
}
func (m *mockRepo) ListQueryTypes(_ context.Context, ontologyIdentifier string) ([]oms.QueryType, error) {
	var out []oms.QueryType
	for _, qt := range m.queryTypes {
		ontologyMatch := qt.OntologyRID == ontologyIdentifier || m.matchOntologyByApiName(ontologyIdentifier, qt.OntologyRID)
		if ontologyMatch {
			out = append(out, qt)
		}
	}
	return out, nil
}
func (m *mockRepo) UpdateQueryType(_ context.Context, _ *oms.QueryType) error { return nil }
func (m *mockRepo) DeleteQueryType(_ context.Context, _ string) error         { return nil }

// ActionLog stubs
func (m *mockRepo) InsertActionLog(_ context.Context, _ *oms.ActionLog) error { return nil }
func (m *mockRepo) ListActionLogs(_ context.Context, _ string, limit, offset int) ([]oms.ActionLog, error) {
	if m.actionLogs == nil {
		return nil, nil
	}
	start := offset
	if start > len(m.actionLogs) {
		return nil, nil
	}
	end := start + limit
	if end > len(m.actionLogs) || limit <= 0 {
		end = len(m.actionLogs)
	}
	return m.actionLogs[start:end], nil
}
func (m *mockRepo) CountActionLogs(_ context.Context, _ string) (int, error) { return 0, nil }

// ObjectHistory stubs (Tier 2.3)
func (m *mockRepo) InsertObjectHistory(_ context.Context, _ *oms.ObjectHistory) error {
	return nil
}
func (m *mockRepo) ListObjectHistory(_ context.Context, _, _ string, _ int) ([]oms.ObjectHistory, error) {
	return nil, nil
}
func (m *mockRepo) GetObjectVersionCount(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}

// Search stubs
func (m *mockRepo) SearchOntologyResources(_ context.Context, _, _ string) ([]oms.SearchResult, error) {
	return nil, nil
}

// Snapshot stubs
func (m *mockRepo) CreateSnapshot(_ context.Context, _ *oms.OntologySnapshot) error { return nil }
func (m *mockRepo) ListSnapshots(_ context.Context, _ string) ([]oms.OntologySnapshot, error) {
	return nil, nil
}
func (m *mockRepo) GetSnapshot(_ context.Context, _ string, _ int) (*oms.OntologySnapshot, error) {
	return nil, oms.ErrNotFound
}
func (m *mockRepo) GetOntologyVersion(_ context.Context, _ string) (int, error)       { return 0, nil }
func (m *mockRepo) IncrementOntologyVersion(_ context.Context, _ string) (int, error) { return 1, nil }

func (m *mockRepo) UpdateOntology(_ context.Context, o *oms.Ontology) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	for i := range m.ontologies {
		if m.ontologies[i].RID == o.RID {
			m.ontologies[i] = *o
			return nil
		}
	}
	return oms.ErrNotFound
}

func (m *mockRepo) GetProperty(_ context.Context, rid string) (*oms.Property, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for i := range m.properties {
		if m.properties[i].RID == rid {
			return &m.properties[i], nil
		}
	}
	return nil, oms.ErrNotFound
}

func (m *mockRepo) UpdateProperty(_ context.Context, p *oms.Property) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	for i := range m.properties {
		if m.properties[i].RID == p.RID {
			m.properties[i] = *p
			return nil
		}
	}
	return oms.ErrNotFound
}

func (m *mockRepo) ListLinkTypes(_ context.Context, ontologyRID string) ([]oms.LinkType, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []oms.LinkType
	for _, lt := range m.linkTypes {
		if lt.OntologyRID == ontologyRID {
			result = append(result, lt)
		}
	}
	return result, nil
}

func (m *mockRepo) UpdateLinkType(_ context.Context, lt *oms.LinkType) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	for i := range m.linkTypes {
		if m.linkTypes[i].RID == lt.RID {
			m.linkTypes[i] = *lt
			return nil
		}
	}
	return oms.ErrNotFound
}

func (m *mockRepo) DeleteLinkType(_ context.Context, rid string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	for i := range m.linkTypes {
		if m.linkTypes[i].RID == rid {
			m.linkTypes = append(m.linkTypes[:i], m.linkTypes[i+1:]...)
			return nil
		}
	}
	return oms.ErrNotFound
}

func (m *mockRepo) DeleteActionType(_ context.Context, rid string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	for i := range m.actionTypes {
		if m.actionTypes[i].RID == rid {
			m.actionTypes = append(m.actionTypes[:i], m.actionTypes[i+1:]...)
			return nil
		}
	}
	return oms.ErrNotFound
}

// --- Helper ---

func parseJSON(t *testing.T, data []byte) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to parse JSON response: %v\nbody: %s", err, string(data))
	}
	return m
}

// --- V2 Read Endpoint Tests (10 tests) ---

func TestListOntologies_Empty(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies", handler.ListOntologies)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := parseJSON(t, w.Body.Bytes())
	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 0 {
		t.Errorf("expected empty array, got %d items", len(data))
	}
}

func TestListOntologies_WithData(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
			{RID: "ri.ontology.main.ontology.2", APIName: "prod", DisplayName: "Production"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies", handler.ListOntologies)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := parseJSON(t, w.Body.Bytes())
	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 2 {
		t.Errorf("expected 2 ontologies, got %d", len(data))
	}

	first := data[0].(map[string]interface{})
	if first["apiName"] != "test" {
		t.Errorf("expected first ontology apiName 'test', got %v", first["apiName"])
	}
}

func TestGetOntology_Found(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test Ontology", Description: "A test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}", handler.GetOntology)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := parseJSON(t, w.Body.Bytes())
	if body["apiName"] != "test" {
		t.Errorf("expected apiName 'test', got %v", body["apiName"])
	}
	if body["displayName"] != "Test Ontology" {
		t.Errorf("expected displayName 'Test Ontology', got %v", body["displayName"])
	}
	if body["rid"] != "ri.ontology.main.ontology.1" {
		t.Errorf("expected rid, got %v", body["rid"])
	}
}

func TestGetOntology_NotFound(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}", handler.GetOntology)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}

	body := parseJSON(t, w.Body.Bytes())
	if body["errorCode"] != "NOT_FOUND" {
		t.Errorf("expected errorCode NOT_FOUND, got %v", body["errorCode"])
	}
}

func TestListObjectTypes_Empty(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes", handler.ListObjectTypes)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/objectTypes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := parseJSON(t, w.Body.Bytes())
	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 0 {
		t.Errorf("expected empty array, got %d items", len(data))
	}
}

func TestListObjectTypes_WithData(t *testing.T) {
	repo := &mockRepo{
		objectTypes: []oms.ObjectType{
			{
				RID: "ri.ontology.main.object-type.1", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "employee", DisplayName: "Employee", PrimaryKey: "id",
				Status: "ACTIVE", Visibility: "NORMAL",
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes", handler.ListObjectTypes)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/objectTypes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := parseJSON(t, w.Body.Bytes())
	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 1 {
		t.Errorf("expected 1 object type, got %d", len(data))
	}
}

func TestGetObjectType_Found(t *testing.T) {
	repo := &mockRepo{
		objectTypes: []oms.ObjectType{
			{
				RID: "ri.ontology.main.object-type.1", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "employee", DisplayName: "Employee", PrimaryKey: "employeeId",
				Status: "ACTIVE", Visibility: "NORMAL",
				Properties: []oms.Property{
					{RID: "ri.ontology.main.property.p1", APIName: "employeeId", BaseType: "integer"},
				},
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}", handler.GetObjectType)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/objectTypes/employee", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := parseJSON(t, w.Body.Bytes())
	if body["apiName"] != "employee" {
		t.Errorf("expected apiName 'employee', got %v", body["apiName"])
	}
	if body["primaryKey"] != "employeeId" {
		t.Errorf("expected primaryKey 'employeeId', got %v", body["primaryKey"])
	}
	// Verify wire format includes properties as map
	props, ok := body["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties to be a map (wire format)")
	}
	if _, ok := props["employeeId"]; !ok {
		t.Error("expected employeeId property in wire format")
	}
}

func TestGetObjectType_NotFound(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}", handler.GetObjectType)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/objectTypes/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}

	body := parseJSON(t, w.Body.Bytes())
	if body["errorCode"] != "NOT_FOUND" {
		t.Errorf("expected errorCode NOT_FOUND, got %v", body["errorCode"])
	}
}

func TestListOutgoingLinkTypes_WithData(t *testing.T) {
	repo := &mockRepo{
		objectTypes: []oms.ObjectType{
			{
				RID: "ri.ontology.main.object-type.src", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "employee", DisplayName: "Employee", PrimaryKey: "id",
				Status: "ACTIVE", Visibility: "NORMAL",
			},
		},
		linkTypes: []oms.LinkType{
			{
				RID: "ri.ontology.main.link-type.lt1", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "employeeDept", DisplayName: "Employee Department",
				SourceObjectType: "ri.ontology.main.object-type.src",
				TargetObjectType: "ri.ontology.main.object-type.tgt",
				Cardinality:      "MANY_TO_ONE",
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/outgoingLinkTypes", handler.ListOutgoingLinkTypes)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/objectTypes/employee/outgoingLinkTypes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := parseJSON(t, w.Body.Bytes())
	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 1 {
		t.Errorf("expected 1 link type, got %d", len(data))
	}

	lt := data[0].(map[string]interface{})
	if lt["apiName"] != "employeeDept" {
		t.Errorf("expected apiName 'employeeDept', got %v", lt["apiName"])
	}
	if lt["cardinality"] != "MANY_TO_ONE" {
		t.Errorf("expected cardinality 'MANY_TO_ONE', got %v", lt["cardinality"])
	}
}

func TestListActionTypes_WithData(t *testing.T) {
	repo := &mockRepo{
		actionTypes: []oms.ActionType{
			{
				RID: "ri.ontology.main.action-type.at1", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "createEmployee", DisplayName: "Create Employee",
				Status: "ACTIVE", Parameters: json.RawMessage(`[]`),
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes", handler.ListActionTypes)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/actionTypes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := parseJSON(t, w.Body.Bytes())
	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(data) != 1 {
		t.Errorf("expected 1 action type, got %d", len(data))
	}

	at := data[0].(map[string]interface{})
	if at["apiName"] != "createEmployee" {
		t.Errorf("expected apiName 'createEmployee', got %v", at["apiName"])
	}
}

// --- Admin CRUD Endpoint Tests (12 tests) ---

func TestCreateOntology_Success(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/admin/ontologies", handler.CreateOntology)

	body := `{"apiName":"my-ontology","displayName":"My Ontology","description":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["apiName"] != "my-ontology" {
		t.Errorf("expected apiName 'my-ontology', got %v", resp["apiName"])
	}
	if resp["displayName"] != "My Ontology" {
		t.Errorf("expected displayName 'My Ontology', got %v", resp["displayName"])
	}
	// RID should be generated
	rid, ok := resp["rid"].(string)
	if !ok || rid == "" {
		t.Error("expected generated RID to be present")
	}
	if !strings.HasPrefix(rid, "ri.ontology.main.ontology.") {
		t.Errorf("expected RID to start with 'ri.ontology.main.ontology.', got %s", rid)
	}

	// Verify it was stored
	if len(repo.ontologies) != 1 {
		t.Errorf("expected 1 ontology in repo, got %d", len(repo.ontologies))
	}
}

func TestCreateOntology_MissingApiName(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/admin/ontologies", handler.CreateOntology)

	body := `{"displayName":"My Ontology"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["errorCode"] != "INVALID_ARGUMENT" {
		t.Errorf("expected errorCode INVALID_ARGUMENT, got %v", resp["errorCode"])
	}
}

func TestCreateOntology_Duplicate(t *testing.T) {
	repo := &mockRepo{
		createErr: errors.Join(oms.ErrDuplicate, errors.New("duplicate key")),
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/admin/ontologies", handler.CreateOntology)

	body := `{"apiName":"my-ontology","displayName":"My Ontology"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected status 409, got %d", w.Code)
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["errorCode"] != "CONFLICT" {
		t.Errorf("expected errorCode CONFLICT, got %v", resp["errorCode"])
	}
}

func TestCreateObjectType_Success(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/admin/ontologies/{ontologyApiName}/objectTypes", handler.CreateObjectType)

	body := `{"apiName":"employee","displayName":"Employee","primaryKey":"employeeId","status":"ACTIVE","visibility":"NORMAL"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/ri.ontology.main.ontology.1/objectTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["apiName"] != "employee" {
		t.Errorf("expected apiName 'employee', got %v", resp["apiName"])
	}
	rid, ok := resp["rid"].(string)
	if !ok || !strings.HasPrefix(rid, "ri.ontology.main.object-type.") {
		t.Errorf("expected valid object-type RID, got %v", resp["rid"])
	}
}

func TestCreateObjectType_MissingFields(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/admin/ontologies/{ontologyApiName}/objectTypes", handler.CreateObjectType)

	// Missing apiName, displayName, and primaryKey
	body := `{"status":"ACTIVE"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/ri.ontology.main.ontology.1/objectTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["errorCode"] != "INVALID_ARGUMENT" {
		t.Errorf("expected errorCode INVALID_ARGUMENT, got %v", resp["errorCode"])
	}
}

func TestUpdateObjectType_Success(t *testing.T) {
	repo := &mockRepo{
		objectTypes: []oms.ObjectType{
			{
				RID: "ri.ontology.main.object-type.1", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "employee", DisplayName: "Employee", PrimaryKey: "id",
				Status: "ACTIVE", Visibility: "NORMAL",
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Put("/api/admin/objectTypes/{objectTypeRid}", handler.UpdateObjectType)

	body := `{"displayName":"Updated Employee","status":"DEPRECATED","visibility":"NORMAL"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/objectTypes/ri.ontology.main.object-type.1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["displayName"] != "Updated Employee" {
		t.Errorf("expected displayName 'Updated Employee', got %v", resp["displayName"])
	}
	if resp["status"] != "DEPRECATED" {
		t.Errorf("expected status 'DEPRECATED', got %v", resp["status"])
	}
}

func TestUpdateObjectType_NotFound(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Put("/api/admin/objectTypes/{objectTypeRid}", handler.UpdateObjectType)

	body := `{"displayName":"Updated","status":"ACTIVE","visibility":"NORMAL"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/objectTypes/nonexistent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestDeleteObjectType_Success(t *testing.T) {
	repo := &mockRepo{
		objectTypes: []oms.ObjectType{
			{
				RID: "ri.ontology.main.object-type.1", OntologyRID: "ri.ontology.main.ontology.1",
				APIName: "employee", DisplayName: "Employee", PrimaryKey: "id",
				Status: "EXPERIMENTAL", Visibility: "NORMAL",
			},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Delete("/api/admin/objectTypes/{objectTypeRid}", handler.DeleteObjectType)

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/objectTypes/ri.ontology.main.object-type.1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", w.Code)
	}

	if len(repo.objectTypes) != 0 {
		t.Errorf("expected 0 object types after delete, got %d", len(repo.objectTypes))
	}
}

func TestDeleteObjectType_NotFound(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Delete("/api/admin/objectTypes/{objectTypeRid}", handler.DeleteObjectType)

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/objectTypes/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestCreateProperty_Success(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/admin/objectTypes/{objectTypeRid}/properties", handler.CreateProperty)

	body := `{"apiName":"fullName","baseType":"string","isNullable":true,"isSearchable":true,"isSortable":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/objectTypes/ri.ontology.main.object-type.1/properties", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["apiName"] != "fullName" {
		t.Errorf("expected apiName 'fullName', got %v", resp["apiName"])
	}
	rid, ok := resp["rid"].(string)
	if !ok || !strings.HasPrefix(rid, "ri.ontology.main.property.") {
		t.Errorf("expected valid property RID, got %v", resp["rid"])
	}
}

// TestCreateProperty_EditOnly (US-026) covers the admin CRUD path for the
// is_edit_only column introduced in migration 000019: a POST body carrying
// editOnly=true must land on the Property struct handed to the repository
// *and* be echoed back on the Create response JSON.
func TestCreateProperty_EditOnly(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/admin/objectTypes/{objectTypeRid}/properties", handler.CreateProperty)

	body := `{"apiName":"notes","baseType":"string","editOnly":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/objectTypes/ri.ontology.main.object-type.1/properties", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	if got, _ := resp["editOnly"].(bool); !got {
		t.Errorf("expected Create response editOnly=true, got %v", resp["editOnly"])
	}
	if len(repo.properties) != 1 {
		t.Fatalf("expected 1 stored property, got %d", len(repo.properties))
	}
	if !repo.properties[0].IsEditOnly {
		t.Errorf("expected stored IsEditOnly=true after POST editOnly:true")
	}
}

// TestCreateProperty_EditOnlyDefaultFalse confirms that omitting editOnly in
// the request body yields a property with IsEditOnly=false (zero value) and
// that editOnly is omitted from the JSON response when false.
func TestCreateProperty_EditOnlyDefaultFalse(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/admin/objectTypes/{objectTypeRid}/properties", handler.CreateProperty)

	body := `{"apiName":"freight","baseType":"double"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/objectTypes/ri.ontology.main.object-type.1/properties", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", w.Code)
	}
	if len(repo.properties) != 1 {
		t.Fatalf("expected one stored property, got %d", len(repo.properties))
	}
	if repo.properties[0].IsEditOnly {
		t.Errorf("expected stored IsEditOnly=false, got true")
	}
}

// TestUpdateProperty_EditOnlyToggle (US-026) covers toggling the editOnly
// flag both on and off via PUT. Because the current schema stores the flag
// as an optional pointer on UpdatePropertyRequest, omitting it must leave
// the stored value untouched while passing false must clear it.
func TestUpdateProperty_EditOnlyToggle(t *testing.T) {
	repo := &mockRepo{
		properties: []oms.Property{{
			RID:           "ri.ontology.main.property.notes",
			ObjectTypeRID: "ri.ontology.main.object-type.1",
			APIName:       "notes",
			BaseType:      "string",
			IsEditOnly:    false,
		}},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Put("/api/admin/properties/{propertyRid}", handler.UpdateProperty)

	// Flip it on.
	req := httptest.NewRequest(http.MethodPut, "/api/admin/properties/ri.ontology.main.property.notes", strings.NewReader(`{"editOnly":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on update-on, got %d: %s", w.Code, w.Body.String())
	}
	if !repo.properties[0].IsEditOnly {
		t.Errorf("expected stored IsEditOnly=true after PUT editOnly:true")
	}

	// Flip it back off.
	req2 := httptest.NewRequest(http.MethodPut, "/api/admin/properties/ri.ontology.main.property.notes", strings.NewReader(`{"editOnly":false}`))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 on update-off, got %d: %s", w2.Code, w2.Body.String())
	}
	if repo.properties[0].IsEditOnly {
		t.Errorf("expected stored IsEditOnly=false after PUT editOnly:false")
	}

	// Omitting editOnly must not flip the flag (idempotent no-op).
	repo.properties[0].IsEditOnly = true
	req3 := httptest.NewRequest(http.MethodPut, "/api/admin/properties/ri.ontology.main.property.notes", strings.NewReader(`{"displayName":"Notes"}`))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200 on update-omit, got %d: %s", w3.Code, w3.Body.String())
	}
	if !repo.properties[0].IsEditOnly {
		t.Errorf("expected stored IsEditOnly unchanged (true) when editOnly omitted from body")
	}
}

func TestCreateLinkType_Success(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/admin/ontologies/{ontologyApiName}/linkTypes", handler.CreateLinkType)

	body := `{"apiName":"employeeDept","displayName":"Employee Department","objectTypeApiName":"employee","linkedObjectTypeApiName":"department","cardinality":"MANY_TO_ONE"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/ri.ontology.main.ontology.1/linkTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["apiName"] != "employeeDept" {
		t.Errorf("expected apiName 'employeeDept', got %v", resp["apiName"])
	}
	rid, ok := resp["rid"].(string)
	if !ok || !strings.HasPrefix(rid, "ri.ontology.main.link-type.") {
		t.Errorf("expected valid link-type RID, got %v", resp["rid"])
	}
}

func TestCreateActionType_Success(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/admin/ontologies/{ontologyApiName}/actionTypes", handler.CreateActionType)

	bodyJSON := map[string]interface{}{
		"apiName":     "createEmployee",
		"displayName": "Create Employee",
		"status":      "ACTIVE",
		"parameters":  []interface{}{map[string]interface{}{"id": "name", "type": "string"}},
		"rules":       []interface{}{},
	}
	bodyBytes, _ := json.Marshal(bodyJSON)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/ri.ontology.main.ontology.1/actionTypes", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["apiName"] != "createEmployee" {
		t.Errorf("expected apiName 'createEmployee', got %v", resp["apiName"])
	}
	rid, ok := resp["rid"].(string)
	if !ok || !strings.HasPrefix(rid, "ri.ontology.main.action-type.") {
		t.Errorf("expected valid action-type RID, got %v", resp["rid"])
	}
}

func (m *mockRepo) CreateSecurityPolicy(_ context.Context, _ *oms.SecurityPolicy) error { return nil }
func (m *mockRepo) GetSecurityPolicy(_ context.Context, _ string) (*oms.SecurityPolicy, error) {
	return nil, nil
}
func (m *mockRepo) ListSecurityPolicies(_ context.Context, _ string) ([]oms.SecurityPolicy, error) {
	return nil, nil
}
func (m *mockRepo) UpdateSecurityPolicy(_ context.Context, _ *oms.SecurityPolicy) error { return nil }
func (m *mockRepo) DeleteSecurityPolicy(_ context.Context, _ string) error              { return nil }
