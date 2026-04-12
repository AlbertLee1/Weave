package funnel

import (
	"context"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"

	"github.com/liyang/weave/pkg/index"
)

// TestApplyBatch_WritesMarkingsKeywordField — US-051 Red:
// The funnel consumer must persist Edit.Markings into Bleve under the
// reserved keyword field "_markings" so the policy engine's auto-marking
// clause can AND-combine a TermQuery against the same field. The write
// path goes through ApplyBatch -> applyBatchEdits -> ApplyBatch(index)
// so both CREATE and MODIFY produce a doc whose "_markings" field is
// searchable by an exact-case TermQuery.
func TestApplyBatch_WritesMarkingsKeywordField(t *testing.T) {
	consumer, mgr := setupTestConsumer(t)

	scoped := index.ScopedKey(testOntology, "employee")

	batch := EditBatch{
		ID:              "batch-markings-create",
		OntologyAPIName: testOntology,
		Timestamp:       time.Now(),
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-1",
				Properties: map[string]interface{}{
					"employeeId": "emp-1",
					"name":       "Alice",
				},
				Markings: []string{"ACME", "TOP_SECRET"},
			},
		},
	}
	if err := consumer.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch create: %v", err)
	}

	searchTerm := func(term string) uint64 {
		q := bleve.NewTermQuery(term)
		q.SetField("_markings")
		req := bleve.NewSearchRequest(q)
		req.Size = 10
		res, err := mgr.Search(scoped, req)
		if err != nil {
			t.Fatalf("search _markings=%s: %v", term, err)
		}
		return res.Total
	}

	if got := searchTerm("ACME"); got != 1 {
		t.Errorf("_markings=ACME hits = %d, want 1", got)
	}
	if got := searchTerm("TOP_SECRET"); got != 1 {
		t.Errorf("_markings=TOP_SECRET hits = %d, want 1", got)
	}
	// Wrong case must NOT match (keyword field, not text).
	if got := searchTerm("acme"); got != 0 {
		t.Errorf("_markings=acme (wrong case) hits = %d, want 0", got)
	}

	// MODIFY must replace the marking set — emp-1 loses TOP_SECRET.
	modify := EditBatch{
		ID:              "batch-markings-modify",
		OntologyAPIName: testOntology,
		Timestamp:       time.Now(),
		Edits: []Edit{
			{
				Type:       EditTypeModify,
				ObjectType: "employee",
				PrimaryKey: "emp-1",
				Properties: map[string]interface{}{
					"employeeId": "emp-1",
					"name":       "Alice",
				},
				Markings: []string{"ACME"},
			},
		},
	}
	if err := consumer.ApplyBatch(context.Background(), modify); err != nil {
		t.Fatalf("ApplyBatch modify: %v", err)
	}
	if got := searchTerm("ACME"); got != 1 {
		t.Errorf("post-modify _markings=ACME hits = %d, want 1", got)
	}
	if got := searchTerm("TOP_SECRET"); got != 0 {
		t.Errorf("post-modify _markings=TOP_SECRET hits = %d, want 0", got)
	}
}

// TestApplyBatch_NoMarkingsLeavesFieldAbsent — regression guard:
// Edits without any Markings must NOT emit an empty _markings key so that
// (a) queries against _markings:* stay fast, and (b) the auto-marking
// clause's "public doc = no marking" semantics is not confused by an
// empty-array value.
func TestApplyBatch_NoMarkingsLeavesFieldAbsent(t *testing.T) {
	consumer, mgr := setupTestConsumer(t)
	scoped := index.ScopedKey(testOntology, "employee")

	batch := EditBatch{
		ID:              "batch-nomarkings",
		OntologyAPIName: testOntology,
		Timestamp:       time.Now(),
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-2",
				Properties: map[string]interface{}{
					"employeeId": "emp-2",
					"name":       "Bob",
				},
			},
		},
	}
	if err := consumer.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}

	// Fetch the doc and assert _markings is not present in stored fields.
	q := bleve.NewDocIDQuery([]string{"emp-2"})
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"*"}
	req.Size = 1
	res, err := mgr.Search(scoped, req)
	if err != nil {
		t.Fatalf("search by id: %v", err)
	}
	if res.Total != 1 {
		t.Fatalf("expected 1 hit, got %d", res.Total)
	}
	if v, ok := res.Hits[0].Fields["_markings"]; ok {
		t.Errorf("_markings stored on doc without markings: %v", v)
	}
}
