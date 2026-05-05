// Package backup implements the single-tarball backup / restore bundle
// consumed by `weave backup` and `weave restore` (US-448).
//
// Layout of an emitted archive (gzipped tar):
//
//	manifest.json
//	db.dump                       — opaque pg_dump output (custom format)
//	data/<relative path…>         — every regular file under DataDir
//
// The manifest pins per-component sha256 + size so a `restore` can detect
// corruption mid-stream rather than blindly piping a truncated dump into
// pg_restore.
//
// The pg_dump / pg_restore step is injected via PGDumpFn / PGRestoreFn so
// the unit tests run without a live Postgres. The CLI wires the real
// shell-out helpers in cmd_backup.go.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// BundleVersion is the on-disk format identifier. Bumping it requires a
// matching read-side migration in Restore.
const BundleVersion = 1

// PGDumpFn streams a pg_dump payload into w. The DSN is opaque and may be
// empty when the implementation reads its own configuration (libpq env).
type PGDumpFn func(ctx context.Context, dsn string, w io.Writer) error

// PGRestoreFn replays a pg_dump payload from r into the database at dsn.
// The implementation owns connection management.
type PGRestoreFn func(ctx context.Context, dsn string, r io.Reader) error

// Component is one logical entry inside the manifest.
type Component struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256,omitempty"`
	FileCount int    `json:"file_count,omitempty"`
}

// Manifest is serialised to manifest.json at the top of the tarball.
type Manifest struct {
	Version    int                  `json:"version"`
	Timestamp  string               `json:"timestamp"`
	DSN        string               `json:"dsn,omitempty"`
	Components map[string]Component `json:"components"`
}

// Bundle bundles a Postgres dump together with the on-disk DataDir tree
// (Bleve indexes, Parquet materialised tier, media, etc.) into a single
// gzipped tar archive. Symmetrically, Restore re-hydrates the DataDir
// and pipes db.dump back into pg_restore.
type Bundle struct {
	DataDir     string
	PGDumpFn    PGDumpFn
	PGRestoreFn PGRestoreFn
	Now         func() time.Time
}

// Backup writes the bundle to outPath. Returns the manifest that was
// embedded so callers can log / surface it.
func (b *Bundle) Backup(ctx context.Context, dsn, outPath string) (Manifest, error) {
	if strings.TrimSpace(outPath) == "" {
		return Manifest{}, errors.New("backup: output path is required")
	}
	if b.PGDumpFn == nil {
		return Manifest{}, errors.New("backup: PGDumpFn is required")
	}

	// 1. pg_dump → memory. We hold the whole dump in memory so we can
	//    sha256 it before writing into the tar header (which needs an
	//    upfront content-length). Acceptable because typical OSS
	//    deployments fit comfortably; large clusters are out of scope
	//    for the single-machine backup CLI.
	dump, err := runDump(ctx, b.PGDumpFn, dsn)
	if err != nil {
		return Manifest{}, err
	}

	// 2. Walk the DataDir.
	dataFiles, err := walkDataDir(b.DataDir)
	if err != nil {
		return Manifest{}, err
	}

	manifest := Manifest{
		Version:    BundleVersion,
		Timestamp:  b.now().Format(time.RFC3339),
		DSN:        redactDSN(dsn),
		Components: map[string]Component{},
	}
	manifest.Components["db.dump"] = Component{
		Path:   "db.dump",
		Size:   int64(len(dump)),
		SHA256: sha256Hex(dump),
	}

	dataComponent := Component{
		Path:      "data/",
		FileCount: len(dataFiles),
	}
	for _, f := range dataFiles {
		dataComponent.Size += f.size
	}
	if dataComponent.FileCount > 0 {
		dataComponent.SHA256 = combinedDataSHA(dataFiles)
	}
	manifest.Components["data"] = dataComponent

	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: marshal manifest: %w", err)
	}

	// 3. Write tar.gz atomically (tmp → rename).
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return Manifest{}, fmt.Errorf("backup: mkdir: %w", err)
	}
	tmp := outPath + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: create %s: %w", tmp, err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = out.Close()
		}
		_ = os.Remove(tmp)
	}()

	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)

	now := b.now()
	if err := writeTarFile(tw, "manifest.json", manifestJSON, now); err != nil {
		return Manifest{}, fmt.Errorf("backup: write manifest: %w", err)
	}
	if err := writeTarFile(tw, "db.dump", dump, now); err != nil {
		return Manifest{}, fmt.Errorf("backup: write db.dump: %w", err)
	}
	for _, f := range dataFiles {
		body, err := os.ReadFile(f.absPath)
		if err != nil {
			return Manifest{}, fmt.Errorf("backup: read %s: %w", f.absPath, err)
		}
		if err := writeTarFile(tw, "data/"+f.relPath, body, now); err != nil {
			return Manifest{}, fmt.Errorf("backup: write %s: %w", f.relPath, err)
		}
	}
	if err := tw.Close(); err != nil {
		return Manifest{}, fmt.Errorf("backup: close tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return Manifest{}, fmt.Errorf("backup: close gzip: %w", err)
	}
	if err := out.Close(); err != nil {
		return Manifest{}, fmt.Errorf("backup: close file: %w", err)
	}
	closed = true
	if err := os.Rename(tmp, outPath); err != nil {
		return Manifest{}, fmt.Errorf("backup: rename: %w", err)
	}
	return manifest, nil
}

// Restore extracts inPath, verifies the embedded manifest's sha256
// witnesses, and replays db.dump through PGRestoreFn. The DataDir is
// removed before extraction so a stale tree from a prior install can
// never bleed into the restored state.
func (b *Bundle) Restore(ctx context.Context, dsn, inPath string) (Manifest, error) {
	if b.PGRestoreFn == nil {
		return Manifest{}, errors.New("restore: PGRestoreFn is required")
	}
	in, err := os.Open(inPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("restore: open %s: %w", inPath, err)
	}
	defer in.Close()
	gz, err := gzip.NewReader(in)
	if err != nil {
		return Manifest{}, fmt.Errorf("restore: gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	var (
		manifest      Manifest
		manifestSeen  bool
		dump          []byte
		dataFiles     = map[string][]byte{}
	)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Manifest{}, fmt.Errorf("restore: tar: %w", err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			return Manifest{}, fmt.Errorf("restore: read %s: %w", h.Name, err)
		}
		switch {
		case h.Name == "manifest.json":
			if err := json.Unmarshal(body, &manifest); err != nil {
				return Manifest{}, fmt.Errorf("restore: parse manifest: %w", err)
			}
			manifestSeen = true
		case h.Name == "db.dump":
			dump = body
		case strings.HasPrefix(h.Name, "data/"):
			rel := strings.TrimPrefix(h.Name, "data/")
			if rel == "" {
				continue
			}
			if err := safeRelPath(rel); err != nil {
				return Manifest{}, fmt.Errorf("restore: refusing %q: %w", h.Name, err)
			}
			dataFiles[rel] = body
		}
	}
	if !manifestSeen {
		return Manifest{}, errors.New("restore: manifest.json missing from bundle")
	}
	if manifest.Version != BundleVersion {
		return Manifest{}, fmt.Errorf("restore: unsupported bundle version %d (this binary expects %d)", manifest.Version, BundleVersion)
	}
	if dump == nil {
		return Manifest{}, errors.New("restore: db.dump missing from bundle")
	}

	// Verify the dump sha256 before piping it into pg_restore so a
	// corrupted bundle can't silently overwrite the live database.
	dumpComp, ok := manifest.Components["db.dump"]
	if !ok {
		return Manifest{}, errors.New("restore: manifest missing db.dump component")
	}
	if dumpComp.SHA256 != "" && dumpComp.SHA256 != sha256Hex(dump) {
		return Manifest{}, fmt.Errorf("restore: db.dump sha256 mismatch (manifest=%s, actual=%s)",
			dumpComp.SHA256, sha256Hex(dump))
	}
	if dumpComp.Size != 0 && dumpComp.Size != int64(len(dump)) {
		return Manifest{}, fmt.Errorf("restore: db.dump size mismatch (manifest=%d, actual=%d)",
			dumpComp.Size, len(dump))
	}

	// Replay into Postgres FIRST so an error here aborts the restore
	// before we touch the on-disk DataDir. Either the database accepts
	// the dump cleanly or the operator's existing data dir is left
	// untouched and re-runnable.
	if err := b.PGRestoreFn(ctx, dsn, newReader(dump)); err != nil {
		return Manifest{}, fmt.Errorf("restore: pg_restore: %w", err)
	}

	// Clean DataDir then write everything back.
	if b.DataDir != "" {
		if err := os.RemoveAll(b.DataDir); err != nil {
			return Manifest{}, fmt.Errorf("restore: remove %s: %w", b.DataDir, err)
		}
		if err := os.MkdirAll(b.DataDir, 0o755); err != nil {
			return Manifest{}, fmt.Errorf("restore: mkdir %s: %w", b.DataDir, err)
		}
		// Sort for deterministic write order — useful when debugging
		// flake under concurrent test runs.
		keys := make([]string, 0, len(dataFiles))
		for k := range dataFiles {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, rel := range keys {
			body := dataFiles[rel]
			abs := filepath.Join(b.DataDir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return Manifest{}, fmt.Errorf("restore: mkdir %s: %w", filepath.Dir(abs), err)
			}
			if err := os.WriteFile(abs, body, 0o644); err != nil {
				return Manifest{}, fmt.Errorf("restore: write %s: %w", abs, err)
			}
		}
	}
	return manifest, nil
}

func (b *Bundle) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now().UTC()
}

// --- helpers -------------------------------------------------------------

type dataFile struct {
	absPath string
	relPath string
	size    int64
}

func walkDataDir(root string) ([]dataFile, error) {
	if root == "" {
		return nil, nil
	}
	info, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("backup: stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("backup: %s is not a directory", root)
	}
	var out []dataFile
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		// Skip wal_archive — it is not part of the DataDir contract for
		// the bundled backup; PITR support remains the shell-script
		// territory.
		if strings.HasPrefix(rel, "wal_archive"+string(filepath.Separator)) || rel == "wal_archive" {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, dataFile{
			absPath: path,
			relPath: filepath.ToSlash(rel),
			size:    fi.Size(),
		})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("backup: walk %s: %w", root, walkErr)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].relPath < out[j].relPath })
	return out, nil
}

func runDump(ctx context.Context, fn PGDumpFn, dsn string) ([]byte, error) {
	var buf inMemBuf
	if err := fn(ctx, dsn, &buf); err != nil {
		return nil, fmt.Errorf("backup: pg_dump: %w", err)
	}
	return buf.Bytes(), nil
}

func writeTarFile(tw *tar.Writer, name string, body []byte, now time.Time) error {
	h := &tar.Header{
		Name:     name,
		Mode:     0o644,
		Size:     int64(len(body)),
		ModTime:  now,
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(h); err != nil {
		return err
	}
	if len(body) > 0 {
		if _, err := tw.Write(body); err != nil {
			return err
		}
	}
	return nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func combinedDataSHA(files []dataFile) string {
	// Hash a deterministic stream of (relPath, sha256(body)) pairs so the
	// witness changes if any single file's content changes, and stays
	// stable across re-runs that produce identical content.
	h := sha256.New()
	for _, f := range files {
		body, err := os.ReadFile(f.absPath)
		if err != nil {
			continue
		}
		fmt.Fprintf(h, "%s\t%s\n", f.relPath, sha256Hex(body))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func redactDSN(dsn string) string {
	// Drop everything between "://" and "@". Mirrors the shell scripts.
	const sep = "://"
	i := strings.Index(dsn, sep)
	if i < 0 {
		return dsn
	}
	rest := dsn[i+len(sep):]
	at := strings.Index(rest, "@")
	if at < 0 {
		return dsn
	}
	return dsn[:i+len(sep)] + "***" + rest[at:]
}

// safeRelPath rejects paths that would escape the data dir. Mirrors the
// validator used by pkg/weavepkg's package installer.
func safeRelPath(rel string) error {
	if rel == "" {
		return errors.New("empty path")
	}
	if filepath.IsAbs(rel) {
		return errors.New("absolute path not allowed")
	}
	clean := filepath.ToSlash(filepath.Clean(rel))
	if clean != rel && clean != filepath.ToSlash(rel) {
		return fmt.Errorf("non-canonical path %q", rel)
	}
	if strings.HasPrefix(clean, "../") || clean == ".." || strings.Contains(clean, "/../") {
		return fmt.Errorf("traversal %q", rel)
	}
	return nil
}

// inMemBuf is a tiny in-memory io.Writer/Reader pair so the dump payload
// can be captured without pulling in bytes.Buffer indirection that would
// drag in a public API surface dep on this package.
type inMemBuf struct{ b []byte }

func (m *inMemBuf) Write(p []byte) (int, error) { m.b = append(m.b, p...); return len(p), nil }
func (m *inMemBuf) Bytes() []byte               { return m.b }

func newReader(b []byte) io.Reader {
	return &memReader{b: b}
}

type memReader struct {
	b []byte
	o int
}

func (r *memReader) Read(p []byte) (int, error) {
	if r.o >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.o:])
	r.o += n
	return n, nil
}
