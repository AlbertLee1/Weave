// Package sagapg ships the PostgreSQL implementation of
// actions.SagaStore. It lives in its own package (rather than inside
// cmd/server) so the durable saga coordinator can be exercised by tests
// outside the server binary — notably the godog BDD suite under
// test/bdd/, which talks to the same chi handler the server registers.
package sagapg

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/oms"
)

// Store satisfies actions.SagaStore for the durable saga coordinator
// (US-369). Header rows live in action_sagas (one per saga invocation,
// idempotency_key UNIQUE), per-step rows in action_saga_steps,
// dead-letter rows in action_saga_dlq.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a *Store backed by the supplied pgx pool. The pool
// must be connected against a schema that has migration 000083 applied.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 23505") || strings.Contains(msg, "duplicate key value")
}

// coerceJSON substitutes "{}" for nil json.RawMessage. pgx encodes a
// raw nil as the literal "null" which the JSONB column accepts but
// breaks the "empty ⇒ omitted" round-trip.
func coerceJSON(b []byte) []byte {
	if len(b) == 0 {
		return []byte("{}")
	}
	return b
}

func (s *Store) CreateSaga(ctx context.Context, sg *actions.Saga) error {
	idem := sg.IdempotencyKey
	var idemArg interface{}
	if idem == "" {
		idemArg = nil
	} else {
		idemArg = idem
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO action_sagas
		   (saga_id, idempotency_key, ontology, status, requested_by, failure_message, result_json)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		sg.SagaID, idemArg, sg.Ontology, sg.Status, sg.RequestedBy, sg.FailureMessage,
		coerceJSON(sg.ResultJSON),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return actions.ErrSagaIdempotencyConflict
		}
		return err
	}
	return nil
}

func (s *Store) GetSagaByIdempotencyKey(ctx context.Context, key string) (*actions.Saga, error) {
	if key == "" {
		return nil, oms.ErrNotFound
	}
	return s.scanOneSaga(ctx,
		`SELECT saga_id, COALESCE(idempotency_key, ''), ontology, status,
		        requested_by, failure_message, COALESCE(result_json, 'null'::jsonb),
		        created_at, updated_at
		 FROM action_sagas WHERE idempotency_key = $1`, key)
}

func (s *Store) GetSaga(ctx context.Context, sagaID string) (*actions.Saga, error) {
	return s.scanOneSaga(ctx,
		`SELECT saga_id, COALESCE(idempotency_key, ''), ontology, status,
		        requested_by, failure_message, COALESCE(result_json, 'null'::jsonb),
		        created_at, updated_at
		 FROM action_sagas WHERE saga_id = $1`, sagaID)
}

func (s *Store) scanOneSaga(ctx context.Context, query string, args ...interface{}) (*actions.Saga, error) {
	var sg actions.Saga
	var resultJSON []byte
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&sg.SagaID, &sg.IdempotencyKey, &sg.Ontology, &sg.Status,
		&sg.RequestedBy, &sg.FailureMessage, &resultJSON,
		&sg.CreatedAt, &sg.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oms.ErrNotFound
		}
		return nil, err
	}
	if len(resultJSON) > 0 && string(resultJSON) != "null" {
		sg.ResultJSON = json.RawMessage(resultJSON)
	}
	return &sg, nil
}

func (s *Store) UpdateSagaStatus(ctx context.Context, sagaID string, upd actions.SagaUpdate) error {
	sets := []string{"updated_at = NOW()"}
	args := []interface{}{}
	argN := 1
	if upd.Status != "" {
		sets = append(sets, "status = $"+strconv.Itoa(argN))
		args = append(args, upd.Status)
		argN++
	}
	if upd.FailureMessage != nil {
		sets = append(sets, "failure_message = $"+strconv.Itoa(argN))
		args = append(args, *upd.FailureMessage)
		argN++
	}
	if upd.ResultJSON != nil {
		sets = append(sets, "result_json = $"+strconv.Itoa(argN))
		args = append(args, coerceJSON(upd.ResultJSON))
		argN++
	}
	args = append(args, sagaID)
	tag, err := s.pool.Exec(ctx,
		`UPDATE action_sagas SET `+strings.Join(sets, ", ")+` WHERE saga_id = $`+strconv.Itoa(argN),
		args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return oms.ErrNotFound
	}
	return nil
}

func (s *Store) CreateSagaStep(ctx context.Context, step *actions.SagaStep) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO action_saga_steps
		   (step_id, saga_id, step_index, action_type, parameters, edits_json, inverse_edits_json, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		step.StepID, step.SagaID, step.StepIndex, step.ActionType,
		coerceJSON(step.Parameters), coerceJSON(step.EditsJSON), coerceJSON(step.InverseEditsJSON),
		step.Status,
	)
	return err
}

func (s *Store) UpdateSagaStep(ctx context.Context, stepID string, upd actions.SagaStepUpdate) error {
	sets := []string{"updated_at = NOW()"}
	args := []interface{}{}
	argN := 1
	if upd.Status != "" {
		sets = append(sets, "status = $"+strconv.Itoa(argN))
		args = append(args, upd.Status)
		argN++
	}
	if upd.EditsJSON != nil {
		sets = append(sets, "edits_json = $"+strconv.Itoa(argN))
		args = append(args, coerceJSON(upd.EditsJSON))
		argN++
	}
	if upd.InverseEditsJSON != nil {
		sets = append(sets, "inverse_edits_json = $"+strconv.Itoa(argN))
		args = append(args, coerceJSON(upd.InverseEditsJSON))
		argN++
	}
	args = append(args, stepID)
	tag, err := s.pool.Exec(ctx,
		`UPDATE action_saga_steps SET `+strings.Join(sets, ", ")+` WHERE step_id = $`+strconv.Itoa(argN),
		args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return oms.ErrNotFound
	}
	return nil
}

func (s *Store) ListSagas(ctx context.Context, params actions.ListSagasParams) ([]*actions.Saga, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	q := `SELECT saga_id, COALESCE(idempotency_key, ''), ontology, status,
	             requested_by, failure_message, COALESCE(result_json, 'null'::jsonb),
	             created_at, updated_at
	      FROM action_sagas`
	args := []interface{}{}
	conds := []string{}
	if params.Ontology != "" {
		conds = append(conds, "ontology = $"+strconv.Itoa(len(args)+1))
		args = append(args, params.Ontology)
	}
	if params.Status != "" {
		conds = append(conds, "status = $"+strconv.Itoa(len(args)+1))
		args = append(args, params.Status)
	}
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY created_at DESC"
	q += " LIMIT $" + strconv.Itoa(len(args)+1)
	args = append(args, limit)
	if params.Offset > 0 {
		q += " OFFSET $" + strconv.Itoa(len(args)+1)
		args = append(args, params.Offset)
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*actions.Saga, 0)
	for rows.Next() {
		var sg actions.Saga
		var resultJSON []byte
		if err := rows.Scan(&sg.SagaID, &sg.IdempotencyKey, &sg.Ontology, &sg.Status,
			&sg.RequestedBy, &sg.FailureMessage, &resultJSON,
			&sg.CreatedAt, &sg.UpdatedAt); err != nil {
			return nil, err
		}
		if len(resultJSON) > 0 && string(resultJSON) != "null" {
			sg.ResultJSON = json.RawMessage(resultJSON)
		}
		out = append(out, &sg)
	}
	return out, rows.Err()
}

func (s *Store) ListSagaSteps(ctx context.Context, sagaID string) ([]*actions.SagaStep, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT step_id, saga_id, step_index, action_type,
		        COALESCE(parameters, '{}'::jsonb),
		        COALESCE(edits_json, '[]'::jsonb),
		        COALESCE(inverse_edits_json, '[]'::jsonb),
		        status, created_at, updated_at
		 FROM action_saga_steps WHERE saga_id = $1 ORDER BY step_index`, sagaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*actions.SagaStep, 0)
	for rows.Next() {
		var step actions.SagaStep
		var params, edits, inverse []byte
		if err := rows.Scan(&step.StepID, &step.SagaID, &step.StepIndex, &step.ActionType,
			&params, &edits, &inverse, &step.Status, &step.CreatedAt, &step.UpdatedAt); err != nil {
			return nil, err
		}
		step.Parameters = json.RawMessage(params)
		step.EditsJSON = json.RawMessage(edits)
		step.InverseEditsJSON = json.RawMessage(inverse)
		out = append(out, &step)
	}
	return out, rows.Err()
}

func (s *Store) EnqueueDLQ(ctx context.Context, entry *actions.SagaDLQEntry) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO action_saga_dlq
		   (dlq_id, saga_id, step_id, ontology, edits_json, failure_message, status, attempts)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		entry.DLQID, entry.SagaID, entry.StepID, entry.Ontology,
		coerceJSON(entry.EditsJSON), entry.FailureMessage, entry.Status, entry.Attempts,
	)
	return err
}

func (s *Store) ListDLQ(ctx context.Context, status string, limit int) ([]*actions.SagaDLQEntry, error) {
	q := `SELECT dlq_id, saga_id, step_id, ontology,
	             COALESCE(edits_json, '[]'::jsonb),
	             failure_message, status, attempts, last_attempt_at, created_at, updated_at
	      FROM action_saga_dlq`
	args := []interface{}{}
	if status != "" {
		q += " WHERE status = $1"
		args = append(args, status)
	}
	q += " ORDER BY created_at DESC"
	if limit > 0 {
		q += " LIMIT $" + strconv.Itoa(len(args)+1)
		args = append(args, limit)
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*actions.SagaDLQEntry, 0)
	for rows.Next() {
		var e actions.SagaDLQEntry
		var edits []byte
		if err := rows.Scan(&e.DLQID, &e.SagaID, &e.StepID, &e.Ontology,
			&edits, &e.FailureMessage, &e.Status, &e.Attempts, &e.LastAttemptAt,
			&e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		e.EditsJSON = json.RawMessage(edits)
		out = append(out, &e)
	}
	return out, rows.Err()
}

func (s *Store) UpdateDLQStatus(ctx context.Context, dlqID string, upd actions.SagaDLQUpdate) error {
	sets := []string{"updated_at = NOW()"}
	args := []interface{}{}
	argN := 1
	if upd.Status != "" {
		sets = append(sets, "status = $"+strconv.Itoa(argN))
		args = append(args, upd.Status)
		argN++
	}
	if upd.Attempts != nil {
		sets = append(sets, "attempts = $"+strconv.Itoa(argN))
		args = append(args, *upd.Attempts)
		argN++
		sets = append(sets, "last_attempt_at = NOW()")
	}
	if upd.LastAttemptAt != nil {
		sets = append(sets, "last_attempt_at = $"+strconv.Itoa(argN))
		args = append(args, *upd.LastAttemptAt)
		argN++
	}
	if upd.FailureMessage != nil {
		sets = append(sets, "failure_message = $"+strconv.Itoa(argN))
		args = append(args, *upd.FailureMessage)
		argN++
	}
	args = append(args, dlqID)
	tag, err := s.pool.Exec(ctx,
		`UPDATE action_saga_dlq SET `+strings.Join(sets, ", ")+` WHERE dlq_id = $`+strconv.Itoa(argN),
		args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return oms.ErrNotFound
	}
	return nil
}
