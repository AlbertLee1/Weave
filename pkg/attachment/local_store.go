package attachment

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// attachmentRIDPrefix is the only RID shape LocalStore accepts.
const attachmentRIDPrefix = "ri.attachments.main.attachment."

// LocalStore persists attachments on the local filesystem.
//
// Layout (rooted at Root):
//
//	<ridID>.blob         // raw bytes
//	<ridID>.meta.json    // sidecar metadata (filename, mediaType, createdAt, linked)
//
// "ridID" is the UUID suffix of the attachment RID — the ri.attachments.main.attachment.
// prefix is stripped when deriving paths, so operations can never escape Root.
type LocalStore struct {
	root string

	mu sync.Mutex
}

// NewLocalStore returns a LocalStore rooted at dir. The directory is created
// (0o755) if it does not exist.
func NewLocalStore(dir string) *LocalStore {
	_ = os.MkdirAll(dir, 0o755)
	return &LocalStore{root: dir}
}

// NewAttachmentRID allocates a fresh attachment RID.
func NewAttachmentRID() string {
	return attachmentRIDPrefix + uuid.New().String()
}

func (s *LocalStore) validateRID(rid string) (string, error) {
	if !strings.HasPrefix(rid, attachmentRIDPrefix) {
		return "", fmt.Errorf("%w: %q", ErrInvalidRID, rid)
	}
	id := strings.TrimPrefix(rid, attachmentRIDPrefix)
	if id == "" || strings.ContainsAny(id, "/\\") || strings.Contains(id, "..") {
		return "", fmt.Errorf("%w: %q", ErrInvalidRID, rid)
	}
	return id, nil
}

func (s *LocalStore) blobPath(id string) string {
	return filepath.Join(s.root, id+".blob")
}

func (s *LocalStore) metaPath(id string) string {
	return filepath.Join(s.root, id+".meta.json")
}

type diskMeta struct {
	RID       string    `json:"rid"`
	Filename  string    `json:"filename"`
	MediaType string    `json:"mediaType"`
	SizeBytes int64     `json:"sizeBytes"`
	CreatedAt time.Time `json:"createdAt"`
	Linked    bool      `json:"linked"`
}

func (m diskMeta) toAttachment() *Attachment {
	return &Attachment{
		RID:       m.RID,
		Filename:  m.Filename,
		SizeBytes: m.SizeBytes,
		MediaType: m.MediaType,
		CreatedAt: m.CreatedAt,
		Linked:    m.Linked,
	}
}

func (s *LocalStore) readMeta(id string) (*diskMeta, error) {
	b, err := os.ReadFile(s.metaPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var m diskMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("decode meta %s: %w", id, err)
	}
	return &m, nil
}

func (s *LocalStore) writeMeta(id string, m *diskMeta) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp := s.metaPath(id) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.metaPath(id))
}

// Put stores a new attachment and returns its generated RID + metadata.
func (s *LocalStore) Put(ctx context.Context, meta BlobMeta, r io.Reader) (*Attachment, error) {
	return s.PutWithRID(ctx, NewAttachmentRID(), meta, r)
}

// PutWithRID stores a new attachment with a caller-provided RID.
// Returns ErrAlreadyExists if the RID is already taken.
func (s *LocalStore) PutWithRID(ctx context.Context, rid string, meta BlobMeta, r io.Reader) (*Attachment, error) {
	id, err := s.validateRID(rid)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := os.Stat(s.metaPath(id)); err == nil {
		return nil, fmt.Errorf("%w: %s", ErrAlreadyExists, rid)
	}

	blobPath := s.blobPath(id)
	f, err := os.OpenFile(blobPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("create blob: %w", err)
	}
	n, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(blobPath)
		return nil, fmt.Errorf("write blob: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(blobPath)
		return nil, fmt.Errorf("close blob: %w", closeErr)
	}

	mediaType := meta.MediaType
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}

	m := &diskMeta{
		RID:       rid,
		Filename:  meta.Filename,
		MediaType: mediaType,
		SizeBytes: n,
		CreatedAt: time.Now().UTC(),
		Linked:    false,
	}
	if err := s.writeMeta(id, m); err != nil {
		_ = os.Remove(blobPath)
		return nil, fmt.Errorf("write meta: %w", err)
	}
	return m.toAttachment(), nil
}

// Get opens the blob body for reading. Caller must Close.
func (s *LocalStore) Get(ctx context.Context, rid string) (io.ReadCloser, error) {
	id, err := s.validateRID(rid)
	if err != nil {
		return nil, err
	}
	if _, err := s.readMeta(id); err != nil {
		return nil, err
	}
	f, err := os.Open(s.blobPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return f, nil
}

// Stat returns the metadata for an attachment.
func (s *LocalStore) Stat(ctx context.Context, rid string) (*Attachment, error) {
	id, err := s.validateRID(rid)
	if err != nil {
		return nil, err
	}
	m, err := s.readMeta(id)
	if err != nil {
		return nil, err
	}
	return m.toAttachment(), nil
}

// Delete removes the blob and its metadata sidecar.
func (s *LocalStore) Delete(ctx context.Context, rid string) error {
	id, err := s.validateRID(rid)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := os.Stat(s.metaPath(id)); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}

	if err := os.Remove(s.blobPath(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(s.metaPath(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// MarkLinked flags an attachment as owned by a persisted object so the TTL
// cleanup loop will skip it. Idempotent.
func (s *LocalStore) MarkLinked(ctx context.Context, rid string) error {
	id, err := s.validateRID(rid)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.readMeta(id)
	if err != nil {
		return err
	}
	if m.Linked {
		return nil
	}
	m.Linked = true
	return s.writeMeta(id, m)
}

// CleanupUnlinked removes unlinked attachments older than maxAge and returns
// the number of blobs deleted. Errors on individual entries are logged and do
// not abort the sweep.
func (s *LocalStore) CleanupUnlinked(ctx context.Context, maxAge time.Duration) (int, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for _, entry := range entries {
		if ctx.Err() != nil {
			return removed, ctx.Err()
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".meta.json") {
			continue
		}
		id := strings.TrimSuffix(name, ".meta.json")
		m, err := s.readMeta(id)
		if err != nil {
			log.Printf("attachment cleanup: read meta %s: %v", id, err)
			continue
		}
		if m.Linked || !m.CreatedAt.Before(cutoff) {
			continue
		}
		if err := s.Delete(ctx, m.RID); err != nil && err != ErrNotFound {
			log.Printf("attachment cleanup: delete %s: %v", m.RID, err)
			continue
		}
		removed++
	}
	return removed, nil
}

// StartCleanupLoop runs CleanupUnlinked on a ticker until ctx is cancelled.
// It returns a done channel that is closed when the loop exits, so callers
// can wait for graceful shutdown.
func (s *LocalStore) StartCleanupLoop(ctx context.Context, interval, maxAge time.Duration) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := s.CleanupUnlinked(ctx, maxAge); err != nil && ctx.Err() == nil {
					log.Printf("attachment cleanup loop: %v", err)
				}
			}
		}
	}()
	return done
}

// overrideCreatedAt is a test hook that rewrites the persisted CreatedAt for
// an attachment so TTL-related assertions don't need to sleep for hours.
func (s *LocalStore) overrideCreatedAt(rid string, when time.Time) error {
	id, err := s.validateRID(rid)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readMeta(id)
	if err != nil {
		return err
	}
	m.CreatedAt = when.UTC()
	return s.writeMeta(id, m)
}

// compile-time assertion that LocalStore implements BlobStore.
var _ BlobStore = (*LocalStore)(nil)
