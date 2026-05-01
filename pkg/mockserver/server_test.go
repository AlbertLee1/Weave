package mockserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler_ServesSynthesizedResponse(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleSpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	h, err := NewHandler(spec, Options{})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status field = %v", body["status"])
	}
}

func TestHandler_PathParametersResolveTemplate(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleSpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	h, err := NewHandler(spec, Options{})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/northwind", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["apiName"] != "northwind" {
		t.Errorf("apiName = %v", body["apiName"])
	}
}

func TestHandler_NoContentResponse(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleSpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	h, err := NewHandler(spec, Options{})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/v2/ontologies/foo", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rr.Code)
	}
	if rr.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rr.Body.String())
	}
}

func TestHandler_UnknownRouteReturns404(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleSpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	h, err := NewHandler(spec, Options{})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestHandler_FileLoadedOverrideTakesPrecedence(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleSpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	overrides := []Override{
		{
			Method: "GET",
			Path:   "/api/v2/ontologies/{ontologyApiName}",
			Status: 200,
			Body:   json.RawMessage(`{"apiName":"customized","currentVersion":42}`),
		},
	}
	h, err := NewHandler(spec, Options{Overrides: overrides})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/anything", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["apiName"] != "customized" {
		t.Errorf("apiName = %v, want customized", body["apiName"])
	}
	if body["currentVersion"].(float64) != 42 {
		t.Errorf("currentVersion = %v", body["currentVersion"])
	}
}

func TestHandler_OverrideStatusCustom(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleSpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	overrides := []Override{{
		Method: "GET",
		Path:   "/health",
		Status: 503,
		Body:   json.RawMessage(`{"status":"degraded"}`),
	}}
	h, err := NewHandler(spec, Options{Overrides: overrides})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 503 {
		t.Errorf("status = %d, want 503", rr.Code)
	}
}

func TestHandler_RuntimeOverrideEndpoint(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleSpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	h, err := NewHandler(spec, Options{EnableAdmin: true})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	body, _ := json.Marshal(Override{
		Method: "GET",
		Path:   "/health",
		Status: 200,
		Body:   json.RawMessage(`{"status":"runtime"}`),
	})
	postReq := httptest.NewRequest(http.MethodPost, "/__mock/overrides", bytes.NewReader(body))
	postReq.Header.Set("Content-Type", "application/json")
	postRR := httptest.NewRecorder()
	h.ServeHTTP(postRR, postReq)
	if postRR.Code != http.StatusOK && postRR.Code != http.StatusCreated {
		t.Fatalf("override register status = %d, body=%s", postRR.Code, postRR.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, getReq)
	if getRR.Code != 200 {
		t.Fatalf("status = %d", getRR.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(getRR.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["status"] != "runtime" {
		t.Errorf("status = %v, want runtime", got["status"])
	}
}

func TestHandler_AdminEndpointDisabledByDefault(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleSpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	h, err := NewHandler(spec, Options{})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/__mock/overrides", strings.NewReader("{}"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (admin must opt in)", rr.Code)
	}
}

func TestHandler_OverrideClearedByDelete(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleSpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	h, err := NewHandler(spec, Options{
		EnableAdmin: true,
		Overrides: []Override{{
			Method: "GET", Path: "/health", Status: 503, Body: json.RawMessage(`{"status":"down"}`),
		}},
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/__mock/overrides", strings.NewReader(`{"method":"GET","path":"/health"}`))
	delReq.Header.Set("Content-Type", "application/json")
	delRR := httptest.NewRecorder()
	h.ServeHTTP(delRR, delReq)
	if delRR.Code != http.StatusOK && delRR.Code != http.StatusNoContent {
		t.Fatalf("delete override status = %d", delRR.Code)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, getReq)
	if getRR.Code != 200 {
		t.Errorf("status after delete = %d, want 200", getRR.Code)
	}
}

func TestLoadOverrides_FromJSONFile(t *testing.T) {
	data := []byte(`[
		{"method":"GET","path":"/health","status":200,"body":{"status":"loaded"}}
	]`)
	overrides, err := DecodeOverrides(data)
	if err != nil {
		t.Fatalf("DecodeOverrides: %v", err)
	}
	if len(overrides) != 1 {
		t.Fatalf("count = %d", len(overrides))
	}
	if overrides[0].Method != "GET" || overrides[0].Path != "/health" {
		t.Errorf("first = %#v", overrides[0])
	}
}

func TestLiveServerSmoke(t *testing.T) {
	// End-to-end via a real HTTP listener — ensures handler boots cleanly.
	spec, err := ParseSpec([]byte(sampleSpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	h, err := NewHandler(spec, Options{})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte(`"status"`)) {
		t.Errorf("body lacks status field: %s", body)
	}
}
