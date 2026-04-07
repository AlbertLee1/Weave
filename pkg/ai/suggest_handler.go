// Package ai contains the AI-assisted schema suggestion endpoint and the
// pluggable LLM provider abstraction that powers it. The default provider is
// the in-process MockProvider so local development and CI work without an
// external API key.
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
)

// SuggestPropertiesRequest is the wire-format JSON body for the suggest
// endpoint. ObjectTypeName is required; the description and existing
// properties refine the suggestions.
type SuggestPropertiesRequest struct {
	ObjectTypeName        string   `json:"objectTypeName"`
	ObjectTypeDescription string   `json:"objectTypeDescription,omitempty"`
	ExistingProperties    []string `json:"existingProperties,omitempty"`
}

// PropertySuggestion is a single property the LLM proposes for an object
// type. Fields mirror the CreatePropertyInput shape so the UI can paste a
// suggestion straight into the form.
type PropertySuggestion struct {
	APIName     string `json:"apiName"`
	DisplayName string `json:"displayName"`
	BaseType    string `json:"baseType"`
	Description string `json:"description,omitempty"`
	IsArray     bool   `json:"isArray"`
}

// SuggestPropertiesResponse is the wire-format JSON body returned by the
// suggest endpoint.
type SuggestPropertiesResponse struct {
	Suggestions []PropertySuggestion `json:"suggestions"`
}

// LLMProvider is the abstraction over an external (or in-process) language
// model. Implementations must be safe for concurrent use.
type LLMProvider interface {
	Suggest(ctx context.Context, req SuggestPropertiesRequest) ([]PropertySuggestion, error)
}

// NewSuggestHandler returns an http.Handler that decodes a
// SuggestPropertiesRequest, calls the provider, and writes the response.
// Validation errors yield 400; provider errors yield 500.
func NewSuggestHandler(provider LLMProvider) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body SuggestPropertiesRequest
		if err := httputil.ReadJSON(r, &body); err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidSuggestPropertiesRequest", map[string]string{
				"reason": err.Error(),
			}))
			return
		}

		body.ObjectTypeName = strings.TrimSpace(body.ObjectTypeName)
		if body.ObjectTypeName == "" {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingObjectTypeName", map[string]string{
				"reason": "objectTypeName is required",
			}))
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		suggestions, err := provider.Suggest(ctx, body)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("AISuggestionFailed", map[string]string{
				"reason": err.Error(),
			}))
			return
		}
		if suggestions == nil {
			suggestions = []PropertySuggestion{}
		}

		httputil.WriteJSON(w, http.StatusOK, SuggestPropertiesResponse{Suggestions: suggestions})
	})
}

// MockProvider is the deterministic in-process provider used by local dev,
// CI, and as the fallback when WEAVE_AI_PROVIDER is unset or misconfigured.
// It returns a name-aware seed catalogue based on common ontology shapes.
type MockProvider struct{}

// NewMockProvider constructs a MockProvider.
func NewMockProvider() *MockProvider { return &MockProvider{} }

// Suggest returns a deterministic property catalogue derived from the
// ObjectTypeName. The list is filtered against ExistingProperties so the UI
// never offers duplicates.
func (m *MockProvider) Suggest(_ context.Context, req SuggestPropertiesRequest) ([]PropertySuggestion, error) {
	base := mockCatalog(req.ObjectTypeName)
	if len(req.ExistingProperties) == 0 {
		return base, nil
	}
	exclude := map[string]bool{}
	for _, name := range req.ExistingProperties {
		exclude[strings.ToLower(strings.TrimSpace(name))] = true
	}
	out := make([]PropertySuggestion, 0, len(base))
	for _, s := range base {
		if exclude[strings.ToLower(s.APIName)] {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// mockCatalog produces the deterministic suggestion list. The first
// suggestions are universal scaffolding (id, name, createdAt, updatedAt);
// the rest specialise on common name patterns.
func mockCatalog(objectTypeName string) []PropertySuggestion {
	lower := strings.ToLower(objectTypeName)

	suggestions := []PropertySuggestion{
		{APIName: "id", DisplayName: "ID", BaseType: "string", Description: "Primary identifier", IsArray: false},
		{APIName: "name", DisplayName: "Name", BaseType: "string", Description: "Human-readable name", IsArray: false},
		{APIName: "description", DisplayName: "Description", BaseType: "string", Description: "Free-form description", IsArray: false},
		{APIName: "createdAt", DisplayName: "Created At", BaseType: "timestamp", Description: "Creation timestamp", IsArray: false},
		{APIName: "updatedAt", DisplayName: "Updated At", BaseType: "timestamp", Description: "Last update timestamp", IsArray: false},
	}

	switch {
	case strings.Contains(lower, "customer") || strings.Contains(lower, "user") || strings.Contains(lower, "person") || strings.Contains(lower, "employee"):
		suggestions = append(suggestions,
			PropertySuggestion{APIName: "email", DisplayName: "Email", BaseType: "string", Description: "Contact email address", IsArray: false},
			PropertySuggestion{APIName: "phoneNumber", DisplayName: "Phone Number", BaseType: "string", Description: "Contact phone number", IsArray: false},
			PropertySuggestion{APIName: "tags", DisplayName: "Tags", BaseType: "string", Description: "Free-form tags", IsArray: true},
		)
	case strings.Contains(lower, "order") || strings.Contains(lower, "invoice") || strings.Contains(lower, "transaction"):
		suggestions = append(suggestions,
			PropertySuggestion{APIName: "totalAmount", DisplayName: "Total Amount", BaseType: "decimal", Description: "Total monetary amount", IsArray: false},
			PropertySuggestion{APIName: "currency", DisplayName: "Currency", BaseType: "string", Description: "ISO currency code", IsArray: false},
			PropertySuggestion{APIName: "status", DisplayName: "Status", BaseType: "string", Description: "Workflow status", IsArray: false},
		)
	case strings.Contains(lower, "product") || strings.Contains(lower, "item") || strings.Contains(lower, "sku"):
		suggestions = append(suggestions,
			PropertySuggestion{APIName: "price", DisplayName: "Price", BaseType: "decimal", Description: "Unit price", IsArray: false},
			PropertySuggestion{APIName: "sku", DisplayName: "SKU", BaseType: "string", Description: "Stock keeping unit", IsArray: false},
			PropertySuggestion{APIName: "inStock", DisplayName: "In Stock", BaseType: "boolean", Description: "Availability flag", IsArray: false},
		)
	case strings.Contains(lower, "event") || strings.Contains(lower, "log"):
		suggestions = append(suggestions,
			PropertySuggestion{APIName: "eventType", DisplayName: "Event Type", BaseType: "string", Description: "Event category", IsArray: false},
			PropertySuggestion{APIName: "occurredAt", DisplayName: "Occurred At", BaseType: "timestamp", Description: "Time the event happened", IsArray: false},
			PropertySuggestion{APIName: "metadata", DisplayName: "Metadata", BaseType: "string", Description: "Additional metadata payload", IsArray: false},
		)
	default:
		suggestions = append(suggestions,
			PropertySuggestion{APIName: "ownerId", DisplayName: "Owner", BaseType: "string", Description: "Identifier of the owning user", IsArray: false},
			PropertySuggestion{APIName: "tags", DisplayName: "Tags", BaseType: "string", Description: "Free-form tags", IsArray: true},
		)
	}

	return suggestions
}

// OpenAIProvider implements LLMProvider against the OpenAI chat completions
// API. The Client and BaseURL fields are exposed so tests can route the
// provider at an httptest server.
type OpenAIProvider struct {
	APIKey  string
	Model   string
	BaseURL string
	Client  *http.Client
}

// openAIChatRequest is the minimal subset of the OpenAI chat-completions
// request body that we use.
type openAIChatRequest struct {
	Model          string              `json:"model"`
	Messages       []openAIChatMessage `json:"messages"`
	ResponseFormat *openAIRespFormat   `json:"response_format,omitempty"`
	Temperature    float64             `json:"temperature"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIRespFormat struct {
	Type string `json:"type"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message openAIChatMessage `json:"message"`
	} `json:"choices"`
}

// Suggest calls OpenAI chat completions with a prompt that asks for a strict
// JSON suggestion list, then parses the assistant content as
// SuggestPropertiesResponse.
func (p *OpenAIProvider) Suggest(ctx context.Context, req SuggestPropertiesRequest) ([]PropertySuggestion, error) {
	if p.APIKey == "" {
		return nil, fmt.Errorf("openai provider: OPENAI_API_KEY is not set")
	}
	model := p.Model
	if model == "" {
		model = "gpt-4o-mini"
	}
	baseURL := p.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	systemPrompt := "You are a data modeling assistant. Suggest a concise list of properties for an ontology object type. " +
		"Respond ONLY with a JSON object of the shape {\"suggestions\":[{\"apiName\":\"...\",\"displayName\":\"...\",\"baseType\":\"string|integer|long|double|decimal|boolean|date|timestamp\",\"description\":\"...\",\"isArray\":false}]}. " +
		"apiName must be camelCase. Avoid duplicates of the existing properties supplied by the user."

	userParts := []string{fmt.Sprintf("Object type: %s", req.ObjectTypeName)}
	if req.ObjectTypeDescription != "" {
		userParts = append(userParts, fmt.Sprintf("Description: %s", req.ObjectTypeDescription))
	}
	if len(req.ExistingProperties) > 0 {
		userParts = append(userParts, fmt.Sprintf("Existing properties: %s", strings.Join(req.ExistingProperties, ", ")))
	}

	body := openAIChatRequest{
		Model: model,
		Messages: []openAIChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: strings.Join(userParts, "\n")},
		},
		ResponseFormat: &openAIRespFormat{Type: "json_object"},
		Temperature:    0.2,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call openai: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai returned status %d", resp.StatusCode)
	}

	var chat openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chat); err != nil {
		return nil, fmt.Errorf("decode openai response: %w", err)
	}
	if len(chat.Choices) == 0 {
		return nil, fmt.Errorf("openai returned no choices")
	}

	var parsed SuggestPropertiesResponse
	if err := json.Unmarshal([]byte(chat.Choices[0].Message.Content), &parsed); err != nil {
		return nil, fmt.Errorf("parse model output: %w", err)
	}
	return parsed.Suggestions, nil
}

// NewProviderFromEnv returns the LLMProvider selected by environment vars.
//
//	WEAVE_AI_PROVIDER  = "" | "mock"   -> MockProvider
//	WEAVE_AI_PROVIDER  = "openai"      -> OpenAIProvider (requires OPENAI_API_KEY)
//
// When the OpenAI provider is requested but OPENAI_API_KEY is missing, this
// falls back to the MockProvider so the server still boots cleanly.
func NewProviderFromEnv() LLMProvider {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WEAVE_AI_PROVIDER"))) {
	case "openai":
		key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
		if key == "" {
			return NewMockProvider()
		}
		model := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
		return &OpenAIProvider{
			APIKey: key,
			Model:  model,
		}
	default:
		return NewMockProvider()
	}
}
