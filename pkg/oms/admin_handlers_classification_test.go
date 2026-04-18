package oms_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// US-262: ObjectType / Property classification metadata round-trips through
// admin handlers (POST/PUT) and is persisted on the underlying repository.
// Unknown labels are rejected with a typed 400 so typos never land in the
// column.

func newObjectTypeRouter(handler *oms.OMSHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Post("/api/admin/ontologies/{ontologyApiName}/objectTypes", handler.CreateObjectType)
	r.Put("/api/admin/objectTypes/{objectTypeRid}", handler.UpdateObjectType)
	return r
}

func newPropertyRouter(handler *oms.OMSHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Post("/api/admin/objectTypes/{objectTypeRid}/properties", handler.CreateProperty)
	r.Put("/api/admin/properties/{propertyRid}", handler.UpdateProperty)
	return r
}

func TestCreateObjectType_WithClassification_Persisted(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	r := newObjectTypeRouter(oms.NewOMSHandler(repo))

	body := `{
		"apiName":        "customer",
		"displayName":    "Customer",
		"primaryKey":     "id",
		"classification": "Confidential"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/objectTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if got, _ := resp["classification"].(string); got != "Confidential" {
		t.Errorf("expected classification=Confidential on response, got %v", resp["classification"])
	}
	if len(repo.objectTypes) != 1 {
		t.Fatalf("expected 1 object type persisted, got %d", len(repo.objectTypes))
	}
	if repo.objectTypes[0].Classification != "Confidential" {
		t.Errorf("stored Classification = %q, want Confidential", repo.objectTypes[0].Classification)
	}
}

func TestCreateObjectType_WithoutClassification_DefaultsEmptyAndOmitted(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	r := newObjectTypeRouter(oms.NewOMSHandler(repo))

	body := `{
		"apiName":     "order",
		"displayName": "Order",
		"primaryKey":  "id"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/objectTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if _, present := resp["classification"]; present {
		t.Errorf("expected classification omitted when unset, got %v", resp["classification"])
	}
	if repo.objectTypes[0].Classification != "" {
		t.Errorf("expected stored Classification='', got %q", repo.objectTypes[0].Classification)
	}
}

func TestCreateObjectType_UnknownClassification_Rejected(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	r := newObjectTypeRouter(oms.NewOMSHandler(repo))

	body := `{
		"apiName":        "bad",
		"displayName":    "Bad",
		"primaryKey":     "id",
		"classification": "TopSecret"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/objectTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown classification, got %d: %s", w.Code, w.Body.String())
	}
	if len(repo.objectTypes) != 0 {
		t.Errorf("expected no object types persisted after validation failure, got %d", len(repo.objectTypes))
	}
}

func TestUpdateObjectType_Classification_OmitPreserves(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	repo.objectTypes = append(repo.objectTypes, oms.ObjectType{
		RID:            "ri.ontology.main.object-type.customer",
		OntologyRID:    ontRID,
		APIName:        "customer",
		DisplayName:    "Customer",
		PrimaryKey:     "id",
		Status:         "ACTIVE",
		Visibility:     "NORMAL",
		Classification: "PII",
	})
	r := newObjectTypeRouter(oms.NewOMSHandler(repo))

	// Omit classification entirely — must NOT silently clear it.
	body := `{"displayName":"Customer v2","status":"ACTIVE","visibility":"NORMAL"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/objectTypes/"+repo.objectTypes[0].RID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := repo.objectTypes[0].Classification; got != "PII" {
		t.Errorf("expected Classification preserved as PII after omitted-field PUT, got %q", got)
	}
}

func TestUpdateObjectType_Classification_ExplicitEmptyClears(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	repo.objectTypes = append(repo.objectTypes, oms.ObjectType{
		RID:            "ri.ontology.main.object-type.customer",
		OntologyRID:    ontRID,
		APIName:        "customer",
		DisplayName:    "Customer",
		PrimaryKey:     "id",
		Status:         "ACTIVE",
		Visibility:     "NORMAL",
		Classification: "Secret",
	})
	r := newObjectTypeRouter(oms.NewOMSHandler(repo))

	body := `{"displayName":"Customer","status":"ACTIVE","visibility":"NORMAL","classification":""}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/objectTypes/"+repo.objectTypes[0].RID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := repo.objectTypes[0].Classification; got != "" {
		t.Errorf("expected Classification cleared (empty), got %q", got)
	}
}

func TestUpdateObjectType_Classification_ExplicitValueAssigns(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	repo.objectTypes = append(repo.objectTypes, oms.ObjectType{
		RID:         "ri.ontology.main.object-type.customer",
		OntologyRID: ontRID,
		APIName:     "customer",
		DisplayName: "Customer",
		PrimaryKey:  "id",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	})
	r := newObjectTypeRouter(oms.NewOMSHandler(repo))

	body := `{"displayName":"Customer","status":"ACTIVE","visibility":"NORMAL","classification":"Internal"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/objectTypes/"+repo.objectTypes[0].RID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := repo.objectTypes[0].Classification; got != "Internal" {
		t.Errorf("expected Classification=Internal, got %q", got)
	}
}

func TestUpdateObjectType_Classification_UnknownRejected(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	repo.objectTypes = append(repo.objectTypes, oms.ObjectType{
		RID:            "ri.ontology.main.object-type.customer",
		OntologyRID:    ontRID,
		APIName:        "customer",
		DisplayName:    "Customer",
		PrimaryKey:     "id",
		Status:         "ACTIVE",
		Visibility:     "NORMAL",
		Classification: "Internal",
	})
	r := newObjectTypeRouter(oms.NewOMSHandler(repo))

	body := `{"displayName":"Customer","status":"ACTIVE","visibility":"NORMAL","classification":"SuperSecret"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/objectTypes/"+repo.objectTypes[0].RID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown classification, got %d: %s", w.Code, w.Body.String())
	}
	if got := repo.objectTypes[0].Classification; got != "Internal" {
		t.Errorf("expected prior Classification=Internal preserved after rejection, got %q", got)
	}
}

func TestCreateProperty_WithClassification_Persisted(t *testing.T) {
	repo := &mockRepo{}
	// Seed ObjectType so the CreateProperty handler has a valid parent (the
	// handler doesn't validate this today, but keep the fixture realistic).
	repo.objectTypes = append(repo.objectTypes, oms.ObjectType{
		RID: "ri.ontology.main.object-type.customer", APIName: "customer",
	})
	r := newPropertyRouter(oms.NewOMSHandler(repo))

	body := `{"apiName":"ssn","baseType":"string","classification":"PII"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/objectTypes/"+repo.objectTypes[0].RID+"/properties", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if got, _ := resp["classification"].(string); got != "PII" {
		t.Errorf("expected classification=PII, got %v", resp["classification"])
	}
	if len(repo.properties) != 1 {
		t.Fatalf("expected 1 property persisted, got %d", len(repo.properties))
	}
	if repo.properties[0].Classification != "PII" {
		t.Errorf("stored Classification = %q, want PII", repo.properties[0].Classification)
	}
}

func TestCreateProperty_UnknownClassification_Rejected(t *testing.T) {
	repo := &mockRepo{}
	repo.objectTypes = append(repo.objectTypes, oms.ObjectType{
		RID: "ri.ontology.main.object-type.customer", APIName: "customer",
	})
	r := newPropertyRouter(oms.NewOMSHandler(repo))

	body := `{"apiName":"ssn","baseType":"string","classification":"Sensitive"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/objectTypes/"+repo.objectTypes[0].RID+"/properties", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if len(repo.properties) != 0 {
		t.Errorf("expected no properties persisted on validation failure")
	}
}

func TestUpdateProperty_Classification_TriState(t *testing.T) {
	repo := &mockRepo{}
	repo.properties = append(repo.properties, oms.Property{
		RID:            "ri.ontology.main.property.ssn",
		ObjectTypeRID:  "ri.ontology.main.object-type.customer",
		APIName:        "ssn",
		BaseType:       "string",
		Classification: "PII",
	})
	r := newPropertyRouter(oms.NewOMSHandler(repo))

	// omit -> preserve
	req := httptest.NewRequest(http.MethodPut, "/api/admin/properties/"+repo.properties[0].RID, strings.NewReader(`{"displayName":"SSN"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("omit: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if repo.properties[0].Classification != "PII" {
		t.Errorf("omit: expected PII preserved, got %q", repo.properties[0].Classification)
	}

	// explicit value -> assign
	req = httptest.NewRequest(http.MethodPut, "/api/admin/properties/"+repo.properties[0].RID, strings.NewReader(`{"classification":"Secret"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assign: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if repo.properties[0].Classification != "Secret" {
		t.Errorf("assign: expected Secret, got %q", repo.properties[0].Classification)
	}

	// empty string -> clear
	req = httptest.NewRequest(http.MethodPut, "/api/admin/properties/"+repo.properties[0].RID, strings.NewReader(`{"classification":""}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("clear: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if repo.properties[0].Classification != "" {
		t.Errorf("clear: expected empty Classification, got %q", repo.properties[0].Classification)
	}

	// unknown -> 400
	req = httptest.NewRequest(http.MethodPut, "/api/admin/properties/"+repo.properties[0].RID, strings.NewReader(`{"classification":"Bogus"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
