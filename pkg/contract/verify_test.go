package contract

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func mustJSON(s string) interface{} {
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		panic(err)
	}
	return v
}

// stubHandler is a tiny in-process handler used to exercise the verifier
// without depending on the full server router. It dispatches by "method path"
// to the configured response.
type stubHandler struct {
	routes map[string]func(http.ResponseWriter, *http.Request)
}

func (h *stubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + r.URL.Path
	if fn, ok := h.routes[key]; ok {
		fn(w, r)
		return
	}
	http.NotFound(w, r)
}

func newStubHandler() *stubHandler {
	return &stubHandler{routes: map[string]func(http.ResponseWriter, *http.Request){}}
}

func TestVerifyInteraction_HappyPath(t *testing.T) {
	h := newStubHandler()
	h.routes["GET /health"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","timestamp":"2026-05-02T00:00:00Z"}`))
	}
	in := Interaction{
		Description: "GET /health returns ok",
		Request:     Request{Method: "GET", Path: "/health"},
		Response: Response{
			Status: 200,
			Body:   json.RawMessage(`{"status":"ok","timestamp":"placeholder"}`),
			Matchers: map[string]MatcherRule{
				"$.timestamp": {Match: "type", Value: "string"},
			},
		},
	}
	if err := VerifyInteraction(h, in, VerifyOptions{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyInteraction_StatusMismatchFails(t *testing.T) {
	h := newStubHandler()
	h.routes["GET /missing"] = func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}
	in := Interaction{
		Description: "GET /missing should be 200",
		Request:     Request{Method: "GET", Path: "/missing"},
		Response:    Response{Status: 200},
	}
	err := VerifyInteraction(h, in, VerifyOptions{})
	if err == nil {
		t.Fatal("expected status mismatch error")
	}
	if !strings.Contains(err.Error(), "status") {
		t.Errorf("error should mention status: %v", err)
	}
}

func TestVerifyInteraction_BodyMismatchFails(t *testing.T) {
	h := newStubHandler()
	h.routes["GET /bad"] = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"DEGRADED"}`))
	}
	in := Interaction{
		Description: "GET /bad",
		Request:     Request{Method: "GET", Path: "/bad"},
		Response: Response{
			Status: 200,
			Body:   json.RawMessage(`{"status":"ok"}`),
		},
	}
	err := VerifyInteraction(h, in, VerifyOptions{})
	if err == nil {
		t.Fatal("expected body mismatch error")
	}
}

func TestVerifyInteraction_PostJSONBody(t *testing.T) {
	h := newStubHandler()
	h.routes["POST /api/v2/echo"] = func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"echoed": body["msg"]})
	}
	in := Interaction{
		Description: "POST /echo round-trips message",
		Request: Request{
			Method:  "POST",
			Path:    "/api/v2/echo",
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    json.RawMessage(`{"msg":"hello"}`),
		},
		Response: Response{
			Status: 200,
			Body:   json.RawMessage(`{"echoed":"hello"}`),
		},
	}
	if err := VerifyInteraction(h, in, VerifyOptions{}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestVerifyInteraction_QueryParamsForwarded(t *testing.T) {
	h := newStubHandler()
	h.routes["GET /search"] = func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		_, _ = w.Write([]byte(`{"q":"` + q + `"}`))
	}
	in := Interaction{
		Description: "GET /search with q",
		Request: Request{
			Method: "GET",
			Path:   "/search",
			Query:  map[string]string{"q": "hello"},
		},
		Response: Response{
			Status: 200,
			Body:   json.RawMessage(`{"q":"hello"}`),
		},
	}
	if err := VerifyInteraction(h, in, VerifyOptions{}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestVerifyInteraction_AuthHookStampsHeader(t *testing.T) {
	h := newStubHandler()
	h.routes["GET /protected"] = func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
	in := Interaction{
		Description: "GET /protected requires bearer",
		Request:     Request{Method: "GET", Path: "/protected"},
		Response: Response{
			Status: 200,
			Body:   json.RawMessage(`{"ok":true}`),
		},
	}
	opts := VerifyOptions{
		SetAuth: func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer test-token")
		},
	}
	if err := VerifyInteraction(h, in, opts); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestVerifyPact_AggregatesErrorsPerInteraction(t *testing.T) {
	h := newStubHandler()
	h.routes["GET /ok"] = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}
	pact := &Pact{
		Consumer: Participant{Name: "c"},
		Provider: Participant{Name: "p"},
		Interactions: []Interaction{
			{
				Description: "ok one",
				Request:     Request{Method: "GET", Path: "/ok"},
				Response:    Response{Status: 200, Body: json.RawMessage(`{}`)},
			},
			{
				Description: "broken one",
				Request:     Request{Method: "GET", Path: "/missing"},
				Response:    Response{Status: 200},
			},
		},
	}
	errs := VerifyPact(h, pact, VerifyOptions{})
	if len(errs) != 1 {
		t.Fatalf("want 1 error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), "broken one") {
		t.Errorf("error should namespace by description: %v", errs[0])
	}
}
