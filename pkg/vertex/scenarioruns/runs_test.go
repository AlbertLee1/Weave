package scenarioruns_test

import (
	"errors"
	"testing"

	"github.com/liyang/weave/pkg/vertex/scenarioruns"
)

func TestRetryPolicy_Given_ZeroMaxAttempts_When_Normalize_Then_DefaultsTo3(t *testing.T) {
	got := scenarioruns.RetryPolicy{}.Normalize()
	if got.MaxAttempts != 3 {
		t.Fatalf("MaxAttempts: got %d want 3", got.MaxAttempts)
	}
}

func TestRetryPolicy_Given_NegativeBackoff_When_Normalize_Then_ClampedToZero(t *testing.T) {
	got := scenarioruns.RetryPolicy{MaxAttempts: 5, BackoffMs: -10}.Normalize()
	if got.BackoffMs != 0 {
		t.Fatalf("BackoffMs: got %d want 0", got.BackoffMs)
	}
	if got.MaxAttempts != 5 {
		t.Fatalf("MaxAttempts: got %d want 5", got.MaxAttempts)
	}
}

func TestIsTerminal_Given_StatusValues_When_Check_Then_OnlyTerminalReturnsTrue(t *testing.T) {
	cases := map[scenarioruns.RunStatus]bool{
		scenarioruns.RunStatusPending:   false,
		scenarioruns.RunStatusRunning:   false,
		scenarioruns.RunStatusSucceeded: true,
		scenarioruns.RunStatusFailed:    true,
		scenarioruns.RunStatusCanceled:  true,
	}
	for status, want := range cases {
		if got := scenarioruns.IsTerminal(status); got != want {
			t.Errorf("IsTerminal(%q): got %v want %v", status, got, want)
		}
	}
}

func TestIsResumable_Given_StatusValues_When_Check_Then_OnlyPendingOrRunning(t *testing.T) {
	cases := map[scenarioruns.RunStatus]bool{
		scenarioruns.RunStatusPending:   true,
		scenarioruns.RunStatusRunning:   true,
		scenarioruns.RunStatusSucceeded: false,
		scenarioruns.RunStatusFailed:    false,
		scenarioruns.RunStatusCanceled:  false,
	}
	for status, want := range cases {
		if got := scenarioruns.IsResumable(status); got != want {
			t.Errorf("IsResumable(%q): got %v want %v", status, got, want)
		}
	}
}

func TestSkipCompleted_Given_PartialProgress_When_Skip_Then_ReturnsRemaining(t *testing.T) {
	all := []scenarioruns.Activity{
		{ID: "a1", Layer: 0},
		{ID: "a2", Layer: 0},
		{ID: "a3", Layer: 1},
		{ID: "a4", Layer: 2},
	}
	out := scenarioruns.SkipCompleted(all, []string{"a1", "a3"})
	if len(out) != 2 {
		t.Fatalf("len: got %d want 2", len(out))
	}
	if out[0].ID != "a2" || out[1].ID != "a4" {
		t.Fatalf("got %#v", out)
	}
}

func TestSkipCompleted_Given_NoCompleted_When_Skip_Then_ReturnsAll(t *testing.T) {
	all := []scenarioruns.Activity{{ID: "a1"}, {ID: "a2"}}
	out := scenarioruns.SkipCompleted(all, nil)
	if len(out) != 2 {
		t.Fatalf("len: got %d want 2", len(out))
	}
}

func TestSkipCompleted_Given_AllCompleted_When_Skip_Then_ReturnsEmpty(t *testing.T) {
	all := []scenarioruns.Activity{{ID: "a1"}, {ID: "a2"}}
	out := scenarioruns.SkipCompleted(all, []string{"a1", "a2"})
	if len(out) != 0 {
		t.Fatalf("len: got %d want 0", len(out))
	}
}

func TestErrSentinels_Given_PackageInit_When_Compare_Then_AreDistinct(t *testing.T) {
	if scenarioruns.ErrRunNotFound == nil {
		t.Fatal("ErrRunNotFound nil")
	}
	if scenarioruns.ErrAlreadyTerminal == nil {
		t.Fatal("ErrAlreadyTerminal nil")
	}
	if errors.Is(scenarioruns.ErrRunNotFound, scenarioruns.ErrAlreadyTerminal) {
		t.Fatal("sentinels collapsed")
	}
}
