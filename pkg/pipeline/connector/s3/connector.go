// Package s3 implements an S3-compatible read connector for the
// pipeline framework (US-293). It batches an S3 / MinIO bucket prefix
// into pages of decoded rows, supports CSV and Parquet formats, and
// keeps the SDK plumbing isolated behind a tiny ObjectClient interface
// so unit tests run with no AWS credentials.
//
// The connector follows the same pagination contract as the JDBC
// connector (US-292): ReadPage returns rows + an opaque cursor + a
// hasMore flag, and resumes from the cursor on the next call. Cursor
// state is JSON over a {queue, listToken} pair — a buffered list of
// pending object keys plus the S3 ListObjectsV2 continuation token.
//
// Example wiring (production, against MinIO):
//
//	client, err := s3.NewAWSClient(ctx, s3.AWSConfig{
//	    Region:       "us-east-1",
//	    Endpoint:     "http://minio:9000",
//	    UsePathStyle: true,
//	    AccessKey:    accessKey,
//	    SecretKey:    secretKey,
//	})
//	c, err := s3.New(client, s3.Config{
//	    Bucket:   "events",
//	    Prefix:   "2026/04/",
//	    Format:   s3.FormatCSV,
//	    PageSize: 1000,
//	})
//	for cursor, more := "", true; more; {
//	    rows, next, hasMore, err := c.ReadPage(ctx, cursor)
//	    …
//	    cursor, more = next, hasMore
//	}
package s3

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Format identifies the on-disk encoding of S3 objects in the source
// bucket / prefix. The connector calls the matching decoder on each
// downloaded blob.
type Format string

const (
	// FormatCSV decodes objects as RFC 4180 CSV with a header row.
	// Override the column delimiter, comment marker, etc. via
	// Config.CSV.
	FormatCSV Format = "csv"
	// FormatParquet decodes objects via parquet-go. Reads the entire
	// object into memory before decoding (parquet is a random-access
	// format) so the per-object size budget is bounded by available
	// RAM.
	FormatParquet Format = "parquet"
)

// DefaultPageSize is the maximum object count returned per
// ListObjectsV2 round-trip when Config.PageSize <= 0. Mirrors the JDBC
// connector default and the AWS SDK's own ListObjects per-call cap of
// 1000.
const DefaultPageSize = 1000

// MaxPageSize caps the per-list page size so a misconfigured caller
// can't request more than the AWS SDK's hard limit.
const MaxPageSize = 1000

// Config describes one S3 source. Bucket + Prefix narrow the set of
// objects scanned; Format selects the decoder; CSV carries
// format-specific tuning.
type Config struct {
	// Bucket is the S3 bucket to list. Required.
	Bucket string
	// Prefix narrows the scan to keys beginning with this string.
	// Empty (the default) scans the whole bucket.
	Prefix string
	// Format selects the on-disk decoder. Required.
	Format Format
	// PageSize bounds the number of object keys requested per
	// ListObjectsV2 round-trip. Defaults to DefaultPageSize; values
	// above MaxPageSize are clamped on read.
	PageSize int
	// CSV carries CSV-specific decoder options. Ignored when Format
	// != FormatCSV.
	CSV CSVOptions
}

// effectivePageSize applies the default + cap rules.
func (c *Config) effectivePageSize() int {
	if c.PageSize <= 0 {
		return DefaultPageSize
	}
	if c.PageSize > MaxPageSize {
		return MaxPageSize
	}
	return c.PageSize
}

// Validate reports the first structural issue with c. Pure function;
// safe to call from admin handlers / pipeline-DSL parsers before any
// network I/O.
func (c Config) Validate() error {
	if c.Bucket == "" {
		return errors.New("s3: Config.Bucket must not be empty")
	}
	switch c.Format {
	case FormatCSV, FormatParquet:
		// ok
	case "":
		return errors.New("s3: Config.Format must not be empty (csv|parquet)")
	default:
		return fmt.Errorf("s3: unsupported Format %q (supported: csv, parquet)", c.Format)
	}
	if c.PageSize < 0 {
		return fmt.Errorf("s3: Config.PageSize must be >= 0 (got %d)", c.PageSize)
	}
	return nil
}

// ObjectInfo is one entry in a ListObjects response. Size is the byte
// length reported by S3; the connector uses it for sanity-bounding the
// in-memory download budget but otherwise treats it as advisory.
type ObjectInfo struct {
	Key  string
	Size int64
}

// ObjectClient is the connector's view of an S3-compatible API. The
// interface is intentionally narrow — two methods — so unit tests can
// satisfy it with an in-memory map and the production wrapper around
// aws-sdk-go-v2 stays a thin adapter.
type ObjectClient interface {
	// ListObjects enumerates keys under prefix, paginated by an
	// opaque continuationToken. The first call passes "". The returned
	// nextToken is "" when the listing is exhausted.
	ListObjects(ctx context.Context, prefix string, continuationToken string) (objects []ObjectInfo, nextToken string, err error)
	// GetObject returns the decoded body bytes for key. Implementations
	// may stream internally but must return a fully buffered slice — the
	// parquet decoder needs random access via io.ReaderAt and the CSV
	// decoder is happiest with a bounded buffer.
	GetObject(ctx context.Context, key string) ([]byte, error)
}

// Connector is one open S3 source. Connectors are safe for concurrent
// use; the underlying ObjectClient is responsible for its own
// concurrency story.
type Connector struct {
	client ObjectClient
	cfg    Config
}

// New wraps an ObjectClient + Config in a Connector. cfg is validated
// at construction; subsequent ReadPage calls trust the config.
func New(client ObjectClient, cfg Config) (*Connector, error) {
	if client == nil {
		return nil, errors.New("s3: New requires a non-nil ObjectClient")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Connector{client: client, cfg: cfg}, nil
}

// cursorState is the JSON shape encoded into the opaque cursor string.
// Queue is the buffered list of pending object keys (already returned
// by S3 but not yet read); ListToken is the S3 ListObjectsV2
// continuation token used to fetch the next batch when the queue is
// drained.
type cursorState struct {
	Queue     []string `json:"queue,omitempty"`
	ListToken string   `json:"listToken,omitempty"`
}

// encodeCursor serialises s as base64-of-JSON. base64 keeps the cursor
// URL-safe and opaque for callers; JSON keeps the schema extensible.
func encodeCursor(s cursorState) string {
	if len(s.Queue) == 0 && s.ListToken == "" {
		return ""
	}
	buf, _ := json.Marshal(s)
	return base64.URLEncoding.EncodeToString(buf)
}

// decodeCursor reverses encodeCursor. Empty input returns the zero
// state (first-page semantics). Malformed input is a hard error so
// callers can't accidentally restart a paginated read by passing
// garbage.
func decodeCursor(cur string) (cursorState, error) {
	if cur == "" {
		return cursorState{}, nil
	}
	raw, err := base64.URLEncoding.DecodeString(cur)
	if err != nil {
		return cursorState{}, fmt.Errorf("s3: cursor base64 decode: %w", err)
	}
	var s cursorState
	if err := json.Unmarshal(raw, &s); err != nil {
		return cursorState{}, fmt.Errorf("s3: cursor json decode: %w", err)
	}
	return s, nil
}

// ReadPage downloads and decodes ONE object per call.
//
// cursor semantics:
//   - "" — first call. The connector lists the prefix, queues the
//     returned keys, and reads the first one.
//   - non-empty — opaque base64-encoded JSON of {queue, listToken}.
//     Callers MUST round-trip the value verbatim.
//
// Return values:
//   - rows: the decoded rows from one object. Each map keys columns by
//     CSV header name (FormatCSV) or parquet schema field path
//     (FormatParquet). Values are Go-native types: string for CSV,
//     int64 / float64 / bool / string / time.Time / etc. for Parquet.
//     An object with no data rows still returns []map[string]any{} —
//     never nil.
//   - nextCursor: the cursor for the next call. Empty when hasMore is
//     false.
//   - hasMore: true when the connector knows there's at least one more
//     object to read. False when the queue is empty AND there's no S3
//     continuation token left.
//   - err: surfaced from the ObjectClient or the decoder. The cursor
//     state is left unmodified on err so callers may resume from the
//     same cursor on the next attempt.
//
// One redundant call can occur when the bucket holds an exact multiple
// of the page size and the ListObjects call returns a continuation
// token even though no further objects exist; that's the standard
// tradeoff for cursor pagination without a HEAD round-trip.
func (c *Connector) ReadPage(ctx context.Context, cursor string) (rows []map[string]any, nextCursor string, hasMore bool, err error) {
	state, err := decodeCursor(cursor)
	if err != nil {
		return nil, "", false, err
	}

	// Drain the queue first; refill from S3 only when empty AND the
	// listing isn't exhausted.
	if len(state.Queue) == 0 {
		if cursor != "" && state.ListToken == "" {
			// Exhausted listing AND empty queue ⇒ end of stream.
			return []map[string]any{}, "", false, nil
		}
		objects, nextToken, listErr := c.client.ListObjects(ctx, c.cfg.Prefix, state.ListToken)
		if listErr != nil {
			return nil, "", false, fmt.Errorf("s3: list objects: %w", listErr)
		}
		page := c.cfg.effectivePageSize()
		if len(objects) > page {
			objects = objects[:page]
		}
		state.ListToken = nextToken
		for _, obj := range objects {
			// Skip "directory marker" keys (zero-byte objects whose key
			// ends in "/") — they appear in some MinIO tools but carry
			// no data.
			if strings.HasSuffix(obj.Key, "/") {
				continue
			}
			state.Queue = append(state.Queue, obj.Key)
		}
		// If the listing yielded nothing AND the token is exhausted,
		// we've reached the end of the bucket prefix.
		if len(state.Queue) == 0 && state.ListToken == "" {
			return []map[string]any{}, "", false, nil
		}
		// If listing yielded nothing but the token is non-empty, recurse
		// once to advance — keeps the caller's loop simple.
		if len(state.Queue) == 0 {
			return c.ReadPage(ctx, encodeCursor(state))
		}
	}

	key := state.Queue[0]
	state.Queue = state.Queue[1:]

	data, err := c.client.GetObject(ctx, key)
	if err != nil {
		return nil, "", false, fmt.Errorf("s3: get object %q: %w", key, err)
	}

	rows, err = decodeObject(c.cfg.Format, data, c.cfg.CSV)
	if err != nil {
		return nil, "", false, fmt.Errorf("s3: decode %q: %w", key, err)
	}

	hasMore = len(state.Queue) > 0 || state.ListToken != ""
	if !hasMore {
		return rows, "", false, nil
	}
	return rows, encodeCursor(state), true, nil
}
