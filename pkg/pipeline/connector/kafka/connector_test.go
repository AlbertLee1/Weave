package kafka

import (
	"context"
	"errors"
	"testing"
	"time"
)

// stubReader is a hand-rolled MessageReader that returns canned messages
// (and optionally injected errors) so the connector can be exercised
// without standing up a real Kafka broker.
type stubReader struct {
	messages []Message
	errs     []error
	idx      int
	closed   bool
	closeErr error
	// blockOnDrain, when true, makes ReadMessage block on ctx.Done()
	// after the canned messages are exhausted — simulates a real reader
	// waiting for new traffic.
	blockOnDrain bool
}

func (s *stubReader) ReadMessage(ctx context.Context) (Message, error) {
	if s.idx < len(s.errs) && s.errs[s.idx] != nil {
		err := s.errs[s.idx]
		s.idx++
		return Message{}, err
	}
	if s.idx >= len(s.messages) {
		if s.blockOnDrain {
			<-ctx.Done()
			return Message{}, ctx.Err()
		}
		return Message{}, errors.New("stub: no more messages")
	}
	m := s.messages[s.idx]
	s.idx++
	return m, nil
}

func (s *stubReader) Close() error {
	s.closed = true
	return s.closeErr
}

func validConfig() Config {
	return Config{
		Brokers: []string{"localhost:9092"},
		Topic:   "events",
		GroupID: "weave-test",
	}
}

func TestConfig_Validate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "ok", mutate: func(*Config) {}},
		{name: "missing brokers", mutate: func(c *Config) { c.Brokers = nil }, wantErr: "Brokers"},
		{name: "blank broker", mutate: func(c *Config) { c.Brokers = []string{"  "} }, wantErr: "Brokers[0]"},
		{name: "missing topic", mutate: func(c *Config) { c.Topic = "" }, wantErr: "Topic"},
		{name: "missing group", mutate: func(c *Config) { c.GroupID = "" }, wantErr: "GroupID"},
		{name: "negative MinBytes", mutate: func(c *Config) { c.MinBytes = -1 }, wantErr: "MinBytes"},
		{name: "negative MaxBytes", mutate: func(c *Config) { c.MaxBytes = -1 }, wantErr: "MaxBytes"},
		{name: "negative MaxWait", mutate: func(c *Config) { c.MaxWait = -1 }, wantErr: "MaxWait"},
		{name: "negative CommitInterval", mutate: func(c *Config) { c.CommitInterval = -1 }, wantErr: "CommitInterval"},
		{name: "negative BatchSize", mutate: func(c *Config) { c.BatchSize = -1 }, wantErr: "BatchSize"},
		{name: "min > max bytes", mutate: func(c *Config) { c.MinBytes = 10; c.MaxBytes = 5 }, wantErr: "MinBytes"},
		{name: "bad value format", mutate: func(c *Config) { c.ValueFormat = "yaml" }, wantErr: "ValueFormat"},
		{name: "json format ok", mutate: func(c *Config) { c.ValueFormat = ValueFormatJSON }},
		{name: "string format ok", mutate: func(c *Config) { c.ValueFormat = ValueFormatString }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			tc.mutate(&cfg)
			err := cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error mentioning %q, got nil", tc.wantErr)
			}
			if !contains(err.Error(), tc.wantErr) {
				t.Fatalf("err %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestNew_RejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	if _, err := New(Config{}); err == nil {
		t.Fatal("want error from New on empty config")
	}
}

func TestNewWithReader_RejectsNil(t *testing.T) {
	t.Parallel()
	if _, err := NewWithReader(nil, validConfig()); err == nil {
		t.Fatal("want error when reader is nil")
	}
	if _, err := NewWithReader(&stubReader{}, Config{}); err == nil {
		t.Fatal("want error when config is invalid")
	}
}

func TestReadBatch_RawValue(t *testing.T) {
	t.Parallel()
	r := &stubReader{messages: []Message{
		{Topic: "events", Partition: 0, Offset: 1, Key: []byte("k1"), Value: []byte("hello"), Time: time.Unix(100, 0)},
		{Topic: "events", Partition: 0, Offset: 2, Key: []byte("k2"), Value: []byte("world"), Time: time.Unix(101, 0)},
	}, blockOnDrain: true}
	c, err := NewWithReader(r, validConfig())
	if err != nil {
		t.Fatalf("NewWithReader: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	rows, hasMore, err := c.ReadBatch(ctx, 10)
	if err != nil {
		t.Fatalf("ReadBatch: %v", err)
	}
	if hasMore {
		t.Fatalf("expected hasMore=false (exhausted before max)")
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	row := rows[0]
	if got := row["topic"]; got != "events" {
		t.Errorf("topic=%v", got)
	}
	if got := row["offset"]; got != int64(1) {
		t.Errorf("offset=%v (%T)", got, got)
	}
	if v, ok := row["value"].([]byte); !ok || string(v) != "hello" {
		t.Errorf("value=%v (%T)", row["value"], row["value"])
	}
	if k, ok := row["key"].([]byte); !ok || string(k) != "k1" {
		t.Errorf("key=%v (%T)", row["key"], row["key"])
	}
	if _, ok := row["timestamp"].(time.Time); !ok {
		t.Errorf("timestamp type=%T", row["timestamp"])
	}
}

func TestReadBatch_JSONValue(t *testing.T) {
	t.Parallel()
	r := &stubReader{messages: []Message{
		{Offset: 1, Value: []byte(`{"id":42,"name":"alice"}`)},
		{Offset: 2, Value: []byte(`null`)},
		{Offset: 3, Value: nil},
	}, blockOnDrain: true}
	cfg := validConfig()
	cfg.ValueFormat = ValueFormatJSON
	c, _ := NewWithReader(r, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	rows, _, err := c.ReadBatch(ctx, 5)
	if err != nil {
		t.Fatalf("ReadBatch: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	val, ok := rows[0]["value"].(map[string]any)
	if !ok {
		t.Fatalf("row[0].value not a map: %T", rows[0]["value"])
	}
	if val["name"] != "alice" {
		t.Errorf("name=%v", val["name"])
	}
	if rows[1]["value"] != nil {
		t.Errorf("row[1].value want nil (json null), got %v", rows[1]["value"])
	}
	if rows[2]["value"] != nil {
		t.Errorf("row[2].value want nil (empty bytes), got %v", rows[2]["value"])
	}
}

func TestReadBatch_StringValue(t *testing.T) {
	t.Parallel()
	r := &stubReader{messages: []Message{
		{Offset: 1, Value: []byte("hello")},
	}, blockOnDrain: true}
	cfg := validConfig()
	cfg.ValueFormat = ValueFormatString
	c, _ := NewWithReader(r, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	rows, _, _ := c.ReadBatch(ctx, 1)
	if rows[0]["value"] != "hello" {
		t.Fatalf("value=%v", rows[0]["value"])
	}
}

func TestReadBatch_FillsToMax_HasMoreTrue(t *testing.T) {
	t.Parallel()
	msgs := []Message{
		{Offset: 1, Value: []byte("a")},
		{Offset: 2, Value: []byte("b")},
		{Offset: 3, Value: []byte("c")},
	}
	r := &stubReader{messages: msgs, blockOnDrain: true}
	c, _ := NewWithReader(r, validConfig())
	rows, hasMore, err := c.ReadBatch(context.Background(), 2)
	if err != nil {
		t.Fatalf("ReadBatch: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	if !hasMore {
		t.Fatalf("want hasMore=true (filled to max)")
	}
}

func TestReadBatch_DeadlineExceeded_ReturnsPartial(t *testing.T) {
	t.Parallel()
	r := &stubReader{messages: []Message{
		{Offset: 1, Value: []byte("a")},
	}, blockOnDrain: true}
	c, _ := NewWithReader(r, validConfig())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	rows, hasMore, err := c.ReadBatch(ctx, 100)
	if err != nil {
		t.Fatalf("want nil error on deadline (got partial), got %v", err)
	}
	if hasMore {
		t.Fatalf("hasMore should be false after deadline")
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row partial, got %d", len(rows))
	}
}

func TestReadBatch_ReaderError_Propagates(t *testing.T) {
	t.Parallel()
	boom := errors.New("broker down")
	r := &stubReader{
		messages: []Message{{Offset: 1, Value: []byte("a")}},
		errs:     []error{nil, boom},
	}
	c, _ := NewWithReader(r, validConfig())
	rows, hasMore, err := c.ReadBatch(context.Background(), 5)
	if err == nil {
		t.Fatalf("want error from underlying reader")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err %v does not wrap %v", err, boom)
	}
	if hasMore {
		t.Fatalf("hasMore should be false on error")
	}
	if len(rows) != 1 {
		t.Fatalf("want partial 1 row before error, got %d", len(rows))
	}
}

func TestReadBatch_JSONDecodeError(t *testing.T) {
	t.Parallel()
	r := &stubReader{messages: []Message{
		{Offset: 9, Value: []byte("not-json")},
	}, blockOnDrain: true}
	cfg := validConfig()
	cfg.ValueFormat = ValueFormatJSON
	c, _ := NewWithReader(r, cfg)
	_, _, err := c.ReadBatch(context.Background(), 5)
	if err == nil {
		t.Fatalf("want decode error")
	}
	if !contains(err.Error(), "offset 9") {
		t.Fatalf("err %q should mention offset 9", err)
	}
}

func TestReadBatch_DefaultBatchSize(t *testing.T) {
	t.Parallel()
	// Provide one message so we can verify max<=0 still reads at least
	// one message and exits via blockOnDrain.
	r := &stubReader{messages: []Message{{Offset: 1, Value: []byte("a")}}, blockOnDrain: true}
	cfg := validConfig()
	cfg.BatchSize = 5
	c, _ := NewWithReader(r, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	rows, _, err := c.ReadBatch(ctx, 0)
	if err != nil {
		t.Fatalf("ReadBatch: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
}

func TestReadBatch_HeadersExposed(t *testing.T) {
	t.Parallel()
	r := &stubReader{messages: []Message{
		{Offset: 1, Value: []byte("v"), Headers: []Header{
			{Key: "trace-id", Value: []byte("abc")},
			{Key: "x-source", Value: []byte("web")},
		}},
	}, blockOnDrain: true}
	c, _ := NewWithReader(r, validConfig())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	rows, _, _ := c.ReadBatch(ctx, 1)
	hdrs, ok := rows[0]["headers"].(map[string]any)
	if !ok {
		t.Fatalf("headers type %T", rows[0]["headers"])
	}
	if v, _ := hdrs["trace-id"].([]byte); string(v) != "abc" {
		t.Errorf("trace-id=%v", hdrs["trace-id"])
	}
	if v, _ := hdrs["x-source"].([]byte); string(v) != "web" {
		t.Errorf("x-source=%v", hdrs["x-source"])
	}
}

func TestReadBatch_NoHeadersOmitsKey(t *testing.T) {
	t.Parallel()
	r := &stubReader{messages: []Message{{Offset: 1, Value: []byte("v")}}, blockOnDrain: true}
	c, _ := NewWithReader(r, validConfig())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	rows, _, _ := c.ReadBatch(ctx, 1)
	if _, ok := rows[0]["headers"]; ok {
		t.Fatalf("headers key should be absent when there are none")
	}
}

func TestClose_DelegatesToReader(t *testing.T) {
	t.Parallel()
	r := &stubReader{}
	c, _ := NewWithReader(r, validConfig())
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !r.closed {
		t.Fatalf("underlying reader was not closed")
	}
}

func TestMaxBatchSizeClamp(t *testing.T) {
	t.Parallel()
	r := &stubReader{messages: []Message{{Offset: 1, Value: []byte("a")}}, blockOnDrain: true}
	c, _ := NewWithReader(r, validConfig())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	// max well above MaxBatchSize — should not blow memory; we only
	// have one message followed by a deadline-block, so partial=1.
	rows, hasMore, err := c.ReadBatch(ctx, MaxBatchSize+1)
	if err != nil {
		t.Fatalf("ReadBatch: %v", err)
	}
	if hasMore {
		t.Fatalf("hasMore=true unexpected")
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
}

func contains(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
