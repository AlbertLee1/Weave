package index_test

import (
	"testing"

	"github.com/liyang/weave/pkg/index"
)

// US-044: Bleve index keys must be scoped per ontology so that two ontologies
// containing an ObjectType with the same API name keep their data isolated.

func TestScopedKey_FormatsAsOntologyDoubleUnderscoreObjectType(t *testing.T) {
	got := index.ScopedKey("northwind", "Employee")
	want := "northwind__Employee"
	if got != want {
		t.Fatalf("ScopedKey(%q, %q) = %q, want %q", "northwind", "Employee", got, want)
	}
}

func TestScopedKey_TwoOntologiesYieldDifferentKeys(t *testing.T) {
	a := index.ScopedKey("northwind", "Order")
	b := index.ScopedKey("chinook", "Order")
	if a == b {
		t.Fatalf("expected ScopedKey to differ for two ontologies sharing an objectType apiName, got %q == %q", a, b)
	}
}

func TestManager_ScopedKeysAreIsolated(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	keyA := index.ScopedKey("northwind", "Order")
	keyB := index.ScopedKey("chinook", "Order")

	if _, err := mgr.EnsureIndex(keyA, sampleProperties()); err != nil {
		t.Fatalf("EnsureIndex(A): %v", err)
	}
	if _, err := mgr.EnsureIndex(keyB, sampleProperties()); err != nil {
		t.Fatalf("EnsureIndex(B): %v", err)
	}

	if err := mgr.IndexDocument(keyA, "1", map[string]interface{}{"name": "northwind-row"}); err != nil {
		t.Fatalf("IndexDocument(A): %v", err)
	}
	if err := mgr.IndexDocument(keyB, "1", map[string]interface{}{"name": "chinook-row"}); err != nil {
		t.Fatalf("IndexDocument(B): %v", err)
	}

	countA, err := mgr.DocCount(keyA)
	if err != nil {
		t.Fatalf("DocCount(A): %v", err)
	}
	countB, err := mgr.DocCount(keyB)
	if err != nil {
		t.Fatalf("DocCount(B): %v", err)
	}

	if countA != 1 || countB != 1 {
		t.Fatalf("expected each scoped index to hold exactly 1 doc, got A=%d B=%d", countA, countB)
	}
}
