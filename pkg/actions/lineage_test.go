package actions

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// fakeLineageStore captures the edges written by the executor so tests can
// assert against them without touching PG. Concurrent-safe because the
// executor's side-effect goroutines (US-241 progress) and the synchronous
// commit path may both invoke InsertLineageEdge in future variants.
type fakeLineageStore struct {
	mu    sync.Mutex
	edges []oms.LineageEdge
	err   error
}

func (f *fakeLineageStore) InsertLineageEdge(_ context.Context, edge *oms.LineageEdge) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	cp := *edge
	f.edges = append(f.edges, cp)
	return nil
}

func (f *fakeLineageStore) ListUpstreamLineage(_ context.Context, downstream string, _ int) ([]oms.LineageEdge, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []oms.LineageEdge
	for _, e := range f.edges {
		if e.DownstreamRID == downstream {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeLineageStore) ListDownstreamLineage(_ context.Context, upstream string, _ int) ([]oms.LineageEdge, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []oms.LineageEdge
	for _, e := range f.edges {
		if e.UpstreamRID == upstream {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeLineageStore) snapshot() []oms.LineageEdge {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]oms.LineageEdge, len(f.edges))
	copy(out, f.edges)
	return out
}

// TestExecutor_Apply_RecordsLineageEdge verifies that a single Apply with
// a MODIFY rule writes one lineage edge whose upstream is the action log
// RID and downstream is the object's lineage RID.
func TestExecutor_Apply_RecordsLineageEdge(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("renameEmployee", []ParameterDef{
				{ID: "primaryKey", Type: "string", Required: true},
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{
					Type:       "modifyObject",
					ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					},
				},
			}),
		},
	}
	exec := NewExecutor(repo, nil)
	store := &fakeLineageStore{}
	exec.SetLineageStore(store)

	result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "renameEmployee",
		Parameters: map[string]interface{}{"primaryKey": "EMP-001", "name": "Alice"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := store.snapshot()
	if len(got) != 1 {
		t.Fatalf("expected 1 lineage edge, got %d", len(got))
	}
	edge := got[0]
	if edge.Operation != string(funnel.EditTypeModify) {
		t.Errorf("expected operation %q, got %q", funnel.EditTypeModify, edge.Operation)
	}
	if len(result.Edits) != 1 {
		t.Fatalf("expected 1 edit on the result, got %d", len(result.Edits))
	}
	wantDownstream := oms.ObjectLineageRID(result.Edits[0].ObjectType, result.Edits[0].PrimaryKey)
	if edge.DownstreamRID != wantDownstream {
		t.Errorf("expected downstream %q, got %q", wantDownstream, edge.DownstreamRID)
	}
	if len(repo.insertedLogs) != 1 {
		t.Fatalf("expected 1 action log inserted, got %d", len(repo.insertedLogs))
	}
	wantUpstream := oms.ActionLogLineageRID(repo.insertedLogs[0].ID)
	if wantUpstream == "" {
		t.Fatalf("expected mock action log to be assigned a non-zero ID")
	}
	if edge.UpstreamRID != wantUpstream {
		t.Errorf("expected upstream %q, got %q", wantUpstream, edge.UpstreamRID)
	}
}

// TestExecutor_Apply_LineageDisabledByDefault verifies that an executor
// without a wired LineageStore behaves identically to before (no panic,
// no implicit recording).
func TestExecutor_Apply_LineageDisabledByDefault(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "primaryKey", Type: "string", Required: true},
			}, []Rule{
				{
					Type:       "createObject",
					ObjectType: "Employee",
				},
			}),
		},
	}
	exec := NewExecutor(repo, nil)

	if _, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "createEmployee",
		Parameters: map[string]interface{}{"primaryKey": "EMP-001"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestExecutor_Apply_LineageStoreErrorNonFatal verifies that a failing
// LineageStore does not abort the apply — lineage is best-effort
// observability, not a write barrier.
func TestExecutor_Apply_LineageStoreErrorNonFatal(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "primaryKey", Type: "string", Required: true},
			}, []Rule{
				{
					Type:       "createObject",
					ObjectType: "Employee",
				},
			}),
		},
	}
	exec := NewExecutor(repo, nil)
	store := &fakeLineageStore{err: errors.New("pg unreachable")}
	exec.SetLineageStore(store)

	if _, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "createEmployee",
		Parameters: map[string]interface{}{"primaryKey": "EMP-001"},
	}); err != nil {
		t.Fatalf("expected apply to succeed despite lineage failure, got: %v", err)
	}
}

// TestExecutor_Apply_LineageSkipsLinkEdits verifies that link edits do
// not produce object-level lineage rows — link edges already live in
// link_edges and lineage is per-object provenance.
func TestExecutor_Apply_LineageSkipsLinkEdits(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("assign", []ParameterDef{
				{ID: "fromKey", Type: "string", Required: true},
				{ID: "toKey", Type: "string", Required: true},
			}, []Rule{
				{
					Type:                   "createLink",
					LinkTypeAPIName:        "employee_project",
					SourceObjectPrimaryKey: "fromKey",
					TargetObjectPrimaryKey: "toKey",
				},
			}),
		},
	}
	exec := NewExecutor(repo, nil)
	store := &fakeLineageStore{}
	exec.SetLineageStore(store)

	if _, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "assign",
		Parameters: map[string]interface{}{"fromKey": "EMP-001", "toKey": "PRJ-001"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := store.snapshot(); len(got) != 0 {
		t.Errorf("expected 0 lineage edges for a link-only action, got %d", len(got))
	}
}
