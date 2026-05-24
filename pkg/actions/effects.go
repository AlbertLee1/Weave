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
	// MaxRetries caps the number of retry attempts AFTER the initial call
	// for transient failures (network errors, 5xx, 408, 429). Total call
	// count on persistent failure is 1 + MaxRetries. Zero or negative
	// values fall through to the package default (defaultMaxRetries = 3),
	// matching Foundry's "transient errors are retried" wire contract.
	MaxRetries int `json:"maxRetries,omitempty"`
	// RetryBackoffMilliseconds is the base delay between retry attempts.
	// Each subsequent attempt waits 2^attempt × base (exponential), capped
	// at maxRetryBackoffMilliseconds. Zero or negative falls back to
	// defaultRetryBackoffMilliseconds = 100.
	RetryBackoffMilliseconds int `json:"retryBackoffMilliseconds,omitempty"`
}

// Webhook retry defaults. Picked to match common production conventions:
// 3 retries gives ~700ms tail latency on persistent failure while almost
// always covering single-node restarts or transient blips.
const (
	defaultMaxRetries               = 3
	defaultRetryBackoffMilliseconds = 100
	maxRetryBackoffMilliseconds     = 5_000
)

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

	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	backoffMs := cfg.RetryBackoffMilliseconds
	if backoffMs <= 0 {
		backoffMs = defaultRetryBackoffMilliseconds
	}

	// Retry loop. Total attempts = 1 + maxRetries. Retryable failures
	// (network error, 5xx, 408, 429) trigger exponential backoff
	// (2^attempt × backoffMs, capped). Non-retryable failures (other 4xx
	// = caller bugs) fail immediately so misconfigured webhooks surface
	// to oncall instead of silently consuming retry budget.
	var lastErr error
	totalAttempts := 1 + maxRetries
	for attempt := 0; attempt < totalAttempts; attempt++ {
		if attempt > 0 {
			delay := time.Duration(backoffMs*(1<<uint(attempt-1))) * time.Millisecond
			if delay > time.Duration(maxRetryBackoffMilliseconds)*time.Millisecond {
				delay = time.Duration(maxRetryBackoffMilliseconds) * time.Millisecond
			}
			time.Sleep(delay)
		}

		statusCode, attemptErr := doWebhookAttempt(client, cfg, body)
		if attemptErr == nil {
			return nil
		}
		lastErr = attemptErr

		// 4xx other than 408/429 is a caller bug — no amount of retrying
		// will fix a bad URL, missing auth header, or malformed payload.
		if !isRetryableStatus(statusCode) {
			return fmt.Errorf("webhook: non-retryable failure on attempt 1: %w", attemptErr)
		}
	}
	return fmt.Errorf("webhook: gave up after %d attempts: %w", totalAttempts, lastErr)
}

// doWebhookAttempt makes one HTTP call. Returns (statusCode, error).
// statusCode is 0 when the request failed before getting a response
// (network error / timeout) — treated as retryable.
func doWebhookAttempt(client *http.Client, cfg webhookConfig, body []byte) (int, error) {
	req, err := http.NewRequest(http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		// Transport-level error — no status code yet. Always retryable.
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("non-2xx response: %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

// isRetryableStatus reports whether an HTTP status code (or 0 for a
// transport-level error) should be retried per Foundry's side-effect
// dispatch contract. 5xx, 408, and 429 are transient; other 4xx are
// caller bugs and fail fast.
func isRetryableStatus(statusCode int) bool {
	if statusCode == 0 {
		return true // network / timeout
	}
	if statusCode >= 500 && statusCode < 600 {
		return true
	}
	if statusCode == http.StatusRequestTimeout {
		return true // 408
	}
	if statusCode == http.StatusTooManyRequests {
		return true // 429
	}
	return false
}

func executeLogEffect(result ActionResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("log: marshal result: %w", err)
	}
	log.Printf("action side-effect log: %s", string(data))
	return nil
}
