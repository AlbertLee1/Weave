package main

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liyang/weave/pkg/oms"
)

// pgFunctionExecutionStore satisfies oms.FunctionExecutionStore for the
// Function-replay audit log (US-370). Rows live in function_executions; the
// /execute and /replay handlers both write here so the replay endpoint can
// compare a fresh hash against the recorded one.
type pgFunctionExecutionStore struct {
	pool *pgxpool.Pool
}

func newPGFunctionExecutionStore(pool *pgxpool.Pool) *pgFunctionExecutionStore {
	return &pgFunctionExecutionStore{pool: pool}
}

func (s *pgFunctionExecutionStore) RecordExecution(ctx context.Context, exec *oms.FunctionExecution) error {
	if exec == nil || exec.ExecutionID == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO function_executions
		   (execution_id, function_rid, function_name, function_version, ontology_rid,
		    input_hash, output_hash, input_json, output_json, error_message,
		    requested_by, is_replay, replay_of, executed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		 ON CONFLICT (execution_id) DO NOTHING`,
		exec.ExecutionID, exec.FunctionRID, exec.FunctionName, exec.FunctionVersion, exec.OntologyRID,
		exec.InputHash, exec.OutputHash, coerceJSON(exec.InputJSON), coerceJSON(exec.OutputJSON), exec.ErrorMessage,
		exec.RequestedBy, exec.IsReplay, exec.ReplayOf, exec.ExecutedAt,
	)
	return err
}

func (s *pgFunctionExecutionStore) GetExecution(ctx context.Context, executionID string) (*oms.FunctionExecution, error) {
	if executionID == "" {
		return nil, oms.ErrExecutionNotFound
	}
	row := s.pool.QueryRow(ctx,
		`SELECT execution_id, function_rid, function_name, function_version, ontology_rid,
		        input_hash, output_hash, input_json, output_json, error_message,
		        requested_by, is_replay, replay_of, executed_at
		 FROM function_executions WHERE execution_id = $1`, executionID)
	return scanFunctionExecution(row)
}

func (s *pgFunctionExecutionStore) FindByInputHash(ctx context.Context, functionRID, version, inputHash string) (*oms.FunctionExecution, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT execution_id, function_rid, function_name, function_version, ontology_rid,
		        input_hash, output_hash, input_json, output_json, error_message,
		        requested_by, is_replay, replay_of, executed_at
		 FROM function_executions
		 WHERE function_rid = $1 AND function_version = $2 AND input_hash = $3
		 ORDER BY executed_at DESC LIMIT 1`, functionRID, version, inputHash)
	return scanFunctionExecution(row)
}

func (s *pgFunctionExecutionStore) ListExecutions(ctx context.Context, functionRID, version string, limit int) ([]*oms.FunctionExecution, error) {
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	var (
		rows pgx.Rows
		err  error
	)
	if version != "" {
		rows, err = s.pool.Query(ctx,
			`SELECT execution_id, function_rid, function_name, function_version, ontology_rid,
			        input_hash, output_hash, input_json, output_json, error_message,
			        requested_by, is_replay, replay_of, executed_at
			 FROM function_executions
			 WHERE function_rid = $1 AND function_version = $2
			 ORDER BY executed_at DESC LIMIT $3`, functionRID, version, limit)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT execution_id, function_rid, function_name, function_version, ontology_rid,
			        input_hash, output_hash, input_json, output_json, error_message,
			        requested_by, is_replay, replay_of, executed_at
			 FROM function_executions
			 WHERE function_rid = $1
			 ORDER BY executed_at DESC LIMIT $2`, functionRID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*oms.FunctionExecution
	for rows.Next() {
		exec, err := scanFunctionExecutionRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, exec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanFunctionExecution(r pgx.Row) (*oms.FunctionExecution, error) {
	exec := &oms.FunctionExecution{}
	var inputJSON, outputJSON []byte
	if err := r.Scan(
		&exec.ExecutionID, &exec.FunctionRID, &exec.FunctionName, &exec.FunctionVersion, &exec.OntologyRID,
		&exec.InputHash, &exec.OutputHash, &inputJSON, &outputJSON, &exec.ErrorMessage,
		&exec.RequestedBy, &exec.IsReplay, &exec.ReplayOf, &exec.ExecutedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oms.ErrExecutionNotFound
		}
		return nil, err
	}
	exec.InputJSON = inputJSON
	exec.OutputJSON = outputJSON
	return exec, nil
}

func scanFunctionExecutionRows(rows pgx.Rows) (*oms.FunctionExecution, error) {
	exec := &oms.FunctionExecution{}
	var inputJSON, outputJSON []byte
	if err := rows.Scan(
		&exec.ExecutionID, &exec.FunctionRID, &exec.FunctionName, &exec.FunctionVersion, &exec.OntologyRID,
		&exec.InputHash, &exec.OutputHash, &inputJSON, &outputJSON, &exec.ErrorMessage,
		&exec.RequestedBy, &exec.IsReplay, &exec.ReplayOf, &exec.ExecutedAt,
	); err != nil {
		return nil, err
	}
	exec.InputJSON = inputJSON
	exec.OutputJSON = outputJSON
	return exec, nil
}

