package index_test

import (
	"context"
	"os"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/liyang/weave/pkg/index"
)

// TestBDD_Rehydrate_KillBleveDirAndRebuildFromSource is the round-146
// closing of Gap-R3: the PRD called for a "wipe the Bleve dir → restart →
// rebuild from PG/Parquet → query results identical to pre-wipe" end-to-
// end scenario. The RebuildWithOptions plumbing has been in place since
// US-408, but no test wired the actual "directory disappears under us"
// disaster path through it. This BDD test pins that contract — the
// rehydrate path must be fully recoverable from the source-of-truth and
// queries must return semantically equivalent results.
//
// Scenario (Given / When / Then):
//
//	Given a Bleve index manager with 3 Customer rows already in the
//	  "northwind" ontology (newRebuildFixture seeds ALFKI/USA +
//	  ANATR/Mexico + ANTON/Mexico), and the operator can query by
//	  country.
//	When  disaster strikes — the operator closes the manager and wipes
//	  the entire WEAVE_DATA_DIR via os.RemoveAll, then a fresh manager
//	  opens against the same path with no on-disk indexes.
//	Then  a rebuild via RebuildWithOptions repopulates the same scoped
//	  key from the source, IndexedCount equals the original count, the
//	  ScopedKey is identical, and the same country=Mexico / country=USA
//	  term queries return the same hit counts as before the wipe.
func TestBDD_Rehydrate_KillBleveDirAndRebuildFromSource(t *testing.T) {
	dataDir := t.TempDir()
	repo, src := newRebuildFixture()
	scopedKey := index.ScopedKey("northwind", "Customer")
	req := index.RebuildRequest{OntologyAPIName: "northwind", ObjectTypeAPIName: "Customer"}
	ctx := context.Background()

	// --- Given: bring the index up via a normal rebuild ----------------
	mgr := index.NewManager(dataDir)
	res, err := index.RebuildWithOptions(ctx, mgr, repo, src, req, index.RebuildOptions{})
	if err != nil {
		t.Fatalf("initial RebuildWithOptions: %v", err)
	}
	if res.IndexedCount != 3 {
		t.Fatalf("initial IndexedCount = %d, want 3", res.IndexedCount)
	}

	beforeMexico := countByCountryRehydrate(t, mgr, scopedKey, "Mexico")
	beforeUSA := countByCountryRehydrate(t, mgr, scopedKey, "USA")
	if beforeMexico != 2 {
		t.Fatalf("baseline country=Mexico hits = %d, want 2", beforeMexico)
	}
	if beforeUSA != 1 {
		t.Fatalf("baseline country=USA hits = %d, want 1", beforeUSA)
	}

	// --- When: disaster — close mgr and wipe the whole Bleve dir. -----
	if err := mgr.Close(); err != nil {
		t.Fatalf("close pre-wipe mgr: %v", err)
	}
	if err := os.RemoveAll(dataDir); err != nil {
		t.Fatalf("wipe dataDir: %v", err)
	}
	// Recreate the dataDir as an empty mount — mirrors how a k8s
	// operator would mount a fresh PV after losing the old one.
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("recreate empty dataDir: %v", err)
	}

	// --- Then: fresh mgr + rebuild reaches the same state -------------
	mgr2 := index.NewManager(dataDir)
	defer mgr2.Close()

	res2, err := index.RebuildWithOptions(ctx, mgr2, repo, src, req, index.RebuildOptions{})
	if err != nil {
		t.Fatalf("recovery RebuildWithOptions: %v", err)
	}
	if res2.IndexedCount != res.IndexedCount {
		t.Fatalf("recovery IndexedCount = %d, want %d (equivalent to baseline)",
			res2.IndexedCount, res.IndexedCount)
	}
	if res2.ScopedKey != res.ScopedKey {
		t.Errorf("ScopedKey drift across rebuilds: pre=%q post=%q",
			res.ScopedKey, res2.ScopedKey)
	}

	afterMexico := countByCountryRehydrate(t, mgr2, scopedKey, "Mexico")
	afterUSA := countByCountryRehydrate(t, mgr2, scopedKey, "USA")
	if afterMexico != beforeMexico {
		t.Errorf("post-recovery country=Mexico = %d, want %d (equivalent to baseline)",
			afterMexico, beforeMexico)
	}
	if afterUSA != beforeUSA {
		t.Errorf("post-recovery country=USA = %d, want %d (equivalent to baseline)",
			afterUSA, beforeUSA)
	}
}

// countByCountryRehydrate searches the scoped index by the not_analyzed
// "country" field and returns the hit count. Mirrors the term-query
// pattern used by rebuild_test.go so disaster-recovery semantics stay
// directly comparable with the in-place rebuild path.
func countByCountryRehydrate(t *testing.T, mgr *index.Manager, scopedKey, country string) int {
	t.Helper()
	q := bleve.NewTermQuery(country)
	q.SetField("country")
	res, err := mgr.Search(scopedKey, bleve.NewSearchRequest(q))
	if err != nil {
		t.Fatalf("Search country=%q: %v", country, err)
	}
	return int(res.Total)
}
