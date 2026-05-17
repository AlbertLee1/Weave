package gdpr

import (
	"context"
	"errors"
	"testing"
)

// fakeCascade satisfies any of the cascade-deleter interfaces in
// steps_cascade.go (they share the same DeleteAllForUser signature). The
// step constructors only depend on the method, so a single fake covers
// all of them.
type fakeCascade struct {
	calls []string
	rows  int
	err   error
}

func (f *fakeCascade) DeleteAllForUser(_ context.Context, userID string) (int, error) {
	f.calls = append(f.calls, userID)
	return f.rows, f.err
}

func TestCommentsCascadeStep_DelegatesToDeleter(t *testing.T) {
	d := &fakeCascade{rows: 3}
	step := NewCommentsCascadeStep(d)
	if step.Name() != "comments_cascade" {
		t.Errorf("name = %q, want comments_cascade", step.Name())
	}
	n, err := step.Erase(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("rows = %d, want 3", n)
	}
	if len(d.calls) != 1 || d.calls[0] != "alice" {
		t.Errorf("calls = %v, want [alice]", d.calls)
	}
}

func TestCascadeSteps_NilAdapterIsNoop(t *testing.T) {
	cases := []struct {
		name string
		step Step
	}{
		{"comments", NewCommentsCascadeStep(nil)},
		{"reactions", NewReactionsCascadeStep(nil)},
		{"watches", NewWatchesCascadeStep(nil)},
		{"user_preferences", NewUserPrefsCascadeStep(nil)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n, err := c.step.Erase(context.Background(), "alice")
			if err != nil {
				t.Errorf("nil adapter returned err: %v", err)
			}
			if n != 0 {
				t.Errorf("nil adapter rows = %d, want 0", n)
			}
		})
	}
}

func TestCascadeStep_PropagatesError(t *testing.T) {
	want := errors.New("db down")
	step := NewWatchesCascadeStep(&fakeCascade{err: want})
	_, err := step.Erase(context.Background(), "alice")
	if !errors.Is(err, want) {
		t.Errorf("got %v, want %v", err, want)
	}
}
