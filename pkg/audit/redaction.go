package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// Redacted-event sentinels. Operators searching the audit log for a
// specific actor will see this prefix instead of the original ID; the
// suffix is a deterministic sha256 of the original actor_id so a cross-
// reference is still possible without re-identifying the user.
const (
	RedactedActorPrefix = "gdpr_redacted:"
	RedactedIP          = ""
	RedactedUserAgent   = ""
)

// RedactionStore tracks which actor_ids have been redacted under GDPR.
// Both reads (Has, List) and writes (Add) are needed by the GDPR erase
// orchestrator and the RedactingStore decorator below.
type RedactionStore interface {
	Add(ctx context.Context, actorID, reason string) error
	Has(ctx context.Context, actorID string) (bool, error)
}

// MemoryRedactionStore is the in-memory test double for RedactionStore.
type MemoryRedactionStore struct {
	mu   sync.Mutex
	rows map[string]string // actorID -> reason
}

// NewMemoryRedactionStore returns an empty in-memory RedactionStore.
func NewMemoryRedactionStore() *MemoryRedactionStore {
	return &MemoryRedactionStore{rows: map[string]string{}}
}

func (s *MemoryRedactionStore) Add(_ context.Context, actorID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[actorID] = reason
	return nil
}

func (s *MemoryRedactionStore) Has(_ context.Context, actorID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.rows[actorID]
	return ok, nil
}

// RedactingStore wraps a Store so List/Insert observe a redaction overlay.
//
// On List: every event whose ActorID is in the RedactionStore has its
// PII fields rewritten in-place — ActorID becomes "gdpr_redacted:<sha>",
// IP / UserAgent / DiffJSON are cleared. The original chain_seq /
// prev_hash / entry_hash fields are PRESERVED so a verifier walking the
// chain via ListChain still sees the original linkage; the redaction
// happens at the API surface only.
//
// Insert is delegated unchanged. The RedactingStore never refuses to
// write a new event for a redacted actor — that case is rare (GDPR
// erasure typically deletes the user account so no further events can
// be authored under that ID) but if it occurs the new event is
// captured normally and the redaction overlay applies on read.
type RedactingStore struct {
	inner      Store
	redactions RedactionStore
}

// NewRedactingStore wraps inner so its List output is overlaid with the
// redaction view from redactions. A nil RedactionStore makes the
// decorator a passthrough (zero allocation, useful for degraded mode).
func NewRedactingStore(inner Store, redactions RedactionStore) *RedactingStore {
	return &RedactingStore{inner: inner, redactions: redactions}
}

func (s *RedactingStore) Insert(ctx context.Context, evt AuditEvent) error {
	return s.inner.Insert(ctx, evt)
}

func (s *RedactingStore) List(ctx context.Context, f ListFilter) ([]AuditEvent, error) {
	events, err := s.inner.List(ctx, f)
	if err != nil {
		return nil, err
	}
	if s.redactions == nil || len(events) == 0 {
		return events, nil
	}
	for i := range events {
		hit, herr := s.redactions.Has(ctx, events[i].ActorID)
		if herr != nil {
			// Fail closed: a transient lookup error must NOT leak the
			// raw event. Treat the row as if it were redacted.
			ApplyRedaction(&events[i])
			continue
		}
		if hit {
			ApplyRedaction(&events[i])
		}
	}
	return events, nil
}

// ApplyRedaction rewrites the PII-bearing fields of evt in place. The
// ChainSeq / PrevHash / EntryHash columns are preserved so the audit
// chain remains independently verifiable via ListChain + VerifyChain.
//
// Exposed for callers (e.g. SDK consumers, future export pipelines)
// that want to apply the same redaction view without going through
// RedactingStore.
func ApplyRedaction(evt *AuditEvent) {
	if evt == nil {
		return
	}
	evt.ActorID = RedactActorID(evt.ActorID)
	evt.IP = RedactedIP
	evt.UserAgent = RedactedUserAgent
	evt.DiffJSON = nil
}

// RedactActorID maps an original actor_id to a stable, non-reversible
// sentinel of the form "gdpr_redacted:<hex sha256>". The hash is
// deterministic so two events from the same actor still group together
// in operator dashboards (without revealing the underlying identity).
// An empty input maps to a fixed sentinel so the wire stays non-empty.
func RedactActorID(actorID string) string {
	if actorID == "" {
		return RedactedActorPrefix + "anonymous"
	}
	sum := sha256.Sum256([]byte(actorID))
	return RedactedActorPrefix + hex.EncodeToString(sum[:])
}
