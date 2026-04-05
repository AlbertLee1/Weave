package actions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// SideEffect defines an effect triggered after successful action execution.
type SideEffect struct {
	Type   string          `json:"type"`   // "webhook", "log"
	Config json.RawMessage `json:"config"`
}

// ActionResult carries the result data passed to side effects.
type ActionResult struct {
	ActionRID string                 `json:"actionRid"`
	BatchID   string                 `json:"batchId"`
	Edits     interface{}            `json:"edits,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// webhookConfig is the config for the "webhook" side effect type.
type webhookConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	// TimeoutSeconds controls the HTTP client timeout (default 10s).
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
}

// ExecuteSideEffects executes side effects after a successful action.
// Execution is best-effort: errors are logged but not returned to the caller.
// An empty or nil effects JSON is a no-op.
func ExecuteSideEffects(effectsJSON json.RawMessage, result ActionResult) error {
	if len(effectsJSON) == 0 || string(effectsJSON) == "null" || string(effectsJSON) == "[]" {
		return nil
	}

	// Support either a single effect object or an array of effects.
	if effectsJSON[0] == '[' {
		var effects []SideEffect
		if err := json.Unmarshal(effectsJSON, &effects); err != nil {
			return fmt.Errorf("parse side effects: %w", err)
		}
		for _, e := range effects {
			if err := executeSingleEffect(e, result); err != nil {
				// Best-effort: log and continue.
				log.Printf("side effect %q failed: %v", e.Type, err)
			}
		}
		return nil
	}

	var e SideEffect
	if err := json.Unmarshal(effectsJSON, &e); err != nil {
		return fmt.Errorf("parse side effects: %w", err)
	}
	if err := executeSingleEffect(e, result); err != nil {
		log.Printf("side effect %q failed: %v", e.Type, err)
	}
	return nil
}

func executeSingleEffect(e SideEffect, result ActionResult) error {
	switch e.Type {
	case "webhook":
		return executeWebhookEffect(e.Config, result)
	case "log":
		return executeLogEffect(result)
	default:
		return fmt.Errorf("unknown side effect type: %q", e.Type)
	}
}

func executeWebhookEffect(configJSON json.RawMessage, result ActionResult) error {
	var cfg webhookConfig
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return fmt.Errorf("webhook: invalid config: %w", err)
	}
	if cfg.URL == "" {
		return fmt.Errorf("webhook: url is required")
	}

	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("webhook: marshal result: %w", err)
	}

	timeout := 10 * time.Second
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}

	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: non-2xx response: %d", resp.StatusCode)
	}
	return nil
}

func executeLogEffect(result ActionResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("log: marshal result: %w", err)
	}
	log.Printf("action side-effect log: %s", string(data))
	return nil
}
