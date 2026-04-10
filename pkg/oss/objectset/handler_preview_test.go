package objectset_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// --- POST /api/v2/ontologies/{o}/objectSets/loadObjectsMultipleObjectTypes ---

func TestLoadObjectsMultipleObjectTypes_Success(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)

	body := map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type":       "base",
			"objectType": "employee",
		},
		"select": []string{"id", "name"},
	}
	bodyBytes, _ := json.Marshal(body)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjectsMultipleObjectTypes", handler.LoadObjectsMultipleObjectTypes)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/myOntology/objectSets/loadObjectsMultipleObjectTypes?preview=true", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatalf("expected data array, got %T", resp["data"])
	}
	if len(data) != 2 {
		t.Fatalf("expected 2 items, got %d", len(data))
	}

	// Preview wire format uses $ prefix, not __ prefix
	raw := w.Body.String()
	if strings.Contains(raw, "__rid") || strings.Contains(raw, "__primaryKey") || strings.Contains(raw, "__apiName") {
		t.Errorf("preview response must not contain __rid/__primaryKey/__apiName; got: %s", raw)
	}
	if !strings.Contains(raw, "$rid") || !strings.Contains(raw, "$primaryKey") || !strings.Contains(raw, "$apiName") {
		t.Errorf("preview response must contain $rid/$primaryKey/$apiName; got: %s", raw)
	}

	first, ok := data[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected first item to be object, got %T", data[0])
	}
	if first["$apiName"] != "employee" {
		t.Errorf("expected $apiName=employee, got %v", first["$apiName"])
	}
	if _, ok := first["$primaryKey"]; !ok {
		t.Errorf("expected $primaryKey in item")
	}
	if _, ok := first["$rid"]; !ok {
		t.Errorf("expected $rid in item")
	}
}

func TestLoadObjectsMultipleObjectTypes_PreviewRequired(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)

	body := map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type":       "base",
			"objectType": "employee",
		},
		"select": []string{"id", "name"},
	}
	bodyBytes, _ := json.Marshal(body)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjectsMultipleObjectTypes", handler.LoadObjectsMultipleObjectTypes)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/myOntology/objectSets/loadObjectsMultipleObjectTypes", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without preview=true, got %d", w.Code)
	}
}

func TestLoadObjectsMultipleObjectTypes_SelectRequired(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)

	body := map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type":       "base",
			"objectType": "employee",
		},
	}
	bodyBytes, _ := json.Marshal(body)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjectsMultipleObjectTypes", handler.LoadObjectsMultipleObjectTypes)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/myOntology/objectSets/loadObjectsMultipleObjectTypes?preview=true", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when select is missing, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLoadObjectsMultipleObjectTypes_MissingObjectSet(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)

	body := map[string]interface{}{
		"select": []string{"id"},
	}
	bodyBytes, _ := json.Marshal(body)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjectsMultipleObjectTypes", handler.LoadObjectsMultipleObjectTypes)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/myOntology/objectSets/loadObjectsMultipleObjectTypes?preview=true", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// --- POST /api/v2/ontologies/{o}/objectSets/loadObjectsOrInterfaces ---

func TestLoadObjectsOrInterfaces_Success(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)

	body := map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type":       "base",
			"objectType": "employee",
		},
		"select": []string{"id", "name"},
	}
	bodyBytes, _ := json.Marshal(body)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjectsOrInterfaces", handler.LoadObjectsOrInterfaces)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/myOntology/objectSets/loadObjectsOrInterfaces?preview=true", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	raw := w.Body.String()
	if strings.Contains(raw, "__rid") {
		t.Errorf("preview response must not contain __rid; got: %s", raw)
	}
	if !strings.Contains(raw, "$apiName") {
		t.Errorf("preview response must contain $apiName; got: %s", raw)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatalf("expected data array, got %T", resp["data"])
	}
	if len(data) != 2 {
		t.Fatalf("expected 2 items, got %d", len(data))
	}
}

func TestLoadObjectsOrInterfaces_PreviewRequired(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)

	body := map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type":       "base",
			"objectType": "employee",
		},
		"select": []string{"id"},
	}
	bodyBytes, _ := json.Marshal(body)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjectsOrInterfaces", handler.LoadObjectsOrInterfaces)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/myOntology/objectSets/loadObjectsOrInterfaces", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without preview=true, got %d", w.Code)
	}
}

func TestLoadObjectsOrInterfaces_SelectRequired(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)

	body := map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type":       "base",
			"objectType": "employee",
		},
	}
	bodyBytes, _ := json.Marshal(body)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjectsOrInterfaces", handler.LoadObjectsOrInterfaces)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/myOntology/objectSets/loadObjectsOrInterfaces?preview=true", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
