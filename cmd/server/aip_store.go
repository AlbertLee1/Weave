package main

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

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
	branch := m.BranchID
	if branch == "" {
		branch = aip.DefaultBranchID
	}

	// Auto-link to last message on the branch when caller didn't supply
	// a parent — preserves the linear-history contract for legacy
	// callers that haven't opted into the US-374 branch-tree API.
	parentID := m.ParentMessageID
	if parentID == nil {
		var lastID int64
		err := s.pool.QueryRow(ctx,
			`SELECT id FROM aip_messages
			 WHERE thread_id = $1 AND COALESCE(branch_id, $2) = $2
			 ORDER BY id DESC LIMIT 1`,
			m.ThreadID, branch).Scan(&lastID)
		if err == nil {
			pid := lastID
			parentID = &pid
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}

	err := s.pool.QueryRow(ctx,
		`INSERT INTO aip_messages
		     (thread_id, role, content, token_count, tool_calls, tool_call_id, tool_name,
		      parent_message_id, branch_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id, created_at`,
		m.ThreadID, m.Role, m.Content, m.TokenCount,
		toolCallsJSON, m.ToolCallID, m.ToolName,
		parentID, branch).Scan(&id, &createdAt)
	if err != nil {
		if strings.Contains(err.Error(), "aip_messages_thread_id_fkey") ||
			strings.Contains(err.Error(), "violates foreign key constraint") {
			return aip.ErrThreadNotFound
		}
		return err
	}
	m.ID = id
	m.CreatedAt = createdAt
	m.BranchID = branch
	m.ParentMessageID = parentID
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
		        parent_message_id, COALESCE(branch_id, $2),
		        created_at
		 FROM aip_messages WHERE thread_id = $1 ORDER BY id ASC`, threadID, aip.DefaultBranchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*aip.Message
	for rows.Next() {
		var m aip.Message
		var toolCallsJSON []byte
		var parent *int64
		if err := rows.Scan(&m.ID, &m.ThreadID, &m.Role, &m.Content, &m.TokenCount,
			&toolCallsJSON, &m.ToolCallID, &m.ToolName,
			&parent, &m.BranchID, &m.CreatedAt); err != nil {
			return nil, err
		}
		if len(toolCallsJSON) > 0 {
			if err := json.Unmarshal(toolCallsJSON, &m.ToolCalls); err != nil {
				return nil, err
			}
		}
		m.ParentMessageID = parent
		out = append(out, &m)
	}
	return out, rows.Err()
}

// GetMessage returns the message with the given numeric id. Returns
// aip.ErrMessageNotFound when the row does not exist.
func (s *pgAIPStore) GetMessage(ctx context.Context, id int64) (*aip.Message, error) {
	var m aip.Message
	var toolCallsJSON []byte
	var parent *int64
	err := s.pool.QueryRow(ctx,
		`SELECT id, thread_id, role, content, token_count,
		        tool_calls, COALESCE(tool_call_id, ''), COALESCE(tool_name, ''),
		        parent_message_id, COALESCE(branch_id, $2),
		        created_at
		 FROM aip_messages WHERE id = $1`, id, aip.DefaultBranchID).Scan(
		&m.ID, &m.ThreadID, &m.Role, &m.Content, &m.TokenCount,
		&toolCallsJSON, &m.ToolCallID, &m.ToolName,
		&parent, &m.BranchID, &m.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, aip.ErrMessageNotFound
		}
		return nil, err
	}
	if len(toolCallsJSON) > 0 {
		if err := json.Unmarshal(toolCallsJSON, &m.ToolCalls); err != nil {
			return nil, err
		}
	}
	m.ParentMessageID = parent
	return &m, nil
}

// ForkThread creates newThread, walks the source thread's pivot ancestor
// chain (root → pivot inclusive), then re-inserts each ancestor into
// the fresh thread under branch_id='main' with a freshly chained
// parent_message_id. The whole fork executes in a single transaction so
// a partial failure leaves no orphan thread row.
func (s *pgAIPStore) ForkThread(ctx context.Context, srcThreadID string, pivotMessageID int64, newThread *aip.Thread) (*aip.Thread, []*aip.Message, error) {
	if newThread == nil {
		return nil, nil, errors.New("aip: fork thread is nil")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	// Confirm source exists.
	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM aip_threads WHERE id = $1)`, srcThreadID).Scan(&exists); err != nil {
		return nil, nil, err
	}
	if !exists {
		return nil, nil, aip.ErrThreadNotFound
	}

	// Confirm pivot exists and belongs to source.
	var pivotThread string
	if err := tx.QueryRow(ctx,
		`SELECT thread_id FROM aip_messages WHERE id = $1`, pivotMessageID).Scan(&pivotThread); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, aip.ErrMessageNotFound
		}
		return nil, nil, err
	}
	if pivotThread != srcThreadID {
		return nil, nil, aip.ErrPivotThreadMismatch
	}

	// Walk pivot → root via parent_message_id and reverse. CTE produces
	// rows ordered root-first via depth.
	rows, err := tx.Query(ctx, `
		WITH RECURSIVE chain(id, role, content, token_count, tool_calls, tool_call_id, tool_name, depth) AS (
		    SELECT id, role, content, token_count, tool_calls, COALESCE(tool_call_id, ''), COALESCE(tool_name, ''), 0
		      FROM aip_messages
		     WHERE id = $1
		    UNION ALL
		    SELECT m.id, m.role, m.content, m.token_count, m.tool_calls,
		           COALESCE(m.tool_call_id, ''), COALESCE(m.tool_name, ''),
		           c.depth + 1
		      FROM aip_messages m
		      JOIN chain c ON m.id = (
		          SELECT parent_message_id FROM aip_messages WHERE id = c.id
		      )
		)
		SELECT role, content, token_count, tool_calls, tool_call_id, tool_name
		  FROM chain
		 ORDER BY depth DESC
	`, pivotMessageID)
	if err != nil {
		return nil, nil, err
	}
	type pendingMsg struct {
		role         string
		content      string
		tokenCount   int
		toolCalls    []byte
		toolCallID   string
		toolName     string
		parsedCalls  []aip.ToolCall
	}
	var pending []pendingMsg
	for rows.Next() {
		var p pendingMsg
		if err := rows.Scan(&p.role, &p.content, &p.tokenCount, &p.toolCalls, &p.toolCallID, &p.toolName); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if len(p.toolCalls) > 0 {
			if err := json.Unmarshal(p.toolCalls, &p.parsedCalls); err != nil {
				rows.Close()
				return nil, nil, err
			}
		}
		pending = append(pending, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	// Insert the new thread row.
	_, err = tx.Exec(ctx,
		`INSERT INTO aip_threads (id, title, provider, model, system_prompt, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		newThread.ID, newThread.Title, newThread.Provider, newThread.Model, newThread.SystemPrompt, newThread.CreatedBy,
	)
	if err != nil {
		if isAIPUniqueViolation(err) {
			return nil, nil, aip.ErrThreadAlreadyExists
		}
		return nil, nil, err
	}

	// Re-insert chain into the new thread with a re-linked parent chain
	// and branch_id='main'. Each insert returns the new id which becomes
	// the next message's parent.
	copied := make([]*aip.Message, 0, len(pending))
	var prevID *int64
	for _, p := range pending {
		var newID int64
		var newCreated time.Time
		err := tx.QueryRow(ctx,
			`INSERT INTO aip_messages
			      (thread_id, role, content, token_count, tool_calls, tool_call_id, tool_name,
			       parent_message_id, branch_id)
			  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			 RETURNING id, created_at`,
			newThread.ID, p.role, p.content, p.tokenCount,
			p.toolCalls, p.toolCallID, p.toolName,
			prevID, aip.DefaultBranchID).Scan(&newID, &newCreated)
		if err != nil {
			return nil, nil, err
		}
		var parentCp *int64
		if prevID != nil {
			pid := *prevID
			parentCp = &pid
		}
		copied = append(copied, &aip.Message{
			ID:              newID,
			ThreadID:        newThread.ID,
			Role:            p.role,
			Content:         p.content,
			TokenCount:      p.tokenCount,
			ToolCalls:       p.parsedCalls,
			ToolCallID:      p.toolCallID,
			ToolName:        p.toolName,
			ParentMessageID: parentCp,
			BranchID:        aip.DefaultBranchID,
			CreatedAt:       newCreated,
		})
		prevID = &newID
	}

	// Pull the canonical thread row back with its server-side timestamps.
	var stored aip.Thread
	err = tx.QueryRow(ctx,
		`SELECT id, title, provider, COALESCE(model,''), COALESCE(system_prompt,''),
		        COALESCE(created_by,''), created_at, updated_at
		 FROM aip_threads WHERE id = $1`, newThread.ID).
		Scan(&stored.ID, &stored.Title, &stored.Provider, &stored.Model, &stored.SystemPrompt,
			&stored.CreatedBy, &stored.CreatedAt, &stored.UpdatedAt)
	if err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	committed = true
	*newThread = stored
	return &stored, copied, nil
}
