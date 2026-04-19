package main

import (
	"context"
	"testing"
	"time"

	"github.com/liyang/weave/internal/config"
	"github.com/liyang/weave/pkg/audit"
)

// fakeRetentionStore satisfies retention.Store without touching PG —
// the scheduler integration checks startAuditRetention's wiring
// decisions, not the store itself.
type fakeRetentionStore struct{}

func (fakeRetentionStore) ListBefore(context.Context, time.Time, int64, int) ([]audit.AuditEvent, error) {
	return nil, nil
}
func (fakeRetentionStore) DeleteBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

// fakeS3Uploader captures uploads so the archive branch is testable.
type fakeS3Uploader struct {
	calls int
}

func (f *fakeS3Uploader) PutObject(_ context.Context, _ string, _ string, _ []byte) error {
	f.calls++
	return nil
}

func TestStartAuditRetention_DisabledReturnsNil(t *testing.T) {
	sched := startAuditRetention(context.Background(),
		config.AuditExportConfig{RetentionDays: 0},
		fakeRetentionStore{}, nil)
	if sched != nil {
		t.Fatalf("expected nil scheduler when RetentionDays=0, got %v", sched)
	}
}

func TestStartAuditRetention_DeleteOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sched := startAuditRetention(ctx,
		config.AuditExportConfig{
			RetentionDays:      30,
			RetentionInterval:  time.Hour,
			RetentionBatchSize: 100,
			RetentionArchive:   "none",
		},
		fakeRetentionStore{}, nil)
	if sched == nil {
		t.Fatalf("expected scheduler when RetentionDays>0")
	}
	defer sched.Stop()
	if sched.Interval() != time.Hour {
		t.Fatalf("Interval()=%v want 1h", sched.Interval())
	}
}

func TestStartAuditRetention_S3ArchiveRequestedNoUploader_Warns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Missing S3Uploader ⇒ scheduler still starts, in delete-only mode.
	sched := startAuditRetention(ctx,
		config.AuditExportConfig{
			RetentionDays:    30,
			RetentionArchive: "s3",
			S3Bucket:         "audit-bucket",
		},
		fakeRetentionStore{}, nil)
	if sched == nil {
		t.Fatalf("expected scheduler to start despite missing uploader")
	}
	sched.Stop()
}

func TestStartAuditRetention_S3ArchiveRequestedNoBucket_Warns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	up := &fakeS3Uploader{}
	sched := startAuditRetention(ctx,
		config.AuditExportConfig{
			RetentionDays:    30,
			RetentionArchive: "s3",
			S3Bucket:         "", // missing
		},
		fakeRetentionStore{}, up)
	if sched == nil {
		t.Fatalf("expected scheduler to start despite missing bucket")
	}
	sched.Stop()
}

func TestStartAuditRetention_S3ArchiveWiredWhenBothPresent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	up := &fakeS3Uploader{}
	sched := startAuditRetention(ctx,
		config.AuditExportConfig{
			RetentionDays:     30,
			RetentionArchive:  "s3",
			S3Bucket:          "audit-bucket",
			RetentionS3Prefix: "retention",
		},
		fakeRetentionStore{}, up)
	if sched == nil {
		t.Fatalf("expected scheduler to start with s3 uploader wired")
	}
	sched.Stop()
}

func TestStartAuditRetention_UnknownArchive_FallsBackToDeleteOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sched := startAuditRetention(ctx,
		config.AuditExportConfig{
			RetentionDays:    30,
			RetentionArchive: "gcs", // unsupported
		},
		fakeRetentionStore{}, nil)
	if sched == nil {
		t.Fatalf("expected scheduler to start for unknown archive kind (delete-only)")
	}
	sched.Stop()
}
