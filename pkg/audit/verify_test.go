package audit

import (
	"context"
	"strings"
	"testing"
)

func TestParseRootFile_ReadsAllLines(t *testing.T) {
	input := "2026-04-16\taaaa\n2026-04-17\tbbbb\n"
	entries, err := ParseRootFile(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseRootFile: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Day != "2026-04-16" || entries[0].RootHash != "aaaa" {
		t.Errorf("entries[0] = %+v", entries[0])
	}
	if entries[1].Day != "2026-04-17" || entries[1].RootHash != "bbbb" {
		t.Errorf("entries[1] = %+v", entries[1])
	}
}

func TestParseRootFile_IgnoresBlankLines(t *testing.T) {
	input := "\n2026-04-16\taaaa\n\n\n2026-04-17\tbbbb\n\n"
	entries, err := ParseRootFile(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseRootFile: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestParseRootFile_RejectsMalformedLine(t *testing.T) {
	input := "2026-04-16 aaaa\n"
	_, err := ParseRootFile(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for space-delimited (non-tab) line")
	}
}

func TestVerifyRootFile_HappyPath(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_ = Record(ctx, store, AuditEvent{
			ActorID: "u", Action: "A", ResourceType: "T", ResourceRID: "ri.x",
		})
	}
	chain, _ := store.ListChain(ctx)
	root := ComputeRootHash(chain)

	// Group every event under a single day so the root-file entry matches.
	daily := map[string][]AuditEvent{"2026-04-18": chain}
	rootFile := "2026-04-18\t" + root + "\n"

	entries, _ := ParseRootFile(strings.NewReader(rootFile))
	if err := VerifyRootFile(entries, daily); err != nil {
		t.Fatalf("VerifyRootFile happy path: %v", err)
	}
}

func TestVerifyRootFile_DetectsTamperedRoot(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_ = Record(ctx, store, AuditEvent{
			ActorID: "u", Action: "A", ResourceType: "T", ResourceRID: "ri.x",
		})
	}
	chain, _ := store.ListChain(ctx)
	daily := map[string][]AuditEvent{"2026-04-18": chain}
	rootFile := "2026-04-18\t" + strings.Repeat("0", 64) + "\n"
	entries, _ := ParseRootFile(strings.NewReader(rootFile))

	err := VerifyRootFile(entries, daily)
	if err == nil {
		t.Fatal("expected root-hash mismatch to be detected")
	}
	if !strings.Contains(err.Error(), "2026-04-18") {
		t.Errorf("error should mention day: %v", err)
	}
}

func TestVerifyRootFile_MissingDayInDB(t *testing.T) {
	// Root file anchors a day we don't have any DB rows for — that's
	// evidence rows were DELETED after anchoring. VerifyRootFile must
	// surface this rather than silently skipping the missing key.
	rootFile := "2026-04-18\t" + strings.Repeat("a", 64) + "\n"
	entries, _ := ParseRootFile(strings.NewReader(rootFile))

	err := VerifyRootFile(entries, map[string][]AuditEvent{})
	if err == nil {
		t.Fatal("expected missing-day to be flagged")
	}
	if !strings.Contains(err.Error(), "2026-04-18") {
		t.Errorf("error should mention day: %v", err)
	}
}
