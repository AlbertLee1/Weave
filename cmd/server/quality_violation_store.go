package main

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/pipeline/quality"
)

// pgQualityViolationStore satisfies quality.ViolationStore by
// persisting Violation rows into the quality_violations table (US-296
// migration 000071). Lives in cmd/server to keep
// pkg/pipeline/quality free of any pgx import — same dep trick as
// pgPipelineStore + pgAIPLogicStore.
type pgQualityViolationStore struct {
	pool *pgxpool.Pool
}

func newPGQualityViolationStore(pool *pgxpool.Pool) *pgQualityViolationStore {
	return &pgQualityViolationStore{pool: pool}
}

func (s *pgQualityViolationStore) InsertViolation(ctx context.Context, v *quality.Violation) error {
	if v == nil {
		return errors.New("quality: violation is nil")
	}
	if v.ID == "" {
		return errors.New("quality: violation id must not be empty")
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO quality_violations
		   (id, pipeline_id, run_id, node_name, rule_name, rule_type,
		    field, row_index, row_key, reason, value, detected_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, COALESCE($12, NOW()))`,
		v.ID, v.PipelineID, v.RunID, v.NodeName, v.RuleName, string(v.RuleType),
		v.Field, v.RowIndex, v.RowKey, v.Reason, v.Value, qualityViolationTimestamp(v.DetectedAt),
	)
	return err
}

func (s *pgQualityViolationStore) InsertViolations(ctx context.Context, vs []*quality.Violation) error {
	for _, v := range vs {
		if err := s.InsertViolation(ctx, v); err != nil {
			return err
		}
	}
	return nil
}

func (s *pgQualityViolationStore) ListViolations(ctx context.Context, filter quality.ListFilter) ([]*quality.Violation, error) {
	q := `SELECT id, pipeline_id, run_id, node_name, rule_name, rule_type,
	             field, row_index, row_key, reason, value, detected_at
	      FROM quality_violations`
	args := []interface{}{}
	wheres := []string{}
	if filter.PipelineID != "" {
		args = append(args, filter.PipelineID)
		wheres = append(wheres, "pipeline_id = $"+strconv.Itoa(len(args)))
	}
	if filter.RunID != "" {
		args = append(args, filter.RunID)
		wheres = append(wheres, "run_id = $"+strconv.Itoa(len(args)))
	}
	if filter.RuleName != "" {
		args = append(args, filter.RuleName)
		wheres = append(wheres, "rule_name = $"+strconv.Itoa(len(args)))
	}
	if len(wheres) > 0 {
		q += " WHERE " + strings.Join(wheres, " AND ")
	}
	q += " ORDER BY detected_at DESC, id ASC"
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		q += " LIMIT $" + strconv.Itoa(len(args))
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*quality.Violation
	for rows.Next() {
		var v quality.Violation
		var ruleType string
		if err := rows.Scan(&v.ID, &v.PipelineID, &v.RunID, &v.NodeName, &v.RuleName, &ruleType,
			&v.Field, &v.RowIndex, &v.RowKey, &v.Reason, &v.Value, &v.DetectedAt); err != nil {
			return nil, err
		}
		v.RuleType = quality.RuleType(ruleType)
		out = append(out, &v)
	}
	return out, rows.Err()
}

// qualityViolationTimestamp returns t for the INSERT bind site or nil
// when t is zero — letting the column DEFAULT NOW() stamp the row.
func qualityViolationTimestamp(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}
