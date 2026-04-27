package main

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/aip"
)

// pgAIPStore satisfies aip.Store by persisting Thread + Message rows
// into the aip_threads / aip_messages tables (US-279). Lives in
// cmd/server/ to keep pkg/aip free of any pgx import — same dep
// trick as pgFeatureFlagsStore + pgTenantQuotaStore.
type pgAIPStore struct {
	pool *pgxpool.Pool
}

func newPGAIPStore(pool *pgxpool.Pool) *pgAIPStore { return &pgAIPStore{pool: pool} }

func isAIPUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 23505") || strings.Contains(msg, "duplicate key value")
}

func (s *pgAIPStore) CreateThread(ctx context.Context, t *aip.Thread) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO aip_threads (id, title, provider, model, system_prompt, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		t.ID, t.Title, t.Provider, t.Model, t.SystemPrompt, t.CreatedBy,
	)
	if err != nil {
		if isAIPUniqueViolation(err) {
			return aip.ErrThreadAlreadyExists
		}
		return err
	}
	fresh, err := s.GetThread(ctx, t.ID)
	if err != nil {
		return err
	}
	*t = *fresh
	return nil
}

func (s *pgAIPStore) GetThread(ctx context.Context, id string) (*aip.Thread, error) {
	var t aip.Thread
	err := s.pool.QueryRow(ctx,
		`SELECT id, title, provider, COALESCE(model,''), COALESCE(system_prompt,''),
		        COALESCE(created_by,''), created_at, updated_at
		 FROM aip_threads WHERE id = $1`, id).
		Scan(&t.ID, &t.Title, &t.Provider, &t.Model, &t.SystemPrompt,
			&t.CreatedBy, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, aip.ErrThreadNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (s *pgAIPStore) ListThreads(ctx context.Context, createdBy string) ([]*aip.Thread, error) {
	var rows pgx.Rows
	var err error
	if createdBy == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT id, title, provider, COALESCE(model,''), COALESCE(system_prompt,''),
			        COALESCE(created_by,''), created_at, updated_at
			 FROM aip_threads ORDER BY created_at DESC, id ASC`)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT id, title, provider, COALESCE(model,''), COALESCE(system_prompt,''),
			        COALESCE(created_by,''), created_at, updated_at
			 FROM aip_threads WHERE created_by = $1 ORDER BY created_at DESC, id ASC`, createdBy)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*aip.Thread
	for rows.Next() {
		var t aip.Thread
		if err := rows.Scan(&t.ID, &t.Title, &t.Provider, &t.Model, &t.SystemPrompt,
			&t.CreatedBy, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

func (s *pgAIPStore) UpdateThread(ctx context.Context, id string, upd aip.ThreadUpdate) error {
	args := []interface{}{}
	sets := []string{"updated_at = NOW()"}
	argN := 1
	if upd.Title != nil {
		sets = append(sets, "title = $"+strconv.Itoa(argN))
		args = append(args, *upd.Title)
		argN++
	}
	if upd.Model != nil {
		sets = append(sets, "model = $"+strconv.Itoa(argN))
		args = append(args, *upd.Model)
		argN++
	}
	if upd.SystemPrompt != nil {
		sets = append(sets, "system_prompt = $"+strconv.Itoa(argN))
		args = append(args, *upd.SystemPrompt)
		argN++
	}
	args = append(args, id)
	tag, err := s.pool.Exec(ctx,
		`UPDATE aip_threads SET `+strings.Join(sets, ", ")+
			` WHERE id = $`+strconv.Itoa(argN), args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return aip.ErrThreadNotFound
	}
	return nil
}

func (s *pgAIPStore) DeleteThread(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM aip_threads WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return aip.ErrThreadNotFound
	}
	return nil
}

func (s *pgAIPStore) AppendMessage(ctx context.Context, m *aip.Message) error {
	var id int64
	var createdAt = m.CreatedAt
	var toolCallsJSON []byte
	if len(m.ToolCalls) > 0 {
		buf, err := json.Marshal(m.ToolCalls)
		if err != nil {
			return err
		}
		toolCallsJSON = buf
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO aip_messages (thread_id, role, content, token_count, tool_calls, tool_call_id, tool_name)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, created_at`,
		m.ThreadID, m.Role, m.Content, m.TokenCount,
		toolCallsJSON, m.ToolCallID, m.ToolName).Scan(&id, &createdAt)
	if err != nil {
		if strings.Contains(err.Error(), "aip_messages_thread_id_fkey") ||
			strings.Contains(err.Error(), "violates foreign key constraint") {
			return aip.ErrThreadNotFound
		}
		return err
	}
	m.ID = id
	m.CreatedAt = createdAt
	if _, err := s.pool.Exec(ctx,
		`UPDATE aip_threads SET updated_at = NOW() WHERE id = $1`, m.ThreadID); err != nil {
		return err
	}
	return nil
}

func (s *pgAIPStore) ListMessages(ctx context.Context, threadID string) ([]*aip.Message, error) {
	// Confirm thread exists so missing thread → ErrThreadNotFound.
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM aip_threads WHERE id = $1)`, threadID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, aip.ErrThreadNotFound
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, thread_id, role, content, token_count,
		        tool_calls, COALESCE(tool_call_id, ''), COALESCE(tool_name, ''),
		        created_at
		 FROM aip_messages WHERE thread_id = $1 ORDER BY id ASC`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*aip.Message
	for rows.Next() {
		var m aip.Message
		var toolCallsJSON []byte
		if err := rows.Scan(&m.ID, &m.ThreadID, &m.Role, &m.Content, &m.TokenCount,
			&toolCallsJSON, &m.ToolCallID, &m.ToolName, &m.CreatedAt); err != nil {
			return nil, err
		}
		if len(toolCallsJSON) > 0 {
			if err := json.Unmarshal(toolCallsJSON, &m.ToolCalls); err != nil {
				return nil, err
			}
		}
		out = append(out, &m)
	}
	return out, rows.Err()
}
