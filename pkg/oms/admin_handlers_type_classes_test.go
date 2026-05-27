package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// VTX-010: LinkType.TypeClasses round-trips through admin handlers
// (POST + PUT + GET) and the in-memory mock repo persists the field.
// Each Vertex graph LinkType must be tagged with one of:
//   - vertex:link_primary_direction
//   - vertex:link_undirectional
//   - vertex:link_bidirectional
// which the front-end uses to pick edge arrow style.

func TestCreateLinkType_Given_TypeClasses_When_POST_Then_Persisted(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	r := newLinkTypeRouter(oms.NewOMSHandler(repo))

	body := `{
		"apiName":                 "parentChild",
		"displayName":             "Parent Child",
		"objectTypeApiName":       "parent",
		"linkedObjectTypeApiName": "child",
		"cardinality":             "ONE_TO_MANY",
		"typeClasses":             ["vertex:link_primary_direction"]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/linkTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	classes, ok := resp["typeClasses"].([]interface{})
	if !ok {
		t.Fatalf("expected typeClasses array on response, got %v", resp["typeClasses"])
	}
	if len(classes) != 1 || classes[0] != "vertex:link_primary_direction" {
		t.Errorf("expected typeClasses=[vertex:link_primary_direction], got %v", classes)
	}
	if len(repo.linkTypes) != 1 {
		t.Fatalf("expected 1 link type persisted, got %d", len(repo.linkTypes))
	}
	got := repo.linkTypes[0].TypeClasses
	if len(got) != 1 || got[0] != "vertex:link_primary_direction" {
		t.Errorf("stored TypeClasses = %v, want [vertex:link_primary_direction]", got)
	}
}

func TestCreateLinkType_Given_UnknownTypeClass_When_POST_Then_400(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	r := newLinkTypeRouter(oms.NewOMSHandler(repo))

	body := `{
		"apiName":                 "parentChild",
		"displayName":             "Parent Child",
		"objectTypeApiName":       "parent",
		"linkedObjectTypeApiName": "child",
		"cardinality":             "ONE_TO_MANY",
		"typeClasses":             ["vertex:bogus_class"]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/linkTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown type class, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateLinkType_Given_NoTypeClasses_When_POST_Then_OmittedOnWire(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	r := newLinkTypeRouter(oms.NewOMSHandler(repo))

	body := `{
		"apiName":                 "parentChild",
		"displayName":             "Parent Child",
		"objectTypeApiName":       "parent",
		"linkedObjectTypeApiName": "child",
		"cardinality":             "ONE_TO_MANY"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/linkTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	// omitempty: empty/nil slice should NOT appear on the wire.
	if _, present := resp["typeClasses"]; present {
		t.Errorf("expected typeClasses omitted when empty, got %v", resp["typeClasses"])
	}
}

func TestUpdateLinkType_Given_TypeClassesOmitted_When_PUT_Then_Preserved(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	existing := oms.LinkType{
		RID:              "ri.lt.x",
		OntologyRID:      ontRID,
		APIName:          "x",
		DisplayName:      "X",
		SourceObjectType: "a",
		TargetObjectType: "b",
		Cardinality:      "ONE_TO_MANY",
		TypeClasses:      []string{"vertex:link_bidirectional"},
	}
	repo.linkTypes = append(repo.linkTypes, existing)
	r := newLinkTypeRouter(oms.NewOMSHandler(repo))

	// Omit typeClasses entirely — must NOT silently clear.
	body := `{"displayName":"renamed"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/linkTypes/"+existing.RID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got := repo.linkTypes[0].TypeClasses
	if len(got) != 1 || got[0] != "vertex:link_bidirectional" {
		t.Errorf("expected TypeClasses preserved as [vertex:link_bidirectional], got %v", got)
	}
}

func TestUpdateLinkType_Given_TypeClassesExplicitEmpty_When_PUT_Then_Cleared(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	existing := oms.LinkType{
		RID:              "ri.lt.x",
		OntologyRID:      ontRID,
		APIName:          "x",
		DisplayName:      "X",
		SourceObjectType: "a",
		TargetObjectType: "b",
		Cardinality:      "ONE_TO_MANY",
		TypeClasses:      []string{"vertex:link_primary_direction"},
	}
	repo.linkTypes = append(repo.linkTypes, existing)
	r := newLinkTypeRouter(oms.NewOMSHandler(repo))

	body := `{"displayName":"X","typeClasses":[]}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/linkTypes/"+existing.RID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got := repo.linkTypes[0].TypeClasses
	if len(got) != 0 {
		t.Errorf("expected TypeClasses cleared after explicit empty array, got %v", got)
	}
}

func TestUpdateLinkType_Given_TypeClassesReplaced_When_PUT_Then_Replaced(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	existing := oms.LinkType{
		RID:              "ri.lt.x",
		OntologyRID:      ontRID,
		APIName:          "x",
		DisplayName:      "X",
		SourceObjectType: "a",
		TargetObjectType: "b",
		Cardinality:      "ONE_TO_MANY",
		TypeClasses:      []string{"vertex:link_undirectional"},
	}
	repo.linkTypes = append(repo.linkTypes, existing)
	r := newLinkTypeRouter(oms.NewOMSHandler(repo))

	body := `{"displayName":"X","typeClasses":["vertex:link_bidirectional"]}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/linkTypes/"+existing.RID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got := repo.linkTypes[0].TypeClasses
	if len(got) != 1 || got[0] != "vertex:link_bidirectional" {
		t.Errorf("expected TypeClasses replaced with [vertex:link_bidirectional], got %v", got)
	}
}

// LinkType.ToWireJSON omits typeClasses when empty and includes it when set.
// Keeps the wire shape backwards compatible.
func TestLinkType_ToWireJSON_Given_NoTypeClasses_Then_Omitted(t *testing.T) {
	lt := &oms.LinkType{
		RID:              "ri.lt.x",
		APIName:          "x",
		DisplayName:      "X",
		SourceObjectType: "a",
		TargetObjectType: "b",
		Cardinality:      "ONE_TO_MANY",
	}
	data, err := lt.ToWireJSON()
	if err != nil {
		t.Fatalf("ToWireJSON: %v", err)
	}
	var wire map[string]interface{}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, present := wire["typeClasses"]; present {
		t.Errorf("expected typeClasses omitted when empty, got %v", wire["typeClasses"])
	}
}

func TestLinkType_ToWireJSON_Given_TypeClassesSet_Then_PresentOnWire(t *testing.T) {
	lt := &oms.LinkType{
		RID:              "ri.lt.x",
		APIName:          "x",
		DisplayName:      "X",
		SourceObjectType: "a",
		TargetObjectType: "b",
		Cardinality:      "ONE_TO_MANY",
		TypeClasses:      []string{"vertex:link_primary_direction"},
	}
	data, err := lt.ToWireJSON()
	if err != nil {
		t.Fatalf("ToWireJSON: %v", err)
	}
	var wire map[string]interface{}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	classes, ok := wire["typeClasses"].([]interface{})
	if !ok {
		t.Fatalf("expected typeClasses array on wire, got %v", wire["typeClasses"])
	}
	if len(classes) != 1 || classes[0] != "vertex:link_primary_direction" {
		t.Errorf("expected typeClasses=[vertex:link_primary_direction] on wire, got %v", classes)
	}
}
