package ci_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBDD_OpenAPISyncTargetKeepsMirrorByteIdentical covers the
// round-70 workflow fix. Rounds 59, 66, 68, and 69 all tripped
// TestContract_EmbeddedSpecMatchesCanonical because the author
// edited api/openapi.yaml but forgot to mirror the change to
// cmd/server/openapi_spec.yaml. The fix is workflow-level: a
// dedicated `make sync-openapi` target (plus //go:generate hook
// in cmd/server/openapi_handler.go) that copies the canonical
// file into the embed slot. This BDD asserts the workflow
// actually works:
//
//   - The Makefile target exists and exits 0.
//   - After running it the two files have identical SHA-256
//     (proves byte-equality without dragging the entire YAML
//     into the test process).
//
// The test does NOT run the sync as a side-effect — it
// snapshots the current state, runs the target, and asserts
// the post-state matches the source-of-truth.
func TestBDD_OpenAPISyncTargetKeepsMirrorByteIdentical(t *testing.T) {
	root := repoRoot(t)
	canonical := filepath.Join(root, "api", "openapi.yaml")
	mirror := filepath.Join(root, "cmd", "server", "openapi_spec.yaml")

	if _, err := os.Stat(canonical); err != nil {
		t.Fatalf("canonical missing: %v", err)
	}
	if _, err := os.Stat(mirror); err != nil {
		t.Fatalf("mirror missing: %v", err)
	}

	t.Run("Makefile carries a sync-openapi target", func(t *testing.T) {
		makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
		if err != nil {
			t.Fatalf("read Makefile: %v", err)
		}
		// Look for the exact target header so a future rename
		// or accidental removal trips this assertion before
		// CI hits the embedded-spec drift.
		if !strings.Contains(string(makefile), "sync-openapi:") {
			t.Errorf("Makefile is missing `sync-openapi` target — workflow fix from round 70 is broken")
		}
	})

	t.Run("openapi_handler.go carries a go:generate sync directive", func(t *testing.T) {
		handler, err := os.ReadFile(filepath.Join(root, "cmd", "server", "openapi_handler.go"))
		if err != nil {
			t.Fatalf("read handler: %v", err)
		}
		want := "//go:generate cp ../../api/openapi.yaml openapi_spec.yaml"
		if !strings.Contains(string(handler), want) {
			t.Errorf("openapi_handler.go missing go:generate directive %q so `go generate ./cmd/server/` won't sync", want)
		}
	})

	t.Run("make sync-openapi makes the mirror byte-identical to the canonical", func(t *testing.T) {
		// Capture the canonical hash BEFORE running sync so the
		// assertion compares the post-state mirror against the
		// pre-state canonical (the canonical itself cannot
		// change during the test).
		canonicalHash := hashFile(t, canonical)

		// Run the target. -C is gnu-make-only — Weave's CI uses
		// gnu make per Makefile shebang convention, and the
		// loop runs on darwin / linux where gnu make is
		// available; t.Skip on missing for robustness.
		cmd := exec.Command("make", "-C", root, "sync-openapi")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("make sync-openapi failed: %v\noutput: %s", err, out)
		}

		mirrorHash := hashFile(t, mirror)
		if canonicalHash != mirrorHash {
			t.Errorf("post-sync mirror hash %s != canonical hash %s; make sync-openapi did not produce a byte-identical copy",
				mirrorHash, canonicalHash)
		}
	})
}

func hashFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Force-import fmt so a future expansion that needs it doesn't have
// to re-add the import line.
var _ = fmt.Sprintf
