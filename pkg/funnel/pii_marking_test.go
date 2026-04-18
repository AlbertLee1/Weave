package funnel

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/security/pii"
)

// stubPIIDetector lets a test pin which property maps fire as PII so
// the test never depends on the real regex behaviour — that's covered
// by pkg/security/pii's own unit tests.
type stubPIIDetector struct {
	hit  bool
	hits []map[string]interface{}
}

func (s *stubPIIDetector) DetectPII(properties map[string]interface{}) bool {
	s.hits = append(s.hits, properties)
	return s.hit
}

// readDocMarkings returns the `_markings` field for emp/pk after a write.
func readDocMarkings(t *testing.T, mgr *index.Manager, pk string) []string {
	t.Helper()
	q := bleve.NewDocIDQuery([]string{pk})
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"*"}
	req.Size = 1
	res, err := mgr.Search(index.ScopedKey(testOntology, "employee"), req)
	if err != nil {
		t.Fatalf("search %s: %v", pk, err)
	}
	if res.Total == 0 {
		t.Fatalf("expected hit for %s", pk)
	}
	out := decodeMarkings(map[string]interface{}{"_markings": res.Hits[0].Fields["_markings"]})
	sort.Strings(out)
	return out
}

// TestPIIAutoTag_AddsMarkingOnDetection verifies that a CREATE edit
// whose Properties trigger the detector lands in Bleve with the PII
// marking attached, even though the writer didn't set Markings.
func TestPIIAutoTag_AddsMarkingOnDetection(t *testing.T) {
	consumer, mgr := setupTestConsumer(t)
	consumer.SetPIIDetector(&stubPIIDetector{hit: true})

	batch := EditBatch{
		ID:              "b-1",
		OntologyAPIName: testOntology,
		Timestamp:       time.Now(),
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-1",
				Properties: map[string]interface{}{
					"employeeId": "emp-1",
					"name":       "alice@example.com",
				},
			},
		},
	}
	if err := consumer.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}

	got := readDocMarkings(t, mgr, "emp-1")
	if len(got) != 1 || got[0] != "PII" {
		t.Errorf("PII auto-tag failed: got %v, want [PII]", got)
	}
}

// TestPIIAutoTag_NoDetectionLeavesMarkingsAbsent verifies that an edit
// whose detector returns false leaves the indexed doc with no
// `_markings` field — the consumer must not auto-write empty marking
// arrays (which would defeat the "no marking = public" semantics).
func TestPIIAutoTag_NoDetectionLeavesMarkingsAbsent(t *testing.T) {
	consumer, mgr := setupTestConsumer(t)
	consumer.SetPIIDetector(&stubPIIDetector{hit: false})

	batch := EditBatch{
		ID:              "b-2",
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

	q := bleve.NewDocIDQuery([]string{"emp-2"})
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"*"}
	req.Size = 1
	res, err := mgr.Search(index.ScopedKey(testOntology, "employee"), req)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if res.Total != 1 {
		t.Fatalf("expected 1 hit, got %d", res.Total)
	}
	if v, ok := res.Hits[0].Fields["_markings"]; ok {
		t.Errorf("expected no _markings for non-PII doc, got %v", v)
	}
}

// TestPIIAutoTag_PreservesExistingMarkings ensures the auto-tag merges
// PII into an existing marking set (deduplicating, not replacing).
func TestPIIAutoTag_PreservesExistingMarkings(t *testing.T) {
	consumer, mgr := setupTestConsumer(t)
	consumer.SetPIIDetector(&stubPIIDetector{hit: true})

	batch := EditBatch{
		ID:              "b-3",
		OntologyAPIName: testOntology,
		Timestamp:       time.Now(),
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-3",
				Properties: map[string]interface{}{
					"employeeId": "emp-3",
					"name":       "Carol",
				},
				Markings: []string{"CONFIDENTIAL"},
			},
		},
	}
	if err := consumer.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}

	got := readDocMarkings(t, mgr, "emp-3")
	want := []string{"CONFIDENTIAL", "PII"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("expected markings %v, got %v", want, got)
	}
}

// TestPIIAutoTag_NoDuplicatePIIMarking guards against a case where the
// writer already set the PII marking and the detector also fires —
// the resulting Markings slice must NOT carry "PII" twice.
func TestPIIAutoTag_NoDuplicatePIIMarking(t *testing.T) {
	consumer, mgr := setupTestConsumer(t)
	consumer.SetPIIDetector(&stubPIIDetector{hit: true})

	batch := EditBatch{
		ID:              "b-4",
		OntologyAPIName: testOntology,
		Timestamp:       time.Now(),
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-4",
				Properties: map[string]interface{}{
					"employeeId": "emp-4",
					"name":       "Dan",
				},
				Markings: []string{"PII"},
			},
		},
	}
	if err := consumer.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}

	got := readDocMarkings(t, mgr, "emp-4")
	if len(got) != 1 || got[0] != "PII" {
		t.Errorf("expected single PII marking, got %v", got)
	}
}

// TestPIIAutoTag_NilDetectorIsNoop confirms the unwired path leaves
// edits alone — same shape as every other optional consumer hook.
func TestPIIAutoTag_NilDetectorIsNoop(t *testing.T) {
	consumer, mgr := setupTestConsumer(t)
	// No SetPIIDetector call.

	batch := EditBatch{
		ID:              "b-5",
		OntologyAPIName: testOntology,
		Timestamp:       time.Now(),
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-5",
				Properties: map[string]interface{}{
					"employeeId": "emp-5",
					"name":       "alice@example.com",
				},
			},
		},
	}
	if err := consumer.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}

	q := bleve.NewDocIDQuery([]string{"emp-5"})
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"*"}
	req.Size = 1
	res, err := mgr.Search(index.ScopedKey(testOntology, "employee"), req)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if v, ok := res.Hits[0].Fields["_markings"]; ok {
		t.Errorf("nil detector should leave _markings absent, got %v", v)
	}
}

// TestPIIAutoTag_DeleteEditsAreSkipped ensures the detector is never
// consulted for DELETE edits (DELETE has no Properties to scan).
func TestPIIAutoTag_DeleteEditsAreSkipped(t *testing.T) {
	consumer, mgr := setupTestConsumer(t)

	// Seed a row first so the DELETE has something to remove.
	create := EditBatch{
		ID:              "b-seed",
		OntologyAPIName: testOntology,
		Timestamp:       time.Now(),
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-6",
				Properties: map[string]interface{}{"employeeId": "emp-6", "name": "Eve"},
			},
		},
	}
	if err := consumer.ApplyBatch(context.Background(), create); err != nil {
		t.Fatalf("seed: %v", err)
	}

	det := &stubPIIDetector{hit: true}
	consumer.SetPIIDetector(det)

	del := EditBatch{
		ID:              "b-del",
		OntologyAPIName: testOntology,
		Timestamp:       time.Now(),
		Edits: []Edit{
			{
				Type:       EditTypeDelete,
				ObjectType: "employee",
				PrimaryKey: "emp-6",
			},
		},
	}
	if err := consumer.ApplyBatch(context.Background(), del); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if len(det.hits) != 0 {
		t.Errorf("detector should not be called for DELETE, got %d call(s)", len(det.hits))
	}

	// Doc should be gone.
	q := bleve.NewDocIDQuery([]string{"emp-6"})
	req := bleve.NewSearchRequest(q)
	req.Size = 1
	res, err := mgr.Search(index.ScopedKey(testOntology, "employee"), req)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if res.Total != 0 {
		t.Fatalf("expected emp-6 deleted, got %d hits", res.Total)
	}
}

// TestPIIAutoTag_RealScannerIntegration is a thin smoke test that
// wires the real pkg/security/pii.Scanner in place of the stub so we
// catch end-to-end regressions if either side's marking field name
// drifts. Property values trigger every detector category at least
// once.
func TestPIIAutoTag_RealScannerIntegration(t *testing.T) {
	consumer, mgr := setupTestConsumer(t)
	consumer.SetPIIDetector(pii.NewScanner())

	batch := EditBatch{
		ID:              "b-real",
		OntologyAPIName: testOntology,
		Timestamp:       time.Now(),
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-real",
				Properties: map[string]interface{}{
					"employeeId": "emp-real",
					"name":       "Frank Smith — frank@example.com",
				},
			},
		},
	}
	if err := consumer.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}

	got := readDocMarkings(t, mgr, "emp-real")
	if len(got) != 1 || got[0] != pii.PIIMarkingName {
		t.Errorf("real scanner integration: got markings %v, want [%s]", got, pii.PIIMarkingName)
	}
}
