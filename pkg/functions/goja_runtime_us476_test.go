package functions

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/functions/fnerrors"
)

// US-476: the security engineer's locked-down sandbox profile.
// PRD literal: "8 层递归、100MB heap、1s 超时（可配）" + negative tests for
// infinite recursion + timeout cancellation.
//
// These tests pin down four invariants that US-218 left implicit:
//  1. MaxCallStackSize is configurable per Config (was hardcoded at 1024).
//  2. RestrictedConfig() returns the PRD US-476 numerals exactly so callers
//     who want the safety profile can opt-in by name.
//  3. With the restricted stack quota, infinite recursion aborts in
//     milliseconds via a goja stack-overflow error — not via the timeout
//     watchdog (so security incidents don't burn the full 1s budget before
//     unwinding).
//  4. With the restricted timeout, an infinite loop is cancelled within
//     ~2s of the configured 1s deadline and surfaces fnerrors.ErrTimeout.

func TestRestrictedConfig_MatchesPRDQuotas_US476(t *testing.T) {
	cfg := RestrictedConfig()
	if cfg.MaxExecutionTime != 1*time.Second {
		t.Errorf("RestrictedConfig.MaxExecutionTime = %v, want 1s (PRD US-476)", cfg.MaxExecutionTime)
	}
	const wantMem = int64(100 * 1024 * 1024)
	if cfg.MaxMemoryBytes != wantMem {
		t.Errorf("RestrictedConfig.MaxMemoryBytes = %d bytes, want 100MiB (PRD US-476)", cfg.MaxMemoryBytes)
	}
	if cfg.MaxCallStackSize != 8 {
		t.Errorf("RestrictedConfig.MaxCallStackSize = %d, want 8 (PRD US-476)", cfg.MaxCallStackSize)
	}
}

func TestConfig_MaxCallStackSize_IsConfigurable_US476(t *testing.T) {
	// PRD acceptance: 可配. Verify the field exists and Execute honors it
	// by using a smaller-than-default value (8) and a larger one (256).
	for _, depth := range []int{8, 32, 256} {
		depth := depth
		t.Run("depth_"+itoa(depth), func(t *testing.T) {
			cfg := Config{
				MaxExecutionTime: 5 * time.Second,
				MaxMemoryBytes:   64 * 1024 * 1024,
				MaxCallStackSize: depth,
			}
			rt := NewRuntime(cfg)
			start := time.Now()
			_, err := rt.Execute(context.Background(), `
				function main(input) {
					function recurse(n) { return recurse(n + 1); }
					return recurse(0);
				}
			`, nil)
			elapsed := time.Since(start)
			if err == nil {
				t.Fatalf("depth=%d: expected stack overflow error", depth)
			}
			// Stack quota MUST be tripped before the 5s timeout — if depth=8
			// took 5s to abort, the runtime is enforcing timeout not stack.
			if elapsed > 2*time.Second {
				t.Fatalf("depth=%d: aborted in %v — expected stack-quota trip well under 2s, looks like timeout instead", depth, elapsed)
			}
			// Goja stack overflow errors carry "stack" / "overflow" or the
			// recurse call-site marker; never the timeout sentinel.
			if errors.Is(err, fnerrors.ErrTimeout) {
				t.Fatalf("depth=%d: expected stack-quota error, got ErrTimeout: %v", depth, err)
			}
			es := err.Error()
			if !strings.Contains(es, "stack") && !strings.Contains(es, "overflow") && !strings.Contains(es, "at recurse") {
				t.Fatalf("depth=%d: expected stack-quota error text, got %q", depth, es)
			}
		})
	}
}

func TestRestrictedConfig_AbortsInfiniteRecursion_US476(t *testing.T) {
	// PRD negative test #1: 无限递归被 abort.
	// RestrictedConfig pins MaxCallStackSize = 8, so an infinite recursive
	// helper must trip in well under the 1s timeout and never reach it.
	rt := NewRuntime(RestrictedConfig())
	start := time.Now()
	_, err := rt.Execute(context.Background(), `
		function main(input) {
			function infinite() { return infinite(); }
			return infinite();
		}
	`, nil)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected stack-overflow error for infinite recursion")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("infinite recursion aborted in %v — expected stack-quota trip in <500ms (timeout is 1s)", elapsed)
	}
	if errors.Is(err, fnerrors.ErrTimeout) {
		t.Fatalf("infinite recursion fell through to timeout instead of stack quota: %v", err)
	}
	if errors.Is(err, fnerrors.ErrMemoryLimit) {
		t.Fatalf("infinite recursion fell through to memory quota instead of stack quota: %v", err)
	}
}

func TestRestrictedConfig_CancelsTimeoutFunction_US476(t *testing.T) {
	// PRD negative test #2: 超时函数被 cancel.
	// RestrictedConfig pins MaxExecutionTime = 1s. An infinite while-loop
	// must be cancelled within a small margin and surface ErrTimeout.
	rt := NewRuntime(RestrictedConfig())
	start := time.Now()
	_, err := rt.Execute(context.Background(), `
		function main(input) {
			while (true) {}
		}
	`, nil)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error for infinite loop")
	}
	if !errors.Is(err, fnerrors.ErrTimeout) {
		t.Fatalf("expected ErrTimeout sentinel, got %v", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("timeout cancellation took %v — expected ~1s + small margin", elapsed)
	}
	if elapsed < 900*time.Millisecond {
		// The watchdog should not fire before the budget elapses; if it does,
		// some prior test's leftover Interrupt may have leaked in.
		t.Fatalf("timeout fired in %v — expected ~1s (budget violated early)", elapsed)
	}
}

func TestConfig_ZeroMaxCallStackSize_FallsBackToDefault_US476(t *testing.T) {
	// Backward-compat invariant: Config{MaxCallStackSize: 0} should not
	// silently uncap recursion. Execute must inherit the package default
	// (1024) so callers that built Config literals before US-476 still
	// have stack protection.
	cfg := Config{
		MaxExecutionTime: 5 * time.Second,
		MaxMemoryBytes:   64 * 1024 * 1024,
		MaxCallStackSize: 0,
	}
	rt := NewRuntime(cfg)
	start := time.Now()
	_, err := rt.Execute(context.Background(), `
		function main(input) {
			function recurse(n) { return recurse(n + 1); }
			return recurse(0);
		}
	`, nil)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected stack overflow even when MaxCallStackSize is zero (default fallback)")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("zero MaxCallStackSize did not fall back to a sensible default; aborted in %v", elapsed)
	}
}

func TestDefaultConfig_StillSetsStackQuota_US476(t *testing.T) {
	// US-218 DefaultConfig (5s/128MB) stays unchanged numerically for
	// callers who built on top of it. US-476 only ADDS the new stack-quota
	// field with the historical default value so existing wiring is
	// unaffected.
	cfg := DefaultConfig()
	if cfg.MaxCallStackSize <= 0 {
		t.Fatalf("DefaultConfig.MaxCallStackSize = %d, want a positive stack quota", cfg.MaxCallStackSize)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
