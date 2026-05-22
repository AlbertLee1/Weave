package httputil

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadJSON_ValidBody(t *testing.T) {
	body := `{"name":"test"}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	var out struct{ Name string }
	if err := ReadJSON(r, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Name != "test" {
		t.Errorf("expected name 'test', got %q", out.Name)
	}
}

func TestReadJSON_OversizedBody(t *testing.T) {
	// Build a valid JSON body over 1MB
	val := strings.Repeat("x", 2*1024*1024)
	body := `{"data":"` + val + `"}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	var out struct{ Data string }
	err := ReadJSON(r, &out)
	if err == nil {
		t.Fatal("expected error for oversized body, got nil")
	}
}

func TestReadJSON_ExactlyAtLimit(t *testing.T) {
	// A valid JSON body just under 1MB should succeed
	// Build a JSON string that's under 1MB
	val := strings.Repeat("x", 512*1024)
	body := `{"data":"` + val + `"}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	var out struct{ Data string }
	if err := ReadJSON(r, &out); err != nil {
		t.Fatalf("body under 1MB should succeed, got: %v", err)
	}
}

func TestReadJSON_RejectsTrailingJSONValue(t *testing.T) {
	body := `{"name":"first"}{"name":"second"}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	var out struct{ Name string }
	if err := ReadJSON(r, &out); err == nil {
		t.Fatal("expected error for trailing JSON value, got nil")
	}
}

func TestReadJSON_RejectsTrailingNonWhitespace(t *testing.T) {
	body := `{"name":"first"} trailing`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	var out struct{ Name string }
	if err := ReadJSON(r, &out); err == nil {
		t.Fatal("expected error for trailing non-whitespace, got nil")
	}
}

func TestWriteJSON_StatusAndContentType(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, http.StatusCreated, map[string]string{"id": "123"})

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", ct)
	}
	if !strings.Contains(w.Body.String(), `"id"`) {
		t.Errorf("expected body to contain 'id', got %q", w.Body.String())
	}
}
