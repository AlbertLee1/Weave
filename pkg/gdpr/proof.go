package gdpr

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// ProofPayload is the canonical pre-image of an erase job's ProofHash.
// Auditors recompute ComputeProofHash(payload) over the persisted job
// fields to verify a deletion claim end-to-end.
//
// Field order is irrelevant on the wire — Marshal sorts keys
// alphabetically — but the Go struct ordering is kept stable so a
// future reviewer reading the struct literally sees the same shape the
// hash commits to.
type ProofPayload struct {
	UserID       string             `json:"userId"`
	Status       string             `json:"status"`
	ErrorMessage string             `json:"errorMessage,omitempty"`
	RequestedBy  string             `json:"requestedBy,omitempty"`
	Steps        []ProofPayloadStep `json:"steps"`
}

// ProofPayloadStep is the per-step contribution to ProofPayload. The
// duration is intentionally NOT included so two replays on different
// hardware produce identical proof hashes.
type ProofPayloadStep struct {
	Name         string `json:"name"`
	RowsAffected int    `json:"rowsAffected"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// ComputeProofHash returns the sha256 hex digest of the canonical JSON
// encoding of payload. Empty payload (zero ProofPayload) still produces
// a stable hash so callers don't have to special-case "no steps ran".
//
// Canonical encoding: encoding/json with default key emission (struct
// field order matches declaration), no indentation, no trailing newline.
// json.Marshal is deterministic for primitive-only payloads with no map
// fields, which is true here — the steps slice preserves the order in
// which the orchestrator ran them.
func ComputeProofHash(payload ProofPayload) string {
	encoded, err := json.Marshal(payload)
	if err != nil {
		// json.Marshal on a primitive-only payload cannot fail. If it
		// somehow does, surface a deterministic sentinel rather than
		// returning the empty string (which would shadow "no proof
		// available yet" semantics).
		return "sha256:error"
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// BuildProofPayload converts a finished ErasureJob into the canonical
// ProofPayload. Only the fields that contribute to the hash are
// projected — timestamps, progress, and the requesting actor's display
// fields are excluded by design.
func BuildProofPayload(job *ErasureJob) ProofPayload {
	if job == nil {
		return ProofPayload{Steps: []ProofPayloadStep{}}
	}
	steps := make([]ProofPayloadStep, 0, len(job.Steps))
	for _, s := range job.Steps {
		steps = append(steps, ProofPayloadStep{
			Name:         s.Name,
			RowsAffected: s.RowsAffected,
			ErrorMessage: s.ErrorMessage,
		})
	}
	return ProofPayload{
		UserID:       job.UserID,
		Status:       job.Status,
		ErrorMessage: job.ErrorMessage,
		RequestedBy:  job.RequestedBy,
		Steps:        steps,
	}
}
