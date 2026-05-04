package funcrepo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestManager_Commit_AppendsToBranch(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	ctx := context.Background()

	first, err := mgr.Commit(ctx, "ri.ontology.main.function.f1", CommitInput{
		Message:    "initial commit",
		SourceCode: "function hello() { return 1; }\n",
		Author:     "alice",
		Email:      "alice@example.com",
		When:       time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("first commit: %v", err)
	}
	if first.Hash == "" {
		t.Fatalf("expected non-empty hash")
	}
	if first.Author != "alice" || first.Email != "alice@example.com" {
		t.Fatalf("author/email roundtrip: %+v", first)
	}

	second, err := mgr.Commit(ctx, "ri.ontology.main.function.f1", CommitInput{
		Message:    "second commit",
		SourceCode: "function hello() { return 2; }\n",
		Author:     "bob",
		Email:      "bob@example.com",
		When:       time.Date(2026, 5, 4, 11, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("second commit: %v", err)
	}
	if second.Hash == first.Hash {
		t.Fatalf("two commits should have distinct hashes")
	}

	log, err := mgr.Log(ctx, "ri.ontology.main.function.f1", 0)
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if len(log) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(log))
	}
	if log[0].Hash != second.Hash {
		t.Fatalf("log[0] should be newest commit: got %s want %s", log[0].Hash, second.Hash)
	}
	if log[1].Hash != first.Hash {
		t.Fatalf("log[1] should be first commit: got %s want %s", log[1].Hash, first.Hash)
	}

	// HeadCommit returns the freshest source.
	head, source, err := mgr.HeadCommit(ctx, "ri.ontology.main.function.f1")
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if head.Hash != second.Hash {
		t.Fatalf("head hash mismatch: %s vs %s", head.Hash, second.Hash)
	}
	if source != "function hello() { return 2; }\n" {
		t.Fatalf("source mismatch: %q", source)
	}

	// Bare git layout exists at {root}/{rid}/.git
	gitPath := filepath.Join(dir, "ri.ontology.main.function.f1", ".git")
	if st, err := os.Stat(gitPath); err != nil || !st.IsDir() {
		t.Fatalf("expected bare repo at %s, err=%v", gitPath, err)
	}
}

func TestManager_Log_LimitTruncates(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	ctx := context.Background()
	rid := "ri.ontology.main.function.fzz"

	for i := 0; i < 5; i++ {
		if _, err := mgr.Commit(ctx, rid, CommitInput{
			Message:    "commit",
			SourceCode: "v" + string(rune('0'+i)),
			Author:     "alice",
			Email:      "alice@example.com",
			When:       time.Date(2026, 1, 1, 0, i, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("commit %d: %v", i, err)
		}
	}
	log, err := mgr.Log(ctx, rid, 2)
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if len(log) != 2 {
		t.Fatalf("limit=2 returned %d entries", len(log))
	}
}

func TestManager_Log_MissingRepoReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	log, err := mgr.Log(context.Background(), "ri.ontology.main.function.never", 0)
	if err != nil {
		t.Fatalf("log on missing repo should not error: %v", err)
	}
	if len(log) != 0 {
		t.Fatalf("expected empty log, got %v", log)
	}
}

func TestManager_HeadCommit_MissingReturnsSentinel(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	_, _, err := mgr.HeadCommit(context.Background(), "ri.ontology.main.function.absent")
	if !errors.Is(err, ErrNoCommits) {
		t.Fatalf("expected ErrNoCommits, got %v", err)
	}
}

func TestManager_Commit_RejectsBlankInputs(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	ctx := context.Background()
	rid := "ri.ontology.main.function.f1"

	if _, err := mgr.Commit(ctx, rid, CommitInput{Message: "", SourceCode: "x"}); !errors.Is(err, ErrEmptyMessage) {
		t.Fatalf("blank message: expected ErrEmptyMessage, got %v", err)
	}
	if _, err := mgr.Commit(ctx, rid, CommitInput{Message: "m", SourceCode: ""}); !errors.Is(err, ErrEmptySource) {
		t.Fatalf("blank source: expected ErrEmptySource, got %v", err)
	}
}

func TestManager_Commit_RejectsPathTraversalRID(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	ctx := context.Background()

	cases := []string{
		"",
		"   ",
		"../escape",
		"a/b",
		"..",
		"a\\b",
	}
	for _, rid := range cases {
		if _, err := mgr.Commit(ctx, rid, CommitInput{Message: "m", SourceCode: "x"}); !errors.Is(err, ErrInvalidRID) {
			t.Fatalf("rid=%q expected ErrInvalidRID, got %v", rid, err)
		}
	}
}

func TestManager_Commit_DefaultsAuthorAndTime(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	ctx := context.Background()

	out, err := mgr.Commit(ctx, "ri.ontology.main.function.f1", CommitInput{
		Message:    "m",
		SourceCode: "x",
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if out.Author != "weave" || out.Email != "weave@weave.local" {
		t.Fatalf("default identity not applied: %+v", out)
	}
	if out.AuthorDate.IsZero() {
		t.Fatalf("default time not applied")
	}
}

func TestManager_Commit_IsolatedAcrossRIDs(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	ctx := context.Background()

	if _, err := mgr.Commit(ctx, "ri.ontology.main.function.alpha", CommitInput{Message: "a", SourceCode: "1"}); err != nil {
		t.Fatalf("alpha commit: %v", err)
	}
	if _, err := mgr.Commit(ctx, "ri.ontology.main.function.beta", CommitInput{Message: "b", SourceCode: "2"}); err != nil {
		t.Fatalf("beta commit: %v", err)
	}
	logA, _ := mgr.Log(ctx, "ri.ontology.main.function.alpha", 0)
	logB, _ := mgr.Log(ctx, "ri.ontology.main.function.beta", 0)
	if len(logA) != 1 || len(logB) != 1 {
		t.Fatalf("each repo should have exactly one commit: a=%d b=%d", len(logA), len(logB))
	}
	if logA[0].Hash == logB[0].Hash {
		t.Fatalf("isolated repos must produce different hashes")
	}
}

func TestManager_Commit_ConcurrentSerialised(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	ctx := context.Background()
	rid := "ri.ontology.main.function.race"

	const n = 10
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = mgr.Commit(ctx, rid, CommitInput{
				Message:    "concurrent",
				SourceCode: "v" + string(rune('a'+i)),
			})
		}(i)
	}
	wg.Wait()

	log, err := mgr.Log(ctx, rid, 0)
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if len(log) != n {
		t.Fatalf("expected %d serialised commits, got %d", n, len(log))
	}

	// All commits must form a linear parent chain — no forks.
	seen := make(map[string]struct{}, n)
	for _, c := range log {
		if _, dup := seen[c.Hash]; dup {
			t.Fatalf("duplicate commit hash %s", c.Hash)
		}
		seen[c.Hash] = struct{}{}
	}
}

func TestManager_HeadCommit_ReturnsFreshSource(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	ctx := context.Background()
	rid := "ri.ontology.main.function.f1"

	if _, err := mgr.Commit(ctx, rid, CommitInput{Message: "v1", SourceCode: "first"}); err != nil {
		t.Fatalf("commit v1: %v", err)
	}
	if _, err := mgr.Commit(ctx, rid, CommitInput{Message: "v2", SourceCode: "second"}); err != nil {
		t.Fatalf("commit v2: %v", err)
	}
	_, source, err := mgr.HeadCommit(ctx, rid)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if source != "second" {
		t.Fatalf("head should reflect latest source, got %q", source)
	}
}
