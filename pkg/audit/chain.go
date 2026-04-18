package audit

// US-266: tamper-proof audit log hash chain.
//
// Every AuditEvent carries two new fields: PrevHash (the EntryHash of the
// preceding event in the chain) and EntryHash (sha256 over the canonical
// envelope of THIS event plus PrevHash). Together they form an append-only
// Merkle-style chain: flipping any byte of any historical event, or
// reordering/dropping any event, changes the running hash downstream and
// VerifyChain flags the divergence.
//
// Canonicalisation is deliberate: JSONB round-trip through Postgres
// re-serialises whitespace and sorts keys non-deterministically, so the
// hash MUST be computed over a stable canonical form (sorted keys, RFC3339
// UTC timestamps, diff parsed to a Go value then re-emitted). HashEvent is
// idempotent under re-entry — verifying a row twice produces the same hash.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// HashEvent returns the hex-encoded sha256 over the canonical envelope of
// evt chained to prevHash. An empty prevHash represents the head of the
// chain (the very first audit entry ever written).
//
// The canonical envelope is a JSON object with alphabetically-sorted keys:
//
//	{
//	  "action":       <string>,
//	  "actor_id":     <string>,
//	  "diff":         <canonical JSON | null>,
//	  "id":           <string>,
//	  "ip":           <string>,
//	  "prev_hash":    <string>,
//	  "resource_rid": <string>,
//	  "resource_type":<string>,
//	  "ts":           <RFC3339Nano UTC string>,
//	  "user_agent":   <string>
//	}
//
// Returns an error when DiffJSON is set but not valid JSON.
func HashEvent(prevHash string, evt AuditEvent) (string, error) {
	envelope := map[string]interface{}{
		"action":        evt.Action,
		"actor_id":      evt.ActorID,
		"id":            evt.ID,
		"ip":            evt.IP,
		"prev_hash":     prevHash,
		"resource_rid":  evt.ResourceRID,
		"resource_type": evt.ResourceType,
		"ts":            evt.Timestamp.UTC().Format("2006-01-02T15:04:05.000000000Z07:00"),
		"user_agent":    evt.UserAgent,
	}
	if len(evt.DiffJSON) == 0 {
		envelope["diff"] = nil
	} else {
		var parsed interface{}
		if err := json.Unmarshal(evt.DiffJSON, &parsed); err != nil {
			return "", fmt.Errorf("audit: diff_json is not valid JSON: %w", err)
		}
		envelope["diff"] = parsed
	}
	bytes, err := canonicalJSON(envelope)
	if err != nil {
		return "", fmt.Errorf("audit: canonicalise envelope: %w", err)
	}
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:]), nil
}

// ComputeRootHash returns the sha256 over the concatenated EntryHash values
// of events (ordered as given). It's the daily publication artefact: when
// published to an append-only file every day, operators can re-verify
// history by re-computing root hashes from the DB chain and comparing
// byte-for-byte against the file.
//
// Returns the empty string for nil/empty input so the caller can treat "no
// events today" as a skip rather than a hash over the empty string.
func ComputeRootHash(events []AuditEvent) string {
	if len(events) == 0 {
		return ""
	}
	h := sha256.New()
	for _, e := range events {
		h.Write([]byte(e.EntryHash))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ChainVerificationError describes the FIRST inconsistency found while
// walking a chain. Index is the 0-based position in the input slice;
// EventID / ChainSeq identify the row for operator follow-up. Reason is a
// short human-readable description of the failure mode.
type ChainVerificationError struct {
	Index    int
	EventID  string
	ChainSeq int64
	Reason   string
}

func (e *ChainVerificationError) Error() string {
	return fmt.Sprintf("audit chain verification failed at index %d (chain_seq=%d, id=%s): %s",
		e.Index, e.ChainSeq, e.EventID, e.Reason)
}

// VerifyChain walks events (assumed ordered by chain_seq ASC) and returns
// the first inconsistency it finds, wrapped in a *ChainVerificationError.
// Checks, in order:
//
//   - chain_seq is strictly increasing by 1 starting from the first row
//     (any gap = dropped row or non-contiguous export)
//   - prev_hash == previous row's entry_hash (chain linkage intact)
//   - entry_hash == HashEvent(prev_hash, row) (row content not tampered)
//
// Returns nil for an empty chain.
func VerifyChain(events []AuditEvent) error {
	for i, e := range events {
		// chain_seq gap check. Expect chain_seq[0] = first row's own
		// value (could be > 1 if a subrange was loaded), and each
		// subsequent increments by exactly 1 within the given slice.
		if i > 0 && e.ChainSeq != events[i-1].ChainSeq+1 {
			return &ChainVerificationError{
				Index:    i,
				EventID:  e.ID,
				ChainSeq: e.ChainSeq,
				Reason: fmt.Sprintf("chain_seq gap: expected %d, got %d",
					events[i-1].ChainSeq+1, e.ChainSeq),
			}
		}

		// prev_hash linkage check.
		var expectedPrev string
		if i > 0 {
			expectedPrev = events[i-1].EntryHash
		}
		if e.PrevHash != expectedPrev {
			return &ChainVerificationError{
				Index:    i,
				EventID:  e.ID,
				ChainSeq: e.ChainSeq,
				Reason: fmt.Sprintf("prev_hash mismatch: expected %q, got %q",
					expectedPrev, e.PrevHash),
			}
		}

		// Content hash check (this is what catches "someone edited
		// columns directly in PG").
		computed, err := HashEvent(e.PrevHash, e)
		if err != nil {
			return &ChainVerificationError{
				Index: i, EventID: e.ID, ChainSeq: e.ChainSeq,
				Reason: "recompute hash: " + err.Error(),
			}
		}
		if computed != e.EntryHash {
			return &ChainVerificationError{
				Index:    i,
				EventID:  e.ID,
				ChainSeq: e.ChainSeq,
				Reason: fmt.Sprintf("entry hash mismatch: computed %s, stored %s",
					computed, e.EntryHash),
			}
		}
	}
	return nil
}

// canonicalJSON encodes v with object keys sorted recursively so that two
// logically-equal maps marshal to the same bytes. Mirrors the helper in
// pkg/functions/cache.canonicalJSON; duplicated here to avoid a cross-
// package dependency just for a 50-line encoder.
func canonicalJSON(v interface{}) ([]byte, error) {
	switch typed := v.(type) {
	case nil:
		return []byte("null"), nil
	case map[string]interface{}:
		keys := make([]string, 0, len(typed))
		for k := range typed {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := []byte{'{'}
		for i, k := range keys {
			if i > 0 {
				out = append(out, ',')
			}
			keyBytes, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			out = append(out, keyBytes...)
			out = append(out, ':')
			child, err := canonicalJSON(typed[k])
			if err != nil {
				return nil, err
			}
			out = append(out, child...)
		}
		out = append(out, '}')
		return out, nil
	case []interface{}:
		out := []byte{'['}
		for i, item := range typed {
			if i > 0 {
				out = append(out, ',')
			}
			child, err := canonicalJSON(item)
			if err != nil {
				return nil, err
			}
			out = append(out, child...)
		}
		out = append(out, ']')
		return out, nil
	default:
		return json.Marshal(typed)
	}
}
