package main

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/comments"
)

// pgCommentsStore satisfies comments.Store by persisting rows to the
// comments table (US-334). Lives in cmd/server/ rather than
// pkg/comments/ so the package stays free of any pgx import — same
// dep-direction trick as pgSavedSearchesStore.
type pgCommentsStore struct {
	pool *pgxpool.Pool
}

func newPGCommentsStore(pool *pgxpool.Pool) *pgCommentsStore {
	return &pgCommentsStore{pool: pool}
}

func (s *pgCommentsStore) Create(ctx context.Context, c *comments.Comment) error {
	if c.ParentID != "" {
		var parentTarget string
		var parentDeleted *string
		var parentParent *string
		err := s.pool.QueryRow(ctx,
			`SELECT target_rid, deleted_at::text, parent_id::text
			   FROM comments WHERE id = $1`, c.ParentID).
			Scan(&parentTarget, &parentDeleted, &parentParent)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return comments.ErrInvalidParent
			}
			return err
		}
		if parentDeleted != nil {
			return comments.ErrInvalidParent
		}
		if parentTarget != c.TargetRID {
			return comments.ErrInvalidParent
		}
		if parentParent != nil && *parentParent != "" {
			return comments.ErrInvalidParent
		}
	}
	var parentArg interface{}
	if c.ParentID == "" {
		parentArg = nil
	} else {
		parentArg = c.ParentID
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO comments (id, target_rid, body, author, parent_id)
		 VALUES ($1, $2, $3, $4, $5)`,
		c.ID, c.TargetRID, c.Body, c.Author, parentArg,
	)
	if err != nil {
		return err
	}
	fresh, err := s.Get(ctx, c.ID)
	if err != nil {
		return err
	}
	*c = *fresh
	return nil
}

const commentColumns = `id::text, target_rid, CASE WHEN deleted_at IS NULL THEN body ELSE '' END,
       author, COALESCE(parent_id::text, ''),
       created_at, updated_at, deleted_at`

func scanComment(row pgx.Row) (*comments.Comment, error) {
	var c comments.Comment
	if err := row.Scan(&c.ID, &c.TargetRID, &c.Body, &c.Author, &c.ParentID,
		&c.CreatedAt, &c.UpdatedAt, &c.DeletedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *pgCommentsStore) Get(ctx context.Context, id string) (*comments.Comment, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+commentColumns+` FROM comments WHERE id = $1`, id)
	c, err := scanComment(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, comments.ErrNotFound
		}
		return nil, err
	}
	return c, nil
}

func (s *pgCommentsStore) List(ctx context.Context, q comments.ListQuery) ([]*comments.Comment, int, error) {
	args := []interface{}{q.TargetRID}
	clauses := []string{"target_rid = $1"}
	if q.ParentID != "" {
		args = append(args, q.ParentID)
		clauses = append(clauses, "parent_id = $"+strconv.Itoa(len(args)))
	}
	if !q.IncludeDeleted {
		// Soft-deleted rows STILL surface in List so the SPA can
		// render [deleted] tombstones — Body is redacted by the
		// SELECT projection above. The flag stays as future-proofing
		// for an admin audit endpoint.
		_ = q.IncludeDeleted
	}
	where := strings.Join(clauses, " AND ")

	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM comments WHERE `+where, args...).
		Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = comments.DefaultPageLimit
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	args = append(args, limit, offset)
	rows, err := s.pool.Query(ctx,
		`SELECT `+commentColumns+` FROM comments WHERE `+where+
			` ORDER BY created_at ASC, id ASC`+
			` LIMIT $`+strconv.Itoa(len(args)-1)+
			` OFFSET $`+strconv.Itoa(len(args)),
		args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*comments.Comment
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (s *pgCommentsStore) Update(ctx context.Context, id, author string, upd comments.Update) error {
	if upd.Body == nil {
		// No-op update — bump updated_at? Match memory store semantics
		// which always bumps. UPDATE with only updated_at = NOW()
		// preserves the row's etag-shape change-detection signal.
		tag, err := s.pool.Exec(ctx,
			`UPDATE comments SET updated_at = NOW()
			   WHERE id = $1 AND deleted_at IS NULL AND author = $2`,
			id, author)
		if err != nil {
			return err
		}
		return classifyMutationResult(s.pool, ctx, id, author, tag.RowsAffected())
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE comments SET body = $3, updated_at = NOW()
		   WHERE id = $1 AND deleted_at IS NULL AND author = $2`,
		id, author, *upd.Body)
	if err != nil {
		return err
	}
	return classifyMutationResult(s.pool, ctx, id, author, tag.RowsAffected())
}

func (s *pgCommentsStore) Delete(ctx context.Context, id, author string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE comments SET deleted_at = NOW(), updated_at = NOW()
		   WHERE id = $1 AND deleted_at IS NULL AND author = $2`,
		id, author)
	if err != nil {
		return err
	}
	return classifyMutationResult(s.pool, ctx, id, author, tag.RowsAffected())
}

// classifyMutationResult turns a "no rows updated" outcome into the
// right typed error: ErrNotFound when the row is missing or already
// soft-deleted, ErrForbidden when the row exists and is live but
// authored by someone else. Two-step PG lookup pattern — same shape as
// the session-delete 404-vs-403 split (US-249 prior art).
func classifyMutationResult(pool *pgxpool.Pool, ctx context.Context, id, author string, rowsAffected int64) error {
	if rowsAffected > 0 {
		return nil
	}
	var rowAuthor string
	var deletedAt *string
	err := pool.QueryRow(ctx,
		`SELECT author, deleted_at::text FROM comments WHERE id = $1`, id).
		Scan(&rowAuthor, &deletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return comments.ErrNotFound
		}
		return err
	}
	if deletedAt != nil {
		return comments.ErrNotFound
	}
	if rowAuthor != author {
		return comments.ErrForbidden
	}
	return comments.ErrNotFound
}
