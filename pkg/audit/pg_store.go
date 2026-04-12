package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore is a Postgres-backed audit event Store. It maps directly to the
// audit_events table from migration 000020.
type PGStore struct {
	pool *pgxpool.Pool
}

// NewPGStore wraps a pgx pool as an audit Store.
func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

func (s *PGStore) Insert(ctx context.Context, evt AuditEvent) error {
	var diffArg any
	if len(evt.DiffJSON) > 0 {
		diffArg = []byte(evt.DiffJSON)
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO audit_events (id, actor_id, action, resource_type, resource_rid, diff_json, ip, user_agent, ts)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		evt.ID, evt.ActorID, evt.Action, evt.ResourceType, evt.ResourceRID,
		diffArg, evt.IP, evt.UserAgent, evt.Timestamp)
	return err
}

func (s *PGStore) List(ctx context.Context, f ListFilter) ([]AuditEvent, error) {
	var where []string
	var args []any
	argN := 1

	if f.ActorID != "" {
		where = append(where, fmt.Sprintf("actor_id = $%d", argN))
		args = append(args, f.ActorID)
		argN++
	}
	if f.Action != "" {
		where = append(where, fmt.Sprintf("action = $%d", argN))
		args = append(args, f.Action)
		argN++
	}
	if f.ResourceType != "" {
		where = append(where, fmt.Sprintf("resource_type = $%d", argN))
		args = append(args, f.ResourceType)
		argN++
	}
	if f.From != nil {
		where = append(where, fmt.Sprintf("ts >= $%d", argN))
		args = append(args, *f.From)
		argN++
	}
	if f.To != nil {
		where = append(where, fmt.Sprintf("ts <= $%d", argN))
		args = append(args, *f.To)
		argN++
	}

	q := "SELECT id, actor_id, action, resource_type, resource_rid, diff_json, ip, user_agent, ts FROM audit_events"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY ts DESC"

	if f.PageSize > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.PageSize)
	}
	if f.Offset > 0 {
		q += fmt.Sprintf(" OFFSET %d", f.Offset)
	}

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []AuditEvent
	for rows.Next() {
		var e AuditEvent
		var diff []byte
		var ts time.Time
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Action, &e.ResourceType,
			&e.ResourceRID, &diff, &e.IP, &e.UserAgent, &ts); err != nil {
			return nil, err
		}
		if diff != nil {
			e.DiffJSON = json.RawMessage(diff)
		}
		e.Timestamp = ts
		events = append(events, e)
	}
	return events, rows.Err()
}
