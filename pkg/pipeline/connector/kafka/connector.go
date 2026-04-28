// Package kafka implements a Kafka consumer-group read connector for
// the pipeline framework (US-295). It is the streaming counterpart to
// the polling REST (US-294) / S3 (US-293) / JDBC (US-292) connectors:
// rather than driving pagination from a cursor on the wire, the
// connector relies on Kafka's consumer-group machinery to track offsets
// server-side, with optional auto-commit at a caller-tunable interval.
//
// The connector wraps segmentio/kafka-go's *kafka.Reader behind a
// MessageReader interface so tests can stub in canned message streams
// without standing up a real broker.
//
// Example wiring (in cmd/server or a connector loader):
//
//	c, err := kafka.New(kafka.Config{
//	    Brokers:        []string{"kafka.internal:9092"},
//	    Topic:          "events",
//	    GroupID:        "weave-pipeline",
//	    CommitInterval: 1 * time.Second,
//	    ValueFormat:    kafka.ValueFormatJSON,
//	})
//	defer c.Close()
//	for {
//	    ctx, cancel := context.WithTimeout(parent, 5*time.Second)
//	    rows, hasMore, err := c.ReadBatch(ctx, 500)
//	    cancel()
//	    if err != nil { … }
//	    publish(rows)
//	    if !hasMore { sleepUntilNextTick() }
//	}
//
// Pure-Go dependency only — preserves the project's CGO_ENABLED=0
// Dockerfile invariant (kafka-go has no cgo bindings).
package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

// Defaults applied to corresponding zero-valued Config fields. Picked
// to be safe out-of-the-box: small fetch floor (1 byte so the broker
// returns whatever's available), 10MiB ceiling (matches kafka-go's
// recommended default), 1s wait window, 1s auto-commit cadence.
const (
	DefaultMinBytes       = 1
	DefaultMaxBytes       = 10 * 1024 * 1024
	DefaultMaxWait        = 1 * time.Second
	DefaultCommitInterval = 1 * time.Second
	DefaultBatchSize      = 1000
	// MaxBatchSize caps ReadBatch's per-call message budget so a
	// misconfigured caller can't exhaust memory in one shot.
	MaxBatchSize = 100000
)

// ValueFormat selects how the message Value byte slice is exposed in
// the row map produced by ReadBatch.
type ValueFormat string

const (
	// ValueFormatRaw exposes the raw []byte under "value" (default).
	// JSON-marshalling a row will base64-encode the bytes per Go's
	// encoding/json default.
	ValueFormatRaw ValueFormat = ""
	// ValueFormatJSON decodes Value as JSON and exposes the resulting
	// object/array/scalar under "value". Empty / null payloads decode
	// to nil. Decode failures bubble up as ReadBatch errors so a
	// schema drift surfaces loudly instead of silently dropping rows.
	ValueFormatJSON ValueFormat = "json"
	// ValueFormatString exposes the value as a UTF-8 string. Useful
	// for line-oriented logs where downstream code wants strings, not
	// []byte.
	ValueFormatString ValueFormat = "string"
)

// Config describes one Kafka consumer-group source. Validate is the
// source of truth for what counts as well-formed; New / NewWithReader
// call it before doing any work.
type Config struct {
	// Brokers is the bootstrap broker list (host:port). Required;
	// non-empty entries.
	Brokers []string

	// Topic is the topic name to consume from. Required.
	Topic string

	// GroupID is the consumer group identifier. Required — the
	// connector always reads as a group member so offsets persist
	// across restarts.
	GroupID string

	// MinBytes / MaxBytes bound the fetch size returned by the broker
	// per batch. Zero values fall back to DefaultMinBytes /
	// DefaultMaxBytes. MinBytes must be <= MaxBytes.
	MinBytes int
	MaxBytes int

	// MaxWait is the broker's maximum wait window when fewer than
	// MinBytes are available. Defaults to DefaultMaxWait when zero.
	MaxWait time.Duration

	// CommitInterval enables auto-commit at the given cadence. When
	// zero, the underlying *kafka.Reader runs in manual-commit mode —
	// the connector itself never calls CommitMessages, so callers
	// using auto-commit MUST set this to a positive value (or rely on
	// the New() default which substitutes DefaultCommitInterval). A
	// negative value is rejected.
	CommitInterval time.Duration

	// StartOffset is the read position used when the consumer group
	// has no committed offset for a partition. Pass kafkago.FirstOffset
	// (-2) to start from the beginning, kafkago.LastOffset (-1) to
	// start from the tail. Zero is treated by kafka-go as LastOffset.
	StartOffset int64

	// ValueFormat selects the value-decoding strategy. Empty defaults
	// to ValueFormatRaw.
	ValueFormat ValueFormat

	// BatchSize is the per-call ReadBatch row budget when the caller
	// passes max <= 0. Defaults to DefaultBatchSize when zero;
	// clamped at MaxBatchSize on read.
	BatchSize int

	// Dialer is an optional kafka-go dialer for SASL / TLS / custom
	// transports. nil uses kafka-go's default.
	Dialer *kafkago.Dialer
}

// Validate reports the first structural issue with c. Pure function;
// safe to call from admin handlers / pipeline-DSL parsers before
// attempting to open a connection.
func (c Config) Validate() error {
	if len(c.Brokers) == 0 {
		return errors.New("kafka: Config.Brokers must not be empty")
	}
	for i, b := range c.Brokers {
		if strings.TrimSpace(b) == "" {
			return fmt.Errorf("kafka: Config.Brokers[%d] must not be empty", i)
		}
	}
	if c.Topic == "" {
		return errors.New("kafka: Config.Topic must not be empty")
	}
	if c.GroupID == "" {
		return errors.New("kafka: Config.GroupID must not be empty")
	}
	if c.MinBytes < 0 {
		return fmt.Errorf("kafka: Config.MinBytes must be >= 0 (got %d)", c.MinBytes)
	}
	if c.MaxBytes < 0 {
		return fmt.Errorf("kafka: Config.MaxBytes must be >= 0 (got %d)", c.MaxBytes)
	}
	if c.MaxBytes > 0 && c.MinBytes > c.MaxBytes {
		return fmt.Errorf("kafka: Config.MinBytes (%d) must be <= Config.MaxBytes (%d)", c.MinBytes, c.MaxBytes)
	}
	if c.MaxWait < 0 {
		return fmt.Errorf("kafka: Config.MaxWait must be >= 0 (got %s)", c.MaxWait)
	}
	if c.CommitInterval < 0 {
		return fmt.Errorf("kafka: Config.CommitInterval must be >= 0 (got %s)", c.CommitInterval)
	}
	if c.BatchSize < 0 {
		return fmt.Errorf("kafka: Config.BatchSize must be >= 0 (got %d)", c.BatchSize)
	}
	switch c.ValueFormat {
	case ValueFormatRaw, ValueFormatJSON, ValueFormatString:
	default:
		return fmt.Errorf("kafka: unsupported ValueFormat %q (supported: \"\", json, string)", c.ValueFormat)
	}
	return nil
}

func (c *Config) effectiveMinBytes() int {
	if c.MinBytes <= 0 {
		return DefaultMinBytes
	}
	return c.MinBytes
}

func (c *Config) effectiveMaxBytes() int {
	if c.MaxBytes <= 0 {
		return DefaultMaxBytes
	}
	return c.MaxBytes
}

func (c *Config) effectiveMaxWait() time.Duration {
	if c.MaxWait <= 0 {
		return DefaultMaxWait
	}
	return c.MaxWait
}

func (c *Config) effectiveCommitInterval() time.Duration {
	if c.CommitInterval == 0 {
		return DefaultCommitInterval
	}
	return c.CommitInterval
}

func (c *Config) effectiveBatchSize() int {
	if c.BatchSize <= 0 {
		return DefaultBatchSize
	}
	if c.BatchSize > MaxBatchSize {
		return MaxBatchSize
	}
	return c.BatchSize
}

// Header is one Kafka message header. Mirrors kafkago.Header but kept
// in this package's namespace so tests / downstream consumers don't
// need to import kafka-go directly.
type Header struct {
	Key   string
	Value []byte
}

// Message is the connector-internal message shape; decouples the
// public API from kafka-go types so tests can stub MessageReader
// without pulling kafka-go's struct surface into test fixtures.
type Message struct {
	Topic     string
	Partition int
	Offset    int64
	Key       []byte
	Value     []byte
	Time      time.Time
	Headers   []Header
}

// MessageReader is the minimal interface the Connector consumes. The
// segmentio/kafka-go *kafka.Reader satisfies it via the readerAdapter
// wrapper produced by New().
type MessageReader interface {
	ReadMessage(ctx context.Context) (Message, error)
	Close() error
}

// Connector is one open Kafka source. Connectors are NOT safe for
// concurrent ReadBatch calls — the underlying kafka.Reader serialises
// reads, but ordering matters for offset commits, so callers should
// drive a single goroutine per consumer-group member.
type Connector struct {
	reader MessageReader
	cfg    Config
}

// New builds a Connector backed by a fresh *kafka.Reader configured
// per cfg. cfg is validated before construction so misconfigured
// callers fail fast rather than at first read. Auto-commit is enabled
// by default at DefaultCommitInterval; pass a positive cfg.CommitInterval
// to override.
func New(cfg Config) (*Connector, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	rc := kafkago.ReaderConfig{
		Brokers:        cfg.Brokers,
		Topic:          cfg.Topic,
		GroupID:        cfg.GroupID,
		MinBytes:       cfg.effectiveMinBytes(),
		MaxBytes:       cfg.effectiveMaxBytes(),
		MaxWait:        cfg.effectiveMaxWait(),
		CommitInterval: cfg.effectiveCommitInterval(),
		StartOffset:    cfg.StartOffset,
	}
	if cfg.Dialer != nil {
		rc.Dialer = cfg.Dialer
	}
	r := kafkago.NewReader(rc)
	return &Connector{reader: &readerAdapter{r: r}, cfg: cfg}, nil
}

// NewWithReader wraps a caller-provided MessageReader. Use this for
// tests (with a stub reader) or for advanced production wiring (custom
// kafka.Reader configured with non-default options).
func NewWithReader(reader MessageReader, cfg Config) (*Connector, error) {
	if reader == nil {
		return nil, errors.New("kafka: NewWithReader requires a non-nil MessageReader")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Connector{reader: reader, cfg: cfg}, nil
}

// readerAdapter wraps *kafka.Reader to satisfy MessageReader. Kept
// unexported — callers reach for kafka-go's Reader directly via New(),
// or stub the MessageReader interface for tests via NewWithReader().
type readerAdapter struct {
	r *kafkago.Reader
}

func (a *readerAdapter) ReadMessage(ctx context.Context) (Message, error) {
	m, err := a.r.ReadMessage(ctx)
	if err != nil {
		return Message{}, err
	}
	headers := make([]Header, 0, len(m.Headers))
	for _, h := range m.Headers {
		headers = append(headers, Header{Key: h.Key, Value: h.Value})
	}
	return Message{
		Topic:     m.Topic,
		Partition: m.Partition,
		Offset:    m.Offset,
		Key:       m.Key,
		Value:     m.Value,
		Time:      m.Time,
		Headers:   headers,
	}, nil
}

func (a *readerAdapter) Close() error { return a.r.Close() }

// ReadBatch consumes up to max messages from the underlying reader,
// stopping early when ctx is cancelled or its deadline expires.
//
// Semantics:
//   - max <= 0 falls back to Config.BatchSize (or DefaultBatchSize).
//   - max > MaxBatchSize is clamped to MaxBatchSize.
//   - rows: decoded messages, possibly empty when ctx fires before any
//     message arrives.
//   - hasMore: true when the batch was filled to the (clamped) max;
//     false on context cancel/deadline so callers know to back off.
//   - err: surfaced from the reader's ReadMessage when the cause is
//     NOT a context cancel/deadline; partial rows accumulated before
//     the error are returned alongside the error so callers don't lose
//     work. Context errors are absorbed and the partial batch is
//     returned cleanly with hasMore=false.
//
// With auto-commit enabled (Config.CommitInterval > 0 or default),
// offsets for the messages returned here are flushed asynchronously by
// the underlying kafka.Reader on its own ticker — callers do NOT need
// to commit explicitly.
func (c *Connector) ReadBatch(ctx context.Context, max int) ([]map[string]any, bool, error) {
	limit := max
	if limit <= 0 {
		limit = c.cfg.effectiveBatchSize()
	} else if limit > MaxBatchSize {
		limit = MaxBatchSize
	}
	rows := make([]map[string]any, 0, limit)
	for len(rows) < limit {
		m, err := c.reader.ReadMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return rows, false, nil
			}
			return rows, false, fmt.Errorf("kafka: read message: %w", err)
		}
		row, err := c.decodeMessage(m)
		if err != nil {
			return rows, false, err
		}
		rows = append(rows, row)
	}
	return rows, true, nil
}

// decodeMessage projects one Kafka Message into the row shape returned
// by ReadBatch.
func (c *Connector) decodeMessage(m Message) (map[string]any, error) {
	value, err := decodeValue(m.Value, c.cfg.ValueFormat, m.Offset)
	if err != nil {
		return nil, err
	}
	row := map[string]any{
		"topic":     m.Topic,
		"partition": m.Partition,
		"offset":    m.Offset,
		"timestamp": m.Time,
		"key":       m.Key,
		"value":     value,
	}
	if len(m.Headers) > 0 {
		hdrs := make(map[string]any, len(m.Headers))
		for _, h := range m.Headers {
			hdrs[h.Key] = h.Value
		}
		row["headers"] = hdrs
	}
	return row, nil
}

// decodeValue applies the configured ValueFormat to raw bytes. Empty
// bytes always collapse to nil so downstream consumers see a single
// "absent" sentinel regardless of format.
func decodeValue(raw []byte, format ValueFormat, offset int64) (any, error) {
	switch format {
	case ValueFormatJSON:
		if len(raw) == 0 {
			return nil, nil
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("kafka: decode JSON value at offset %d: %w", offset, err)
		}
		return v, nil
	case ValueFormatString:
		return string(raw), nil
	default:
		return raw, nil
	}
}

// Close releases the underlying reader. Safe to call once; the
// kafka-go Reader returns an error on double-close.
func (c *Connector) Close() error { return c.reader.Close() }
