package main

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/userprefs"
)

// pgUserPrefsStore satisfies userprefs.Store by persisting rows to the
// user_preferences table (US-350). Lives in cmd/server/ rather than
// pkg/userprefs/ so the package stays free of any pgx import — same
// dep-direction trick as pgSavedSearchesStore (US-311).
type pgUserPrefsStore struct {
	pool *pgxpool.Pool
}

func newPGUserPrefsStore(pool *pgxpool.Pool) *pgUserPrefsStore {
	return &pgUserPrefsStore{pool: pool}
}

// preferencePayloadForWrite normalises a JSONB-bound payload — pgx
// encodes a nil json.RawMessage as the string "null", which the column
// happily accepts but breaks the "absent ⇒ {}" round-trip. Mirrors the
// pkg/oms.normaliseSignatureForWrite pattern (US-216).
func preferencePayloadForWrite(p json.RawMessage) []byte {
	if len(p) == 0 {
		return []byte("{}")
	}
	return []byte(p)
}

func (s *pgUserPrefsStore) Get(ctx context.Context, userID string) (*userprefs.Preferences, error) {
	var row userprefs.Preferences
	var notif, hk []byte
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, theme, language,
		        COALESCE(notifications, '{}'::jsonb),
		        COALESCE(hotkeys,       '{}'::jsonb),
		        created_at, updated_at
		 FROM user_preferences WHERE user_id = $1`,
		userID).
		Scan(&row.UserID, &row.Theme, &row.Language,
			&notif, &hk, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, userprefs.ErrNotFound
		}
		return nil, err
	}
	row.Notifications = json.RawMessage(notif)
	row.Hotkeys = json.RawMessage(hk)
	return &row, nil
}

func (s *pgUserPrefsStore) Upsert(ctx context.Context, userID string, upd userprefs.Update) (*userprefs.Preferences, error) {
	if userID == "" {
		return nil, errors.New("userprefs: empty user id")
	}
	if upd.Theme != nil {
		t := userprefs.NormaliseTheme(*upd.Theme)
		if err := userprefs.ValidateTheme(t); err != nil {
			return nil, err
		}
		upd.Theme = &t
	}
	if upd.Language != nil {
		l := userprefs.NormaliseLanguage(*upd.Language)
		if err := userprefs.ValidateLanguage(l); err != nil {
			return nil, err
		}
		upd.Language = &l
	}

	// We INSERT a row with the partial values OR DEFAULTS for unspecified
	// fields, then ON CONFLICT update only the columns the caller
	// supplied. Building the SET clause dynamically keeps "absent ⇒
	// preserve" honest — a fixed UPSERT would COALESCE-clobber existing
	// values whenever a partial update came in.
	args := []interface{}{userID}
	idx := 1
	insertCols := []string{"user_id"}
	insertVals := []string{"$1"}
	updates := []string{"updated_at = NOW()"}

	if upd.Theme != nil {
		idx++
		insertCols = append(insertCols, "theme")
		insertVals = append(insertVals, "$"+strconv.Itoa(idx))
		updates = append(updates, "theme = $"+strconv.Itoa(idx))
		args = append(args, *upd.Theme)
	}
	if upd.Language != nil {
		idx++
		insertCols = append(insertCols, "language")
		insertVals = append(insertVals, "$"+strconv.Itoa(idx))
		updates = append(updates, "language = $"+strconv.Itoa(idx))
		args = append(args, *upd.Language)
	}
	if upd.Notifications != nil {
		idx++
		insertCols = append(insertCols, "notifications")
		insertVals = append(insertVals, "$"+strconv.Itoa(idx))
		updates = append(updates, "notifications = $"+strconv.Itoa(idx))
		args = append(args, preferencePayloadForWrite(*upd.Notifications))
	}
	if upd.Hotkeys != nil {
		idx++
		insertCols = append(insertCols, "hotkeys")
		insertVals = append(insertVals, "$"+strconv.Itoa(idx))
		updates = append(updates, "hotkeys = $"+strconv.Itoa(idx))
		args = append(args, preferencePayloadForWrite(*upd.Hotkeys))
	}

	q := `INSERT INTO user_preferences (` + strings.Join(insertCols, ", ") + `)
	      VALUES (` + strings.Join(insertVals, ", ") + `)
	      ON CONFLICT (user_id) DO UPDATE SET ` + strings.Join(updates, ", ")
	if _, err := s.pool.Exec(ctx, q, args...); err != nil {
		return nil, err
	}
	return s.Get(ctx, userID)
}

// DeleteAllForUser hard-deletes the preferences row PK'd on userID.
// Backs the US-494 GDPR cascade-erase contract.
func (s *pgUserPrefsStore) DeleteAllForUser(ctx context.Context, userID string) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM user_preferences WHERE user_id = $1`, userID)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
