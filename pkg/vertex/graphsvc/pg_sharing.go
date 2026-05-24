package graphsvc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGShareLinkStore is the PostgreSQL-backed ShareLinkStore built on the
// graph_share_links table from migration 000203.
type PGShareLinkStore struct {
	pool *pgxpool.Pool
}

// NewPGShareLinkStore wires a PGShareLinkStore over an existing pgx pool.
func NewPGShareLinkStore(pool *pgxpool.Pool) *PGShareLinkStore {
	return &PGShareLinkStore{pool: pool}
}

// Create inserts a new share-link row. Token uniqueness is enforced by the
// PRIMARY KEY constraint; conflicts (cosmically unlikely for 24 bytes of
// entropy) surface as a generic insert error.
func (s *PGShareLinkStore) Create(ctx context.Context, link *ShareLink) error {
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO graph_share_links (token, graph_rid, created_by, created_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		link.Token, link.GraphRID, link.CreatedBy, link.CreatedAt, nullableShareLinkTime(link.ExpiresAt),
	); err != nil {
		return fmt.Errorf("insert share link: %w", err)
	}
	return nil
}

// Get returns ErrShareLinkNotFound when the token does not match a row.
func (s *PGShareLinkStore) Get(ctx context.Context, token string) (*ShareLink, error) {
	link := &ShareLink{}
	var expiresAt *time.Time
	var revokedAt *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT token, graph_rid, created_by, created_at, expires_at, revoked, revoked_at
		 FROM graph_share_links WHERE token = $1`, token,
	).Scan(&link.Token, &link.GraphRID, &link.CreatedBy, &link.CreatedAt,
		&expiresAt, &link.Revoked, &revokedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrShareLinkNotFound
		}
		return nil, fmt.Errorf("get share link: %w", err)
	}
	if expiresAt != nil {
		link.ExpiresAt = *expiresAt
	}
	if revokedAt != nil {
		link.RevokedAt = *revokedAt
	}
	return link, nil
}

func nullableShareLinkTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

// Revoke flips the row's revoked flag. ErrShareLinkNotFound when the token
// never existed; revoking an already-revoked link is idempotent.
func (s *PGShareLinkStore) Revoke(ctx context.Context, token string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE graph_share_links
		   SET revoked = TRUE, revoked_at = COALESCE(revoked_at, NOW())
		 WHERE token = $1`, token)
	if err != nil {
		return fmt.Errorf("revoke share link: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrShareLinkNotFound
	}
	return nil
}

// ListByGraph returns every share-link for graphRID sorted newest-first.
// Backed by the graph_share_links_graph_idx index on graph_rid from
// migration 000203 so the scan stays index-bounded even on large
// tables. Includes revoked rows — the owner's manage-shares panel
// needs to surface them to explain 410-Gone errors on revoked links.
func (s *PGShareLinkStore) ListByGraph(ctx context.Context, graphRID string) ([]*ShareLink, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT token, graph_rid, created_by, created_at, expires_at, revoked, revoked_at
		   FROM graph_share_links
		  WHERE graph_rid = $1
		  ORDER BY created_at DESC`, graphRID)
	if err != nil {
		return nil, fmt.Errorf("list share links: %w", err)
	}
	defer rows.Close()
	out := []*ShareLink{}
	for rows.Next() {
		link := &ShareLink{}
		var expiresAt *time.Time
		var revokedAt *time.Time
		if err := rows.Scan(&link.Token, &link.GraphRID, &link.CreatedBy, &link.CreatedAt,
			&expiresAt, &link.Revoked, &revokedAt); err != nil {
			return nil, fmt.Errorf("scan share link: %w", err)
		}
		if expiresAt != nil {
			link.ExpiresAt = *expiresAt
		}
		if revokedAt != nil {
			link.RevokedAt = *revokedAt
		}
		out = append(out, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate share links: %w", err)
	}
	return out, nil
}

var _ ShareLinkStore = (*PGShareLinkStore)(nil)
