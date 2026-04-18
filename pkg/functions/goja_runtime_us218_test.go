package functions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/functions/fnerrors"
)

// US-218: callers must be able to distinguish a CPU timeout (→ HTTP 408)
// from a memory-limit overrun (→ HTTP 429) via the typed sentinels in
// pkg/functions/fnerrors. The runtime interrupts goja with tagged
// reasons, and wrapGojaError promotes those tags into the public errors
// consumed by pkg/oms.handlers_function.go.

func TestExecute_TimeoutReturnsErrTimeoutSentinel(t *testing.T) {
	cfg := Config{
		MaxExecutionTime: 200 * time.Millisecond,
		MaxMemoryBytes:   128 * 1024 * 1024,
	}
	rt := NewRuntime(cfg)

	_, err := rt.Execute(context.Background(), `
		function main(input) {
			while (true) {}
		}
	`, nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, fnerrors.ErrTimeout) {
		t.Fatalf("expected errors.Is(err, fnerrors.ErrTimeout); got %v", err)
	}
	if errors.Is(err, fnerrors.ErrMemoryLimit) {
		t.Fatalf("expected timeout to NOT match ErrMemoryLimit; got %v", err)
	}
}

func TestExecute_MemoryLimitReturnsErrMemoryLimitSentinel(t *testing.T) {
	cfg := Config{
		MaxExecutionTime: 10 * time.Second,
		MaxMemoryBytes:   1 * 1024 * 1024,
	}
	rt := NewRuntime(cfg)

	_, err := rt.Execute(context.Background(), `
		function main(input) {
			var arr = [];
			for (var i = 0; i < 10000000; i++) {
				arr.push("x".repeat(1000));
			}
			return arr.length;
		}
	`, nil)
	if err == nil {
		t.Fatal("expected memory limit error")
	}
	if !errors.Is(err, fnerrors.ErrMemoryLimit) {
		t.Fatalf("expected errors.Is(err, fnerrors.ErrMemoryLimit); got %v", err)
	}
	if errors.Is(err, fnerrors.ErrTimeout) {
		t.Fatalf("expected memory limit to NOT match ErrTimeout; got %v", err)
	}
}

func TestExecute_ThrownErrorIsNotQuotaSentinel(t *testing.T) {
	rt := NewRuntime(DefaultConfig())
	_, err := rt.Execute(context.Background(), `
		function main(input) {
			throw new Error("user-land failure");
		}
	`, nil)
	if err == nil {
		t.Fatal("expected thrown error")
	}
	if errors.Is(err, fnerrors.ErrTimeout) || errors.Is(err, fnerrors.ErrMemoryLimit) {
		t.Fatalf("non-quota error should not match sentinels; got %v", err)
	}
}

func TestDefaultConfig_MatchesPRDQuotas(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxExecutionTime != 5*time.Second {
		t.Errorf("DefaultConfig.MaxExecutionTime = %v, want 5s", cfg.MaxExecutionTime)
	}
	if cfg.MaxMemoryBytes != 128*1024*1024 {
		t.Errorf("DefaultConfig.MaxMemoryBytes = %d bytes, want 128MiB (PRD US-218)", cfg.MaxMemoryBytes)
	}
}
