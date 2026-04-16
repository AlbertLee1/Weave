package automate

import (
	"encoding/json"
	"time"
)

// RetryPolicy configures automatic retry behavior for failed automation effects.
type RetryPolicy struct {
	MaxRetries int `json:"maxRetries"`
	BackoffMs  int `json:"backoffMs"`
}

// ParseRetryPolicy parses retry policy from JSON. Returns nil if no policy.
func ParseRetryPolicy(raw json.RawMessage) *RetryPolicy {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var p RetryPolicy
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil
	}
	if p.MaxRetries <= 0 {
		return nil
	}
	return &p
}

// BackoffDuration calculates the exponential backoff duration for a given attempt.
// Backoff = backoffMs * 2^attempt (exponential).
func (p *RetryPolicy) BackoffDuration(attempt int) time.Duration {
	base := p.BackoffMs
	if base <= 0 {
		base = 1000
	}
	ms := base
	for i := 0; i < attempt; i++ {
		ms *= 2
	}
	return time.Duration(ms) * time.Millisecond
}
