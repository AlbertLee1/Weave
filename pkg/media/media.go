// Package media implements the storage layer backing the `media` base type.
//
// Media properties carry large binary payloads (images, files) and are stored
// content-addressably: the on-disk path is derived from the SHA-256 of the
// bytes plus a realm + year/month partition. Multiple catalog rows with the
// same sha256 share a single physical file — dedup is enforced by the store
// on Put.
//
// The physical layout is:
//
//	<root>/<realm>/<YYYY>/<MM>/<sha256>
//
// Callers persist one MediaAsset row per logical upload via
// MediaAssetStore (implemented by *oms.PGRepository). The store here is
// concerned only with bytes on disk.
package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MediaRIDPrefix is the canonical RID shape for media assets.
const MediaRIDPrefix = "ri.media.main.asset."

// ErrNotFound is returned when a media asset's physical file is missing.
var ErrNotFound = errors.New("media: not found")

// ErrInvalidRealm is returned when a realm contains path separators or other
// characters that would let a caller escape the storage root.
var ErrInvalidRealm = errors.New("media: invalid realm")

// NewMediaRID allocates a fresh media asset RID.
func NewMediaRID() string {
	return MediaRIDPrefix + uuid.New().String()
}

// Asset is the in-memory record returned by Put. It captures everything the
// caller needs to persist a media_assets row.
type Asset struct {
	RID       string
	Realm     string
	Filename  string
	MIME      string
	SizeBytes int64
	SHA256    string
	Path      string // path relative to the storage root
	CreatedBy string
	CreatedAt time.Time
}

// PutOptions carries the optional metadata supplied by the caller on upload.
type PutOptions struct {
	Realm     string
	Filename  string
	MIME      string
	CreatedBy string
}

// Store is the FS-backed media blob store. It is safe for concurrent use.
type Store struct {
	root    string
	nowFunc func() time.Time

	mu sync.Mutex
}

// NewStore returns a Store rooted at dir. The directory is created (0o755)
// if it does not exist.
func NewStore(dir string) *Store {
	_ = os.MkdirAll(dir, 0o755)
	return &Store{root: dir, nowFunc: time.Now}
}

// SetNowFunc overrides the wall clock. Tests use this to exercise the
// YYYY/MM partitioning deterministically.
func (s *Store) SetNowFunc(f func() time.Time) {
	s.nowFunc = f
}

// Root returns the absolute path of the storage root.
func (s *Store) Root() string {
	return s.root
}

// Put streams r through a SHA-256 hasher and a temp file, then moves the
// file into its content-addressed location. If the destination already
// exists (same realm + sha256) the temp file is removed and the existing
// blob is reused — this is the dedup behaviour mandated by US-203.
//
// Put always allocates a fresh RID via NewMediaRID, even on dedup — the
// table row is the reference count; only the bytes are shared.
func (s *Store) Put(ctx context.Context, opts PutOptions, r io.Reader) (*Asset, error) {
	realm := strings.TrimSpace(opts.Realm)
	if realm == "" {
		realm = "main"
	}
	if err := validateRealm(realm); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return nil, fmt.Errorf("ensure root: %w", err)
	}

	tmp, err := os.CreateTemp(s.root, ".upload-*")
	if err != nil {
		return nil, fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(tmp, hasher), r)
	closeErr := tmp.Close()
	if copyErr != nil {
		cleanup()
		return nil, fmt.Errorf("write temp: %w", copyErr)
	}
	if closeErr != nil {
		cleanup()
		return nil, fmt.Errorf("close temp: %w", closeErr)
	}
	if err := ctx.Err(); err != nil {
		cleanup()
		return nil, err
	}

	sum := hex.EncodeToString(hasher.Sum(nil))
	now := s.nowFunc().UTC()
	relDir := filepath.Join(realm, fmt.Sprintf("%04d", now.Year()), fmt.Sprintf("%02d", int(now.Month())))
	relPath := filepath.Join(relDir, sum)
	absDir := filepath.Join(s.root, relDir)
	absPath := filepath.Join(s.root, relPath)

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := os.Stat(absPath); err == nil {
		cleanup()
	} else if os.IsNotExist(err) {
		if err := os.MkdirAll(absDir, 0o755); err != nil {
			cleanup()
			return nil, fmt.Errorf("ensure content dir: %w", err)
		}
		if err := os.Rename(tmpPath, absPath); err != nil {
			cleanup()
			return nil, fmt.Errorf("promote blob: %w", err)
		}
	} else {
		cleanup()
		return nil, fmt.Errorf("stat content path: %w", err)
	}

	mime := strings.TrimSpace(opts.MIME)
	if mime == "" {
		mime = "application/octet-stream"
	}

	return &Asset{
		RID:       NewMediaRID(),
		Realm:     realm,
		Filename:  opts.Filename,
		MIME:      mime,
		SizeBytes: written,
		SHA256:    sum,
		Path:      filepath.ToSlash(relPath),
		CreatedBy: opts.CreatedBy,
		CreatedAt: now,
	}, nil
}

// Open returns an io.ReadCloser for the file at path (relative to root).
// Callers MUST Close.
func (s *Store) Open(ctx context.Context, relPath string) (io.ReadCloser, error) {
	abs, err := s.resolvePath(relPath)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return f, nil
}

// DeletePath removes the physical file. The caller is responsible for
// checking the reference count first — this store holds no catalog.
func (s *Store) DeletePath(ctx context.Context, relPath string) error {
	abs, err := s.resolvePath(relPath)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(abs); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// Exists reports whether the file at relPath is present on disk.
func (s *Store) Exists(relPath string) (bool, error) {
	abs, err := s.resolvePath(relPath)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(abs)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// resolvePath joins relPath onto root and verifies the result stays within
// root. This is the single guard against path-traversal input reaching the
// filesystem.
func (s *Store) resolvePath(relPath string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(relPath))
	if clean == "." || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("media: invalid path %q", relPath)
	}
	abs := filepath.Join(s.root, clean)
	rootAbs, err := filepath.Abs(s.root)
	if err != nil {
		return "", err
	}
	absResolved, err := filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absResolved, rootAbs+string(filepath.Separator)) && absResolved != rootAbs {
		return "", fmt.Errorf("media: path escapes root: %q", relPath)
	}
	return absResolved, nil
}

func validateRealm(realm string) error {
	if realm == "" {
		return fmt.Errorf("%w: empty", ErrInvalidRealm)
	}
	if strings.ContainsAny(realm, "/\\") || strings.Contains(realm, "..") {
		return fmt.Errorf("%w: %q", ErrInvalidRealm, realm)
	}
	return nil
}
