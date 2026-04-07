package actions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// TestHTTPDispatcher_SuccessfulCall verifies the happy path: dispatcher posts
// a JSON envelope to {baseURL}/{functionRID} and returns the converted edits.
func TestHTTPDispatcher_SuccessfulCall(t *testing.T) {
	var receivedPath string
	var receivedBody FunctionRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		resp := FunctionResponse{
			Edits: []FunctionEdit{
				{
					Type:       "CREATE",
					ObjectType: "Employee",
					PrimaryKey: "emp-99",
					Properties: map[string]interface{}{"name": "Alice"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	d := NewHTTPDispatcher(srv.URL)
	at := &oms.ActionType{
		RID:         "ri.ontology.main.action-type.create-employee",
		APIName:     "createEmployee",
		FunctionRID: "ri.functions.main.function.create-employee-fn",
	}
	params := map[string]interface{}{"name": "Alice"}

	edits, err := d.Dispatch(context.Background(), at, params)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if !strings.HasSuffix(receivedPath, "/ri.functions.main.function.create-employee-fn") {
		t.Errorf("expected path to end with functionRID, got %q", receivedPath)
	}
	if receivedBody.ActionTypeRID != at.RID {
		t.Errorf("body actionTypeRid: got %q", receivedBody.ActionTypeRID)
	}
	if receivedBody.ActionTypeAPI != at.APIName {
		t.Errorf("body actionTypeApiName: got %q", receivedBody.ActionTypeAPI)
	}
	if receivedBody.FunctionRID != at.FunctionRID {
		t.Errorf("body functionRid: got %q", receivedBody.FunctionRID)
	}
	if receivedBody.Parameters["name"] != "Alice" {
		t.Errorf("body parameters.name: got %v", receivedBody.Parameters["name"])
	}

	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if edits[0].Type != funnel.EditTypeCreate {
		t.Errorf("edit type: got %s", edits[0].Type)
	}
	if edits[0].PrimaryKey != "emp-99" {
		t.Errorf("primary key: got %s", edits[0].PrimaryKey)
	}
}

// TestHTTPDispatcher_ErrorResponse verifies that a 200 with the Error field
// set is propagated as a Go error (function rejected the call).
func TestHTTPDispatcher_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := FunctionResponse{Error: "permission denied"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	d := NewHTTPDispatcher(srv.URL)
	at := &oms.ActionType{FunctionRID: "fn-1"}
	_, err := d.Dispatch(context.Background(), at, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error from function error field")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("expected error to contain 'permission denied', got %v", err)
	}
}

// TestHTTPDispatcher_Timeout verifies a hung function trips the deadline.
func TestHTTPDispatcher_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow function — sleep longer than the dispatcher timeout.
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(FunctionResponse{})
	}))
	defer srv.Close()

	d := NewHTTPDispatcher(srv.URL)
	d.Timeout = 50 * time.Millisecond

	at := &oms.ActionType{FunctionRID: "fn-slow"}
	_, err := d.Dispatch(context.Background(), at, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// TestHTTPDispatcher_5xxStatus verifies a server-side error is surfaced with
// a status hint so operators can correlate against function logs.
func TestHTTPDispatcher_5xxStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	d := NewHTTPDispatcher(srv.URL)
	at := &oms.ActionType{FunctionRID: "fn-broken"}
	_, err := d.Dispatch(context.Background(), at, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to mention status 500, got %v", err)
	}
}

// TestHTTPDispatcher_InvalidJSON verifies a malformed body is reported
// instead of silently producing zero edits.
func TestHTTPDispatcher_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{not json"))
	}))
	defer srv.Close()

	d := NewHTTPDispatcher(srv.URL)
	at := &oms.ActionType{FunctionRID: "fn-bad-json"}
	_, err := d.Dispatch(context.Background(), at, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected JSON decode error")
	}
}

// TestHTTPDispatcher_PassesHeaders verifies user-supplied headers reach the
// function (used for API keys, tracing, tenant IDs).
func TestHTTPDispatcher_PassesHeaders(t *testing.T) {
	var seenAPIKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAPIKey = r.Header.Get("X-API-Key")
		_ = json.NewEncoder(w).Encode(FunctionResponse{})
	}))
	defer srv.Close()

	d := NewHTTPDispatcher(srv.URL)
	d.Headers = map[string]string{"X-API-Key": "secret-token"}

	at := &oms.ActionType{FunctionRID: "fn-1"}
	if _, err := d.Dispatch(context.Background(), at, map[string]interface{}{}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if seenAPIKey != "secret-token" {
		t.Errorf("expected X-API-Key 'secret-token', got %q", seenAPIKey)
	}
}

// TestHTTPDispatcher_BaseURLTrailingSlash verifies URLs with and without a
// trailing slash join to the same path so config typos don't break dispatch.
func TestHTTPDispatcher_BaseURLTrailingSlash(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewEncoder(w).Encode(FunctionResponse{})
	}))
	defer srv.Close()

	d := NewHTTPDispatcher(srv.URL + "/")
	at := &oms.ActionType{FunctionRID: "fn-x"}
	if _, err := d.Dispatch(context.Background(), at, map[string]interface{}{}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if path != "/fn-x" {
		t.Errorf("expected path /fn-x, got %q", path)
	}
}
