package actions

import (
	"context"
	"testing"
)

func TestExecutor_CancelJob_FiresRegisteredCancel(t *testing.T) {
	e := &Executor{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.RegisterJobCancel("job-1", cancel)

	if !e.CancelJob("job-1") {
		t.Fatalf("CancelJob returned false; expected true for registered job")
	}
	select {
	case <-ctx.Done():
		// expected
	default:
		t.Fatalf("ctx not cancelled after CancelJob")
	}
	// Second cancel must report false — registry entry was consumed.
	if e.CancelJob("job-1") {
		t.Fatalf("second CancelJob returned true; expected false")
	}
}

func TestExecutor_CancelJob_UnknownIDReturnsFalse(t *testing.T) {
	e := &Executor{}
	if e.CancelJob("missing") {
		t.Fatalf("CancelJob returned true for missing job")
	}
	if e.CancelJob("") {
		t.Fatalf("CancelJob returned true for empty jobID")
	}
}

func TestExecutor_UnregisterJobCancel_PreventsCancel(t *testing.T) {
	e := &Executor{}
	_, cancel := context.WithCancel(context.Background())
	e.RegisterJobCancel("job-1", cancel)
	e.UnregisterJobCancel("job-1")
	if e.CancelJob("job-1") {
		t.Fatalf("CancelJob returned true after UnregisterJobCancel")
	}
}

func TestExecutor_RegisterJobCancel_NoOpOnEmpty(t *testing.T) {
	e := &Executor{}
	_, cancel := context.WithCancel(context.Background())
	// Empty jobID and nil cancel are both no-ops.
	e.RegisterJobCancel("", cancel)
	e.RegisterJobCancel("job", nil)
	if e.CancelJob("") {
		t.Fatalf("CancelJob(\"\") returned true")
	}
	if e.CancelJob("job") {
		t.Fatalf("CancelJob(\"job\") returned true after nil cancel registration")
	}
}
