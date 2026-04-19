package gdpr

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/audit"
)

func TestExporter_RejectsEmptyUserID(t *testing.T) {
	e := NewExporter()
	var buf bytes.Buffer
	_, err := e.WriteZip(context.Background(), "", &buf)
	if err == nil {
		t.Fatal("expected error on empty userID")
	}
}

func TestExporter_EmptySourcesEmitsValidZipWithDataJSON(t *testing.T) {
	e := NewExporter()
	fixedClock := time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC)
	e.SetNowFunc(func() time.Time { return fixedClock })

	var buf bytes.Buffer
	bundle, err := e.WriteZip(context.Background(), "user:alice", &buf)
	if err != nil {
		t.Fatalf("WriteZip: %v", err)
	}
	if bundle.UserID != "user:alice" {
		t.Errorf("bundle.UserID = %q, want user:alice", bundle.UserID)
	}
	if !bundle.GeneratedAt.Equal(fixedClock) {
		t.Errorf("GeneratedAt = %v, want %v", bundle.GeneratedAt, fixedClock)
	}

	files := unzipFiles(t, buf.Bytes())
	if len(files) != 1 {
		t.Fatalf("zip file count = %d, want 1 (data.json), got %v", len(files), keysOf(files))
	}
	raw, ok := files["data.json"]
	if !ok {
		t.Fatal("zip is missing data.json")
	}

	var got ExportBundle
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal data.json: %v", err)
	}
	if got.UserID != "user:alice" {
		t.Errorf("data.json UserID = %q", got.UserID)
	}
	if got.Profile != nil {
		t.Errorf("expected nil Profile, got %#v", got.Profile)
	}
}

func TestExporter_PopulatesAllSources(t *testing.T) {
	e := NewExporter()
	e.Profile = profileSourceFunc(func(_ context.Context, uid string) (*ExportProfile, error) {
		return &ExportProfile{ID: uid, Email: "alice@example.com", Name: "Alice"}, nil
	})
	e.Roles = rolesSourceFunc(func(_ context.Context, _ string) ([]string, error) {
		return []string{"admin", "viewer"}, nil
	})
	e.OntRoles = ontRoleSourceFunc(func(_ context.Context, _ string) (map[string]string, error) {
		return map[string]string{"ri.ont.main.ontology.o1": "editor"}, nil
	})
	e.Audit = auditSourceFunc(func(_ context.Context, uid string) ([]audit.AuditEvent, error) {
		return []audit.AuditEvent{{ID: "a1", ActorID: uid, Action: "CREATE"}}, nil
	})
	e.MediaAssets = mediaSourceFunc(func(_ context.Context, _ string) ([]MediaAssetInfo, error) {
		return []MediaAssetInfo{
			{RID: "ri.media.main.asset.m1", Realm: "main", Filename: "pic.png", MIME: "image/png", SizeBytes: 3, SHA256: "abc", Path: "main/2026/04/abc"},
		}, nil
	})
	e.MediaBlobs = mediaBlobSourceFunc(func(_ context.Context, relPath string) (io.ReadCloser, error) {
		if relPath != "main/2026/04/abc" {
			t.Errorf("unexpected blob path: %s", relPath)
		}
		return io.NopCloser(strings.NewReader("png")), nil
	})

	var buf bytes.Buffer
	bundle, err := e.WriteZip(context.Background(), "user:alice", &buf)
	if err != nil {
		t.Fatalf("WriteZip: %v", err)
	}
	if bundle.Profile == nil || bundle.Profile.Email != "alice@example.com" {
		t.Errorf("profile wrong: %#v", bundle.Profile)
	}
	if len(bundle.Roles) != 2 {
		t.Errorf("roles = %v", bundle.Roles)
	}
	if bundle.OntologyRoles["ri.ont.main.ontology.o1"] != "editor" {
		t.Errorf("ontology roles = %v", bundle.OntologyRoles)
	}
	if len(bundle.AuditEvents) != 1 || bundle.AuditEvents[0].ID != "a1" {
		t.Errorf("audit events = %v", bundle.AuditEvents)
	}
	if len(bundle.Media) != 1 {
		t.Fatalf("media entries = %d", len(bundle.Media))
	}
	if bundle.Media[0].RelativePath != "media/ri.media.main.asset.m1/pic.png" {
		t.Errorf("relative path = %q", bundle.Media[0].RelativePath)
	}

	files := unzipFiles(t, buf.Bytes())
	blob, ok := files["media/ri.media.main.asset.m1/pic.png"]
	if !ok {
		t.Fatalf("media file missing. files=%v", keysOf(files))
	}
	if string(blob) != "png" {
		t.Errorf("media bytes = %q, want png", string(blob))
	}
}

func TestExporter_MissingBlobSkippedButMetadataRemains(t *testing.T) {
	e := NewExporter()
	e.MediaAssets = mediaSourceFunc(func(_ context.Context, _ string) ([]MediaAssetInfo, error) {
		return []MediaAssetInfo{
			{RID: "ri.media.main.asset.ok", Filename: "ok.txt", Path: "found/ok"},
			{RID: "ri.media.main.asset.lost", Filename: "missing.txt", Path: "nope/missing"},
		}, nil
	})
	e.MediaBlobs = mediaBlobSourceFunc(func(_ context.Context, relPath string) (io.ReadCloser, error) {
		if relPath == "nope/missing" {
			return nil, errors.New("not found")
		}
		return io.NopCloser(strings.NewReader("body")), nil
	})

	var buf bytes.Buffer
	bundle, err := e.WriteZip(context.Background(), "user:alice", &buf)
	if err != nil {
		t.Fatalf("WriteZip: %v", err)
	}
	if len(bundle.Media) != 2 {
		t.Fatalf("media entries = %d", len(bundle.Media))
	}
	if bundle.Media[0].RelativePath == "" {
		t.Error("first entry lost its relative path")
	}
	if bundle.Media[1].RelativePath != "" {
		t.Errorf("missing blob kept relative path = %q", bundle.Media[1].RelativePath)
	}

	files := unzipFiles(t, buf.Bytes())
	if _, ok := files["media/ri.media.main.asset.ok/ok.txt"]; !ok {
		t.Errorf("present blob missing from zip. files=%v", keysOf(files))
	}
	if _, ok := files["media/ri.media.main.asset.lost/missing.txt"]; ok {
		t.Errorf("lost blob should not appear in zip")
	}
}

func TestExporter_FilenameSanitisation(t *testing.T) {
	// A malicious catalog row can't drop files outside media/<rid>/.
	e := NewExporter()
	e.MediaAssets = mediaSourceFunc(func(_ context.Context, _ string) ([]MediaAssetInfo, error) {
		return []MediaAssetInfo{
			{RID: "ri.media.main.asset.evil", Filename: "../../../etc/passwd", Path: "p"},
			{RID: "ri.media.main.asset.dot", Filename: ".", Path: "p"},
			{RID: "ri.media.main.asset.bare", Filename: "", Path: "p"},
		}, nil
	})
	e.MediaBlobs = mediaBlobSourceFunc(func(_ context.Context, _ string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("x")), nil
	})

	var buf bytes.Buffer
	_, err := e.WriteZip(context.Background(), "user:alice", &buf)
	if err != nil {
		t.Fatalf("WriteZip: %v", err)
	}
	files := unzipFiles(t, buf.Bytes())
	for name := range files {
		if strings.Contains(name, "..") {
			t.Errorf("traversal leaked into zip: %s", name)
		}
		if strings.HasPrefix(name, "/") {
			t.Errorf("absolute path leaked into zip: %s", name)
		}
	}
	// dot-only and empty filenames should fall back to "blob".
	if _, ok := files["media/ri.media.main.asset.bare/blob"]; !ok {
		t.Errorf("empty filename did not fall back to blob: %v", keysOf(files))
	}
}

func TestExporter_ProfileErrorAborts(t *testing.T) {
	e := NewExporter()
	e.Profile = profileSourceFunc(func(context.Context, string) (*ExportProfile, error) {
		return nil, errors.New("db down")
	})
	var buf bytes.Buffer
	_, err := e.WriteZip(context.Background(), "user:alice", &buf)
	if err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("expected profile error, got %v", err)
	}
}

func TestExporter_ContextCancellationAbortsBlobStreaming(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	e := NewExporter()
	e.MediaAssets = mediaSourceFunc(func(context.Context, string) ([]MediaAssetInfo, error) {
		return []MediaAssetInfo{
			{RID: "ri.media.main.asset.a", Filename: "a.txt", Path: "a"},
			{RID: "ri.media.main.asset.b", Filename: "b.txt", Path: "b"},
		}, nil
	})
	e.MediaBlobs = mediaBlobSourceFunc(func(_ context.Context, relPath string) (io.ReadCloser, error) {
		if relPath == "a" {
			cancel()
			return io.NopCloser(strings.NewReader("x")), nil
		}
		return io.NopCloser(strings.NewReader("y")), nil
	})

	var buf bytes.Buffer
	_, err := e.WriteZip(ctx, "user:alice", &buf)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want ctx.Canceled", err)
	}
}

// --- source adapters for tests ---

type profileSourceFunc func(ctx context.Context, uid string) (*ExportProfile, error)

func (f profileSourceFunc) Profile(ctx context.Context, uid string) (*ExportProfile, error) {
	return f(ctx, uid)
}

type rolesSourceFunc func(ctx context.Context, uid string) ([]string, error)

func (f rolesSourceFunc) ListUserRoles(ctx context.Context, uid string) ([]string, error) {
	return f(ctx, uid)
}

type ontRoleSourceFunc func(ctx context.Context, uid string) (map[string]string, error)

func (f ontRoleSourceFunc) ListUserOntologyRoles(ctx context.Context, uid string) (map[string]string, error) {
	return f(ctx, uid)
}

type auditSourceFunc func(ctx context.Context, uid string) ([]audit.AuditEvent, error)

func (f auditSourceFunc) ListByActor(ctx context.Context, uid string) ([]audit.AuditEvent, error) {
	return f(ctx, uid)
}

type mediaSourceFunc func(ctx context.Context, uid string) ([]MediaAssetInfo, error)

func (f mediaSourceFunc) ListUserMedia(ctx context.Context, uid string) ([]MediaAssetInfo, error) {
	return f(ctx, uid)
}

type mediaBlobSourceFunc func(ctx context.Context, relPath string) (io.ReadCloser, error)

func (f mediaBlobSourceFunc) Open(ctx context.Context, relPath string) (io.ReadCloser, error) {
	return f(ctx, relPath)
}

// --- helpers ---

func unzipFiles(t *testing.T, raw []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open zip entry %s: %v", f.Name, err)
		}
		body, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read zip entry %s: %v", f.Name, err)
		}
		out[f.Name] = body
	}
	return out
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Ensure fmt import is used (small safety net; gets optimised away).
var _ = fmt.Sprintf
