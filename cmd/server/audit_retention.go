package main

import (
	"context"
	"log/slog"
	"strings"

	"github.com/liyang/weave/internal/config"
	"github.com/liyang/weave/pkg/audit"
	auditexport "github.com/liyang/weave/pkg/audit/export"
	"github.com/liyang/weave/pkg/audit/retention"
)

// startAuditRetention builds and starts the US-269 retention scheduler
// when cfg.RetentionDays > 0. Returns the scheduler (so main can
// defer Stop) or nil when retention is disabled.
//
// The archive sink is wired from cfg.RetentionArchive:
//   - "none" (default) — delete-only sweep
//   - "s3" + uploader != nil — archives ndjson objects under
//     <RetentionS3Prefix>/... in the same bucket the SIEM exporter
//     uses; operators must also set WEAVE_AUDIT_EXPORT_S3_BUCKET
//
// If an archive kind is requested but its uploader isn't wired, the
// scheduler falls back to delete-only and a warning is logged — the
// deletion half of retention is the only mandatory guarantee.
func startAuditRetention(
	ctx context.Context,
	cfg config.AuditExportConfig,
	store retention.Store,
	s3Uploader auditexport.S3Uploader,
) *retention.Scheduler {
	if cfg.RetentionDays <= 0 {
		return nil
	}

	svc := retention.NewService(store, cfg.RetentionDays)
	if cfg.RetentionBatchSize > 0 {
		svc.SetBatchSize(cfg.RetentionBatchSize)
	}

	switch strings.ToLower(strings.TrimSpace(cfg.RetentionArchive)) {
	case "", "none":
		// delete-only
	case "s3":
		if s3Uploader == nil {
			slog.Warn("audit retention archive=s3 requested but no S3Uploader wired; running delete-only")
		} else if strings.TrimSpace(cfg.S3Bucket) == "" {
			slog.Warn("audit retention archive=s3 requires WEAVE_AUDIT_EXPORT_S3_BUCKET; running delete-only")
		} else {
			exp := auditexport.NewS3Exporter(s3Uploader, auditexport.S3Options{
				Bucket: cfg.S3Bucket,
				Prefix: cfg.RetentionS3Prefix,
			})
			svc.SetArchiver(exporterArchiver{exp: exp})
		}
	default:
		slog.Warn("audit retention: unknown archive kind; running delete-only",
			"archive", cfg.RetentionArchive)
	}

	sched := retention.NewScheduler(svc, cfg.RetentionInterval)
	sched.Start(ctx)
	slog.Info("audit retention enabled",
		"days", cfg.RetentionDays,
		"interval", sched.Interval(),
		"batch_size", svc.BatchSize(),
		"archive", svc.ArchiverName(),
	)
	return sched
}

// exporterArchiver adapts an auditexport.Exporter into the
// retention.Archiver interface. Thin method forwarders — the
// underlying exporter already speaks batch-of-events semantics so no
// additional translation is needed.
type exporterArchiver struct {
	exp auditexport.Exporter
}

func (a exporterArchiver) Archive(ctx context.Context, batch []audit.AuditEvent) error {
	return a.exp.Export(ctx, batch)
}

func (a exporterArchiver) Name() string { return a.exp.Name() }
