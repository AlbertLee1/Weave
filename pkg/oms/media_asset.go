package oms

import (
	"context"
	"fmt"
	"time"
)

// MediaAsset is the OMS catalog row for a single media upload. Physical
// bytes live on disk at Path (relative to the media storage root); the row
// is the reference counter for dedup purposes — multiple rows MAY share a
// (realm, sha256) pair and therefore the same Path.
type MediaAsset struct {
	RID       string    `json:"rid"`
	Realm     string    `json:"realm"`
	Filename  string    `json:"filename,omitempty"`
	MIME      string    `json:"mime"`
	SizeBytes int64     `json:"sizeBytes"`
	SHA256    string    `json:"sha256"`
	Path      string    `json:"path"`
	CreatedBy string    `json:"createdBy,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Validate checks the shape of a MediaAsset row before the repo persists it.
func (m MediaAsset) Validate() error {
	if m.RID == "" {
		return fmt.Errorf("media asset requires rid")
	}
	if m.Realm == "" {
		return fmt.Errorf("media asset requires realm")
	}
	if m.SHA256 == "" {
		return fmt.Errorf("media asset requires sha256")
	}
	if m.Path == "" {
		return fmt.Errorf("media asset requires path")
	}
	if m.SizeBytes < 0 {
		return fmt.Errorf("media asset sizeBytes must be >= 0")
	}
	return nil
}

// MediaAssetStore is the narrow read/write surface callers (HTTP handlers,
// GC sweepers) depend on. It is intentionally OUT of oms.Repository so the
// many mock repos scattered through the test tree do not need stubs — see
// the US-202 learning in progress.txt.
type MediaAssetStore interface {
	CreateMediaAsset(ctx context.Context, a *MediaAsset) error
	GetMediaAsset(ctx context.Context, rid string) (*MediaAsset, error)
	DeleteMediaAsset(ctx context.Context, rid string) error
	CountBySHA256(ctx context.Context, realm, sha256 string) (int, error)
	ListByCreatedBy(ctx context.Context, createdBy string, limit int) ([]MediaAsset, error)
}
