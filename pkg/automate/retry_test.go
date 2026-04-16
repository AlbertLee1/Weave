package automate

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseRetryPolicy_Valid(t *testing.T) {
	raw := json.RawMessage(`{"maxRetries":3,"backoffMs":1000}`)
	p := ParseRetryPolicy(raw)
	if p == nil {
		t.Fatal("expected non-nil retry policy")
	}
	if p.MaxRetries != 3 {
		t.Fatalf("expected maxRetries=3, got %d", p.MaxRetries)
	}
	if p.BackoffMs != 1000 {
		t.Fatalf("expected backoffMs=1000, got %d", p.BackoffMs)
	}
}

func TestParseRetryPolicy_Nil(t *testing.T) {
	p := ParseRetryPolicy(nil)
	if p != nil {
		t.Fatal("expected nil for nil input")
	}
}

func TestParseRetryPolicy_NullJSON(t *testing.T) {
	p := ParseRetryPolicy(json.RawMessage(`null`))
	if p != nil {
		t.Fatal("expected nil for null JSON")
	}
}

func TestParseRetryPolicy_InvalidJSON(t *testing.T) {
	p := ParseRetryPolicy(json.RawMessage(`{invalid`))
	if p != nil {
		t.Fatal("expected nil for invalid JSON")
	}
}

func TestParseRetryPolicy_ZeroMaxRetries(t *testing.T) {
	raw := json.RawMessage(`{"maxRetries":0,"backoffMs":1000}`)
	p := ParseRetryPolicy(raw)
	if p != nil {
		t.Fatal("expected nil for maxRetries=0")
	}
}

func TestParseRetryPolicy_NegativeMaxRetries(t *testing.T) {
	raw := json.RawMessage(`{"maxRetries":-1,"backoffMs":500}`)
	p := ParseRetryPolicy(raw)
	if p != nil {
		t.Fatal("expected nil for negative maxRetries")
	}
}

func TestBackoffDuration_Attempt0(t *testing.T) {
	p := &RetryPolicy{MaxRetries: 3, BackoffMs: 1000}
	d := p.BackoffDuration(0)
	if d != 1000*time.Millisecond {
		t.Fatalf("expected 1000ms, got %v", d)
	}
}

func TestBackoffDuration_Attempt1(t *testing.T) {
	p := &RetryPolicy{MaxRetries: 3, BackoffMs: 1000}
	d := p.BackoffDuration(1)
	if d != 2000*time.Millisecond {
		t.Fatalf("expected 2000ms, got %v", d)
	}
}

func TestBackoffDuration_Attempt2(t *testing.T) {
	p := &RetryPolicy{MaxRetries: 3, BackoffMs: 1000}
	d := p.BackoffDuration(2)
	if d != 4000*time.Millisecond {
		t.Fatalf("expected 4000ms, got %v", d)
	}
}

func TestBackoffDuration_DefaultBackoff(t *testing.T) {
	p := &RetryPolicy{MaxRetries: 3, BackoffMs: 0}
	d := p.BackoffDuration(0)
	if d != 1000*time.Millisecond {
		t.Fatalf("expected default 1000ms, got %v", d)
	}
}

func TestBackoffDuration_CustomBackoff(t *testing.T) {
	p := &RetryPolicy{MaxRetries: 5, BackoffMs: 500}
	d := p.BackoffDuration(3)
	// 500 * 2^3 = 4000
	if d != 4000*time.Millisecond {
		t.Fatalf("expected 4000ms, got %v", d)
	}
}
