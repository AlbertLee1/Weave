package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileProvider_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "db_password"), []byte("hunter2\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	p, err := NewFileProvider(dir)
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	got, err := p.Get(context.Background(), "db_password")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != "hunter2" {
		t.Errorf("got %q, want hunter2 (newline trimmed)", got)
	}
}

func TestFileProvider_NotFound(t *testing.T) {
	dir := t.TempDir()
	p, _ := NewFileProvider(dir)
	_, err := p.Get(context.Background(), "missing")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("want ErrSecretNotFound, got %v", err)
	}
}

func TestFileProvider_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "empty"), []byte("\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	p, _ := NewFileProvider(dir)
	_, err := p.Get(context.Background(), "empty")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("empty file should be ErrSecretNotFound, got %v", err)
	}
}

func TestFileProvider_RejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	p, _ := NewFileProvider(dir)
	for _, bad := range []string{"../etc/passwd", "a/b", `a\b`, "..", "../inner"} {
		_, err := p.Get(context.Background(), bad)
		if err == nil {
			t.Errorf("expected error for %q", bad)
		}
		if errors.Is(err, ErrSecretNotFound) {
			t.Errorf("for %q, want validation error not ErrSecretNotFound, got %v", bad, err)
		}
	}
}

func TestFileProvider_DirMustExist(t *testing.T) {
	if _, err := NewFileProvider(""); err == nil {
		t.Error("empty dir should fail")
	}
	if _, err := NewFileProvider(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Error("missing dir should fail")
	}
}
