package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/apps"
)

// pgAppsStore satisfies apps.Store by persisting rows to the `apps` +
// `app_versions` tables (US-391, migration 000094). Lives in cmd/server
// rather than pkg/apps so the package stays free of any pgx import —
// same dep-direction trick as pgDashboardsStore.
//
// Every Update bumps the live row's `version` and inserts a fresh
// snapshot row into `app_versions` inside one transaction so the two
// surfaces never drift.
type pgAppsStore struct {
	pool *pgxpool.Pool
}

func newPGAppsStore(pool *pgxpool.Pool) *pgAppsStore {
	return &pgAppsStore{pool: pool}
}

func isAppsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 23505") ||
		strings.Contains(msg, "duplicate key value")
}

// layoutForWrite normalises a JSONB-bound payload — pgx encodes a nil
// json.RawMessage as the string "null", which the column will accept
// but breaks the "absent ⇒ {}" round-trip.
func layoutForWrite(def json.RawMessage) []byte {
	if len(def) == 0 {
		return []byte("{}")
	}
	return []byte(def)
}

func (s *pgAppsStore) Create(ctx context.Context, app *apps.App, createdBy string) error {
	if err := apps.ValidateName(app.Name); err != nil {
		return err
	}
	if err := apps.ValidateLayout(app.LayoutJSON); err != nil {
		return errors.Join(apps.ErrInvalidLayout, err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx,
		`INSERT INTO apps (rid, name, owner_id, layout_json, version)
		 VALUES ($1, $2, $3, $4, 1)`,
		app.RID, app.Name, app.OwnerID, layoutForWrite(app.LayoutJSON),
	)
	if err != nil {
		if isAppsUniqueViolation(err) {
			return apps.ErrNameConflict
		}
		return err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO app_versions (app_rid, version, name, layout_json, created_by)
		 VALUES ($1, 1, $2, $3, $4)`,
		app.RID, app.Name, layoutForWrite(app.LayoutJSON), createdBy,
	)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	fresh, err := s.getRaw(ctx, app.RID)
	if err != nil {
		return err
	}
	*app = *fresh
	return nil
}

// getRaw fetches a row by RID with no owner gate. Internal — callers
// reach the gated form through Get.
func (s *pgAppsStore) getRaw(ctx context.Context, rid string) (*apps.App, error) {
	var row apps.App
	var layoutBytes []byte
	var pubVersion *int
	var pubAt *time.Time
	var pubBy *string
	err := s.pool.QueryRow(ctx,
		`SELECT rid, name, owner_id,
		        COALESCE(layout_json, '{}'::jsonb), version, created_at, updated_at,
		        published_version, published_at, published_by
		 FROM apps WHERE rid = $1`,
		rid).
		Scan(&row.RID, &row.Name, &row.OwnerID,
			&layoutBytes, &row.Version, &row.CreatedAt, &row.UpdatedAt,
			&pubVersion, &pubAt, &pubBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apps.ErrNotFound
		}
		return nil, err
	}
	row.LayoutJSON = json.RawMessage(layoutBytes)
	row.PublishedVersion = pubVersion
	row.PublishedAt = pubAt
	row.PublishedBy = pubBy
	return &row, nil
}

func (s *pgAppsStore) Get(ctx context.Context, rid, ownerID string) (*apps.App, error) {
	row, err := s.getRaw(ctx, rid)
	if err != nil {
		return nil, err
	}
	if row.OwnerID != ownerID {
		return nil, apps.ErrNotFound
	}
	return row, nil
}

func (s *pgAppsStore) List(ctx context.Context, ownerID string) ([]*apps.App, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT rid, name, owner_id,
		        COALESCE(layout_json, '{}'::jsonb), version, created_at, updated_at,
		        published_version, published_at, published_by
		 FROM apps WHERE owner_id = $1 ORDER BY name ASC`,
		ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*apps.App
	for rows.Next() {
		var r apps.App
		var layoutBytes []byte
		var pubVersion *int
		var pubAt *time.Time
		var pubBy *string
		if err := rows.Scan(&r.RID, &r.Name, &r.OwnerID,
			&layoutBytes, &r.Version, &r.CreatedAt, &r.UpdatedAt,
			&pubVersion, &pubAt, &pubBy); err != nil {
			return nil, err
		}
		r.LayoutJSON = json.RawMessage(layoutBytes)
		r.PublishedVersion = pubVersion
		r.PublishedAt = pubAt
		r.PublishedBy = pubBy
		out = append(out, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *pgAppsStore) Update(ctx context.Context, rid, ownerID string, upd apps.Update, createdBy string) error {
	if upd.Name != nil {
		if err := apps.ValidateName(*upd.Name); err != nil {
			return err
		}
	}
	if upd.LayoutJSON != nil {
		if err := apps.ValidateLayout(*upd.LayoutJSON); err != nil {
			return errors.Join(apps.ErrInvalidLayout, err)
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Lock the live row so the version bump is race-free across
	// concurrent updates.
	var (
		curName    string
		curOwnerID string
		curLayout  []byte
		curVersion int
	)
	err = tx.QueryRow(ctx,
		`SELECT name, owner_id, COALESCE(layout_json, '{}'::jsonb), version
		 FROM apps WHERE rid = $1 FOR UPDATE`,
		rid).Scan(&curName, &curOwnerID, &curLayout, &curVersion)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apps.ErrNotFound
		}
		return err
	}
	if curOwnerID != ownerID {
		return apps.ErrNotFound
	}

	newName := curName
	if upd.Name != nil {
		newName = *upd.Name
	}
	newLayout := curLayout
	if upd.LayoutJSON != nil {
		newLayout = layoutForWrite(*upd.LayoutJSON)
	}
	newVersion := curVersion + 1

	_, err = tx.Exec(ctx,
		`UPDATE apps
		 SET name = $1, layout_json = $2, version = $3, updated_at = NOW()
		 WHERE rid = $4`,
		newName, newLayout, newVersion, rid)
	if err != nil {
		if isAppsUniqueViolation(err) {
			return apps.ErrNameConflict
		}
		return err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO app_versions (app_rid, version, name, layout_json, created_by)
		 VALUES ($1, $2, $3, $4, $5)`,
		rid, newVersion, newName, newLayout, createdBy)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *pgAppsStore) Delete(ctx context.Context, rid, ownerID string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM apps WHERE rid = $1 AND owner_id = $2`,
		rid, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apps.ErrNotFound
	}
	return nil
}

func (s *pgAppsStore) ListVersions(ctx context.Context, rid, ownerID string) ([]*apps.AppVersion, error) {
	// Owner gate: the live row must exist and belong to the caller.
	if _, err := s.Get(ctx, rid, ownerID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT app_rid, version, name,
		        COALESCE(layout_json, '{}'::jsonb), created_at, created_by
		 FROM app_versions WHERE app_rid = $1 ORDER BY version DESC`,
		rid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*apps.AppVersion
	for rows.Next() {
		var r apps.AppVersion
		var layoutBytes []byte
		if err := rows.Scan(&r.AppRID, &r.Version, &r.Name,
			&layoutBytes, &r.CreatedAt, &r.CreatedBy); err != nil {
			return nil, err
		}
		r.LayoutJSON = json.RawMessage(layoutBytes)
		out = append(out, &r)
	}
	return out, rows.Err()
}

func (s *pgAppsStore) Publish(ctx context.Context, rid, ownerID, publishedBy string) (*apps.PublishedAppView, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var (
		curOwnerID string
		curVersion int
		curName    string
	)
	err = tx.QueryRow(ctx,
		`SELECT owner_id, version, name FROM apps WHERE rid = $1 FOR UPDATE`,
		rid).Scan(&curOwnerID, &curVersion, &curName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apps.ErrNotFound
		}
		return nil, err
	}
	if curOwnerID != ownerID {
		return nil, apps.ErrNotFound
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx,
		`UPDATE apps
		 SET published_version = $1, published_at = $2, published_by = $3
		 WHERE rid = $4`,
		curVersion, now, publishedBy, rid); err != nil {
		return nil, err
	}
	var (
		snapName    string
		layoutBytes []byte
	)
	err = tx.QueryRow(ctx,
		`SELECT name, COALESCE(layout_json, '{}'::jsonb)
		 FROM app_versions WHERE app_rid = $1 AND version = $2`,
		rid, curVersion).Scan(&snapName, &layoutBytes)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apps.ErrNotFound
		}
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &apps.PublishedAppView{
		RID:              rid,
		Name:             snapName,
		OwnerID:          curOwnerID,
		PublishedVersion: curVersion,
		PublishedAt:      now,
		PublishedBy:      publishedBy,
		LayoutJSON:       json.RawMessage(layoutBytes),
	}, nil
}

func (s *pgAppsStore) Unpublish(ctx context.Context, rid, ownerID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE apps
		 SET published_version = NULL, published_at = NULL, published_by = NULL
		 WHERE rid = $1 AND owner_id = $2`,
		rid, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apps.ErrNotFound
	}
	return nil
}

func (s *pgAppsStore) GetPublished(ctx context.Context, rid string) (*apps.PublishedAppView, error) {
	var (
		ownerID    string
		pubVersion *int
		pubAt      *time.Time
		pubBy      *string
	)
	err := s.pool.QueryRow(ctx,
		`SELECT owner_id, published_version, published_at, published_by
		 FROM apps WHERE rid = $1`,
		rid).Scan(&ownerID, &pubVersion, &pubAt, &pubBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apps.ErrNotFound
		}
		return nil, err
	}
	if pubVersion == nil {
		return nil, apps.ErrNotPublished
	}
	var (
		snapName    string
		layoutBytes []byte
	)
	err = s.pool.QueryRow(ctx,
		`SELECT name, COALESCE(layout_json, '{}'::jsonb)
		 FROM app_versions WHERE app_rid = $1 AND version = $2`,
		rid, *pubVersion).Scan(&snapName, &layoutBytes)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apps.ErrNotPublished
		}
		return nil, err
	}
	view := &apps.PublishedAppView{
		RID:              rid,
		Name:             snapName,
		OwnerID:          ownerID,
		PublishedVersion: *pubVersion,
		LayoutJSON:       json.RawMessage(layoutBytes),
	}
	if pubAt != nil {
		view.PublishedAt = *pubAt
	}
	if pubBy != nil {
		view.PublishedBy = *pubBy
	}
	return view, nil
}

func (s *pgAppsStore) GetVersion(ctx context.Context, rid string, version int, ownerID string) (*apps.AppVersion, error) {
	if _, err := s.Get(ctx, rid, ownerID); err != nil {
		return nil, err
	}
	var r apps.AppVersion
	var layoutBytes []byte
	err := s.pool.QueryRow(ctx,
		`SELECT app_rid, version, name,
		        COALESCE(layout_json, '{}'::jsonb), created_at, created_by
		 FROM app_versions WHERE app_rid = $1 AND version = $2`,
		rid, version).
		Scan(&r.AppRID, &r.Version, &r.Name, &layoutBytes, &r.CreatedAt, &r.CreatedBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apps.ErrNotFound
		}
		return nil, err
	}
	r.LayoutJSON = json.RawMessage(layoutBytes)
	return &r, nil
}
