package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore is a Postgres-backed audit event Store. It maps directly to the
// audit_events table from migration 000020, extended by migration 000062
// with chain_seq / prev_hash / entry_hash for tamper-proof auditing
// (US-266).
type PGStore struct {
	pool *pgxpool.Pool
}

// NewPGStore wraps a pgx pool as an audit Store.
func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

// chainAdvisoryLockKey is the session-wide advisory-lock key that
// serialises audit chain inserts across all connections. Any 64-bit int
// is fine so long as it's unique within the installation's advisory
// namespace; this magic number was derived from fnv64("audit_events_chain").
const chainAdvisoryLockKey = 7395125168213812345

// Insert writes evt into audit_events, chaining it onto the current tail.
// The chain_seq / prev_hash / entry_hash fields on evt are IGNORED — the
// store is the authority on chain state. All work happens inside a single
// tx that holds a transaction-scoped advisory lock, so concurrent callers
// across multiple pool connections still produce a single coherent chain.
func (s *PGStore) Insert(ctx context.Context, evt AuditEvent) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", chainAdvisoryLockKey); err != nil {
			return fmt.Errorf("audit: acquire chain lock: %w", err)
		}

		var tailHash string
		if err := tx.QueryRow(ctx,
			`SELECT entry_hash FROM audit_events ORDER BY chain_seq DESC LIMIT 1`,
		).Scan(&tailHash); err != nil && err != pgx.ErrNoRows {
			return fmt.Errorf("audit: read chain tail: %w", err)
		}

		evt.PrevHash = tailHash
		hash, err := HashEvent(tailHash, evt)
		if err != nil {
			return err
		}
		evt.EntryHash = hash

		var diffArg any
		if len(evt.DiffJSON) > 0 {
			diffArg = []byte(evt.DiffJSON)
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO audit_events (id, actor_id, action, resource_type, resource_rid,
			    diff_json, ip, user_agent, ts, prev_hash, entry_hash)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			evt.ID, evt.ActorID, evt.Action, evt.ResourceType, evt.ResourceRID,
			diffArg, evt.IP, evt.UserAgent, evt.Timestamp,
			evt.PrevHash, evt.EntryHash)
		return err
	})
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
	if f.ResourceRID != "" {
		where = append(where, fmt.Sprintf("resource_rid = $%d", argN))
		args = append(args, f.ResourceRID)
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

	q := `SELECT id, actor_id, action, resource_type, resource_rid, diff_json,
	             ip, user_agent, ts, chain_seq, prev_hash, entry_hash
	      FROM audit_events`
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

	return s.queryEvents(ctx, q, args)
}

// ListChain returns every audit event ORDERED BY chain_seq ASC for use by
// the verification tool.
func (s *PGStore) ListChain(ctx context.Context) ([]AuditEvent, error) {
	return s.queryEvents(ctx,
		`SELECT id, actor_id, action, resource_type, resource_rid, diff_json,
		        ip, user_agent, ts, chain_seq, prev_hash, entry_hash
		 FROM audit_events
		 ORDER BY chain_seq ASC`, nil)
}

// ListBefore returns up to limit audit events whose timestamp is
// strictly earlier than `before` AND whose chain_seq is strictly
// greater than `cursor`, ORDER BY chain_seq ASC. Used by the retention
// scheduler (US-269) to page through expired rows in chain order so
// the archive sink receives a stable, ordered stream. Pass cursor=0 to
// start from the beginning of the chain.
func (s *PGStore) ListBefore(ctx context.Context, before time.Time, cursor int64, limit int) ([]AuditEvent, error) {
	if limit <= 0 {
		return nil, nil
	}
	return s.queryEvents(ctx,
		`SELECT id, actor_id, action, resource_type, resource_rid, diff_json,
		        ip, user_agent, ts, chain_seq, prev_hash, entry_hash
		 FROM audit_events
		 WHERE ts < $1 AND chain_seq > $2
		 ORDER BY chain_seq ASC
		 LIMIT $3`,
		[]any{before, cursor, limit})
}

// DeleteBefore removes every audit_events row whose timestamp is
// strictly earlier than `before` and returns the number of rows
// removed. Used by the retention scheduler (US-269) after expired rows
// have been handed to the archive sink. Deleting rows intentionally
// breaks the US-266 tamper-proof chain invariant for pre-retention
// history — the archive is the cold-path integrity record for those
// rows. The live chain (retention window onward) remains verifiable
// end-to-end.
func (s *PGStore) DeleteBefore(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM audit_events WHERE ts < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("audit: delete before %s: %w",
			before.UTC().Format(time.RFC3339), err)
	}
	return tag.RowsAffected(), nil
}

// ListChainByDay returns every audit event whose timestamp falls in the
// UTC calendar day containing `day`, ordered by chain_seq ASC. Used by
// the root-hash publisher.
func (s *PGStore) ListChainByDay(ctx context.Context, day time.Time) ([]AuditEvent, error) {
	start := time.Date(day.UTC().Year(), day.UTC().Month(), day.UTC().Day(), 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	return s.queryEvents(ctx,
		`SELECT id, actor_id, action, resource_type, resource_rid, diff_json,
		        ip, user_agent, ts, chain_seq, prev_hash, entry_hash
		 FROM audit_events
		 WHERE ts >= $1 AND ts < $2
		 ORDER BY chain_seq ASC`, []any{start, end})
}

func (s *PGStore) queryEvents(ctx context.Context, q string, args []any) ([]AuditEvent, error) {
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
			&e.ResourceRID, &diff, &e.IP, &e.UserAgent, &ts,
			&e.ChainSeq, &e.PrevHash, &e.EntryHash); err != nil {
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
