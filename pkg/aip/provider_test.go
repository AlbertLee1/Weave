package aip

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegistry_GetMissing(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Get("openai"); !errors.Is(err, ErrProviderNotConfigured) {
		t.Fatalf("expected ErrProviderNotConfigured, got %v", err)
	}
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	r.Register(NewMockProvider())
	got, err := r.Get(ProviderMock)
	if err != nil {
		t.Fatalf("Get(mock): %v", err)
	}
	if got.Name() != ProviderMock {
		t.Errorf("Name = %q want mock", got.Name())
	}
}

func TestRegistry_NamesSorted(t *testing.T) {
	r := NewRegistry()
	r.Register(NewMockProvider())
	r.Register(NewOpenAIProvider(OpenAIConfig{APIKey: "k"}))
	r.Register(NewAnthropicProvider(AnthropicConfig{APIKey: "k"}))
	got := r.Names()
	if len(got) != 3 {
		t.Fatalf("expected 3 names, got %v", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Errorf("Names() should be sorted; got %v", got)
		}
	}
}

func TestMockProvider_Echoes(t *testing.T) {
	p := NewMockProvider()
	resp, err := p.Complete(context.Background(), ChatRequest{
		Messages: []ChatMessage{
			{Role: RoleSystem, Content: "be brief"},
			{Role: RoleUser, Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !strings.Contains(resp.Content, "echo: hello") {
		t.Errorf("expected echo of last user message; got %q", resp.Content)
	}
	if resp.Model == "" {
		t.Errorf("expected default model; got empty")
	}
}

func TestMockProvider_NoUserMessage(t *testing.T) {
	p := NewMockProvider()
	resp, err := p.Complete(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content == "" {
		t.Errorf("expected non-empty fallback content")
	}
}

func TestOpenAIProvider_Complete_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q want Bearer test-key", got)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "\"role\":\"user\"") {
			t.Errorf("request missing user message; body=%s", string(body))
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"model": "gpt-4o-mini",
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": "hi from openai"}},
			},
			"usage": map[string]int{"total_tokens": 17},
		})
	}))
	defer srv.Close()

	p := NewOpenAIProvider(OpenAIConfig{APIKey: "test-key", BaseURL: srv.URL})
	resp, err := p.Complete(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: RoleUser, Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "hi from openai" {
		t.Errorf("Content = %q", resp.Content)
	}
	if resp.TokenCount != 17 {
		t.Errorf("TokenCount = %d want 17", resp.TokenCount)
	}
	if resp.Model != "gpt-4o-mini" {
		t.Errorf("Model = %q", resp.Model)
	}
}

func TestOpenAIProvider_MissingKey(t *testing.T) {
	p := NewOpenAIProvider(OpenAIConfig{})
	_, err := p.Complete(context.Background(), ChatRequest{})
	if !errors.Is(err, ErrProviderNotConfigured) {
		t.Fatalf("expected ErrProviderNotConfigured, got %v", err)
	}
}

func TestOpenAIProvider_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{"message": "bad token", "type": "invalid_request_error"},
		})
	}))
	defer srv.Close()

	p := NewOpenAIProvider(OpenAIConfig{APIKey: "bad", BaseURL: srv.URL})
	_, err := p.Complete(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: RoleUser, Content: "x"}},
	})
	if err == nil {
		t.Fatal("expected error from non-2xx response")
	}
	if !strings.Contains(err.Error(), "bad token") {
		t.Errorf("expected upstream message in error; got %v", err)
	}
}

func TestAnthropicProvider_Complete_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "sk-test" {
			t.Errorf("x-api-key = %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Errorf("anthropic-version header missing")
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "\"system\":\"be brief\"") {
			t.Errorf("system prompt should be hoisted; body=%s", string(body))
		}
		if strings.Contains(string(body), "\"role\":\"system\"") {
			t.Errorf("system message should NOT appear in messages list; body=%s", string(body))
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"model": "claude-sonnet-4-6",
			"content": []map[string]string{
				{"type": "text", "text": "hi from claude"},
			},
			"usage": map[string]int{"input_tokens": 5, "output_tokens": 8},
		})
	}))
	defer srv.Close()

	p := NewAnthropicProvider(AnthropicConfig{APIKey: "sk-test", BaseURL: srv.URL})
	resp, err := p.Complete(context.Background(), ChatRequest{
		Messages: []ChatMessage{
			{Role: RoleSystem, Content: "be brief"},
			{Role: RoleUser, Content: "ping"},
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "hi from claude" {
		t.Errorf("Content = %q", resp.Content)
	}
	if resp.TokenCount != 13 {
		t.Errorf("TokenCount = %d want 13", resp.TokenCount)
	}
}

func TestAnthropicProvider_MissingKey(t *testing.T) {
	p := NewAnthropicProvider(AnthropicConfig{})
	_, err := p.Complete(context.Background(), ChatRequest{})
	if !errors.Is(err, ErrProviderNotConfigured) {
		t.Fatalf("expected ErrProviderNotConfigured, got %v", err)
	}
}

func TestBuildRegistry_FromConfig(t *testing.T) {
	reg, names := BuildRegistry(EnvConfig{
		IncludeMockAlways: true,
		OpenAIAPIKey:      "key",
		AnthropicAPIKey:   "key",
	})
	if len(names) != 3 {
		t.Errorf("expected 3 providers, got %v", names)
	}
	for _, want := range []string{ProviderMock, ProviderOpenAI, ProviderAnthropic} {
		if _, err := reg.Get(want); err != nil {
			t.Errorf("provider %q not registered: %v", want, err)
		}
	}
}

func TestBuildRegistry_NoKeys_OnlyMock(t *testing.T) {
	reg, names := BuildRegistry(EnvConfig{IncludeMockAlways: true})
	if len(names) != 1 || names[0] != ProviderMock {
		t.Errorf("expected only mock, got %v", names)
	}
	if _, err := reg.Get(ProviderOpenAI); !errors.Is(err, ErrProviderNotConfigured) {
		t.Errorf("openai should not be wired when key missing; got err=%v", err)
	}
}
