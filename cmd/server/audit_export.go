package main

import (
	"log/slog"
	"strings"

	"github.com/liyang/weave/internal/config"
	"github.com/liyang/weave/pkg/audit"
	auditexport "github.com/liyang/weave/pkg/audit/export"
)

// newAuditStoreWithExport returns base unchanged when the export pipeline
// is disabled, else wraps it with a TeeStore that buffers events into a
// BatchedExporter configured from cfg.
//
// All failures during exporter construction are LOGGED and the base store
// is returned unchanged — a broken exporter must never take down the
// audit-write path, which is the canonical compliance surface.
func newAuditStoreWithExport(cfg config.AuditExportConfig, base audit.Store) audit.Store {
	kind := strings.ToLower(strings.TrimSpace(cfg.Kind))
	if kind == "" || kind == "disabled" {
		return base
	}

	var exporter auditexport.Exporter
	switch kind {
	case "stdout":
		exporter = auditexport.NewStdoutExporter(nil)

	case "syslog":
		opts := auditexport.SyslogOptions{
			Facility: cfg.SyslogFacility,
			Severity: cfg.SyslogSeverity,
			Hostname: cfg.SyslogHostname,
			AppName:  cfg.SyslogAppName,
		}
		network := strings.ToLower(strings.TrimSpace(cfg.SyslogNetwork))
		if network == "" {
			network = "udp"
		}
		var err error
		switch network {
		case "udp":
			exporter, err = auditexport.NewSyslogExporterUDP(cfg.SyslogAddress, opts)
		case "tcp":
			exporter, err = auditexport.NewSyslogExporterTCP(cfg.SyslogAddress, opts)
		default:
			slog.Warn("audit export disabled: unsupported syslog network",
				"network", network)
			return base
		}
		if err != nil {
			slog.Warn("audit export disabled: syslog dial failed",
				"network", network, "address", cfg.SyslogAddress, "err", err)
			return base
		}

	case "s3":
		// The repo deliberately doesn't import an AWS SDK (CGO_ENABLED=0
		// + go.mod footprint). Operators who set Kind=s3 but haven't
		// wired their own S3Uploader get a clear warn + fall back to
		// the plain PG store.
		slog.Warn("audit export Kind=s3 requires an S3Uploader adapter wired by the operator; falling back to disabled",
			"bucket", cfg.S3Bucket)
		return base

	default:
		slog.Warn("audit export disabled: unknown kind", "kind", kind)
		return base
	}

	batched := auditexport.NewBatchedExporter(exporter, auditexport.BatchedOptions{
		BatchSize: cfg.BatchSize,
		Retry: auditexport.RetryPolicy{
			MaxAttempts:    cfg.RetryMaxAttempts,
			InitialBackoff: cfg.RetryInitialBackoff,
			MaxBackoff:     cfg.RetryMaxBackoff,
			Multiplier:     cfg.RetryMultiplier,
		},
	})
	slog.Info("audit export enabled",
		"kind", kind,
		"batch_size", cfg.BatchSize,
		"retry_max_attempts", cfg.RetryMaxAttempts,
	)
	return auditexport.NewTeeStore(base, batched)
}
