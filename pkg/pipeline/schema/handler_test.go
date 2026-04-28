package schema

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
)

func newRouter() *chi.Mux {
	r := chi.NewRouter()
	NewHandler().RegisterRoutes(r)
	return r
}

func withAuth(req *http.Request) *http.Request {
	user := &auth.User{ID: "user:alice"}
	return req.WithContext(auth.WithUser(req.Context(), user))
}

func TestHandler_InferSchema_CSV(t *testing.T) {
	body := InferRequest{
		Format: "csv",
		Sample: "id,name,active\n1,alice,true\n2,bob,false\n",
		Options: InferOptionsIn{
			SampleRows: 100,
		},
	}
	b, _ := json.Marshal(body)
	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/v2/pipelines/schema/infer", bytes.NewReader(b)))
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var res Result
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.RowsScanned != 2 {
		t.Errorf("RowsScanned=%d", res.RowsScanned)
	}
	if len(res.Fields) != 3 {
		t.Errorf("Fields=%d", len(res.Fields))
	}
}

func TestHandler_InferSchema_JSON(t *testing.T) {
	body := InferRequest{
		Format: "json",
		Sample: `[{"a":1,"b":"x"},{"a":2,"b":"y"}]`,
	}
	b, _ := json.Marshal(body)
	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/v2/pipelines/schema/infer", bytes.NewReader(b)))
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_InferSchema_NoAuth(t *testing.T) {
	body := InferRequest{Format: "csv", Sample: "a\n1\n"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/pipelines/schema/infer", bytes.NewReader(b))
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_InferSchema_MissingSample(t *testing.T) {
	body := InferRequest{Format: "csv"}
	b, _ := json.Marshal(body)
	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/v2/pipelines/schema/infer", bytes.NewReader(b)))
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["errorName"] != "MissingSample" {
		t.Errorf("errorName=%v want MissingSample", resp["errorName"])
	}
}

func TestHandler_InferSchema_UnsupportedFormat(t *testing.T) {
	body := InferRequest{Format: "xml", Sample: "<root/>"}
	b, _ := json.Marshal(body)
	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/v2/pipelines/schema/infer", bytes.NewReader(b)))
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["errorName"] != "UnsupportedFormat" {
		t.Errorf("errorName=%v want UnsupportedFormat", resp["errorName"])
	}
}

func TestHandler_InferSchema_DefaultFormat(t *testing.T) {
	body := InferRequest{Sample: "a,b\n1,foo\n2,bar\n"}
	b, _ := json.Marshal(body)
	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/v2/pipelines/schema/infer", bytes.NewReader(b)))
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_InferSchema_TabDelimiter(t *testing.T) {
	body := InferRequest{
		Format:  "csv",
		Sample:  "a\tb\n1\tfoo\n2\tbar\n",
		Options: InferOptionsIn{Delimiter: "\t"},
	}
	b, _ := json.Marshal(body)
	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/v2/pipelines/schema/infer", bytes.NewReader(b)))
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_InferSchema_BadDelimiter(t *testing.T) {
	body := InferRequest{
		Format:  "csv",
		Sample:  "a,b\n1,2\n",
		Options: InferOptionsIn{Delimiter: "ab"},
	}
	b, _ := json.Marshal(body)
	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/v2/pipelines/schema/infer", bytes.NewReader(b)))
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["errorName"] != "InvalidDelimiter" {
		t.Errorf("errorName=%v", resp["errorName"])
	}
}

func TestHandler_InferSchema_HasHeaderFalse(t *testing.T) {
	hdr := false
	body := InferRequest{
		Format:  "csv",
		Sample:  "1,foo\n2,bar\n",
		Options: InferOptionsIn{HasHeader: &hdr},
	}
	b, _ := json.Marshal(body)
	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/v2/pipelines/schema/infer", bytes.NewReader(b)))
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var res Result
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Fields) != 2 || res.Fields[0].Name != "col1" {
		t.Errorf("synthetic naming missing: %#v", res.Fields)
	}
}

func TestHandler_InferSchema_BodyTooLarge(t *testing.T) {
	huge := strings.Repeat("a,b\n1,2\n", 2*1024*1024) // 16MB > cap
	body := InferRequest{Format: "csv", Sample: huge}
	b, _ := json.Marshal(body)
	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/v2/pipelines/schema/infer", bytes.NewReader(b)))
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["errorName"] != "SampleTooLarge" {
		t.Errorf("errorName=%v want SampleTooLarge", resp["errorName"])
	}
}
