package oss_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBDD_SearchObjectsRejectsTrailingJSONBody_P2A001(t *testing.T) {
	// Given a syntactically valid SearchObjects request followed by a second
	// JSON object, when posted to the OSS API, then the shared decoder must
	// reject the whole body instead of executing the first object only.
	r := setupFacetSearchRouter(t)

	body := `{"select":["articleId"]}{"select":["owner"]}`
	req := httptest.NewRequest(http.MethodPost, facetSearchURL(""), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want %d; body=%s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}

	var apiErr struct {
		ErrorName string `json:"errorName"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("decode error response: %v; body=%s", err, rr.Body.String())
	}
	if apiErr.ErrorName != "InvalidRequestBody" {
		t.Fatalf("errorName=%q want InvalidRequestBody", apiErr.ErrorName)
	}
}
