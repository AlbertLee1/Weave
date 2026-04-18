package export

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/audit"
)

type fakeS3 struct {
	calls []fakeS3Call
	err   error
}

type fakeS3Call struct {
	Bucket string
	Key    string
	Body   []byte
}

func (f *fakeS3) PutObject(_ context.Context, bucket, key string, body []byte) error {
	if f.err != nil {
		return f.err
	}
	// Copy body so later mutations don't race.
	cp := make([]byte, len(body))
	copy(cp, body)
	f.calls = append(f.calls, fakeS3Call{Bucket: bucket, Key: key, Body: cp})
	return nil
}

func TestS3Exporter_WritesNDJSONObject(t *testing.T) {
	fs := &fakeS3{}
	fixedNow := time.Date(2026, 4, 19, 12, 34, 56, 0, time.UTC)
	exp := NewS3Exporter(fs, S3Options{
		Bucket: "audit-logs",
		Prefix: "prod/",
	})
	exp.nowFunc = func() time.Time { return fixedNow }

	evts := []audit.AuditEvent{sampleEvent("a"), sampleEvent("b")}
	if err := exp.Export(context.Background(), evts); err != nil {
		t.Fatalf("Export: %v", err)
	}

	if len(fs.calls) != 1 {
		t.Fatalf("expected 1 PutObject call, got %d", len(fs.calls))
	}
	call := fs.calls[0]
	if call.Bucket != "audit-logs" {
		t.Fatalf("bucket=%q want audit-logs", call.Bucket)
	}
	// Key should be partitioned by date and end with .ndjson
	if !strings.HasPrefix(call.Key, "prod/2026/04/19/") {
		t.Fatalf("key=%q expected prod/2026/04/19/ prefix", call.Key)
	}
	if !strings.HasSuffix(call.Key, ".ndjson") {
		t.Fatalf("key=%q expected .ndjson suffix", call.Key)
	}

	lines := strings.Split(strings.TrimRight(string(call.Body), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 ndjson lines, got %d: %q", len(lines), string(call.Body))
	}
	var got audit.AuditEvent
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("line 0 not JSON: %v", err)
	}
	if got.ID != "a" {
		t.Fatalf("line 0 id=%q want a", got.ID)
	}
}

func TestS3Exporter_EmptyBatchNoop(t *testing.T) {
	fs := &fakeS3{}
	exp := NewS3Exporter(fs, S3Options{Bucket: "b"})
	if err := exp.Export(context.Background(), nil); err != nil {
		t.Fatalf("Export(nil): %v", err)
	}
	if len(fs.calls) != 0 {
		t.Fatalf("expected no upload for empty batch")
	}
}

func TestS3Exporter_RequiresBucket(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic when Bucket is empty")
		}
	}()
	NewS3Exporter(&fakeS3{}, S3Options{})
}

func TestS3Exporter_Name(t *testing.T) {
	exp := NewS3Exporter(&fakeS3{}, S3Options{Bucket: "b"})
	if got := exp.Name(); got != "s3" {
		t.Fatalf("Name()=%q want s3", got)
	}
}
