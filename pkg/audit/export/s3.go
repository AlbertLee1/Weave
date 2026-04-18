package export

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/liyang/weave/pkg/audit"
)

// S3Uploader is the narrow surface the S3 exporter calls. Any AWS SDK
// client (aws-sdk-go-v2 *s3.Client, MinIO client, ceph radosgw etc.) can
// satisfy it via a tiny adapter — the repo deliberately avoids importing
// a specific SDK so the Dockerfile's CGO_ENABLED=0 invariant and the
// go.mod footprint stay intact.
type S3Uploader interface {
	PutObject(ctx context.Context, bucket, key string, body []byte) error
}

// S3Options configures the S3 destination. Bucket is mandatory; Prefix is
// optional and defaults to "". Objects are partitioned under
// "<prefix>/<yyyy>/<mm>/<dd>/<RFC3339>-<uuid>.ndjson" to match the usual
// date-partitioned layout of SIEM/Glue crawlers.
type S3Options struct {
	Bucket string
	Prefix string
}

// S3Exporter uploads each batch as a single NDJSON object to S3 (or any
// S3-compatible object store). Batches are encoded in-memory; this matches
// BatchedExporter's contract of "one Export call == one logical batch".
type S3Exporter struct {
	mu      sync.Mutex
	up      S3Uploader
	opts    S3Options
	nowFunc func() time.Time
}

// NewS3Exporter constructs an S3Exporter. The Bucket option is REQUIRED —
// an empty bucket silently uploads to the wrong place, so guard at
// construction rather than at call time.
func NewS3Exporter(up S3Uploader, opts S3Options) *S3Exporter {
	if strings.TrimSpace(opts.Bucket) == "" {
		panic("audit/export: S3Exporter requires a non-empty Bucket")
	}
	return &S3Exporter{up: up, opts: opts, nowFunc: time.Now}
}

func (e *S3Exporter) Name() string { return "s3" }

func (e *S3Exporter) Export(ctx context.Context, batch []audit.AuditEvent) error {
	if len(batch) == 0 {
		return nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for i := range batch {
		if err := enc.Encode(&batch[i]); err != nil {
			return err
		}
	}

	e.mu.Lock()
	now := e.nowFunc().UTC()
	e.mu.Unlock()

	key := e.buildKey(now)
	return e.up.PutObject(ctx, e.opts.Bucket, key, buf.Bytes())
}

func (e *S3Exporter) buildKey(now time.Time) string {
	// yyyy/mm/dd partitioning + RFC3339-derived filename for S3 lifecycle
	// rules (transition to Glacier after N days, expire after M, etc.).
	date := now.Format("2006/01/02")
	file := fmt.Sprintf("%s-%s.ndjson",
		now.Format("20060102T150405Z"),
		uuid.NewString(),
	)
	prefix := strings.TrimSuffix(e.opts.Prefix, "/")
	if prefix == "" {
		return fmt.Sprintf("%s/%s", date, file)
	}
	return fmt.Sprintf("%s/%s/%s", prefix, date, file)
}
