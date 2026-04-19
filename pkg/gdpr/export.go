package gdpr

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/liyang/weave/pkg/audit"
)

// ExportProfile is the sanitised user identity row included in the export
// bundle. Password hash + MFA secret are INTENTIONALLY omitted — those are
// server-only secrets, not personal data the user can act on, and every
// compliance regime treats "don't re-emit credentials in a data portability
// export" as the safe default.
type ExportProfile struct {
	ID         string    `json:"id"`
	Email      string    `json:"email,omitempty"`
	Name       string    `json:"name,omitempty"`
	MFAEnabled bool      `json:"mfaEnabled"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// MediaAssetInfo is the gdpr-scoped view of a MediaAsset row. Duplicating
// the shape here keeps pkg/gdpr free of pkg/oms / pkg/media imports so the
// package stays easy to test in isolation and the adapter in cmd/server
// owns the translation.
type MediaAssetInfo struct {
	RID       string    `json:"rid"`
	Realm     string    `json:"realm"`
	Filename  string    `json:"filename,omitempty"`
	MIME      string    `json:"mime,omitempty"`
	SizeBytes int64     `json:"sizeBytes"`
	SHA256    string    `json:"sha256"`
	Path      string    `json:"-"` // physical location handed to MediaBlobSource; not exported in JSON
	CreatedAt time.Time `json:"createdAt"`
}

// ExportMedia is the per-media entry serialised into data.json. It mirrors
// MediaAssetInfo plus a RelativePath hint pointing at the blob location
// inside the zip.
type ExportMedia struct {
	RID          string    `json:"rid"`
	Realm        string    `json:"realm"`
	Filename     string    `json:"filename,omitempty"`
	MIME         string    `json:"mime,omitempty"`
	SizeBytes    int64     `json:"sizeBytes"`
	SHA256       string    `json:"sha256"`
	CreatedAt    time.Time `json:"createdAt"`
	RelativePath string    `json:"relativePath,omitempty"`
}

// ExportBundle is the wire shape marshalled to data.json at the root of
// the export zip. Fields are populated from the registered sources; any
// source left unset produces an absent/empty field on the wire.
type ExportBundle struct {
	UserID        string             `json:"userId"`
	GeneratedAt   time.Time          `json:"generatedAt"`
	Profile       *ExportProfile     `json:"profile,omitempty"`
	Roles         []string           `json:"roles,omitempty"`
	OntologyRoles map[string]string  `json:"ontologyRoles,omitempty"`
	AuditEvents   []audit.AuditEvent `json:"auditEvents,omitempty"`
	Media         []ExportMedia      `json:"media,omitempty"`
}

// ProfileSource returns the sanitised profile for userID. Implementations
// MAY return (nil, nil) when the user has been erased — the exporter then
// omits the field from data.json so the export still succeeds for audit-
// retention / orphan-row cleanup flows.
type ProfileSource interface {
	Profile(ctx context.Context, userID string) (*ExportProfile, error)
}

// RoleSource lists the global roles granted to userID.
type RoleSource interface {
	ListUserRoles(ctx context.Context, userID string) ([]string, error)
}

// OntologyRoleSource lists per-ontology role grants keyed by ontology RID.
type OntologyRoleSource interface {
	ListUserOntologyRoles(ctx context.Context, userID string) (map[string]string, error)
}

// AuditEventSource lists every audit event whose actor_id matches userID.
// Production backs this with audit.Store.List({ActorID: userID}).
type AuditEventSource interface {
	ListByActor(ctx context.Context, userID string) ([]audit.AuditEvent, error)
}

// MediaAssetSource lists every media asset uploaded by userID. The
// production adapter wraps oms.MediaAssetStore.ListByCreatedBy.
type MediaAssetSource interface {
	ListUserMedia(ctx context.Context, userID string) ([]MediaAssetInfo, error)
}

// MediaBlobSource opens the bytes for a media asset by relPath. The
// production adapter wraps media.Store.Open.
type MediaBlobSource interface {
	Open(ctx context.Context, relPath string) (io.ReadCloser, error)
}

// Exporter assembles a GDPR data-export bundle for a single user. All
// sources are optional — unset sources produce absent fields so degraded
// deployments (no media, no audit store, ...) still emit a valid zip.
type Exporter struct {
	Profile     ProfileSource
	Roles       RoleSource
	OntRoles    OntologyRoleSource
	Audit       AuditEventSource
	MediaAssets MediaAssetSource
	MediaBlobs  MediaBlobSource
	nowFunc     func() time.Time
}

// NewExporter returns an Exporter with no sources wired. Callers populate
// the fields directly; the constructor doesn't take every source because
// real deployments compose sources from different subsystems (UserRepo +
// audit.Store + MediaCatalog + MediaStore) and a fat ctor would spell each
// one out at every test callsite.
func NewExporter() *Exporter {
	return &Exporter{nowFunc: time.Now}
}

// SetNowFunc overrides the clock. Matches oms.CachedRepository.nowFunc so
// time-stamped fields in data.json are deterministic under test.
func (e *Exporter) SetNowFunc(fn func() time.Time) {
	if fn != nil {
		e.nowFunc = fn
	}
}

// WriteZip streams the export zip for userID into w.
//
// Layout inside the zip:
//
//	data.json                       canonical export JSON
//	media/<safe-rid>/<filename>     per-media blobs (when MediaBlobs is set)
//
// Each source is read once and the results are bundled into data.json
// before any media blob is streamed; a missing blob is logged to the
// returned bundle (SizeBytes stays the catalog value but RelativePath is
// cleared) rather than aborting the whole export — partial compliance is
// strictly better than zero compliance, same philosophy as the erase
// orchestrator's "run every step even when one fails".
func (e *Exporter) WriteZip(ctx context.Context, userID string, w io.Writer) (*ExportBundle, error) {
	if userID == "" {
		return nil, errors.New("gdpr: userID is required")
	}
	bundle := &ExportBundle{
		UserID:      userID,
		GeneratedAt: e.nowFunc().UTC(),
	}

	if e.Profile != nil {
		p, err := e.Profile.Profile(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("profile: %w", err)
		}
		bundle.Profile = p
	}
	if e.Roles != nil {
		roles, err := e.Roles.ListUserRoles(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("roles: %w", err)
		}
		bundle.Roles = roles
	}
	if e.OntRoles != nil {
		m, err := e.OntRoles.ListUserOntologyRoles(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("ontology roles: %w", err)
		}
		bundle.OntologyRoles = m
	}
	if e.Audit != nil {
		evts, err := e.Audit.ListByActor(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("audit: %w", err)
		}
		bundle.AuditEvents = evts
	}

	var assets []MediaAssetInfo
	if e.MediaAssets != nil {
		var err error
		assets, err = e.MediaAssets.ListUserMedia(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("media assets: %w", err)
		}
	}

	zw := zip.NewWriter(w)

	// Pre-compute every relative path so the in-JSON hint matches the
	// actual zip layout even when a later blob read fails.
	entries := make([]ExportMedia, 0, len(assets))
	for _, a := range assets {
		entries = append(entries, ExportMedia{
			RID:          a.RID,
			Realm:        a.Realm,
			Filename:     a.Filename,
			MIME:         a.MIME,
			SizeBytes:    a.SizeBytes,
			SHA256:       a.SHA256,
			CreatedAt:    a.CreatedAt,
			RelativePath: relativeMediaPath(a),
		})
	}
	bundle.Media = entries

	// Write data.json first — SDK consumers typically read this before
	// touching any blob so putting it at the front shortens the ZIP
	// central directory scan.
	header, err := zw.Create("data.json")
	if err != nil {
		_ = zw.Close()
		return nil, err
	}
	enc := json.NewEncoder(header)
	enc.SetIndent("", "  ")
	if err := enc.Encode(bundle); err != nil {
		_ = zw.Close()
		return nil, fmt.Errorf("encode data.json: %w", err)
	}

	// Stream media blobs. A missing / unreadable blob is skipped — the
	// data.json entry still surfaces the metadata so callers can see what
	// should have been there.
	if e.MediaBlobs != nil {
		for i, a := range assets {
			if err := ctx.Err(); err != nil {
				_ = zw.Close()
				return nil, err
			}
			rel := entries[i].RelativePath
			rc, openErr := e.MediaBlobs.Open(ctx, a.Path)
			if openErr != nil {
				// Clear the hint so the bundle reflects "blob missing".
				bundle.Media[i].RelativePath = ""
				continue
			}
			fh, createErr := zw.Create(rel)
			if createErr != nil {
				_ = rc.Close()
				_ = zw.Close()
				return nil, createErr
			}
			if _, copyErr := io.Copy(fh, rc); copyErr != nil {
				_ = rc.Close()
				_ = zw.Close()
				return nil, fmt.Errorf("copy media %s: %w", a.RID, copyErr)
			}
			_ = rc.Close()
		}
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	return bundle, nil
}

// relativeMediaPath builds the location for asset inside the export zip.
// Pattern: media/<safe-rid>/<safe-filename>, sanitised to forbid path
// traversal (../../etc/passwd) or separator escapes.
func relativeMediaPath(a MediaAssetInfo) string {
	name := sanitiseFilename(a.Filename)
	if name == "" {
		name = "blob"
	}
	return path.Join("media", safeSegment(a.RID), name)
}

// sanitiseFilename strips path separators + leading dots from a filename
// so a malicious catalog row can't place a file outside media/<rid>/.
func sanitiseFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	base := path.Base(name)
	base = strings.TrimLeft(base, ".")
	base = strings.ReplaceAll(base, "/", "_")
	base = strings.ReplaceAll(base, "\\", "_")
	if base == "" || base == "." || base == ".." {
		return ""
	}
	return base
}

// safeSegment scrubs a path segment used in the zip layout. Kept simple
// on purpose — RIDs are ri.{...}.{uuid} shaped so the replacement is
// effectively a no-op in production but catches manual test fixtures.
func safeSegment(s string) string {
	if s == "" {
		return "_"
	}
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	s = strings.ReplaceAll(s, "..", "_")
	return s
}
