package gdpr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestComputeProofHash_Deterministic(t *testing.T) {
	payload := ProofPayload{
		UserID: "user:alice",
		Status: JobStatusSucceeded,
		Steps: []ProofPayloadStep{
			{Name: "sessions", RowsAffected: 2},
			{Name: "user", RowsAffected: 1},
		},
		RequestedBy: "user:admin",
	}
	a := ComputeProofHash(payload)
	b := ComputeProofHash(payload)
	if a != b {
		t.Fatalf("hash not deterministic: %s vs %s", a, b)
	}
	if !strings.HasPrefix(a, "sha256:") {
		t.Errorf("missing sha256 prefix: %s", a)
	}
	if len(a) != len("sha256:")+64 {
		t.Errorf("hash length unexpected: %d", len(a))
	}
}

func TestComputeProofHash_DifferentiatesUserID(t *testing.T) {
	a := ComputeProofHash(ProofPayload{UserID: "user:alice", Status: JobStatusSucceeded, Steps: []ProofPayloadStep{}})
	b := ComputeProofHash(ProofPayload{UserID: "user:bob", Status: JobStatusSucceeded, Steps: []ProofPayloadStep{}})
	if a == b {
		t.Errorf("hash collides across userID: %s", a)
	}
}

func TestComputeProofHash_DifferentiatesStatus(t *testing.T) {
	base := ProofPayload{UserID: "u", Steps: []ProofPayloadStep{}}
	succeeded := base
	succeeded.Status = JobStatusSucceeded
	failed := base
	failed.Status = JobStatusFailed
	if ComputeProofHash(succeeded) == ComputeProofHash(failed) {
		t.Error("succeeded and failed jobs share a hash")
	}
}

func TestComputeProofHash_DifferentiatesStepOrder(t *testing.T) {
	a := ComputeProofHash(ProofPayload{
		UserID: "u", Status: JobStatusSucceeded,
		Steps: []ProofPayloadStep{{Name: "x"}, {Name: "y"}},
	})
	b := ComputeProofHash(ProofPayload{
		UserID: "u", Status: JobStatusSucceeded,
		Steps: []ProofPayloadStep{{Name: "y"}, {Name: "x"}},
	})
	if a == b {
		t.Error("step order is not committed to the hash")
	}
}

func TestComputeProofHash_DifferentiatesRowsAffected(t *testing.T) {
	a := ComputeProofHash(ProofPayload{UserID: "u", Status: JobStatusSucceeded, Steps: []ProofPayloadStep{{Name: "x", RowsAffected: 1}}})
	b := ComputeProofHash(ProofPayload{UserID: "u", Status: JobStatusSucceeded, Steps: []ProofPayloadStep{{Name: "x", RowsAffected: 2}}})
	if a == b {
		t.Error("rowsAffected should be committed to the hash")
	}
}

func TestComputeProofHash_EmptyPayloadStillStable(t *testing.T) {
	a := ComputeProofHash(ProofPayload{})
	b := ComputeProofHash(ProofPayload{})
	if a != b {
		t.Errorf("zero payload not deterministic: %s vs %s", a, b)
	}
	if a == "sha256:error" {
		t.Errorf("zero payload should not surface the error sentinel")
	}
}

func TestBuildProofPayload_ProjectsTerminalState(t *testing.T) {
	job := &ErasureJob{
		JobID:        "j-1",
		UserID:       "user:alice",
		Status:       JobStatusFailed,
		ErrorMessage: "boom",
		RequestedBy:  "user:admin",
		Steps: []StepResult{
			{Name: "sessions", RowsAffected: 5, DurationMs: 12},
			{Name: "user", RowsAffected: 0, ErrorMessage: "boom"},
		},
	}
	payload := BuildProofPayload(job)
	if payload.UserID != job.UserID {
		t.Errorf("UserID = %q, want %q", payload.UserID, job.UserID)
	}
	if payload.Status != job.Status {
		t.Errorf("Status = %q, want %q", payload.Status, job.Status)
	}
	if payload.ErrorMessage != job.ErrorMessage {
		t.Errorf("ErrorMessage = %q, want %q", payload.ErrorMessage, job.ErrorMessage)
	}
	if payload.RequestedBy != job.RequestedBy {
		t.Errorf("RequestedBy = %q, want %q", payload.RequestedBy, job.RequestedBy)
	}
	if len(payload.Steps) != len(job.Steps) {
		t.Fatalf("step count = %d, want %d", len(payload.Steps), len(job.Steps))
	}
	if payload.Steps[1].ErrorMessage != "boom" {
		t.Errorf("step error message did not propagate: %#v", payload.Steps[1])
	}
}

func TestBuildProofPayload_ExcludesDurationFromCanonicalShape(t *testing.T) {
	// DurationMs is wall-clock and would make replays on different
	// hardware produce mismatching hashes. Confirm the canonical
	// projection drops it.
	a := BuildProofPayload(&ErasureJob{
		UserID: "u", Status: JobStatusSucceeded,
		Steps: []StepResult{{Name: "x", RowsAffected: 1, DurationMs: 10}},
	})
	b := BuildProofPayload(&ErasureJob{
		UserID: "u", Status: JobStatusSucceeded,
		Steps: []StepResult{{Name: "x", RowsAffected: 1, DurationMs: 10000}},
	})
	if ComputeProofHash(a) != ComputeProofHash(b) {
		t.Error("duration affected the proof hash — should be excluded")
	}
}

func TestBuildProofPayload_NilJobReturnsEmptySteps(t *testing.T) {
	got := BuildProofPayload(nil)
	if got.Steps == nil {
		t.Error("expected non-nil empty Steps slice on nil input")
	}
	if len(got.Steps) != 0 {
		t.Errorf("Steps len = %d, want 0", len(got.Steps))
	}
}

func TestEraser_StampsProofHashOnTerminalJob(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryJobStore()
	if err := store.CreateJob(ctx, &ErasureJob{
		JobID: "j-1", UserID: "user:alice", Status: JobStatusPending, RequestedBy: "user:admin",
	}); err != nil {
		t.Fatal(err)
	}
	steps := []Step{
		StepFunc{StepName: "sessions", Fn: func(context.Context, string) (int, error) { return 2, nil }},
		StepFunc{StepName: "user", Fn: func(context.Context, string) (int, error) { return 1, nil }},
	}
	final, err := NewEraser(store, steps).Run(ctx, "j-1", "user:alice")
	if err != nil {
		t.Fatal(err)
	}
	if final.ProofHash == "" {
		t.Fatal("proof hash should be stamped on the terminal job")
	}
	stored, err := store.GetJob(ctx, "j-1")
	if err != nil {
		t.Fatal(err)
	}
	if stored.ProofHash != final.ProofHash {
		t.Errorf("stored hash %q != final hash %q", stored.ProofHash, final.ProofHash)
	}

	// Auditor recompute path: rebuild the canonical payload from the
	// persisted row and verify the hash matches what was stamped at
	// terminal time. This is the contract auditors use to verify a
	// deletion claim end-to-end.
	recomputed := ComputeProofHash(BuildProofPayload(stored))
	if recomputed != stored.ProofHash {
		t.Errorf("auditor recompute mismatch: got %s, stored %s", recomputed, stored.ProofHash)
	}
}

func TestEraser_StampsProofHashOnFailedJob(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryJobStore()
	if err := store.CreateJob(ctx, &ErasureJob{JobID: "j-2", UserID: "user:alice", Status: JobStatusPending}); err != nil {
		t.Fatal(err)
	}
	steps := []Step{
		StepFunc{StepName: "fail", Fn: func(context.Context, string) (int, error) {
			return 0, errors.New("boom")
		}},
	}
	final, err := NewEraser(store, steps).Run(ctx, "j-2", "user:alice")
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != JobStatusFailed {
		t.Fatalf("status = %s, want FAILED", final.Status)
	}
	if final.ProofHash == "" {
		t.Fatal("FAILED jobs must still carry a proof hash")
	}
	// Recompute on the post-terminal payload.
	recomputed := ComputeProofHash(BuildProofPayload(final))
	if recomputed != final.ProofHash {
		t.Errorf("hash mismatch after recompute: %s vs %s", recomputed, final.ProofHash)
	}
}

func TestComputeProofHash_CanonicalEncodingMatchesSHA256OfMarshal(t *testing.T) {
	// The canonical encoding contract is "encoding/json default emission";
	// pin that with a literal recompute so a future refactor that
	// silently changes the encoder shape (e.g. adds indentation or sorts
	// keys differently) trips this test.
	payload := ProofPayload{
		UserID:       "u",
		Status:       JobStatusSucceeded,
		ErrorMessage: "",
		RequestedBy:  "admin",
		Steps:        []ProofPayloadStep{{Name: "x", RowsAffected: 7}},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(encoded)
	got := ComputeProofHash(payload)
	expected := "sha256:" + hex.EncodeToString(want[:])
	if got != expected {
		t.Errorf("encoding contract drifted: got %s, want %s", got, expected)
	}
}
