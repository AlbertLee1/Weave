package attachment

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func mustPut(t *testing.T, store BlobStore, filename, mediaType, body string) *Attachment {
	t.Helper()
	att, err := store.Put(context.Background(), BlobMeta{Filename: filename, MediaType: mediaType}, strings.NewReader(body))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	return att
}

func readAll(t *testing.T, rc io.ReadCloser) string {
	t.Helper()
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(b)
}

func TestLocalStore_PutGetStatDelete(t *testing.T) {
	store := NewLocalStore(t.TempDir())

	att := mustPut(t, store, "hello.txt", "text/plain", "hello world")
	if att.RID == "" {
		t.Fatal("expected non-empty RID")
	}
	if !strings.HasPrefix(att.RID, "ri.attachments.main.attachment.") {
		t.Errorf("RID format unexpected: %s", att.RID)
	}
	if att.Filename != "hello.txt" {
		t.Errorf("filename = %s", att.Filename)
	}
	if att.MediaType != "text/plain" {
		t.Errorf("mediaType = %s", att.MediaType)
	}
	if att.SizeBytes != int64(len("hello world")) {
		t.Errorf("sizeBytes = %d", att.SizeBytes)
	}

	// Stat
	got, err := store.Stat(context.Background(), att.RID)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got.Filename != "hello.txt" || got.SizeBytes != int64(len("hello world")) {
		t.Errorf("Stat returned %+v", got)
	}

	// Get
	rc, err := store.Get(context.Background(), att.RID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if body := readAll(t, rc); body != "hello world" {
		t.Errorf("body = %q", body)
	}

	// Delete
	if err := store.Delete(context.Background(), att.RID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Stat(context.Background(), att.RID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
	if _, err := store.Get(context.Background(), att.RID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound on Get after delete, got %v", err)
	}
}

func TestLocalStore_PutWithRID(t *testing.T) {
	store := NewLocalStore(t.TempDir())

	preAllocated := "ri.attachments.main.attachment.abc-123"
	att, err := store.PutWithRID(context.Background(), preAllocated, BlobMeta{Filename: "x.bin", MediaType: "application/octet-stream"}, bytes.NewReader([]byte{1, 2, 3, 4}))
	if err != nil {
		t.Fatalf("PutWithRID: %v", err)
	}
	if att.RID != preAllocated {
		t.Errorf("expected RID %q, got %q", preAllocated, att.RID)
	}
	if att.SizeBytes != 4 {
		t.Errorf("sizeBytes = %d", att.SizeBytes)
	}

	// Duplicate PutWithRID should fail
	_, err = store.PutWithRID(context.Background(), preAllocated, BlobMeta{Filename: "x.bin", MediaType: "application/octet-stream"}, bytes.NewReader([]byte{9, 9}))
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestLocalStore_NotFound(t *testing.T) {
	store := NewLocalStore(t.TempDir())

	if _, err := store.Stat(context.Background(), "ri.attachments.main.attachment.missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
	if _, err := store.Get(context.Background(), "ri.attachments.main.attachment.missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
	if err := store.Delete(context.Background(), "ri.attachments.main.attachment.missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestLocalStore_RejectsInvalidRID(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	cases := []string{"", "not-a-rid", "ri.wrong.main.attachment.abc", "ri.attachments.main.other.abc", "../etc/passwd"}
	for _, rid := range cases {
		if _, err := store.PutWithRID(context.Background(), rid, BlobMeta{Filename: "f"}, strings.NewReader("x")); err == nil {
			t.Errorf("rid %q should be rejected", rid)
		}
	}
}

func TestLocalStore_CleanupUnlinked_RemovesOldUnlinked(t *testing.T) {
	store := NewLocalStore(t.TempDir())

	// Three attachments: old-unlinked, old-linked, fresh.
	oldUnlinked := mustPut(t, store, "a.txt", "text/plain", "aa")
	oldLinked := mustPut(t, store, "b.txt", "text/plain", "bb")
	fresh := mustPut(t, store, "c.txt", "text/plain", "cc")

	// Age oldUnlinked and oldLinked by two hours.
	past := time.Now().Add(-2 * time.Hour)
	if err := store.overrideCreatedAt(oldUnlinked.RID, past); err != nil {
		t.Fatalf("overrideCreatedAt: %v", err)
	}
	if err := store.overrideCreatedAt(oldLinked.RID, past); err != nil {
		t.Fatalf("overrideCreatedAt: %v", err)
	}
	if err := store.MarkLinked(context.Background(), oldLinked.RID); err != nil {
		t.Fatalf("MarkLinked: %v", err)
	}

	removed, err := store.CleanupUnlinked(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("CleanupUnlinked: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}

	if _, err := store.Stat(context.Background(), oldUnlinked.RID); !errors.Is(err, ErrNotFound) {
		t.Errorf("oldUnlinked should be deleted, err=%v", err)
	}
	if _, err := store.Stat(context.Background(), oldLinked.RID); err != nil {
		t.Errorf("oldLinked should survive: %v", err)
	}
	if _, err := store.Stat(context.Background(), fresh.RID); err != nil {
		t.Errorf("fresh should survive: %v", err)
	}
}

func TestLocalStore_StartCleanupLoop(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	att := mustPut(t, store, "a.txt", "text/plain", "hello")
	if err := store.overrideCreatedAt(att.RID, time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatalf("overrideCreatedAt: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := store.StartCleanupLoop(ctx, 20*time.Millisecond, time.Hour)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := store.Stat(context.Background(), att.RID); errors.Is(err, ErrNotFound) {
			cancel()
			<-done
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("attachment %s was not cleaned up by loop", att.RID)
}

func TestLocalStore_MarkLinkedIsIdempotent(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	att := mustPut(t, store, "a.txt", "text/plain", "hi")

	for i := 0; i < 3; i++ {
		if err := store.MarkLinked(context.Background(), att.RID); err != nil {
			t.Fatalf("MarkLinked iter %d: %v", i, err)
		}
	}

	got, err := store.Stat(context.Background(), att.RID)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !got.Linked {
		t.Errorf("attachment should be Linked=true")
	}
}
