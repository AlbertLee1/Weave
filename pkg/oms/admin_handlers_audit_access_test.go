package oms_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// US-264: the per-ObjectType AuditDataAccess toggle must round-trip through
// the admin Create and Update handlers, and the Update path must follow the
// pointer tri-state convention (nil = preserve, explicit bool = overwrite).

func TestCreateObjectType_WithAuditDataAccessTrue_Persisted(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	r := newObjectTypeRouter(oms.NewOMSHandler(repo))

	body := `{
		"apiName":         "patient",
		"displayName":     "Patient",
		"primaryKey":      "id",
		"auditDataAccess": true
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/objectTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if got, _ := resp["auditDataAccess"].(bool); !got {
		t.Errorf("expected auditDataAccess=true on response, got %v", resp["auditDataAccess"])
	}
	if len(repo.objectTypes) != 1 {
		t.Fatalf("expected 1 object type persisted, got %d", len(repo.objectTypes))
	}
	if !repo.objectTypes[0].AuditDataAccess {
		t.Errorf("expected stored AuditDataAccess=true, got false")
	}
}

func TestCreateObjectType_AuditDataAccessDefaultsFalseAndOmitted(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	r := newObjectTypeRouter(oms.NewOMSHandler(repo))

	body := `{"apiName":"widget","displayName":"Widget","primaryKey":"id"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ontologies/"+ontRID+"/objectTypes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	// omitempty on the model means false is omitted entirely on the wire.
	if _, present := resp["auditDataAccess"]; present {
		t.Errorf("expected auditDataAccess key omitted when false, got %v", resp["auditDataAccess"])
	}
	if repo.objectTypes[0].AuditDataAccess {
		t.Errorf("expected stored AuditDataAccess=false by default, got true")
	}
}

func TestUpdateObjectType_AuditDataAccess_OmitPreserves(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	repo.objectTypes = append(repo.objectTypes, oms.ObjectType{
		RID:             "ri.ontology.main.object-type.patient",
		OntologyRID:     ontRID,
		APIName:         "patient",
		DisplayName:     "Patient",
		PrimaryKey:      "id",
		Status:          "ACTIVE",
		Visibility:      "NORMAL",
		AuditDataAccess: true,
	})
	r := newObjectTypeRouter(oms.NewOMSHandler(repo))

	// Update that omits auditDataAccess must leave it on.
	body := `{"displayName":"Patient v2","status":"ACTIVE","visibility":"NORMAL"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/objectTypes/"+repo.objectTypes[0].RID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !repo.objectTypes[0].AuditDataAccess {
		t.Errorf("expected AuditDataAccess preserved as true after omit, got false")
	}
}

func TestUpdateObjectType_AuditDataAccess_ExplicitFalseDisables(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	repo.objectTypes = append(repo.objectTypes, oms.ObjectType{
		RID:             "ri.ontology.main.object-type.patient",
		OntologyRID:     ontRID,
		APIName:         "patient",
		DisplayName:     "Patient",
		PrimaryKey:      "id",
		Status:          "ACTIVE",
		Visibility:      "NORMAL",
		AuditDataAccess: true,
	})
	r := newObjectTypeRouter(oms.NewOMSHandler(repo))

	body := `{"displayName":"Patient","status":"ACTIVE","visibility":"NORMAL","auditDataAccess":false}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/objectTypes/"+repo.objectTypes[0].RID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if repo.objectTypes[0].AuditDataAccess {
		t.Errorf("expected AuditDataAccess disabled after explicit false, got true")
	}
}

func TestUpdateObjectType_AuditDataAccess_ExplicitTrueEnables(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedMockOntology(repo)
	repo.objectTypes = append(repo.objectTypes, oms.ObjectType{
		RID:         "ri.ontology.main.object-type.patient",
		OntologyRID: ontRID,
		APIName:     "patient",
		DisplayName: "Patient",
		PrimaryKey:  "id",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	})
	r := newObjectTypeRouter(oms.NewOMSHandler(repo))

	body := `{"displayName":"Patient","status":"ACTIVE","visibility":"NORMAL","auditDataAccess":true}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/objectTypes/"+repo.objectTypes[0].RID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !repo.objectTypes[0].AuditDataAccess {
		t.Errorf("expected AuditDataAccess enabled after explicit true, got false")
	}
}
