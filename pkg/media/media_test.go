package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s := NewStore(dir)
	// Pin time so the yyyy/mm partition is deterministic across runs.
	s.SetNowFunc(func() time.Time {
		return time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	})
	return s
}

func expectedSHA(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func TestPut_StoresContentAddressably(t *testing.T) {
	s := newTestStore(t)
	content := []byte("hello weave")
	asset, err := s.Put(context.Background(), PutOptions{Realm: "main", Filename: "hello.txt", MIME: "text/plain"}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	want := expectedSHA(content)
	if asset.SHA256 != want {
		t.Fatalf("sha256 mismatch: got %q want %q", asset.SHA256, want)
	}
	if asset.SizeBytes != int64(len(content)) {
		t.Fatalf("size: got %d want %d", asset.SizeBytes, len(content))
	}
	// Path shape: data/media/{realm}/{yyyy}/{mm}/{sha256} — the store root
	// stands in for data/media, so the relative path is realm/yyyy/mm/sha.
	wantPath := filepath.ToSlash(filepath.Join("main", "2026", "04", want))
	if asset.Path != wantPath {
		t.Fatalf("path: got %q want %q", asset.Path, wantPath)
	}
	abs := filepath.Join(s.Root(), filepath.FromSlash(asset.Path))
	got, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("content mismatch")
	}
}

func TestPut_DedupesSameBytes(t *testing.T) {
	s := newTestStore(t)
	content := []byte("repeat me")

	a1, err := s.Put(context.Background(), PutOptions{}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put 1: %v", err)
	}
	a2, err := s.Put(context.Background(), PutOptions{}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put 2: %v", err)
	}
	if a1.RID == a2.RID {
		t.Fatalf("expected distinct RIDs, got %q twice", a1.RID)
	}
	if a1.Path != a2.Path {
		t.Fatalf("expected shared path for dedup, got %q vs %q", a1.Path, a2.Path)
	}
	if a1.SHA256 != a2.SHA256 {
		t.Fatalf("sha256 mismatch across duplicate puts")
	}

	// Exactly one physical file on disk within the date partition.
	partition := filepath.Join(s.Root(), "main", "2026", "04")
	entries, err := os.ReadDir(partition)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 content file after dedup, got %d", len(entries))
	}
}

func TestPut_DifferentBytesProduceDistinctPaths(t *testing.T) {
	s := newTestStore(t)
	a1, err := s.Put(context.Background(), PutOptions{}, bytes.NewReader([]byte("alpha")))
	if err != nil {
		t.Fatal(err)
	}
	a2, err := s.Put(context.Background(), PutOptions{}, bytes.NewReader([]byte("beta")))
	if err != nil {
		t.Fatal(err)
	}
	if a1.Path == a2.Path {
		t.Fatalf("distinct content should produce distinct paths, got %q twice", a1.Path)
	}
	if a1.SHA256 == a2.SHA256 {
		t.Fatalf("distinct content should produce distinct sha256")
	}
}

func TestPut_RealmIsolation(t *testing.T) {
	s := newTestStore(t)
	content := []byte("hello")
	a1, err := s.Put(context.Background(), PutOptions{Realm: "tenant-a"}, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	a2, err := s.Put(context.Background(), PutOptions{Realm: "tenant-b"}, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if a1.SHA256 != a2.SHA256 {
		t.Fatal("sha256 should match across realms for identical bytes")
	}
	if a1.Path == a2.Path {
		t.Fatalf("realms must not share paths, got %q twice", a1.Path)
	}
	if !strings.HasPrefix(a1.Path, "tenant-a/") {
		t.Fatalf("tenant-a path prefix wrong: %q", a1.Path)
	}
	if !strings.HasPrefix(a2.Path, "tenant-b/") {
		t.Fatalf("tenant-b path prefix wrong: %q", a2.Path)
	}
}

func TestPut_EmptyRealmDefaults(t *testing.T) {
	s := newTestStore(t)
	a, err := s.Put(context.Background(), PutOptions{}, bytes.NewReader([]byte("x")))
	if err != nil {
		t.Fatal(err)
	}
	if a.Realm != "main" {
		t.Fatalf("empty realm should default to 'main', got %q", a.Realm)
	}
	if !strings.HasPrefix(a.Path, "main/") {
		t.Fatalf("path should live under main/, got %q", a.Path)
	}
}

func TestPut_InvalidRealm(t *testing.T) {
	s := newTestStore(t)
	bad := []string{"../evil", "a/b", "..", "a\\b"}
	for _, realm := range bad {
		_, err := s.Put(context.Background(), PutOptions{Realm: realm}, bytes.NewReader([]byte("x")))
		if !errors.Is(err, ErrInvalidRealm) {
			t.Fatalf("realm %q: expected ErrInvalidRealm, got %v", realm, err)
		}
	}
}

func TestPut_DefaultMIME(t *testing.T) {
	s := newTestStore(t)
	a, err := s.Put(context.Background(), PutOptions{}, bytes.NewReader([]byte("x")))
	if err != nil {
		t.Fatal(err)
	}
	if a.MIME != "application/octet-stream" {
		t.Fatalf("expected default mime, got %q", a.MIME)
	}
}

func TestPut_YearMonthPartition(t *testing.T) {
	s := newTestStore(t)
	s.SetNowFunc(func() time.Time { return time.Date(2027, 1, 9, 0, 0, 0, 0, time.UTC) })
	a, err := s.Put(context.Background(), PutOptions{Realm: "r"}, bytes.NewReader([]byte("y")))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(a.Path, "/")
	if len(parts) != 4 || parts[0] != "r" || parts[1] != "2027" || parts[2] != "01" {
		t.Fatalf("unexpected partition layout: %q", a.Path)
	}
}

func TestOpenAndDelete(t *testing.T) {
	s := newTestStore(t)
	content := []byte("payload")
	a, err := s.Put(context.Background(), PutOptions{}, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}

	rc, err := s.Open(context.Background(), a.Path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("Open content mismatch")
	}

	if err := s.DeletePath(context.Background(), a.Path); err != nil {
		t.Fatalf("DeletePath: %v", err)
	}
	exists, err := s.Exists(a.Path)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("file should be gone after DeletePath")
	}
	if _, err := s.Open(context.Background(), a.Path); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	if err := s.DeletePath(context.Background(), a.Path); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting twice, got %v", err)
	}
}

func TestOpen_PathTraversalRejected(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Open(context.Background(), "../../etc/passwd")
	if err == nil {
		t.Fatal("expected path-traversal to error")
	}
	_, err = s.Open(context.Background(), "/etc/passwd")
	if err == nil {
		t.Fatal("expected absolute path to error")
	}
}

func TestNewMediaRID_HasPrefix(t *testing.T) {
	rid := NewMediaRID()
	if !strings.HasPrefix(rid, MediaRIDPrefix) {
		t.Fatalf("expected %q prefix, got %q", MediaRIDPrefix, rid)
	}
	if rid == NewMediaRID() {
		t.Fatal("RIDs must be unique")
	}
}

func TestPut_ConcurrentDedup(t *testing.T) {
	s := newTestStore(t)
	content := []byte("same content everywhere")
	const n = 16
	assets := make([]*Asset, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			a, err := s.Put(context.Background(), PutOptions{}, bytes.NewReader(content))
			assets[i] = a
			errs[i] = err
		}(i)
	}
	wg.Wait()

	seen := map[string]struct{}{}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
		seen[assets[i].RID] = struct{}{}
		if assets[i].Path != assets[0].Path {
			t.Fatalf("Put %d: path drift under concurrent dedup", i)
		}
	}
	if len(seen) != n {
		t.Fatalf("expected %d distinct RIDs, got %d", n, len(seen))
	}
	partition := filepath.Join(s.Root(), "main", "2026", "04")
	entries, _ := os.ReadDir(partition)
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 physical file, got %d", len(entries))
	}
}

func TestPut_EmptyBytes(t *testing.T) {
	s := newTestStore(t)
	a, err := s.Put(context.Background(), PutOptions{}, bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	if a.SizeBytes != 0 {
		t.Fatalf("empty upload should report size 0, got %d", a.SizeBytes)
	}
	if a.SHA256 != expectedSHA(nil) {
		t.Fatalf("sha256 mismatch for empty bytes")
	}
}
