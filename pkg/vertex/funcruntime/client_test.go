package funcruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestNewClient_Given_BlankBaseURL_When_Construct_Then_Errors confirms
// the constructor rejects empty input rather than producing a silently
// broken Client that fails at first request.
func TestNewClient_Given_BlankBaseURL_When_Construct_Then_Errors(t *testing.T) {
	cases := []string{"", "   ", "\t\n"}
	for _, base := range cases {
		t.Run(fmt.Sprintf("base=%q", base), func(t *testing.T) {
			if _, err := NewClient(base, nil); err == nil {
				t.Fatalf("expected error for baseURL %q", base)
			}
		})
	}
}

// TestNewClient_Given_BadScheme_When_Construct_Then_Errors confirms
// non-http(s) schemes (e.g. file://) are rejected up front.
func TestNewClient_Given_BadScheme_When_Construct_Then_Errors(t *testing.T) {
	cases := []string{
		"file:///etc/passwd",
		"ftp://localhost:9118",
		"://no-scheme",
	}
	for _, base := range cases {
		t.Run(base, func(t *testing.T) {
			if _, err := NewClient(base, nil); err == nil {
				t.Fatalf("expected error for baseURL %q", base)
			}
		})
	}
}

// TestNewClient_Given_MissingHost_When_Construct_Then_Errors covers
// the boundary where Parse succeeds but Host is empty (e.g. "http://").
func TestNewClient_Given_MissingHost_When_Construct_Then_Errors(t *testing.T) {
	if _, err := NewClient("http://", nil); err == nil {
		t.Fatalf("expected error for baseURL without host")
	}
}

// TestNewClient_Given_TrailingSlash_When_Construct_Then_Canonicalised
// confirms the constructor strips a trailing slash so the resulting
// invoke URL has no double-slash.
func TestNewClient_Given_TrailingSlash_When_Construct_Then_Canonicalised(t *testing.T) {
	c, err := NewClient("http://localhost:9118/", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if got := c.BaseURL(); got != "http://localhost:9118" {
		t.Errorf("BaseURL = %q, want %q", got, "http://localhost:9118")
	}
}

// TestNewClient_Given_NilHTTPClient_When_Construct_Then_UsesDefault
// confirms a default 30s timeout is applied when callers pass nil.
func TestNewClient_Given_NilHTTPClient_When_Construct_Then_UsesDefault(t *testing.T) {
	c, err := NewClient("http://localhost:9118", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.httpClient.Timeout != DefaultTimeout {
		t.Errorf("default timeout = %v, want %v", c.httpClient.Timeout, DefaultTimeout)
	}
}

// TestClient_Given_RuntimeReturns200_When_Invoke_Then_DecodesOutput
// exercises the happy path: client POSTs to /invoke with the right
// content-type and body, server replies 200 with {"output": ...}, and
// the client returns the parsed payload.
func TestClient_Given_RuntimeReturns200_When_Invoke_Then_DecodesOutput(t *testing.T) {
	var captured struct {
		method      string
		path        string
		contentType string
		body        []byte
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.contentType = r.Header.Get("Content-Type")
		captured.body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output": {"delay_minutes": 12.5, "category": "minor"}}`))
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	resp, err := c.Invoke(context.Background(), InvokeRequest{
		Function: "predict_delay",
		Inputs:   map[string]interface{}{"distance_km": 1200.0, "weather": "stormy"},
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if captured.method != http.MethodPost {
		t.Errorf("method = %q, want POST", captured.method)
	}
	if captured.path != "/invoke" {
		t.Errorf("path = %q, want /invoke", captured.path)
	}
	if captured.contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", captured.contentType)
	}

	var sent InvokeRequest
	if err := json.Unmarshal(captured.body, &sent); err != nil {
		t.Fatalf("server got non-JSON body: %v\n%s", err, string(captured.body))
	}
	if sent.Function != "predict_delay" {
		t.Errorf("sent function = %q, want predict_delay", sent.Function)
	}
	if sent.Inputs["distance_km"] != 1200.0 {
		t.Errorf("sent inputs.distance_km = %v, want 1200.0", sent.Inputs["distance_km"])
	}

	out, ok := resp.Output.(map[string]interface{})
	if !ok {
		t.Fatalf("output is not a map: %#v", resp.Output)
	}
	if out["category"] != "minor" {
		t.Errorf("output.category = %v, want minor", out["category"])
	}
}

// TestClient_Given_NilInputs_When_Invoke_Then_SendsEmptyObject
// ensures we never marshal `"inputs": null`, which pydantic would
// reject before the user-friendly field-level errors kick in.
func TestClient_Given_NilInputs_When_Invoke_Then_SendsEmptyObject(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"output": null}`))
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, srv.Client())
	if _, err := c.Invoke(context.Background(), InvokeRequest{Function: "noop"}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if !strings.Contains(string(captured), `"inputs":{}`) {
		t.Errorf("expected inputs:{} in body, got %s", string(captured))
	}
}

// TestClient_Given_TypeMismatch_When_Invoke_Then_Returns422WithFieldErrors
// confirms BDD #2: a 422 with FastAPI's standard detail array maps to
// *ValidationError carrying every field-level entry.
func TestClient_Given_TypeMismatch_When_Invoke_Then_Returns422WithFieldErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{
			"detail": [
				{"loc": ["body", "inputs", "distance_km"], "msg": "value is not a valid float", "type": "type_error.float"},
				{"loc": ["body", "inputs", "weather"], "msg": "field required", "type": "value_error.missing"}
			]
		}`))
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, srv.Client())
	_, err := c.Invoke(context.Background(), InvokeRequest{
		Function: "predict_delay",
		Inputs:   map[string]interface{}{"distance_km": "not-a-number"},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}

	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if ve.Function != "predict_delay" {
		t.Errorf("ValidationError.Function = %q", ve.Function)
	}
	if ve.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("StatusCode = %d, want 422", ve.StatusCode)
	}
	if len(ve.Details) != 2 {
		t.Fatalf("Details length = %d, want 2", len(ve.Details))
	}
	if ve.Details[0].Msg != "value is not a valid float" {
		t.Errorf("Details[0].Msg = %q", ve.Details[0].Msg)
	}
	loc := strings.Join(ve.Details[0].Loc, ".")
	if loc != "body.inputs.distance_km" {
		t.Errorf("Details[0].Loc = %q, want body.inputs.distance_km", loc)
	}
	if !strings.Contains(ve.Error(), "distance_km") {
		t.Errorf("Error() should mention failing field, got %q", ve.Error())
	}
}

// TestClient_Given_Malformed422_When_Invoke_Then_StillReturnsValidationError
// covers the path where the runtime emits 422 with an unexpected body
// shape (e.g. plain string detail). We want callers to keep getting
// *ValidationError so their error-handling stays consistent.
func TestClient_Given_Malformed422_When_Invoke_Then_StillReturnsValidationError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"detail": "garbled validation message"}`))
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, srv.Client())
	_, err := c.Invoke(context.Background(), InvokeRequest{Function: "noop"})

	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if len(ve.Details) == 0 || !strings.Contains(ve.Details[0].Msg, "garbled") {
		t.Errorf("expected salvaged detail in Details, got %#v", ve.Details)
	}
}

// TestClient_Given_SandboxViolation_When_Invoke_Then_Returns403
// confirms BDD #3: a 403 with code=ForbiddenFileAccess wraps into
// *SandboxViolationError preserving the runtime-supplied detail.
func TestClient_Given_SandboxViolation_When_Invoke_Then_Returns403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"detail": "path /etc/passwd is outside sandbox", "code": "ForbiddenFileAccess"}`))
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, srv.Client())
	_, err := c.Invoke(context.Background(), InvokeRequest{Function: "leak_secrets"})

	var sb *SandboxViolationError
	if !errors.As(err, &sb) {
		t.Fatalf("expected *SandboxViolationError, got %T: %v", err, err)
	}
	if sb.Code != "ForbiddenFileAccess" {
		t.Errorf("Code = %q, want ForbiddenFileAccess", sb.Code)
	}
	if !strings.Contains(sb.Detail, "/etc/passwd") {
		t.Errorf("Detail = %q, want to mention /etc/passwd", sb.Detail)
	}
	if !strings.Contains(sb.Error(), "/etc/passwd") {
		t.Errorf("Error() = %q should embed detail", sb.Error())
	}
}

// TestClient_Given_ForbiddenExternalCall_When_Invoke_Then_ReturnsTypedError
// covers VTX-055: a 403 with code=ForbiddenExternalCall must surface
// as *ExternalCallForbiddenError, not *SandboxViolationError, so
// callers can distinguish "function tried to read /etc/passwd" from
// "function tried to call a non-allowlisted external service".
func TestClient_Given_ForbiddenExternalCall_When_Invoke_Then_ReturnsTypedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"detail": "forbidden external call: domain not in allowlist: untrusted.example.com", "code": "ForbiddenExternalCall"}`))
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, srv.Client())
	_, err := c.Invoke(context.Background(), InvokeRequest{Function: "call_external"})

	var ex *ExternalCallForbiddenError
	if !errors.As(err, &ex) {
		t.Fatalf("expected *ExternalCallForbiddenError, got %T: %v", err, err)
	}
	if ex.Code != "ForbiddenExternalCall" {
		t.Errorf("Code = %q, want ForbiddenExternalCall", ex.Code)
	}
	if !strings.Contains(ex.Detail, "untrusted.example.com") {
		t.Errorf("Detail = %q, want to mention untrusted.example.com", ex.Detail)
	}
	if !strings.Contains(ex.Error(), "untrusted.example.com") {
		t.Errorf("Error() = %q should embed detail", ex.Error())
	}
	// A SandboxViolationError must NOT match — they're disjoint types
	// at the Go API surface, otherwise the branch is meaningless.
	var sb *SandboxViolationError
	if errors.As(err, &sb) {
		t.Fatalf("ForbiddenExternalCall must not match *SandboxViolationError")
	}
}

// TestClient_Given_403WithoutCode_When_Invoke_Then_SetsDefaultCode
// makes sure a runtime that forgets the code field still produces a
// usable *SandboxViolationError with a non-empty Code.
func TestClient_Given_403WithoutCode_When_Invoke_Then_SetsDefaultCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"detail": "blocked"}`))
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, srv.Client())
	_, err := c.Invoke(context.Background(), InvokeRequest{Function: "x"})

	var sb *SandboxViolationError
	if !errors.As(err, &sb) {
		t.Fatalf("want *SandboxViolationError, got %T: %v", err, err)
	}
	if sb.Code == "" {
		t.Errorf("expected non-empty default Code")
	}
}

// TestClient_Given_UnknownFunction_When_Invoke_Then_Returns404
// covers the registry-miss path so callers can distinguish "unknown
// function" from "runtime is on fire".
func TestClient_Given_UnknownFunction_When_Invoke_Then_Returns404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail": "function 'nope' not registered"}`))
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, srv.Client())
	_, err := c.Invoke(context.Background(), InvokeRequest{Function: "nope"})

	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("expected *NotFoundError, got %T: %v", err, err)
	}
	if !strings.Contains(nf.Detail, "not registered") {
		t.Errorf("Detail = %q", nf.Detail)
	}
}

// TestClient_Given_5xx_When_Invoke_Then_ReturnsRuntimeError covers
// the catch-all: anything we don't have a typed branch for surfaces
// as *RuntimeError with status, code, and detail preserved.
func TestClient_Given_5xx_When_Invoke_Then_ReturnsRuntimeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"detail": "panic: divide by zero", "code": "RuntimePanic"}`))
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, srv.Client())
	_, err := c.Invoke(context.Background(), InvokeRequest{Function: "boom"})

	var rt *RuntimeError
	if !errors.As(err, &rt) {
		t.Fatalf("expected *RuntimeError, got %T: %v", err, err)
	}
	if rt.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d", rt.StatusCode)
	}
	if rt.Code != "RuntimePanic" {
		t.Errorf("Code = %q, want RuntimePanic", rt.Code)
	}
	if !strings.Contains(rt.Detail, "divide by zero") {
		t.Errorf("Detail = %q", rt.Detail)
	}
}

// TestClient_Given_Malformed200_When_Invoke_Then_ReturnsRuntimeError
// catches a class of runtime bug where the function returned but the
// envelope is corrupted in transit. Better to surface as a server-side
// error than to panic in the caller.
func TestClient_Given_Malformed200_When_Invoke_Then_ReturnsRuntimeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<<not json>>`))
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, srv.Client())
	_, err := c.Invoke(context.Background(), InvokeRequest{Function: "x"})

	var rt *RuntimeError
	if !errors.As(err, &rt) {
		t.Fatalf("expected *RuntimeError, got %T: %v", err, err)
	}
	if !strings.Contains(rt.Detail, "malformed 200") {
		t.Errorf("Detail = %q, want to mention malformed", rt.Detail)
	}
}

// TestClient_Given_NetworkFail_When_Invoke_Then_ReturnsTransportError
// confirms wire-level failures (refused connection) wrap into
// *TransportError. We don't reach the runtime at all; there's no
// status code; callers should be able to distinguish this from any
// runtime-side error.
func TestClient_Given_NetworkFail_When_Invoke_Then_ReturnsTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	// Close immediately so dial succeeds at most once then breaks.
	srv.Close()

	c, _ := NewClient(srv.URL, &http.Client{Timeout: 200 * time.Millisecond})
	_, err := c.Invoke(context.Background(), InvokeRequest{Function: "x"})

	var te *TransportError
	if !errors.As(err, &te) {
		t.Fatalf("expected *TransportError, got %T: %v", err, err)
	}
	if te.Function != "x" {
		t.Errorf("TransportError.Function = %q", te.Function)
	}
	if te.Unwrap() == nil {
		t.Errorf("TransportError.Unwrap() returned nil")
	}
}

// TestClient_Given_BlankFunction_When_Invoke_Then_ErrorsBeforeWire
// keeps the wire pristine when callers pass blank function names —
// pydantic would already 422 on the empty registry lookup, but
// failing locally saves a round-trip and gives a clearer error.
func TestClient_Given_BlankFunction_When_Invoke_Then_ErrorsBeforeWire(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("client must not hit the wire when function is blank")
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, srv.Client())
	if _, err := c.Invoke(context.Background(), InvokeRequest{Function: "   "}); err == nil {
		t.Fatal("expected pre-flight error for blank function")
	}
}

// TestClient_Given_ContextCanceled_When_Invoke_Then_ReturnsTransportError
// confirms the client honours caller-supplied cancellation rather
// than blocking on the runtime indefinitely.
func TestClient_Given_ContextCanceled_When_Invoke_Then_ReturnsTransportError(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-block:
		}
	}))
	defer srv.Close()
	defer close(block)

	c, _ := NewClient(srv.URL, srv.Client())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.Invoke(ctx, InvokeRequest{Function: "slow"})
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
	var te *TransportError
	if !errors.As(err, &te) {
		t.Fatalf("expected *TransportError, got %T: %v", err, err)
	}
}

// TestErrorMessages_Given_TypedErrors_When_Stringified_Then_IncludeContext
// captures the human-readable surfaces we expect to see in logs and
// toasts. Important because the *only* thing some upstream consumers
// (notifications, telemetry) get is err.Error().
func TestErrorMessages_Given_TypedErrors_When_Stringified_Then_IncludeContext(t *testing.T) {
	ve := &ValidationError{Function: "f", StatusCode: 422, Details: []FieldError{{Loc: []string{"body", "a"}, Msg: "bad"}}}
	if !strings.Contains(ve.Error(), "f") || !strings.Contains(ve.Error(), "body.a") {
		t.Errorf("ValidationError.Error() = %q", ve.Error())
	}

	sb := &SandboxViolationError{Function: "f", StatusCode: 403, Code: "Forbidden", Detail: "no"}
	if !strings.Contains(sb.Error(), "Forbidden") {
		t.Errorf("SandboxViolationError.Error() = %q", sb.Error())
	}

	nf := &NotFoundError{Function: "f", StatusCode: 404, Detail: "missing"}
	if !strings.Contains(nf.Error(), "missing") {
		t.Errorf("NotFoundError.Error() = %q", nf.Error())
	}

	rt := &RuntimeError{Function: "f", StatusCode: 500, Code: "X", Detail: "boom"}
	if !strings.Contains(rt.Error(), "boom") {
		t.Errorf("RuntimeError.Error() = %q", rt.Error())
	}

	te := &TransportError{Function: "f", Err: errors.New("EOF")}
	if !strings.Contains(te.Error(), "EOF") {
		t.Errorf("TransportError.Error() = %q", te.Error())
	}
}

// TestParseValidationError_Given_EmptyBody_When_Parse_Then_Falls_Back
// is a unit-level sanity check on the salvage path so a runtime that
// emits a status code with no body still produces a usable error.
func TestParseValidationError_Given_EmptyBody_When_Parse_Then_FallsBack(t *testing.T) {
	err := parseValidationError("f", 422, []byte(""))
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if len(ve.Details) == 0 {
		t.Fatal("expected at least one salvaged detail")
	}
}
