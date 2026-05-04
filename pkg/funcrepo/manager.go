// Package funcrepo manages a per-Function bare git repository (US-415).
//
// Each Function RID maps to one bare repo at {root}/{rid}/.git. Commits are
// produced via go-git plumbing — the repo has no worktree, so callers hand
// in the new full source code and a message; the manager bundles the source
// as a single tree entry and updates refs/heads/main + HEAD atomically per
// commit.
//
// The package is HTTP-free: the OMS handler in pkg/oms wires the wire-format
// shim (POST /functions/{rid}/commits, GET /functions/{rid}/log) on top.
package funcrepo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// DefaultBranch is the canonical primary branch for every Function repo.
// Mirrors the convention used elsewhere in the codebase (US-381 / US-383).
const DefaultBranch = "main"

// DefaultFilename is the path inside the tree that carries the Function
// source code. A single-entry tree keeps the layout predictable for the CLI
// `weave fn pull/push` round-trip and for any future PR/diff UI (US-416).
const DefaultFilename = "function.js"

// Errors surfaced by Manager. Tests use errors.Is to assert the specific
// failure shape without scraping textual messages.
var (
	// ErrInvalidRID is returned when the caller supplies a RID that would
	// escape the root directory (path separators or `..` segments).
	ErrInvalidRID = errors.New("invalid function rid")
	// ErrEmptySource is returned when a commit would produce an empty
	// source code blob — repos must always carry at least one byte so a
	// future `pull` round-trip is unambiguous.
	ErrEmptySource = errors.New("source code must not be empty")
	// ErrEmptyMessage is returned when no commit message is supplied.
	ErrEmptyMessage = errors.New("commit message must not be empty")
	// ErrNoCommits is returned by HeadCommit / Log when the repo exists but
	// has no commits yet on the default branch.
	ErrNoCommits = errors.New("no commits on default branch")
)

// Manager is the package-level entry point. One Manager owns the entire
// `data/repos/` tree; per-RID locking guarantees a single in-flight
// Commit / Log call per repo so concurrent commits never produce a fork
// without a parent link.
type Manager struct {
	rootDir string

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewManager constructs a Manager rooted at the supplied directory. The
// directory is created lazily on first commit so a fresh deploy that never
// commits a Function leaves no on-disk artifact.
func NewManager(rootDir string) *Manager {
	return &Manager{
		rootDir: rootDir,
		locks:   make(map[string]*sync.Mutex),
	}
}

// RootDir is the on-disk root the manager writes under. Exposed for tests
// and CLI surfaces that need to introspect the layout.
func (m *Manager) RootDir() string { return m.rootDir }

// Commit represents one entry returned by Log / HeadCommit. The wire shape
// matches what the OMS handler renders so SDK clients see the same fields.
type Commit struct {
	Hash       string    `json:"hash"`
	Message    string    `json:"message"`
	Author     string    `json:"author"`
	Email      string    `json:"email"`
	AuthorDate time.Time `json:"authorDate"`
}

// CommitInput is the canonical request payload for Manager.Commit. Author /
// Email default to the system identity when blank so unauthenticated test
// surfaces and degraded-mode bootstraps still produce valid commits.
type CommitInput struct {
	Message    string
	SourceCode string
	Author     string
	Email      string
	When       time.Time // optional; zero value falls through to time.Now()
}

// repoLock returns (and lazily allocates) the per-RID mutex. Callers MUST
// release the returned lock with Unlock once the in-flight operation is
// done; the lock survives the call so adjacent callers serialise.
func (m *Manager) repoLock(rid string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l, ok := m.locks[rid]; ok {
		return l
	}
	l := &sync.Mutex{}
	m.locks[rid] = l
	return l
}

// repoPath returns the on-disk path to the bare git directory for the
// supplied RID. The bare data lives inside `.git/` so the parent
// `{root}/{rid}/` matches the layout the AC describes literally.
func (m *Manager) repoPath(rid string) (string, error) {
	if err := validateRID(rid); err != nil {
		return "", err
	}
	return filepath.Join(m.rootDir, rid, ".git"), nil
}

// Commit creates a new commit on refs/heads/main for the supplied RID. If
// the repo does not yet exist it is initialised lazily. The new commit
// chains onto the previous head (when present) so the log surfaces the
// full lineage.
//
// Returns the freshly-allocated Commit including its content-addressable
// hash. Caller-supplied empty source/message return typed sentinels rather
// than raw insert failures so the HTTP layer can map them to 400.
func (m *Manager) Commit(ctx context.Context, rid string, in CommitInput) (Commit, error) {
	if err := ctx.Err(); err != nil {
		return Commit{}, err
	}
	if strings.TrimSpace(in.Message) == "" {
		return Commit{}, ErrEmptyMessage
	}
	if in.SourceCode == "" {
		return Commit{}, ErrEmptySource
	}
	repoPath, err := m.repoPath(rid)
	if err != nil {
		return Commit{}, err
	}

	lock := m.repoLock(rid)
	lock.Lock()
	defer lock.Unlock()

	repo, err := openOrInitBare(repoPath)
	if err != nil {
		return Commit{}, fmt.Errorf("init repo: %w", err)
	}

	when := in.When
	if when.IsZero() {
		when = time.Now()
	}
	sig := object.Signature{
		Name:  defaultIfBlank(in.Author, "weave"),
		Email: defaultIfBlank(in.Email, "weave@weave.local"),
		When:  when,
	}

	// 1. Encode the source code blob.
	blobObj := repo.Storer.NewEncodedObject()
	blobObj.SetType(plumbing.BlobObject)
	bw, err := blobObj.Writer()
	if err != nil {
		return Commit{}, fmt.Errorf("blob writer: %w", err)
	}
	if _, err := bw.Write([]byte(in.SourceCode)); err != nil {
		_ = bw.Close()
		return Commit{}, fmt.Errorf("blob write: %w", err)
	}
	if err := bw.Close(); err != nil {
		return Commit{}, fmt.Errorf("blob close: %w", err)
	}
	blobHash, err := repo.Storer.SetEncodedObject(blobObj)
	if err != nil {
		return Commit{}, fmt.Errorf("store blob: %w", err)
	}

	// 2. Build the single-entry tree.
	tree := &object.Tree{
		Entries: []object.TreeEntry{{
			Name: DefaultFilename,
			Mode: filemode.Regular,
			Hash: blobHash,
		}},
	}
	treeObj := repo.Storer.NewEncodedObject()
	if err := tree.Encode(treeObj); err != nil {
		return Commit{}, fmt.Errorf("encode tree: %w", err)
	}
	treeHash, err := repo.Storer.SetEncodedObject(treeObj)
	if err != nil {
		return Commit{}, fmt.Errorf("store tree: %w", err)
	}

	// 3. Look up the previous head (if any) so we can chain on top.
	var parents []plumbing.Hash
	branchRef := plumbing.NewBranchReferenceName(DefaultBranch)
	if ref, err := repo.Reference(branchRef, true); err == nil && ref != nil {
		parents = []plumbing.Hash{ref.Hash()}
	}

	// 4. Encode the commit object.
	commit := &object.Commit{
		Author:       sig,
		Committer:    sig,
		Message:      in.Message,
		TreeHash:     treeHash,
		ParentHashes: parents,
	}
	commitObj := repo.Storer.NewEncodedObject()
	if err := commit.Encode(commitObj); err != nil {
		return Commit{}, fmt.Errorf("encode commit: %w", err)
	}
	commitHash, err := repo.Storer.SetEncodedObject(commitObj)
	if err != nil {
		return Commit{}, fmt.Errorf("store commit: %w", err)
	}

	// 5. Move refs/heads/main to the new commit. Set HEAD as a symbolic
	//    reference to refs/heads/main so a downstream `git clone` of the
	//    bare directory checks out the same branch by default.
	if err := repo.Storer.SetReference(plumbing.NewHashReference(branchRef, commitHash)); err != nil {
		return Commit{}, fmt.Errorf("update branch ref: %w", err)
	}
	if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, branchRef)); err != nil {
		return Commit{}, fmt.Errorf("update HEAD: %w", err)
	}

	return Commit{
		Hash:       commitHash.String(),
		Message:    in.Message,
		Author:     sig.Name,
		Email:      sig.Email,
		AuthorDate: when,
	}, nil
}

// Log returns commits in newest-first order on refs/heads/main, capped at
// `limit` (limit <= 0 disables the cap). When the repo or branch does not
// yet exist Log returns an empty slice rather than an error so SDK
// consumers can surface "no history yet" without special-casing 404s.
func (m *Manager) Log(ctx context.Context, rid string, limit int) ([]Commit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repoPath, err := m.repoPath(rid)
	if err != nil {
		return nil, err
	}

	lock := m.repoLock(rid)
	lock.Lock()
	defer lock.Unlock()

	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		return []Commit{}, nil
	} else if err != nil {
		return nil, err
	}
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}
	branchRef := plumbing.NewBranchReferenceName(DefaultBranch)
	ref, err := repo.Reference(branchRef, true)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return []Commit{}, nil
		}
		return nil, fmt.Errorf("resolve branch: %w", err)
	}

	iter, err := repo.Log(&git.LogOptions{From: ref.Hash()})
	if err != nil {
		return nil, fmt.Errorf("walk log: %w", err)
	}
	defer iter.Close()

	out := make([]Commit, 0, 16)
	walkErr := iter.ForEach(func(c *object.Commit) error {
		if limit > 0 && len(out) >= limit {
			return storer.ErrStop
		}
		out = append(out, Commit{
			Hash:       c.Hash.String(),
			Message:    c.Message,
			Author:     c.Author.Name,
			Email:      c.Author.Email,
			AuthorDate: c.Author.When,
		})
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, storer.ErrStop) {
		return nil, fmt.Errorf("iterate log: %w", walkErr)
	}
	return out, nil
}

// ErrCommitNotFound is returned by GetCommit when the supplied hash does
// not resolve to a commit object on the repo. Callers map this to 404 at
// the HTTP layer.
var ErrCommitNotFound = errors.New("commit not found")

// GetCommit returns the commit identified by `hash` and the source blob it
// points at on the canonical single-file tree. The hash must be a full
// 40-character hex string (the same form `Log` and `HeadCommit` return).
// ErrNoCommits is returned when the repo / branch is missing entirely;
// ErrCommitNotFound is returned when the hash does not resolve.
//
// This is the read counterpart of `Commit` used by the diff UI (US-416)
// to fetch the source code at any historical revision so two arbitrary
// commits can be diffed against each other without re-walking the whole
// log on the client.
func (m *Manager) GetCommit(ctx context.Context, rid, hash string) (Commit, string, error) {
	if err := ctx.Err(); err != nil {
		return Commit{}, "", err
	}
	if strings.TrimSpace(hash) == "" {
		return Commit{}, "", ErrCommitNotFound
	}
	repoPath, err := m.repoPath(rid)
	if err != nil {
		return Commit{}, "", err
	}

	lock := m.repoLock(rid)
	lock.Lock()
	defer lock.Unlock()

	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		return Commit{}, "", ErrNoCommits
	} else if err != nil {
		return Commit{}, "", err
	}
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return Commit{}, "", fmt.Errorf("open repo: %w", err)
	}
	commitHash := plumbing.NewHash(hash)
	if commitHash.IsZero() {
		return Commit{}, "", ErrCommitNotFound
	}
	commit, err := repo.CommitObject(commitHash)
	if err != nil {
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			return Commit{}, "", ErrCommitNotFound
		}
		return Commit{}, "", fmt.Errorf("load commit: %w", err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return Commit{}, "", fmt.Errorf("load tree: %w", err)
	}
	source := ""
	for _, entry := range tree.Entries {
		if entry.Name != DefaultFilename {
			continue
		}
		blob, err := repo.BlobObject(entry.Hash)
		if err != nil {
			return Commit{}, "", fmt.Errorf("load blob: %w", err)
		}
		reader, err := blob.Reader()
		if err != nil {
			return Commit{}, "", fmt.Errorf("blob reader: %w", err)
		}
		buf := make([]byte, blob.Size)
		_, readErr := readFull(reader, buf)
		_ = reader.Close()
		if readErr != nil {
			return Commit{}, "", fmt.Errorf("blob read: %w", readErr)
		}
		source = string(buf)
		break
	}
	return Commit{
		Hash:       commit.Hash.String(),
		Message:    commit.Message,
		Author:     commit.Author.Name,
		Email:      commit.Author.Email,
		AuthorDate: commit.Author.When,
	}, source, nil
}

// HeadCommit returns the most recent commit on the default branch and the
// source-code blob attached to it. ErrNoCommits is returned when the repo
// exists but has no commits yet (or doesn't exist at all) — callers that
// want "fall through to the canonical Function row" semantics should
// detect that sentinel and skip the repo lookup.
func (m *Manager) HeadCommit(ctx context.Context, rid string) (Commit, string, error) {
	if err := ctx.Err(); err != nil {
		return Commit{}, "", err
	}
	repoPath, err := m.repoPath(rid)
	if err != nil {
		return Commit{}, "", err
	}

	lock := m.repoLock(rid)
	lock.Lock()
	defer lock.Unlock()

	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		return Commit{}, "", ErrNoCommits
	} else if err != nil {
		return Commit{}, "", err
	}
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return Commit{}, "", fmt.Errorf("open repo: %w", err)
	}
	branchRef := plumbing.NewBranchReferenceName(DefaultBranch)
	ref, err := repo.Reference(branchRef, true)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return Commit{}, "", ErrNoCommits
		}
		return Commit{}, "", fmt.Errorf("resolve branch: %w", err)
	}
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return Commit{}, "", fmt.Errorf("load commit: %w", err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return Commit{}, "", fmt.Errorf("load tree: %w", err)
	}
	source := ""
	for _, entry := range tree.Entries {
		if entry.Name != DefaultFilename {
			continue
		}
		blob, err := repo.BlobObject(entry.Hash)
		if err != nil {
			return Commit{}, "", fmt.Errorf("load blob: %w", err)
		}
		reader, err := blob.Reader()
		if err != nil {
			return Commit{}, "", fmt.Errorf("blob reader: %w", err)
		}
		buf := make([]byte, blob.Size)
		_, readErr := readFull(reader, buf)
		_ = reader.Close()
		if readErr != nil {
			return Commit{}, "", fmt.Errorf("blob read: %w", readErr)
		}
		source = string(buf)
		break
	}
	return Commit{
		Hash:       commit.Hash.String(),
		Message:    commit.Message,
		Author:     commit.Author.Name,
		Email:      commit.Author.Email,
		AuthorDate: commit.Author.When,
	}, source, nil
}

// openOrInitBare returns a go-git Repository handle for the supplied path,
// initialising a new bare repo when nothing exists yet. The function is
// idempotent: calling it twice on the same path returns the existing repo
// without clobbering state.
func openOrInitBare(path string) (*git.Repository, error) {
	if _, err := os.Stat(path); err == nil {
		return git.PlainOpen(path)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir parent: %w", err)
	}
	return git.PlainInit(path, true)
}

// validateRID rejects empty / path-escaping inputs so a hostile rid (e.g.
// `../../etc/passwd`) cannot escape the root directory. This mirrors the
// validator pattern used by pkg/weavepkg/validateMigrationFilename.
func validateRID(rid string) error {
	if strings.TrimSpace(rid) == "" {
		return fmt.Errorf("%w: empty", ErrInvalidRID)
	}
	if rid != filepath.Clean(rid) {
		return fmt.Errorf("%w: %q", ErrInvalidRID, rid)
	}
	if strings.Contains(rid, "..") {
		return fmt.Errorf("%w: %q", ErrInvalidRID, rid)
	}
	if strings.ContainsAny(rid, "/\\") {
		return fmt.Errorf("%w: %q", ErrInvalidRID, rid)
	}
	return nil
}

func defaultIfBlank(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// readFull reads len(buf) bytes from r. We avoid io.ReadFull's dependency
// here so the package compiles cleanly without an extra import in tight
// callers; the loop handles short reads from go-git's blob readers.
func readFull(r interface{ Read(p []byte) (int, error) }, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			if total >= len(buf) {
				return total, nil
			}
			return total, err
		}
	}
	return total, nil
}
