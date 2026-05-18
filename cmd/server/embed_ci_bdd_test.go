package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBDD_WebEmbedDoesNotRequireGeneratedDist(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	serverDir := filepath.Dir(testFile)

	embedGo, err := os.ReadFile(filepath.Join(serverDir, "embed.go"))
	if err != nil {
		t.Fatalf("read embed.go: %v", err)
	}
	for _, line := range strings.Split(string(embedGo), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//go:embed") && strings.Contains(line, "web/dist") {
			t.Fatalf("embed directive must not point directly at ignored generated dist: %s", line)
		}
	}

	if _, err := os.Stat(filepath.Join(serverDir, "web", "embed-placeholder.txt")); err != nil {
		t.Fatalf("fresh CI needs a tracked fallback file under cmd/server/web: %v", err)
	}
}
