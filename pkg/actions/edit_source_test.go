package actions

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// TestActionEditSourceUser is the US-020 red test: every Edit emitted by the
// action executor — whether through the rules path or the function-backed
// path, and regardless of edit type — must be tagged Source="user" so the
// funnel consumer's user-edit-wins conflict logic (US-021) can protect them
// from subsequent ingest overwrites.
func TestActionEditSourceUser(t *testing.T) {
	const want = "user"

	// Three rules exercising every EditType so we catch a future regression
	// that only tags a subset of paths (e.g. CREATE but not DELETE).
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("mixedAction", []ParameterDef{
				{ID: "primaryKey", Type: "string", Required: true},
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{
					Type:       "createObject",
					ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					},
				},
				{
					Type:       "modifyObject",
					ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					},
				},
				{
					Type:       "deleteObject",
					ObjectType: "Employee",
				},
			}),
		},
	}
	exec := NewExecutor(repo, nil)

	t.Run("Prepare tags all edits", func(t *testing.T) {
		prep, err := exec.Prepare(context.Background(), "ont-1", &ApplyRequest{
			ActionType: "mixedAction",
			Parameters: map[string]interface{}{
				"primaryKey": "emp-1",
				"name":       "Alice",
			},
		})
		if err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		if len(prep.Edits) != 3 {
			t.Fatalf("expected 3 edits, got %d", len(prep.Edits))
		}
		for i, e := range prep.Edits {
			if e.Source != want {
				t.Errorf("edit %d (%s): Source=%q, want %q", i, e.Type, e.Source, want)
			}
		}
	})

	t.Run("Apply result preserves Source=user", func(t *testing.T) {
		result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
			ActionType: "mixedAction",
			Parameters: map[string]interface{}{
				"primaryKey": "emp-2",
				"name":       "Bob",
			},
		})
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if len(result.Edits) == 0 {
			t.Fatal("expected non-empty edits")
		}
		for i, e := range result.Edits {
			if e.Source != want {
				t.Errorf("result edit %d (%s): Source=%q, want %q", i, e.Type, e.Source, want)
			}
		}
	})

	t.Run("function-backed Prepare tags all edits", func(t *testing.T) {
		fnRepo := &mockOmsRepo{
			actionTypes: []oms.ActionType{
				{
					RID:              "ri.ontology.main.action-type.fn",
					APIName:          "fnAction",
					Parameters:       mustJSON([]ParameterDef{}),
					Rules:            mustJSON([]Rule{}),
					Status:           "ACTIVE",
					IsFunctionBacked: true,
				},
			},
		}
		fnExec := NewExecutor(fnRepo, nil)
		fnExec.SetFunctionDispatcher(&sourceTagDispatcher{
			edits: []funnel.Edit{
				{Type: funnel.EditTypeCreate, ObjectType: "X", PrimaryKey: "x-1"},
				{Type: funnel.EditTypeModify, ObjectType: "X", PrimaryKey: "x-2"},
				{Type: funnel.EditTypeDelete, ObjectType: "X", PrimaryKey: "x-3"},
			},
		})
		prep, err := fnExec.Prepare(context.Background(), "ont-1", &ApplyRequest{
			ActionType: "fnAction",
			Parameters: map[string]interface{}{},
		})
		if err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		for i, e := range prep.Edits {
			if e.Source != want {
				t.Errorf("fn edit %d (%s): Source=%q, want %q", i, e.Type, e.Source, want)
			}
		}
	})
}

// TestFunnelEditSourceJSON guarantees the new Source field round-trips through
// JSON with the exact tag name the funnel consumer and PG storage rely on.
func TestFunnelEditSourceJSON(t *testing.T) {
	orig := funnel.Edit{
		Type:       funnel.EditTypeModify,
		ObjectType: "Employee",
		PrimaryKey: "emp-1",
		Source:     "user",
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"source":"user"`) {
		t.Fatalf("expected json to contain \"source\":\"user\", got %s", data)
	}

	var round funnel.Edit
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round.Source != "user" {
		t.Fatalf("round.Source=%q, want %q", round.Source, "user")
	}

	// An edit with an empty Source field must omit the key on the wire so
	// legacy callers predating US-020 do not get a spurious "source":""
	// on their payloads.
	empty := funnel.Edit{Type: funnel.EditTypeCreate, ObjectType: "X", PrimaryKey: "x-1"}
	emptyData, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal empty: %v", err)
	}
	if strings.Contains(string(emptyData), `"source"`) {
		t.Fatalf("expected empty Source to be omitted, got %s", emptyData)
	}
}

// sourceTagDispatcher is a test-only FunctionDispatcher that returns a fixed
// edit list, mirroring the real dispatcher contract so executor.Prepare runs
// its function-backed branch.
type sourceTagDispatcher struct {
	edits []funnel.Edit
}

func (d *sourceTagDispatcher) Dispatch(_ context.Context, _ *oms.ActionType, _ map[string]interface{}) ([]funnel.Edit, error) {
	out := make([]funnel.Edit, len(d.edits))
	copy(out, d.edits)
	return out, nil
}
