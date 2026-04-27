package aip

import (
	"os"
	"strings"
)

// EnvConfig captures the environment knobs that drive provider wiring.
// Tests construct this directly; production reads from os.Getenv via
// LoadEnvConfig.
type EnvConfig struct {
	OpenAIAPIKey       string
	OpenAIModel        string
	OpenAIBaseURL      string
	AnthropicAPIKey    string
	AnthropicModel     string
	AnthropicBaseURL   string
	IncludeMockAlways  bool
	OverrideProviderID string
}

// LoadEnvConfig reads the canonical AIP environment variables. The
// MockProvider is always registered (cheap, useful for dev / tests);
// OpenAI / Anthropic are registered only when their respective API
// keys are present.
//
//	OPENAI_API_KEY        -> registers an openai provider
//	OPENAI_MODEL          -> override the default model
//	OPENAI_BASE_URL       -> override the API base (test fixtures)
//	ANTHROPIC_API_KEY     -> registers an anthropic provider
//	ANTHROPIC_MODEL       -> override the default model
//	ANTHROPIC_BASE_URL    -> override the API base (test fixtures)
//	WEAVE_AIP_PROVIDER    -> default provider hint (informational)
func LoadEnvConfig() EnvConfig {
	return EnvConfig{
		OpenAIAPIKey:       strings.TrimSpace(os.Getenv("OPENAI_API_KEY")),
		OpenAIModel:        strings.TrimSpace(os.Getenv("OPENAI_MODEL")),
		OpenAIBaseURL:      strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")),
		AnthropicAPIKey:    strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")),
		AnthropicModel:     strings.TrimSpace(os.Getenv("ANTHROPIC_MODEL")),
		AnthropicBaseURL:   strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")),
		IncludeMockAlways:  true,
		OverrideProviderID: strings.TrimSpace(os.Getenv("WEAVE_AIP_PROVIDER")),
	}
}

// BuildRegistry constructs a Registry from cfg. The MockProvider is
// always registered when cfg.IncludeMockAlways is true. OpenAI and
// Anthropic are registered when their API keys are non-empty.
//
// Returns the Registry and the slice of provider names actually wired
// (in deterministic order: mock → openai → anthropic) so callers can
// log a clear boot summary.
func BuildRegistry(cfg EnvConfig) (*Registry, []string) {
	reg := NewRegistry()
	var names []string

	if cfg.IncludeMockAlways {
		reg.Register(NewMockProvider())
		names = append(names, ProviderMock)
	}
	if cfg.OpenAIAPIKey != "" {
		reg.Register(NewOpenAIProvider(OpenAIConfig{
			APIKey:  cfg.OpenAIAPIKey,
			Model:   cfg.OpenAIModel,
			BaseURL: cfg.OpenAIBaseURL,
		}))
		names = append(names, ProviderOpenAI)
	}
	if cfg.AnthropicAPIKey != "" {
		reg.Register(NewAnthropicProvider(AnthropicConfig{
			APIKey:  cfg.AnthropicAPIKey,
			Model:   cfg.AnthropicModel,
			BaseURL: cfg.AnthropicBaseURL,
		}))
		names = append(names, ProviderAnthropic)
	}
	return reg, names
}
