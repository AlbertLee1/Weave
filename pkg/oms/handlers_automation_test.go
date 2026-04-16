package oms_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// --- CreateAutomationRule Tests ---

func TestCreateAutomationRule_Success(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/automationRules", handler.CreateAutomationRule)

	body := `{"name":"daily-sync","description":"Sync data daily","triggerType":"schedule","triggerConfig":{"cron":"0 */6 * * *"},"effects":[{"type":"executeAction","actionTypeApiName":"syncData"}],"createdBy":"admin"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/automationRules", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["name"] != "daily-sync" {
		t.Errorf("name = %v, want %q", resp["name"], "daily-sync")
	}
	if resp["status"] != "active" {
		t.Errorf("status = %v, want %q", resp["status"], "active")
	}
	if resp["triggerType"] != "schedule" {
		t.Errorf("triggerType = %v, want %q", resp["triggerType"], "schedule")
	}
	if resp["ontologyRid"] != "ri.ontology.main.ontology.1" {
		t.Errorf("ontologyRid = %v, want %q", resp["ontologyRid"], "ri.ontology.main.ontology.1")
	}
	if resp["id"] == nil || resp["id"] == "" {
		t.Error("expected id to be generated")
	}
}

func TestCreateAutomationRule_MissingName(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/automationRules", handler.CreateAutomationRule)

	body := `{"triggerType":"schedule"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/automationRules", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestCreateAutomationRule_MissingTriggerType(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/automationRules", handler.CreateAutomationRule)

	body := `{"name":"daily-sync"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/automationRules", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestCreateAutomationRule_InvalidTriggerType(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/automationRules", handler.CreateAutomationRule)

	body := `{"name":"bad","triggerType":"invalid"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/automationRules", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestCreateAutomationRule_OntologyNotFound(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/automationRules", handler.CreateAutomationRule)

	body := `{"name":"daily-sync","triggerType":"schedule"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/nonexistent/automationRules", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

// --- ListAutomationRules Tests ---

func TestListAutomationRules_Success(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		automationRules: []oms.AutomationRule{
			{ID: "rule-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "rule-a", Status: "active", TriggerType: "schedule"},
			{ID: "rule-2", OntologyRID: "ri.ontology.main.ontology.1", Name: "rule-b", Status: "paused", TriggerType: "dataChange"},
			{ID: "rule-3", OntologyRID: "other-ontology", Name: "rule-c", Status: "active", TriggerType: "manual"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/automationRules", handler.ListAutomationRules)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/automationRules", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	data := resp["data"].([]interface{})
	if len(data) != 2 {
		t.Errorf("expected 2 rules, got %d", len(data))
	}
}

func TestListAutomationRules_WithStatusFilter(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
		automationRules: []oms.AutomationRule{
			{ID: "rule-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "rule-a", Status: "active", TriggerType: "schedule"},
			{ID: "rule-2", OntologyRID: "ri.ontology.main.ontology.1", Name: "rule-b", Status: "paused", TriggerType: "dataChange"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/automationRules", handler.ListAutomationRules)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/automationRules?status=active", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	data := resp["data"].([]interface{})
	if len(data) != 1 {
		t.Errorf("expected 1 rule with status=active, got %d", len(data))
	}
	rule := data[0].(map[string]interface{})
	if rule["status"] != "active" {
		t.Errorf("status = %v, want %q", rule["status"], "active")
	}
}

func TestListAutomationRules_Empty(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/automationRules", handler.ListAutomationRules)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/automationRules", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	data := resp["data"].([]interface{})
	if len(data) != 0 {
		t.Errorf("expected empty list, got %d items", len(data))
	}
}

// --- GetAutomationRule Tests ---

func TestGetAutomationRule_Success(t *testing.T) {
	repo := &mockRepo{
		automationRules: []oms.AutomationRule{
			{ID: "rule-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "daily-sync", Status: "active", TriggerType: "schedule", TriggerConfig: json.RawMessage(`{"cron":"0 */6 * * *"}`)},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}", handler.GetAutomationRule)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/automationRules/rule-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["name"] != "daily-sync" {
		t.Errorf("name = %v, want %q", resp["name"], "daily-sync")
	}
	if resp["status"] != "active" {
		t.Errorf("status = %v, want %q", resp["status"], "active")
	}
}

func TestGetAutomationRule_NotFound(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}", handler.GetAutomationRule)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/automationRules/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

// --- UpdateAutomationRule Tests ---

func TestUpdateAutomationRule_Success(t *testing.T) {
	repo := &mockRepo{
		automationRules: []oms.AutomationRule{
			{ID: "rule-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "daily-sync", Status: "active", TriggerType: "schedule"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Put("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}", handler.UpdateAutomationRule)

	body := `{"name":"hourly-sync","description":"Now hourly"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v2/ontologies/test/automationRules/rule-1", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["name"] != "hourly-sync" {
		t.Errorf("name = %v, want %q", resp["name"], "hourly-sync")
	}
	if resp["description"] != "Now hourly" {
		t.Errorf("description = %v, want %q", resp["description"], "Now hourly")
	}
}

func TestUpdateAutomationRule_NotFound(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Put("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}", handler.UpdateAutomationRule)

	body := `{"name":"updated"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v2/ontologies/test/automationRules/nonexistent", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

// --- DeleteAutomationRule Tests ---

func TestDeleteAutomationRule_Success(t *testing.T) {
	repo := &mockRepo{
		automationRules: []oms.AutomationRule{
			{ID: "rule-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "daily-sync", Status: "active", TriggerType: "schedule"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Delete("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}", handler.DeleteAutomationRule)

	req := httptest.NewRequest(http.MethodDelete, "/api/v2/ontologies/test/automationRules/rule-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestDeleteAutomationRule_NotFound(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Delete("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}", handler.DeleteAutomationRule)

	req := httptest.NewRequest(http.MethodDelete, "/api/v2/ontologies/test/automationRules/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

// --- PauseAutomationRule Tests ---

func TestPauseAutomationRule_Success(t *testing.T) {
	repo := &mockRepo{
		automationRules: []oms.AutomationRule{
			{ID: "rule-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "daily-sync", Status: "active", TriggerType: "schedule"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}/pause", handler.PauseAutomationRule)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/automationRules/rule-1/pause", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["status"] != "paused" {
		t.Errorf("status = %v, want %q", resp["status"], "paused")
	}
}

func TestPauseAutomationRule_NotFound(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}/pause", handler.PauseAutomationRule)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/automationRules/nonexistent/pause", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestPauseAutomationRule_AlreadyPaused(t *testing.T) {
	repo := &mockRepo{
		automationRules: []oms.AutomationRule{
			{ID: "rule-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "daily-sync", Status: "paused", TriggerType: "schedule"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}/pause", handler.PauseAutomationRule)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/automationRules/rule-1/pause", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected status 409, got %d; body: %s", w.Code, w.Body.String())
	}
}

// --- ResumeAutomationRule Tests ---

func TestResumeAutomationRule_Success(t *testing.T) {
	repo := &mockRepo{
		automationRules: []oms.AutomationRule{
			{ID: "rule-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "daily-sync", Status: "paused", TriggerType: "schedule"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}/resume", handler.ResumeAutomationRule)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/automationRules/rule-1/resume", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	if resp["status"] != "active" {
		t.Errorf("status = %v, want %q", resp["status"], "active")
	}
}

func TestResumeAutomationRule_NotFound(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}/resume", handler.ResumeAutomationRule)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/automationRules/nonexistent/resume", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestResumeAutomationRule_AlreadyActive(t *testing.T) {
	repo := &mockRepo{
		automationRules: []oms.AutomationRule{
			{ID: "rule-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "daily-sync", Status: "active", TriggerType: "schedule"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}/resume", handler.ResumeAutomationRule)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/automationRules/rule-1/resume", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected status 409, got %d; body: %s", w.Code, w.Body.String())
	}
}

// --- Full CRUD Lifecycle Test ---

func TestAutomationRule_FullLifecycle(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/automationRules", handler.CreateAutomationRule)
	r.Get("/api/v2/ontologies/{ontologyApiName}/automationRules", handler.ListAutomationRules)
	r.Get("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}", handler.GetAutomationRule)
	r.Put("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}", handler.UpdateAutomationRule)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}", handler.DeleteAutomationRule)
	r.Post("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}/pause", handler.PauseAutomationRule)
	r.Post("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}/resume", handler.ResumeAutomationRule)

	// 1. Create
	createBody := `{"name":"daily-sync","triggerType":"schedule","triggerConfig":{"cron":"0 */6 * * *"},"effects":[{"type":"executeAction"}],"createdBy":"admin"}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/automationRules", bytes.NewBufferString(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)

	if createW.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d; body: %s", createW.Code, createW.Body.String())
	}
	createResp := parseJSON(t, createW.Body.Bytes())
	ruleID := createResp["id"].(string)

	// 2. List — should have 1 rule
	listReq := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/automationRules", nil)
	listW := httptest.NewRecorder()
	r.ServeHTTP(listW, listReq)

	if listW.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", listW.Code)
	}
	listResp := parseJSON(t, listW.Body.Bytes())
	data := listResp["data"].([]interface{})
	if len(data) != 1 {
		t.Fatalf("list: expected 1 rule, got %d", len(data))
	}

	// 3. Get
	getReq := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/automationRules/"+ruleID, nil)
	getW := httptest.NewRecorder()
	r.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d; body: %s", getW.Code, getW.Body.String())
	}
	getResp := parseJSON(t, getW.Body.Bytes())
	if getResp["name"] != "daily-sync" {
		t.Errorf("get: name = %v, want %q", getResp["name"], "daily-sync")
	}

	// 4. Update
	updateBody := `{"name":"hourly-sync"}`
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v2/ontologies/test/automationRules/"+ruleID, bytes.NewBufferString(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	r.ServeHTTP(updateW, updateReq)

	if updateW.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d; body: %s", updateW.Code, updateW.Body.String())
	}
	updateResp := parseJSON(t, updateW.Body.Bytes())
	if updateResp["name"] != "hourly-sync" {
		t.Errorf("update: name = %v, want %q", updateResp["name"], "hourly-sync")
	}

	// 5. Pause
	pauseReq := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/automationRules/"+ruleID+"/pause", nil)
	pauseW := httptest.NewRecorder()
	r.ServeHTTP(pauseW, pauseReq)

	if pauseW.Code != http.StatusOK {
		t.Fatalf("pause: expected 200, got %d; body: %s", pauseW.Code, pauseW.Body.String())
	}
	pauseResp := parseJSON(t, pauseW.Body.Bytes())
	if pauseResp["status"] != "paused" {
		t.Errorf("pause: status = %v, want %q", pauseResp["status"], "paused")
	}

	// 6. Resume
	resumeReq := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/automationRules/"+ruleID+"/resume", nil)
	resumeW := httptest.NewRecorder()
	r.ServeHTTP(resumeW, resumeReq)

	if resumeW.Code != http.StatusOK {
		t.Fatalf("resume: expected 200, got %d; body: %s", resumeW.Code, resumeW.Body.String())
	}
	resumeResp := parseJSON(t, resumeW.Body.Bytes())
	if resumeResp["status"] != "active" {
		t.Errorf("resume: status = %v, want %q", resumeResp["status"], "active")
	}

	// 7. Delete
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v2/ontologies/test/automationRules/"+ruleID, nil)
	deleteW := httptest.NewRecorder()
	r.ServeHTTP(deleteW, deleteReq)

	if deleteW.Code != http.StatusNoContent {
		t.Errorf("delete: expected 204, got %d; body: %s", deleteW.Code, deleteW.Body.String())
	}

	// 8. List after delete — should be empty
	listReq2 := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/ri.ontology.main.ontology.1/automationRules", nil)
	listW2 := httptest.NewRecorder()
	r.ServeHTTP(listW2, listReq2)

	listResp2 := parseJSON(t, listW2.Body.Bytes())
	data2 := listResp2["data"].([]interface{})
	if len(data2) != 0 {
		t.Errorf("list after delete: expected 0 rules, got %d", len(data2))
	}
}

// --- ListExecutions Tests ---

func TestListExecutions_Success(t *testing.T) {
	now := time.Now()
	completedAt := now.Add(time.Second)
	repo := &mockRepo{
		automationRules: []oms.AutomationRule{
			{ID: "rule-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "test", Status: "active", TriggerType: "schedule"},
		},
		executions: []oms.AutomationExecution{
			{ID: "exec-1", RuleID: "rule-1", Status: "success", StartedAt: now, CompletedAt: &completedAt},
			{ID: "exec-2", RuleID: "rule-1", Status: "error", Error: "failed", StartedAt: now, CompletedAt: &completedAt, RetryCount: 3},
			{ID: "exec-3", RuleID: "other-rule", Status: "success", StartedAt: now, CompletedAt: &completedAt},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}/executions", handler.ListExecutions)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/automationRules/rule-1/executions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	data := resp["data"].([]interface{})
	if len(data) != 2 {
		t.Fatalf("expected 2 executions for rule-1, got %d", len(data))
	}

	total := resp["total"].(float64)
	if int(total) != 2 {
		t.Fatalf("expected total=2, got %v", total)
	}
}

func TestListExecutions_StatusFilter(t *testing.T) {
	now := time.Now()
	completedAt := now.Add(time.Second)
	repo := &mockRepo{
		automationRules: []oms.AutomationRule{
			{ID: "rule-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "test", Status: "active", TriggerType: "schedule"},
		},
		executions: []oms.AutomationExecution{
			{ID: "exec-1", RuleID: "rule-1", Status: "success", StartedAt: now, CompletedAt: &completedAt},
			{ID: "exec-2", RuleID: "rule-1", Status: "error", Error: "failed", StartedAt: now, CompletedAt: &completedAt},
			{ID: "exec-3", RuleID: "rule-1", Status: "success", StartedAt: now, CompletedAt: &completedAt},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}/executions", handler.ListExecutions)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/automationRules/rule-1/executions?status=error", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	data := resp["data"].([]interface{})
	if len(data) != 1 {
		t.Fatalf("expected 1 execution with status=error, got %d", len(data))
	}
	exec := data[0].(map[string]interface{})
	if exec["status"] != "error" {
		t.Fatalf("expected status 'error', got %v", exec["status"])
	}
}

func TestListExecutions_Pagination(t *testing.T) {
	now := time.Now()
	completedAt := now.Add(time.Second)
	repo := &mockRepo{
		automationRules: []oms.AutomationRule{
			{ID: "rule-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "test", Status: "active", TriggerType: "schedule"},
		},
	}
	for i := 0; i < 10; i++ {
		repo.executions = append(repo.executions, oms.AutomationExecution{
			ID: fmt.Sprintf("exec-%d", i), RuleID: "rule-1", Status: "success", StartedAt: now, CompletedAt: &completedAt,
		})
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}/executions", handler.ListExecutions)

	// Get first page (limit=3, offset=0)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/automationRules/rule-1/executions?limit=3&offset=0", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	data := resp["data"].([]interface{})
	if len(data) != 3 {
		t.Fatalf("expected 3 executions in page, got %d", len(data))
	}
	total := resp["total"].(float64)
	if int(total) != 10 {
		t.Fatalf("expected total=10, got %v", total)
	}

	// Get second page
	req2 := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/automationRules/rule-1/executions?limit=3&offset=3", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	resp2 := parseJSON(t, w2.Body.Bytes())
	data2 := resp2["data"].([]interface{})
	if len(data2) != 3 {
		t.Fatalf("expected 3 executions in page 2, got %d", len(data2))
	}
}

func TestListExecutions_Empty(t *testing.T) {
	repo := &mockRepo{
		automationRules: []oms.AutomationRule{
			{ID: "rule-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "test", Status: "active", TriggerType: "schedule"},
		},
	}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}/executions", handler.ListExecutions)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/automationRules/rule-1/executions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	resp := parseJSON(t, w.Body.Bytes())
	data := resp["data"].([]interface{})
	if len(data) != 0 {
		t.Fatalf("expected empty list, got %d", len(data))
	}
}

func TestListExecutions_RuleNotFound(t *testing.T) {
	repo := &mockRepo{}
	handler := oms.NewOMSHandler(repo)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}/executions", handler.ListExecutions)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/test/automationRules/nonexistent/executions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}
