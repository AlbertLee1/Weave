package main

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/permissionrequests"
)

// pgPermissionRequestsStore satisfies permissionrequests.Store by
// persisting rows to the permission_requests table (US-339). Lives in
// cmd/server/ rather than pkg/permissionrequests/ so the package stays
// free of any pgx import — same dep-direction trick as
// pgCommentsStore / pgWatchesStore / pgDashboardsStore.
type pgPermissionRequestsStore struct {
	pool *pgxpool.Pool
}

func newPGPermissionRequestsStore(pool *pgxpool.Pool) *pgPermissionRequestsStore {
	return &pgPermissionRequestsStore{pool: pool}
}

func (s *pgPermissionRequestsStore) Create(ctx context.Context, r *permissionrequests.Request) error {
	if r.Status == "" {
		r.Status = permissionrequests.StatusPending
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO permission_requests
		   (id, target_rid, requested_by, reason, status)
		 VALUES ($1, $2, $3, $4, $5)`,
		r.ID, r.TargetRID, r.RequestedBy, r.Reason, r.Status,
	)
	if err != nil {
		return err
	}
	fresh, err := s.Get(ctx, r.ID)
	if err != nil {
		return err
	}
	*r = *fresh
	return nil
}

const permissionRequestColumns = `id::text, target_rid, requested_by,
       COALESCE(reason, ''), status,
       COALESCE(decided_by, ''), COALESCE(decision_note, ''),
       created_at, updated_at, decided_at`

func scanPermissionRequest(row pgx.Row) (*permissionrequests.Request, error) {
	var r permissionrequests.Request
	if err := row.Scan(
		&r.ID, &r.TargetRID, &r.RequestedBy, &r.Reason, &r.Status,
		&r.DecidedBy, &r.DecisionNote,
		&r.CreatedAt, &r.UpdatedAt, &r.DecidedAt,
	); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *pgPermissionRequestsStore) Get(ctx context.Context, id string) (*permissionrequests.Request, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+permissionRequestColumns+` FROM permission_requests WHERE id = $1`, id)
	r, err := scanPermissionRequest(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, permissionrequests.ErrNotFound
		}
		return nil, err
	}
	return r, nil
}

func (s *pgPermissionRequestsStore) List(ctx context.Context, q permissionrequests.ListQuery) ([]*permissionrequests.Request, int, error) {
	args := []interface{}{}
	clauses := []string{}
	if q.Status != "" {
		args = append(args, q.Status)
		clauses = append(clauses, "status = $"+strconv.Itoa(len(args)))
	}
	if q.RequestedBy != "" {
		args = append(args, q.RequestedBy)
		clauses = append(clauses, "requested_by = $"+strconv.Itoa(len(args)))
	}
	if q.TargetRID != "" {
		args = append(args, q.TargetRID)
		clauses = append(clauses, "target_rid = $"+strconv.Itoa(len(args)))
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}

	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM permission_requests`+where, args...).
		Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = permissionrequests.DefaultPageLimit
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	args = append(args, limit, offset)
	rows, err := s.pool.Query(ctx,
		`SELECT `+permissionRequestColumns+` FROM permission_requests`+where+
			` ORDER BY created_at DESC, id ASC`+
			` LIMIT $`+strconv.Itoa(len(args)-1)+
			` OFFSET $`+strconv.Itoa(len(args)),
		args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*permissionrequests.Request
	for rows.Next() {
		r, err := scanPermissionRequest(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (s *pgPermissionRequestsStore) Decide(ctx context.Context, id string, dec permissionrequests.Decision) error {
	if !permissionrequests.IsTerminalStatus(dec.Status) {
		return permissionrequests.ErrInvalidStatus
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE permission_requests
		    SET status = $2, decided_by = $3, decision_note = $4,
		        decided_at = NOW(), updated_at = NOW()
		  WHERE id = $1 AND status = 'PENDING'`,
		id, dec.Status, dec.By, dec.Note,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	// Either the row is missing or it's already decided — disambiguate.
	var status string
	if err := s.pool.QueryRow(ctx,
		`SELECT status FROM permission_requests WHERE id = $1`, id).
		Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return permissionrequests.ErrNotFound
		}
		return err
	}
	if permissionrequests.IsTerminalStatus(status) {
		return permissionrequests.ErrAlreadyDecided
	}
	return permissionrequests.ErrNotFound
}
