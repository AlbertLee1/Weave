package oms

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CreateMediaAsset inserts a media_assets row.
func (r *PGRepository) CreateMediaAsset(ctx context.Context, a *MediaAsset) error {
	if err := a.Validate(); err != nil {
		return fmt.Errorf("create media asset: %w", err)
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO media_assets (rid, realm, filename, mime, size_bytes, sha256, path, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		a.RID, a.Realm, a.Filename, a.MIME, a.SizeBytes, a.SHA256, a.Path, a.CreatedBy)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

// GetMediaAsset fetches a single media_assets row by rid.
func (r *PGRepository) GetMediaAsset(ctx context.Context, rid string) (*MediaAsset, error) {
	a := &MediaAsset{}
	err := r.pool.QueryRow(ctx,
		`SELECT rid, realm, filename, mime, size_bytes, sha256, path, created_by, created_at
		 FROM media_assets WHERE rid = $1`, rid).
		Scan(&a.RID, &a.Realm, &a.Filename, &a.MIME, &a.SizeBytes, &a.SHA256, &a.Path, &a.CreatedBy, &a.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return a, nil
}

// DeleteMediaAsset removes a single media_assets row. Reclaiming the
// underlying file is the caller's responsibility (via CountBySHA256).
func (r *PGRepository) DeleteMediaAsset(ctx context.Context, rid string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM media_assets WHERE rid = $1`, rid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CountBySHA256 returns the number of catalog rows pointing at the same
// (realm, sha256). Used by DELETE flows to decide whether the physical file
// can be reclaimed.
func (r *PGRepository) CountBySHA256(ctx context.Context, realm, sha256 string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM media_assets WHERE realm = $1 AND sha256 = $2`,
		realm, sha256).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// ListByCreatedBy returns up to `limit` media assets uploaded by the given
// user, newest first. limit<=0 defaults to 100.
func (r *PGRepository) ListByCreatedBy(ctx context.Context, createdBy string, limit int) ([]MediaAsset, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx,
		`SELECT rid, realm, filename, mime, size_bytes, sha256, path, created_by, created_at
		 FROM media_assets WHERE created_by = $1 ORDER BY created_at DESC LIMIT $2`,
		createdBy, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MediaAsset
	for rows.Next() {
		var a MediaAsset
		if err := rows.Scan(&a.RID, &a.Realm, &a.Filename, &a.MIME, &a.SizeBytes, &a.SHA256, &a.Path, &a.CreatedBy, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}
