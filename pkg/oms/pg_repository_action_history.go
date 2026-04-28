package oms

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ActionLogQuery captures the filterable surface for the
// per-ontology action-history list (US-317). Zero-value fields are treated
// as "no constraint"; ActionTypeRID is matched against the row's
// action_type_rid column directly so callers that already resolved the
// ActionType by api_name can skip a second query.
type ActionLogQuery struct {
	ActionTypeRID string
	Status        string
	UserID        string
	Since         time.Time
	Until         time.Time
	Limit         int
	Offset        int
}

// ListActionLogsByOntology returns action_logs rows scoped to a single
// ontology. action_logs has no ontology column of its own, so we JOIN with
// action_types to get there — same shape downstream consumers (lineage /
// impact) reach for.
func (r *PGRepository) ListActionLogsByOntology(
	ctx context.Context,
	ontologyRID string,
	q ActionLogQuery,
) ([]ActionLog, error) {
	conds, args := buildActionLogConditions(ontologyRID, q)
	args = append(args, clampActionHistoryLimit(q.Limit), clampActionHistoryOffset(q.Offset))
	limitParam := len(args) - 1
	offsetParam := len(args)

	sql := fmt.Sprintf(`SELECT al.id, al.action_type_rid, al.user_id, al.parameters, al.edits,
		COALESCE(al.prev_edits, 'null'::jsonb),
		al.status, COALESCE(al.error_message, ''), al.created_at
		FROM action_logs al
		JOIN action_types at ON at.rid = al.action_type_rid
		WHERE %s
		ORDER BY al.created_at DESC, al.id DESC
		LIMIT $%d OFFSET $%d`,
		strings.Join(conds, " AND "), limitParam, offsetParam)

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ActionLog, 0)
	for rows.Next() {
		var al ActionLog
		if err := rows.Scan(&al.ID, &al.ActionTypeRID, &al.UserID, &al.Parameters,
			&al.Edits, &al.PrevEdits, &al.Status, &al.ErrorMessage, &al.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, al)
	}
	return out, rows.Err()
}

// GetActionLogByOntology returns one action_log row scoped to a specific
// ontology. Cross-ontology lookups return ErrNotFound (404) so existence
// under another tenant cannot be probed via the detail URL. JOIN ensures
// the ontology binding is checked in a single round-trip.
func (r *PGRepository) GetActionLogByOntology(
	ctx context.Context,
	ontologyRID string,
	id int64,
) (*ActionLog, error) {
	al := &ActionLog{}
	err := r.pool.QueryRow(ctx,
		`SELECT al.id, al.action_type_rid, al.user_id, al.parameters, al.edits,
		 COALESCE(al.prev_edits, 'null'::jsonb),
		 al.status, COALESCE(al.error_message, ''), al.created_at
		 FROM action_logs al
		 JOIN action_types at ON at.rid = al.action_type_rid
		 WHERE al.id = $1 AND at.ontology_rid = $2`, id, ontologyRID).
		Scan(&al.ID, &al.ActionTypeRID, &al.UserID, &al.Parameters, &al.Edits,
			&al.PrevEdits, &al.Status, &al.ErrorMessage, &al.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return al, nil
}

// CountActionLogsByOntology returns the total number of action_logs rows
// matching the same filter ListActionLogsByOntology would walk. Used so the
// /actions/history list response can surface a `total` alongside the page.
func (r *PGRepository) CountActionLogsByOntology(
	ctx context.Context,
	ontologyRID string,
	q ActionLogQuery,
) (int, error) {
	conds, args := buildActionLogConditions(ontologyRID, q)
	sql := fmt.Sprintf(`SELECT COUNT(*) FROM action_logs al
		JOIN action_types at ON at.rid = al.action_type_rid
		WHERE %s`, strings.Join(conds, " AND "))
	var n int
	if err := r.pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// buildActionLogConditions assembles the parameterised WHERE fragment shared
// by list + count. ontologyRID is always required so a missing path segment
// can never spill across tenants.
func buildActionLogConditions(ontologyRID string, q ActionLogQuery) ([]string, []interface{}) {
	conds := []string{"at.ontology_rid = $1"}
	args := []interface{}{ontologyRID}
	next := func() string {
		return fmt.Sprintf("$%d", len(args)+1)
	}
	if q.ActionTypeRID != "" {
		conds = append(conds, "al.action_type_rid = "+next())
		args = append(args, q.ActionTypeRID)
	}
	if q.Status != "" {
		conds = append(conds, "al.status = "+next())
		args = append(args, q.Status)
	}
	if q.UserID != "" {
		conds = append(conds, "al.user_id = "+next())
		args = append(args, q.UserID)
	}
	if !q.Since.IsZero() {
		conds = append(conds, "al.created_at >= "+next())
		args = append(args, q.Since)
	}
	if !q.Until.IsZero() {
		conds = append(conds, "al.created_at < "+next())
		args = append(args, q.Until)
	}
	return conds, args
}

// MaxActionHistoryLimit caps a single page response at 500 rows so the wire
// payload stays bounded even when callers omit the parameter.
const MaxActionHistoryLimit = 500

// DefaultActionHistoryLimit is the page size applied when the caller does
// not specify ?limit=, balancing "enough for a useful timeline view" with
// "small enough to keep parameters/edits payload sizes reasonable".
const DefaultActionHistoryLimit = 50

func clampActionHistoryLimit(n int) int {
	if n <= 0 {
		return DefaultActionHistoryLimit
	}
	if n > MaxActionHistoryLimit {
		return MaxActionHistoryLimit
	}
	return n
}

func clampActionHistoryOffset(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
