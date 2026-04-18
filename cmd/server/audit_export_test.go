package main

import (
	"context"
	"testing"
	"time"

	"github.com/liyang/weave/internal/config"
	"github.com/liyang/weave/pkg/audit"
	auditexport "github.com/liyang/weave/pkg/audit/export"
)

func TestNewAuditStoreWithExport_DisabledPassthrough(t *testing.T) {
	base := audit.NewMemoryStore()
	got := newAuditStoreWithExport(config.AuditExportConfig{Kind: "disabled"}, base)
	if got != audit.Store(base) {
		t.Fatalf("disabled kind should return base store unchanged")
	}
}

func TestNewAuditStoreWithExport_EmptyKindPassthrough(t *testing.T) {
	base := audit.NewMemoryStore()
	got := newAuditStoreWithExport(config.AuditExportConfig{}, base)
	if got != audit.Store(base) {
		t.Fatalf("empty kind should return base store unchanged")
	}
}

func TestNewAuditStoreWithExport_StdoutReturnsTeeStore(t *testing.T) {
	base := audit.NewMemoryStore()
	got := newAuditStoreWithExport(config.AuditExportConfig{
		Kind:      "stdout",
		BatchSize: 1,
	}, base)
	if _, ok := got.(*auditexport.TeeStore); !ok {
		t.Fatalf("expected *TeeStore, got %T", got)
	}
	// The wrapper must still persist events to the base store.
	err := audit.Record(context.Background(), got, audit.AuditEvent{
		ActorID:   "u",
		Action:    "x",
		Timestamp: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if len(base.Events()) != 1 {
		t.Fatalf("expected 1 event persisted, got %d", len(base.Events()))
	}
}

func TestNewAuditStoreWithExport_UnknownKindFallsBack(t *testing.T) {
	base := audit.NewMemoryStore()
	got := newAuditStoreWithExport(config.AuditExportConfig{Kind: "kafka"}, base)
	if got != audit.Store(base) {
		t.Fatalf("unknown kind should fall back to base")
	}
}

func TestNewAuditStoreWithExport_S3WithoutSDKFallsBack(t *testing.T) {
	base := audit.NewMemoryStore()
	// S3 kind without an SDK adapter wired: operators get a warn + base fallback.
	got := newAuditStoreWithExport(config.AuditExportConfig{
		Kind:     "s3",
		S3Bucket: "x",
	}, base)
	if got != audit.Store(base) {
		t.Fatalf("s3 without adapter should fall back to base")
	}
}
