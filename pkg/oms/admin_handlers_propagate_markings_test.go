package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// US-261: LinkType.PropagateMarkings round-trips through admin handlers
// (POST + PUT + GET) and the in-memory mock repo persists the field.

func TestCreateLinkType_WithPropagateMarkings_Persisted(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	r := newLinkTypeRouter(oms.NewOMSHandler(repo))

	body := `{
		"apiName":                 "parentChild",
		"displayName":             "Parent Child",
		"objectTypeApiName":       "parent",
		"linkedObjectTypeApiName": "child",
		"cardinality":             "ONE_TO_MANY",
		"propagateMarkings":       true
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/linkTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if got, _ := resp["propagateMarkings"].(bool); !got {
		t.Errorf("expected propagateMarkings=true on response, got %v", resp["propagateMarkings"])
	}
	if len(repo.linkTypes) != 1 {
		t.Fatalf("expected 1 link type persisted, got %d", len(repo.linkTypes))
	}
	if !repo.linkTypes[0].PropagateMarkings {
		t.Errorf("expected stored PropagateMarkings=true, got false")
	}
}

func TestCreateLinkType_WithoutPropagateMarkings_DefaultsFalseAndOmitted(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
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
	// omitempty: false should NOT appear on the wire.
	if _, present := resp["propagateMarkings"]; present {
		t.Errorf("expected propagateMarkings omitted when false, got %v", resp["propagateMarkings"])
	}
	if repo.linkTypes[0].PropagateMarkings {
		t.Errorf("expected stored PropagateMarkings=false")
	}
}

func TestUpdateLinkType_PropagateMarkings_TriStateOmitPreserves(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	existing := oms.LinkType{
		RID:               "ri.lt.x",
		OntologyRID:       ontRID,
		APIName:           "x",
		DisplayName:       "X",
		SourceObjectType:  "a",
		TargetObjectType:  "b",
		Cardinality:       "ONE_TO_MANY",
		PropagateMarkings: true,
	}
	repo.linkTypes = append(repo.linkTypes, existing)
	r := newLinkTypeRouter(oms.NewOMSHandler(repo))

	// Omit propagateMarkings entirely — must NOT silently disable it.
	body := `{"displayName":"renamed"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/linkTypes/"+existing.RID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !repo.linkTypes[0].PropagateMarkings {
		t.Errorf("expected PropagateMarkings preserved as true after omitted-field PUT, got false")
	}
}

func TestUpdateLinkType_PropagateMarkings_ExplicitFalseDisables(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	existing := oms.LinkType{
		RID:               "ri.lt.x",
		OntologyRID:       ontRID,
		APIName:           "x",
		DisplayName:       "X",
		SourceObjectType:  "a",
		TargetObjectType:  "b",
		Cardinality:       "ONE_TO_MANY",
		PropagateMarkings: true,
	}
	repo.linkTypes = append(repo.linkTypes, existing)
	r := newLinkTypeRouter(oms.NewOMSHandler(repo))

	body := `{"displayName":"X","propagateMarkings":false}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/linkTypes/"+existing.RID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if repo.linkTypes[0].PropagateMarkings {
		t.Errorf("expected PropagateMarkings=false after explicit false, got true")
	}
}

func TestUpdateLinkType_PropagateMarkings_ExplicitTrueEnables(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	existing := oms.LinkType{
		RID:              "ri.lt.x",
		OntologyRID:      ontRID,
		APIName:          "x",
		DisplayName:      "X",
		SourceObjectType: "a",
		TargetObjectType: "b",
		Cardinality:      "ONE_TO_MANY",
	}
	repo.linkTypes = append(repo.linkTypes, existing)
	r := newLinkTypeRouter(oms.NewOMSHandler(repo))

	body := `{"displayName":"X","propagateMarkings":true}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/linkTypes/"+existing.RID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !repo.linkTypes[0].PropagateMarkings {
		t.Errorf("expected PropagateMarkings=true after explicit true, got false")
	}
}

// LinkType.ToWireJSON omits propagateMarkings when false and includes it
// when true — keeps the wire shape backwards compatible for pre-US-261
// SDKs that key off field presence.
func TestLinkType_ToWireJSON_PropagateMarkings_OmitWhenFalse(t *testing.T) {
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
	if _, present := wire["propagateMarkings"]; present {
		t.Errorf("expected propagateMarkings omitted when false, got %v", wire["propagateMarkings"])
	}
}

func TestLinkType_ToWireJSON_PropagateMarkings_PresentWhenTrue(t *testing.T) {
	lt := &oms.LinkType{
		RID:               "ri.lt.x",
		APIName:           "x",
		DisplayName:       "X",
		SourceObjectType:  "a",
		TargetObjectType:  "b",
		Cardinality:       "ONE_TO_MANY",
		PropagateMarkings: true,
	}
	data, err := lt.ToWireJSON()
	if err != nil {
		t.Fatalf("ToWireJSON: %v", err)
	}
	var wire map[string]interface{}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got, _ := wire["propagateMarkings"].(bool); !got {
		t.Errorf("expected propagateMarkings=true on wire, got %v", wire["propagateMarkings"])
	}
}
