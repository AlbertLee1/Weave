// Package transactions implements the in-memory backend for the Foundry
// OSv2 OntologyTransaction experimental endpoint
// (POST /api/v2/ontologies/{o}/transactions/{transactionId}/edits).
//
// A Store buffers funnel.Edit values per (ontology, transactionId). The
// endpoint is marked experimental in the OpenAPI spec and gated behind
// ?preview=true — only the "append edits" surface is implemented today.
// Commit / abort / conflict-resolution are intentionally left out until
// the Foundry transaction semantics stabilise.
package transactions

import (
	"context"
	"sync"

	"github.com/liyang/weave/pkg/funnel"
)

// Key identifies a single transaction. Transactions are scoped by ontology
// so the same transactionId value in two different ontologies does not
// collide.
type Key struct {
	Ontology      string
	TransactionID string
}

// Store buffers edits pending on an open transaction. Implementations
// MUST be safe for concurrent use.
type Store interface {
	// AppendEdits appends edits to the given transaction. Creates the
	// transaction on first use.
	AppendEdits(ctx context.Context, key Key, edits []funnel.Edit) error
	// ListEdits returns all edits buffered on the transaction, in the
	// order they were appended. Returns an empty slice for unknown keys.
	ListEdits(ctx context.Context, key Key) ([]funnel.Edit, error)
	// DeleteTransaction removes any buffered edits for the given key.
	// Idempotent: deleting an unknown key is not an error so retries are
	// safe. Round 59 added it to back the DELETE
	// .../transactions/{id} abort endpoint.
	DeleteTransaction(ctx context.Context, key Key) error
}

// MemoryStore is the default single-machine Store backed by a map guarded
// by a RWMutex.
type MemoryStore struct {
	mu   sync.RWMutex
	data map[Key][]funnel.Edit
}

// NewMemoryStore returns an empty in-memory Store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: make(map[Key][]funnel.Edit)}
}

// AppendEdits implements Store.
func (s *MemoryStore) AppendEdits(_ context.Context, key Key, edits []funnel.Edit) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = append(s.data[key], edits...)
	return nil
}

// ListEdits implements Store.
func (s *MemoryStore) ListEdits(_ context.Context, key Key) ([]funnel.Edit, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	src := s.data[key]
	out := make([]funnel.Edit, len(src))
	copy(out, src)
	return out, nil
}

// DeleteTransaction implements Store. Idempotent: the absence of the
// key in the map is not an error.
func (s *MemoryStore) DeleteTransaction(_ context.Context, key Key) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}
