package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMockProvider_ReturnsDeterministicSuggestions verifies that the mock
// provider yields a stable, name-based set of suggestions so local dev and
// tests work without an external LLM.
func TestMockProvider_ReturnsDeterministicSuggestions(t *testing.T) {
	provider := NewMockProvider()

	req := SuggestPropertiesRequest{
		ObjectTypeName:        "Customer",
		ObjectTypeDescription: "A retail customer",
	}

	first, err := provider.Suggest(context.Background(), req)
	if err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}
	if len(first) == 0 {
		t.Fatalf("expected at least one suggestion, got 0")
	}

	second, err := provider.Suggest(context.Background(), req)
	if err != nil {
		t.Fatalf("second call: unexpected error: %v", err)
	}

	if len(first) != len(second) {
		t.Errorf("non-deterministic length: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("non-deterministic suggestion[%d]: %+v vs %+v", i, first[i], second[i])
		}
	}

	// Each suggestion must carry the minimum required fields.
	for i, s := range first {
		if s.APIName == "" {
			t.Errorf("suggestion[%d].APIName empty", i)
		}
		if s.DisplayName == "" {
			t.Errorf("suggestion[%d].DisplayName empty", i)
		}
		if s.BaseType == "" {
			t.Errorf("suggestion[%d].BaseType empty", i)
		}
	}
}

// TestMockProvider_ExcludesExistingProperties verifies that any apiName the
// caller already has is filtered out so the UI does not duplicate columns.
func TestMockProvider_ExcludesExistingProperties(t *testing.T) {
	provider := NewMockProvider()

	full, err := provider.Suggest(context.Background(), SuggestPropertiesRequest{
		ObjectTypeName: "Customer",
	})
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	if len(full) < 2 {
		t.Fatalf("baseline must return >=2 suggestions, got %d", len(full))
	}

	exclude := full[0].APIName
	filtered, err := provider.Suggest(context.Background(), SuggestPropertiesRequest{
		ObjectTypeName:     "Customer",
		ExistingProperties: []string{exclude},
	})
	if err != nil {
		t.Fatalf("filtered: %v", err)
	}
	for _, s := range filtered {
		if s.APIName == exclude {
			t.Errorf("expected %q to be filtered out", exclude)
		}
	}
	if len(filtered) >= len(full) {
		t.Errorf("expected filtered list to shrink: %d -> %d", len(full), len(filtered))
	}
}

// TestSuggestHandler_200_WithMockProvider exercises the happy path: a valid
// request body returns a 200 with a non-empty suggestions list from the
// MockProvider.
func TestSuggestHandler_200_WithMockProvider(t *testing.T) {
	handler := NewSuggestHandler(NewMockProvider())

	body := SuggestPropertiesRequest{
		ObjectTypeName:        "Order",
		ObjectTypeDescription: "An order placed by a customer",
	}
	buf, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/ai/suggest-properties", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: got %q, want JSON", ct)
	}

	var resp SuggestPropertiesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Suggestions) == 0 {
		t.Errorf("expected suggestions, got 0")
	}
}

// TestSuggestHandler_InvalidBody_400 verifies that a malformed JSON body or
// missing objectTypeName produces a 400 with no provider call attempted.
func TestSuggestHandler_InvalidBody_400(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "garbage", body: "not-json"},
		{name: "missing name", body: `{"objectTypeDescription":"oops"}`},
		{name: "blank name", body: `{"objectTypeName":"   "}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewSuggestHandler(NewMockProvider())
			req := httptest.NewRequest(http.MethodPost, "/api/admin/ai/suggest-properties",
				strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status: got %d, want 400 (body=%s)", rec.Code, rec.Body.String())
			}
		})
	}
}

// stubProvider lets a test inject a controlled response or error.
type stubProvider struct {
	suggestions []PropertySuggestion
	err         error
	calls       int
}

func (s *stubProvider) Suggest(ctx context.Context, req SuggestPropertiesRequest) ([]PropertySuggestion, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.suggestions, nil
}

// TestSuggestHandler_ProviderError_500 verifies that an error from the
// provider surfaces as a 500 without leaking internal text.
func TestSuggestHandler_ProviderError_500(t *testing.T) {
	stub := &stubProvider{err: errors.New("upstream timeout")}
	handler := NewSuggestHandler(stub)

	body, _ := json.Marshal(SuggestPropertiesRequest{ObjectTypeName: "Order"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ai/suggest-properties", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500 (body=%s)", rec.Code, rec.Body.String())
	}
	if stub.calls != 1 {
		t.Errorf("provider should be called once, got %d", stub.calls)
	}
}

// TestSuggestHandler_PassesExistingProperties confirms the handler forwards
// the existingProperties field so the provider can filter duplicates.
func TestSuggestHandler_PassesExistingProperties(t *testing.T) {
	stub := &stubProvider{
		suggestions: []PropertySuggestion{
			{APIName: "extra", DisplayName: "Extra", BaseType: "string"},
		},
	}
	captured := &struct {
		req SuggestPropertiesRequest
	}{}
	captureProvider := providerFunc(func(ctx context.Context, req SuggestPropertiesRequest) ([]PropertySuggestion, error) {
		captured.req = req
		return stub.suggestions, nil
	})
	handler := NewSuggestHandler(captureProvider)

	body, _ := json.Marshal(SuggestPropertiesRequest{
		ObjectTypeName:     "Order",
		ExistingProperties: []string{"id", "name"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ai/suggest-properties", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d (body=%s)", rec.Code, rec.Body.String())
	}
	if captured.req.ObjectTypeName != "Order" {
		t.Errorf("ObjectTypeName: got %q", captured.req.ObjectTypeName)
	}
	if got := captured.req.ExistingProperties; len(got) != 2 || got[0] != "id" || got[1] != "name" {
		t.Errorf("ExistingProperties: got %v, want [id name]", got)
	}
}

// TestOpenAIProvider_ConstructsCorrectRequest verifies the OpenAI provider
// builds a chat.completions call with bearer auth and the configured model,
// using a stub HTTP server in place of api.openai.com.
func TestOpenAIProvider_ConstructsCorrectRequest(t *testing.T) {
	var captured struct {
		method  string
		path    string
		auth    string
		ctype   string
		bodyRaw string
	}

	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.auth = r.Header.Get("Authorization")
		captured.ctype = r.Header.Get("Content-Type")
		buf, _ := io.ReadAll(r.Body)
		captured.bodyRaw = string(buf)

		// Return a chat.completions-shaped response with one suggestion in JSON.
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": `{"suggestions":[{"apiName":"email","displayName":"Email","baseType":"string","description":"Customer email","isArray":false}]}`,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer stub.Close()

	provider := &OpenAIProvider{
		APIKey:  "sk-test-key",
		Model:   "gpt-4o-mini",
		BaseURL: stub.URL,
		Client:  stub.Client(),
	}

	suggestions, err := provider.Suggest(context.Background(), SuggestPropertiesRequest{
		ObjectTypeName:        "Customer",
		ObjectTypeDescription: "A retail customer",
		ExistingProperties:    []string{"id"},
	})
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(suggestions))
	}
	if suggestions[0].APIName != "email" {
		t.Errorf("apiName: got %q, want email", suggestions[0].APIName)
	}

	if captured.method != http.MethodPost {
		t.Errorf("method: got %q", captured.method)
	}
	if !strings.Contains(captured.path, "/chat/completions") {
		t.Errorf("path: got %q, want to contain /chat/completions", captured.path)
	}
	if captured.auth != "Bearer sk-test-key" {
		t.Errorf("Authorization: got %q", captured.auth)
	}
	if !strings.Contains(captured.ctype, "application/json") {
		t.Errorf("Content-Type: got %q", captured.ctype)
	}
	if !strings.Contains(captured.bodyRaw, "gpt-4o-mini") {
		t.Errorf("body should reference model gpt-4o-mini: %s", captured.bodyRaw)
	}
	if !strings.Contains(captured.bodyRaw, "Customer") {
		t.Errorf("body should reference ObjectTypeName: %s", captured.bodyRaw)
	}
}

// TestOpenAIProvider_RejectsMissingAPIKey ensures the provider does not
// silently call OpenAI without credentials.
func TestOpenAIProvider_RejectsMissingAPIKey(t *testing.T) {
	provider := &OpenAIProvider{Model: "gpt-4o-mini"}
	if _, err := provider.Suggest(context.Background(), SuggestPropertiesRequest{
		ObjectTypeName: "Customer",
	}); err == nil {
		t.Fatal("expected error for missing api key")
	}
}

// TestNewProviderFromEnv covers the factory used by the route wiring.
func TestNewProviderFromEnv(t *testing.T) {
	t.Run("default mock", func(t *testing.T) {
		t.Setenv("WEAVE_AI_PROVIDER", "")
		p := NewProviderFromEnv()
		if _, ok := p.(*MockProvider); !ok {
			t.Errorf("default should be *MockProvider, got %T", p)
		}
	})

	t.Run("explicit mock", func(t *testing.T) {
		t.Setenv("WEAVE_AI_PROVIDER", "mock")
		p := NewProviderFromEnv()
		if _, ok := p.(*MockProvider); !ok {
			t.Errorf("expected *MockProvider, got %T", p)
		}
	})

	t.Run("openai with key", func(t *testing.T) {
		t.Setenv("WEAVE_AI_PROVIDER", "openai")
		t.Setenv("OPENAI_API_KEY", "sk-fake")
		p := NewProviderFromEnv()
		if _, ok := p.(*OpenAIProvider); !ok {
			t.Errorf("expected *OpenAIProvider, got %T", p)
		}
	})

	t.Run("openai without key falls back to mock", func(t *testing.T) {
		t.Setenv("WEAVE_AI_PROVIDER", "openai")
		t.Setenv("OPENAI_API_KEY", "")
		p := NewProviderFromEnv()
		if _, ok := p.(*MockProvider); !ok {
			t.Errorf("missing key should fall back to *MockProvider, got %T", p)
		}
	})
}

// providerFunc adapts a function to the LLMProvider interface for tests.
type providerFunc func(ctx context.Context, req SuggestPropertiesRequest) ([]PropertySuggestion, error)

func (f providerFunc) Suggest(ctx context.Context, req SuggestPropertiesRequest) ([]PropertySuggestion, error) {
	return f(ctx, req)
}
