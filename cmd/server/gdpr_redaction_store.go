package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// pgGDPRRedactionStore satisfies audit.RedactionStore by overlaying
// gdpr_redactions onto the audit List path. The audit chain itself is
// never mutated — the RedactingStore decorator scrubs PII at read time
// for any row whose actor_id has been added here.
type pgGDPRRedactionStore struct {
	pool *pgxpool.Pool
}

func newPGGDPRRedactionStore(pool *pgxpool.Pool) *pgGDPRRedactionStore {
	return &pgGDPRRedactionStore{pool: pool}
}

func (s *pgGDPRRedactionStore) Add(ctx context.Context, actorID, reason string) error {
	if reason == "" {
		reason = "gdpr_erase"
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO gdpr_redactions (actor_id, reason)
		 VALUES ($1, $2)
		 ON CONFLICT (actor_id) DO UPDATE
		   SET reason = EXCLUDED.reason, redacted_at = NOW()`,
		actorID, reason)
	return err
}

func (s *pgGDPRRedactionStore) Has(ctx context.Context, actorID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM gdpr_redactions WHERE actor_id = $1)`,
		actorID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}
