package secrets

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileProvider satisfies Provider by reading values from per-key files
// under a directory. Compatible with the docker / kubernetes secrets
// volume convention: the file at $dir/<key> contains the secret value.
//
// File contents have a single trailing newline trimmed, so editors that
// stamp the usual final-newline don't accidentally include it in the
// secret string. Empty files are treated as not-found.
type FileProvider struct {
	dir string
}

// NewFileProvider returns a Provider rooted at dir. dir must already
// exist; callers are expected to mount their secrets volume there.
func NewFileProvider(dir string) (*FileProvider, error) {
	if dir == "" {
		return nil, errors.New("FileProvider requires a non-empty dir")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("FileProvider stat %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("FileProvider: %q is not a directory", dir)
	}
	return &FileProvider{dir: dir}, nil
}

func (p *FileProvider) Name() string { return "file" }

func (p *FileProvider) Get(_ context.Context, key string) (string, error) {
	if strings.ContainsAny(key, `/\`) || strings.Contains(key, "..") {
		return "", fmt.Errorf("FileProvider: invalid key %q", key)
	}
	path := filepath.Join(p.dir, key)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrSecretNotFound
		}
		return "", fmt.Errorf("FileProvider read %q: %w", path, err)
	}
	value := strings.TrimRight(string(raw), "\n")
	if value == "" {
		return "", ErrSecretNotFound
	}
	return value, nil
}
