package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/parquet-go/parquet-go"
)

// memoryClient is an in-memory ObjectClient used by every test in this
// package. It satisfies the interface with no network or filesystem
// dependency and hands out objects in deterministic key order so
// pagination tests can assert exact sequences.
type memoryClient struct {
	objects   map[string][]byte
	pageSize  int    // override page size; 0 ⇒ no override
	listError error  // non-nil ⇒ ListObjects returns this
	getError  error  // non-nil ⇒ GetObject returns this
	getOnly   string // when non-empty, GetObject only succeeds for keys equal to this
}

func newMemoryClient(objects map[string][]byte) *memoryClient {
	return &memoryClient{objects: objects, pageSize: 2}
}

func (m *memoryClient) ListObjects(ctx context.Context, prefix, token string) ([]ObjectInfo, string, error) {
	if m.listError != nil {
		return nil, "", m.listError
	}
	keys := make([]string, 0, len(m.objects))
	for k := range m.objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	start := 0
	if token != "" {
		// token is "key=<lastSeen>" — return everything strictly greater.
		last := strings.TrimPrefix(token, "after:")
		idx := sort.SearchStrings(keys, last)
		// Advance past last (key already returned).
		for idx < len(keys) && keys[idx] <= last {
			idx++
		}
		start = idx
	}
	end := start + m.pageSize
	if end > len(keys) {
		end = len(keys)
	}
	out := make([]ObjectInfo, 0, end-start)
	for _, k := range keys[start:end] {
		out = append(out, ObjectInfo{Key: k, Size: int64(len(m.objects[k]))})
	}
	next := ""
	if end < len(keys) && len(out) > 0 {
		next = "after:" + out[len(out)-1].Key
	}
	return out, next, nil
}

func (m *memoryClient) GetObject(ctx context.Context, key string) ([]byte, error) {
	if m.getError != nil {
		return nil, m.getError
	}
	if m.getOnly != "" && key != m.getOnly {
		return nil, fmt.Errorf("getOnly: %q != %q", key, m.getOnly)
	}
	data, ok := m.objects[key]
	if !ok {
		return nil, fmt.Errorf("memoryClient: missing key %q", key)
	}
	return data, nil
}

// drainAll loops ReadPage until !hasMore and returns the flattened row
// slice plus the call count. Useful for end-to-end pagination tests.
func drainAll(t *testing.T, c *Connector) ([]map[string]any, int) {
	t.Helper()
	all := []map[string]any{}
	calls := 0
	cursor := ""
	for {
		rows, next, more, err := c.ReadPage(context.Background(), cursor)
		if err != nil {
			t.Fatalf("ReadPage[%d]: %v", calls, err)
		}
		calls++
		all = append(all, rows...)
		if !more {
			break
		}
		cursor = next
		if calls > 100 {
			t.Fatalf("drainAll: cursor pagination did not terminate after 100 calls")
		}
	}
	return all, calls
}

func TestConfig_Validate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"empty", Config{}, true},
		{"missing-bucket", Config{Format: FormatCSV}, true},
		{"missing-format", Config{Bucket: "b"}, true},
		{"unknown-format", Config{Bucket: "b", Format: "yaml"}, true},
		{"good-csv", Config{Bucket: "b", Format: FormatCSV}, false},
		{"good-parquet", Config{Bucket: "b", Format: FormatParquet, PageSize: 100}, false},
		{"negative-pagesize", Config{Bucket: "b", Format: FormatCSV, PageSize: -1}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestConfig_DefaultPageSize(t *testing.T) {
	cfg := Config{Bucket: "b", Format: FormatCSV}
	if cfg.effectivePageSize() != DefaultPageSize {
		t.Errorf("default page size = %d want %d", cfg.effectivePageSize(), DefaultPageSize)
	}
	cfg.PageSize = MaxPageSize + 100
	if got := cfg.effectivePageSize(); got != MaxPageSize {
		t.Errorf("oversized page size = %d want clamp to %d", got, MaxPageSize)
	}
}

func TestNew_RequiresClient(t *testing.T) {
	if _, err := New(nil, Config{Bucket: "b", Format: FormatCSV}); err == nil {
		t.Fatal("nil client should be rejected")
	}
}

func TestNew_RequiresValidConfig(t *testing.T) {
	client := newMemoryClient(nil)
	if _, err := New(client, Config{}); err == nil {
		t.Fatal("empty config should be rejected")
	}
}

func TestReadPage_CSV_SingleObject(t *testing.T) {
	client := newMemoryClient(map[string][]byte{
		"events/01.csv": []byte("id,name\n1,alice\n2,bob\n"),
	})
	c, err := New(client, Config{Bucket: "b", Prefix: "events/", Format: FormatCSV})
	if err != nil {
		t.Fatal(err)
	}
	rows, next, more, err := c.ReadPage(context.Background(), "")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%d want 2", len(rows))
	}
	if rows[0]["id"] != "1" || rows[0]["name"] != "alice" {
		t.Errorf("rows[0]=%v", rows[0])
	}
	if more {
		t.Errorf("single object ⇒ more should be false; got next=%q", next)
	}
	if next != "" {
		t.Errorf("next=%q want empty", next)
	}
}

func TestReadPage_CSV_NoHeader(t *testing.T) {
	client := newMemoryClient(map[string][]byte{
		"a.csv": []byte("1,alice\n2,bob\n"),
	})
	c, err := New(client, Config{
		Bucket: "b", Format: FormatCSV,
		CSV: CSVOptions{NoHeader: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, _, _, err := c.ReadPage(context.Background(), "")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%d want 2", len(rows))
	}
	if rows[0]["col_0"] != "1" || rows[0]["col_1"] != "alice" {
		t.Errorf("rows[0]=%v", rows[0])
	}
}

func TestReadPage_CSV_Pagination(t *testing.T) {
	client := newMemoryClient(map[string][]byte{
		"x/01.csv": []byte("id\n1\n"),
		"x/02.csv": []byte("id\n2\n"),
		"x/03.csv": []byte("id\n3\n"),
		"x/04.csv": []byte("id\n4\n"),
		"x/05.csv": []byte("id\n5\n"),
	})
	client.pageSize = 2
	c, err := New(client, Config{Bucket: "b", Prefix: "x/", Format: FormatCSV})
	if err != nil {
		t.Fatal(err)
	}
	all, calls := drainAll(t, c)
	if len(all) != 5 {
		t.Fatalf("rows=%d want 5", len(all))
	}
	// All five files were 1 row each ⇒ one ReadPage per file = 5 calls.
	if calls != 5 {
		t.Errorf("calls=%d want 5", calls)
	}
	got := []string{}
	for _, r := range all {
		got = append(got, fmt.Sprintf("%v", r["id"]))
	}
	want := []string{"1", "2", "3", "4", "5"}
	if !equalStrings(got, want) {
		t.Errorf("got=%v want=%v", got, want)
	}
}

func TestReadPage_CSV_PrefixFilter(t *testing.T) {
	client := newMemoryClient(map[string][]byte{
		"a/keep.csv":  []byte("id\n1\n"),
		"b/skip.csv":  []byte("id\n2\n"),
		"a/keep2.csv": []byte("id\n3\n"),
	})
	c, err := New(client, Config{Bucket: "b", Prefix: "a/", Format: FormatCSV})
	if err != nil {
		t.Fatal(err)
	}
	all, _ := drainAll(t, c)
	if len(all) != 2 {
		t.Fatalf("rows=%d want 2 (prefix should exclude b/)", len(all))
	}
	for _, r := range all {
		v := fmt.Sprintf("%v", r["id"])
		if v == "2" {
			t.Errorf("row from b/ leaked: %v", r)
		}
	}
}

func TestReadPage_CSV_DirectoryMarkerSkipped(t *testing.T) {
	client := newMemoryClient(map[string][]byte{
		"data/":     []byte{}, // MinIO-style directory marker
		"data/a.csv": []byte("id\n1\n"),
	})
	c, err := New(client, Config{Bucket: "b", Prefix: "data/", Format: FormatCSV})
	if err != nil {
		t.Fatal(err)
	}
	all, _ := drainAll(t, c)
	if len(all) != 1 {
		t.Fatalf("rows=%d want 1 (marker should be skipped)", len(all))
	}
}

func TestReadPage_CSV_EmptyPrefix(t *testing.T) {
	client := newMemoryClient(map[string][]byte{})
	c, err := New(client, Config{Bucket: "b", Prefix: "missing/", Format: FormatCSV})
	if err != nil {
		t.Fatal(err)
	}
	rows, next, more, err := c.ReadPage(context.Background(), "")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if len(rows) != 0 || more || next != "" {
		t.Errorf("empty prefix should return empty/false/empty: rows=%d more=%v next=%q", len(rows), more, next)
	}
}

func TestReadPage_PropagatesListError(t *testing.T) {
	client := newMemoryClient(map[string][]byte{"a": []byte("id\n1\n")})
	client.listError = errors.New("simulated list failure")
	c, _ := New(client, Config{Bucket: "b", Format: FormatCSV})
	if _, _, _, err := c.ReadPage(context.Background(), ""); err == nil {
		t.Fatal("expected list error to surface")
	}
}

func TestReadPage_PropagatesGetError(t *testing.T) {
	client := newMemoryClient(map[string][]byte{"a.csv": []byte("id\n1\n")})
	client.getError = errors.New("simulated get failure")
	c, _ := New(client, Config{Bucket: "b", Format: FormatCSV})
	if _, _, _, err := c.ReadPage(context.Background(), ""); err == nil {
		t.Fatal("expected get error to surface")
	}
}

func TestReadPage_RejectsBadCursor(t *testing.T) {
	client := newMemoryClient(map[string][]byte{"a.csv": []byte("id\n1\n")})
	c, _ := New(client, Config{Bucket: "b", Format: FormatCSV})
	if _, _, _, err := c.ReadPage(context.Background(), "not-base64!@#$"); err == nil {
		t.Fatal("malformed cursor should fail")
	}
}

func TestReadPage_DecodeError(t *testing.T) {
	// CSV with mismatched fields per row; default FieldsPerRecord=0
	// requires uniform width.
	client := newMemoryClient(map[string][]byte{
		"bad.csv": []byte("id,name\n1\n"),
	})
	c, _ := New(client, Config{Bucket: "b", Format: FormatCSV})
	if _, _, _, err := c.ReadPage(context.Background(), ""); err == nil {
		t.Fatal("expected decode error for ragged CSV")
	}
}

// --- Parquet ---

type parquetTestRow struct {
	ID    int64   `parquet:"id"`
	Name  string  `parquet:"name"`
	Score float64 `parquet:"score"`
	Flag  bool    `parquet:"flag"`
}

func writeParquet(t *testing.T, rows []parquetTestRow) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := parquet.NewGenericWriter[parquetTestRow](&buf)
	if _, err := w.Write(rows); err != nil {
		t.Fatalf("parquet write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("parquet close: %v", err)
	}
	return buf.Bytes()
}

func TestReadPage_Parquet_SingleObject(t *testing.T) {
	data := writeParquet(t, []parquetTestRow{
		{ID: 1, Name: "alice", Score: 1.5, Flag: true},
		{ID: 2, Name: "bob", Score: 2.5, Flag: false},
	})
	client := newMemoryClient(map[string][]byte{"x.parquet": data})
	c, err := New(client, Config{Bucket: "b", Format: FormatParquet})
	if err != nil {
		t.Fatal(err)
	}
	rows, _, more, err := c.ReadPage(context.Background(), "")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%d want 2", len(rows))
	}
	if more {
		t.Errorf("single object ⇒ more=false")
	}
	if rows[0]["id"] != int64(1) {
		t.Errorf("row[0].id = %v (%T) want int64(1)", rows[0]["id"], rows[0]["id"])
	}
	if rows[0]["name"] != "alice" {
		t.Errorf("row[0].name = %v want alice", rows[0]["name"])
	}
	if rows[0]["score"] != 1.5 {
		t.Errorf("row[0].score = %v want 1.5", rows[0]["score"])
	}
	if rows[0]["flag"] != true {
		t.Errorf("row[0].flag = %v want true", rows[0]["flag"])
	}
}

func TestReadPage_Parquet_Pagination(t *testing.T) {
	client := newMemoryClient(map[string][]byte{
		"p/01.parquet": writeParquet(t, []parquetTestRow{{ID: 1, Name: "a"}}),
		"p/02.parquet": writeParquet(t, []parquetTestRow{{ID: 2, Name: "b"}}),
		"p/03.parquet": writeParquet(t, []parquetTestRow{{ID: 3, Name: "c"}}),
	})
	c, err := New(client, Config{Bucket: "b", Prefix: "p/", Format: FormatParquet})
	if err != nil {
		t.Fatal(err)
	}
	all, calls := drainAll(t, c)
	if len(all) != 3 {
		t.Fatalf("rows=%d want 3", len(all))
	}
	if calls != 3 {
		t.Errorf("calls=%d want 3", calls)
	}
}

func TestReadPage_Parquet_EmptyData(t *testing.T) {
	client := newMemoryClient(map[string][]byte{"empty.parquet": []byte{}})
	c, err := New(client, Config{Bucket: "b", Format: FormatParquet})
	if err != nil {
		t.Fatal(err)
	}
	rows, _, _, err := c.ReadPage(context.Background(), "")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("empty parquet ⇒ 0 rows; got %d", len(rows))
	}
}

func TestReadPage_UnsupportedFormatDispatch(t *testing.T) {
	// Validate rejects unknown formats at construction; verify the
	// decode dispatch also errors defensively if a Connector is built
	// past Validate (e.g. via direct struct literal).
	c := &Connector{client: newMemoryClient(map[string][]byte{"a": []byte("hi")}), cfg: Config{Bucket: "b", Format: "yaml"}}
	if _, _, _, err := c.ReadPage(context.Background(), ""); err == nil {
		t.Fatal("dispatch should reject unknown format")
	}
}

// --- Cursor encoding round-trip (regression for opaque cursor handling) ---

func TestCursorRoundTrip(t *testing.T) {
	cases := []cursorState{
		{},
		{Queue: []string{"a", "b"}},
		{ListToken: "tkn"},
		{Queue: []string{"a"}, ListToken: "tkn"},
	}
	for _, s := range cases {
		enc := encodeCursor(s)
		got, err := decodeCursor(enc)
		if err != nil {
			t.Fatalf("decode(%q): %v", enc, err)
		}
		if !equalStrings(got.Queue, s.Queue) || got.ListToken != s.ListToken {
			t.Errorf("round-trip mismatch: got=%+v want=%+v", got, s)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
