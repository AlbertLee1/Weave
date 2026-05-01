package mockserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSpec_LoadsRealOpenAPI(t *testing.T) {
	data, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Skipf("real spec unavailable: %v", err)
	}
	spec, err := ParseSpec(data)
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	if len(spec.Operations) < 50 {
		t.Errorf("operations = %d, expected dozens for the real spec", len(spec.Operations))
	}

	h, err := NewHandler(spec, Options{})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "status") {
		t.Errorf("body missing status field: %s", rr.Body.String())
	}
}
