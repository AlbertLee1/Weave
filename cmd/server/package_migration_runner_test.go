package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

func TestPGPackageMigrationRunner_PersistsFilesEvenWithoutPG(t *testing.T) {
	tmp := t.TempDir()
	runner := newPGPackageMigrationRunner(tmp, "")

	files := []oms.PackageMigrationEntry{
		{Filename: "000001_init.up.sql", Content: []byte("SELECT 1;")},
		{Filename: "000001_init.down.sql", Content: []byte("DROP IF EXISTS x;")},
	}
	n, err := runner.RunPackageMigrations(context.Background(), "northwind", files)
	if err != nil {
		t.Fatalf("RunPackageMigrations: %v", err)
	}
	if n != 2 {
		t.Fatalf("written count = %d, want 2", n)
	}

	expectedDir := filepath.Join(tmp, "installed_packages", "northwind", "migrations")
	for _, fname := range []string{"000001_init.up.sql", "000001_init.down.sql"} {
		body, err := os.ReadFile(filepath.Join(expectedDir, fname))
		if err != nil {
			t.Fatalf("read %s: %v", fname, err)
		}
		if len(body) == 0 {
			t.Fatalf("file %s is empty", fname)
		}
	}
}

func TestPGPackageMigrationRunner_RejectsBlankPackageName(t *testing.T) {
	tmp := t.TempDir()
	runner := newPGPackageMigrationRunner(tmp, "")
	_, err := runner.RunPackageMigrations(context.Background(), "  ", []oms.PackageMigrationEntry{
		{Filename: "001.up.sql", Content: []byte("SELECT 1;")},
	})
	if err == nil {
		t.Fatalf("expected error for blank package name")
	}
}

func TestPGPackageMigrationRunner_RejectsPathTraversingFilename(t *testing.T) {
	tmp := t.TempDir()
	runner := newPGPackageMigrationRunner(tmp, "")
	_, err := runner.RunPackageMigrations(context.Background(), "x", []oms.PackageMigrationEntry{
		{Filename: "../etc/passwd", Content: []byte("SELECT 1;")},
	})
	if err == nil || !strings.Contains(err.Error(), "basename") {
		t.Fatalf("expected basename error, got %v", err)
	}
}

func TestPGPackageMigrationRunner_NoFilesIsNoOp(t *testing.T) {
	tmp := t.TempDir()
	runner := newPGPackageMigrationRunner(tmp, "")
	n, err := runner.RunPackageMigrations(context.Background(), "x", nil)
	if err != nil {
		t.Fatalf("expected no error for empty files, got %v", err)
	}
	if n != 0 {
		t.Fatalf("expected n=0, got %d", n)
	}
	// Directory should NOT have been created.
	if _, err := os.Stat(filepath.Join(tmp, "installed_packages", "x")); !os.IsNotExist(err) {
		t.Fatalf("empty file list should not create directory; stat err = %v", err)
	}
}

func TestPGPackageMigrationRunner_IsIdempotentRewrite(t *testing.T) {
	tmp := t.TempDir()
	runner := newPGPackageMigrationRunner(tmp, "")
	files := []oms.PackageMigrationEntry{
		{Filename: "001.up.sql", Content: []byte("V1;")},
	}
	if _, err := runner.RunPackageMigrations(context.Background(), "x", files); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Re-run with updated content — atomic rename means the file body is
	// replaced cleanly.
	files[0].Content = []byte("V2;")
	if _, err := runner.RunPackageMigrations(context.Background(), "x", files); err != nil {
		t.Fatalf("second run: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(tmp, "installed_packages", "x", "migrations", "001.up.sql"))
	if string(body) != "V2;" {
		t.Fatalf("re-run did not overwrite body: %q", body)
	}
}

func TestValidatePackageMigrationFilename(t *testing.T) {
	cases := []struct {
		name    string
		wantErr bool
	}{
		{"000001_init.up.sql", false},
		{"a.sql", false},
		{"", true},
		{"../escape.sql", true},
		{"sub/dir.sql", true},
		{"..", true},
	}
	for _, tc := range cases {
		err := validatePackageMigrationFilename(tc.name)
		gotErr := err != nil
		if gotErr != tc.wantErr {
			t.Errorf("validatePackageMigrationFilename(%q) gotErr=%v want=%v err=%v",
				tc.name, gotErr, tc.wantErr, err)
		}
	}
}
