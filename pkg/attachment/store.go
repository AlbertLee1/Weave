// Package attachment implements the blob storage layer backing Foundry
// OSv2 Attachment, AttachmentProperty and MediaReference endpoints.
//
// It exposes a BlobStore interface with a local-filesystem implementation
// (LocalStore). Attachments are created in an "unlinked" state; a background
// TTL loop deletes unlinked blobs older than a configurable age so abandoned
// uploads do not accumulate.
package attachment

import (
	"context"
	"errors"
	"io"
	"time"
)

// Attachment is the wire-compatible Foundry AttachmentV2 record plus a few
// server-internal fields (CreatedAt, Linked) that do not leak to clients.
type Attachment struct {
	RID       string    `json:"rid"`
	Filename  string    `json:"filename"`
	SizeBytes int64     `json:"sizeBytes"`
	MediaType string    `json:"mediaType"`
	CreatedAt time.Time `json:"-"`
	Linked    bool      `json:"-"`
}

// BlobMeta carries the filename + media type supplied by the uploader.
type BlobMeta struct {
	Filename  string
	MediaType string
}

// BlobStore is the abstraction backing attachment upload / download.
//
// Implementations MUST be safe for concurrent use.
type BlobStore interface {
	Put(ctx context.Context, meta BlobMeta, r io.Reader) (*Attachment, error)
	PutWithRID(ctx context.Context, rid string, meta BlobMeta, r io.Reader) (*Attachment, error)
	Get(ctx context.Context, rid string) (io.ReadCloser, error)
	Stat(ctx context.Context, rid string) (*Attachment, error)
	Delete(ctx context.Context, rid string) error
	MarkLinked(ctx context.Context, rid string) error
	CleanupUnlinked(ctx context.Context, maxAge time.Duration) (int, error)
}

// ErrNotFound is returned when an attachment RID has no associated blob.
var ErrNotFound = errors.New("attachment: not found")

// ErrAlreadyExists is returned by PutWithRID when the RID is already taken.
var ErrAlreadyExists = errors.New("attachment: already exists")

// ErrInvalidRID is returned when a caller supplies a RID that is not a
// well-formed ri.attachments.main.attachment.* identifier.
var ErrInvalidRID = errors.New("attachment: invalid rid")
