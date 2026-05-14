package graphsvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGTemplateStore is the PostgreSQL-backed TemplateStore built on the
// graph_templates table from migration 000201.
type PGTemplateStore struct {
	pool *pgxpool.Pool
}

// NewPGTemplateStore wires a PGTemplateStore over an existing pgx pool.
func NewPGTemplateStore(pool *pgxpool.Pool) *PGTemplateStore {
	return &PGTemplateStore{pool: pool}
}

// Create inserts a new template row. Caller is responsible for minting the RID
// so the handler can return it in the 201 response before the DB round-trip
// commits.
func (s *PGTemplateStore) Create(ctx context.Context, t *GraphTemplate) error {
	params := t.ParameterizedFields
	if params == nil {
		params = []string{}
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal parameterized fields: %w", err)
	}
	parameters := t.Parameters
	if len(parameters) == 0 {
		parameters = json.RawMessage(`{}`)
	}
	err = s.pool.QueryRow(ctx,
		`INSERT INTO graph_templates
		   (rid, source_graph_rid, name, payload, parameterized_fields, parameters, created_by)
		 VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6::jsonb, $7)
		 RETURNING created_at`,
		t.RID, t.SourceGraphRID, t.Name, []byte(t.Payload), paramsJSON,
		[]byte(parameters), t.CreatedBy,
	).Scan(&t.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert template: %w", err)
	}
	return nil
}

// Get returns ErrTemplateNotFound when the RID does not match a row.
func (s *PGTemplateStore) Get(ctx context.Context, ridStr string) (*GraphTemplate, error) {
	t := &GraphTemplate{}
	var payloadRaw, paramsFieldsRaw, parametersRaw []byte
	err := s.pool.QueryRow(ctx,
		`SELECT rid, source_graph_rid, name, payload, parameterized_fields, parameters, created_by, created_at
		 FROM graph_templates WHERE rid = $1`, ridStr,
	).Scan(&t.RID, &t.SourceGraphRID, &t.Name, &payloadRaw, &paramsFieldsRaw,
		&parametersRaw, &t.CreatedBy, &t.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTemplateNotFound
		}
		return nil, fmt.Errorf("get template: %w", err)
	}
	t.Payload = json.RawMessage(payloadRaw)
	t.Parameters = json.RawMessage(parametersRaw)
	if len(paramsFieldsRaw) > 0 {
		if err := json.Unmarshal(paramsFieldsRaw, &t.ParameterizedFields); err != nil {
			return nil, fmt.Errorf("decode parameterized fields: %w", err)
		}
	}
	return t, nil
}

var _ TemplateStore = (*PGTemplateStore)(nil)
