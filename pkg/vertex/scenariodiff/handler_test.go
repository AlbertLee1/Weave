package scenariodiff_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/scenarios"
	"github.com/liyang/weave/pkg/vertex/scenariodiff"
)

// editsReader satisfies scenariodiff.EditsReader by canning a fixed reply set
// indexed by scenarioRid. Errors per scenarioRid let tests exercise the
// not-found path without standing up a full Repo.
type editsReader struct {
	edits map[string][]scenarios.ScenarioEdit
	err   error
}

func (r *editsReader) ListEdits(_ context.Context, scenarioRID string) ([]scenarios.ScenarioEdit, error) {
	if r.err != nil {
		return nil, r.err
	}
	edits, ok := r.edits[scenarioRID]
	if !ok {
		return nil, scenarios.ErrScenarioNotFound
	}
	return edits, nil
}

func newTestRouter(t *testing.T, reader scenariodiff.EditsReader, base scenariodiff.BaseLoader) chi.Router {
	t.Helper()
	h := scenariodiff.NewHandler(reader, base)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

// TestHandler_Given_ModifyPropertyEdit_When_GETDiff_Then_200WithEditedObjects is the
// straight-through wire test for spec BDD #1+#2: GET /scenarios/{rid}/diff
// returns the four buckets and editedObjects carries oldValue/newValue.
func TestHandler_Given_ModifyPropertyEdit_When_GETDiff_Then_200WithEditedObjects(t *testing.T) {
	reader := &editsReader{
		edits: map[string][]scenarios.ScenarioEdit{
			"ri.vertex.main.scenario.s1": {
				{Seq: 1, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: raw(150)},
			},
		},
	}
	base := newStubBaseLoader(&scenarios.ObjectView{
		ObjectType: "Airport", ObjectID: "JFK",
		Properties: map[string]json.RawMessage{"capacity": raw(100)},
	})
	r := newTestRouter(t, reader, base)

	req := httptest.NewRequest(http.MethodGet, "/api/vertex/v1/scenarios/ri.vertex.main.scenario.s1/diff", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		EditedObjects []struct {
			ObjectType string `json:"objectType"`
			ObjectID   string `json:"objectId"`
			Changes    []struct {
				Property string          `json:"property"`
				OldValue json.RawMessage `json:"oldValue"`
				NewValue json.RawMessage `json:"newValue"`
			} `json:"changes"`
		} `json:"editedObjects"`
		CreatedObjects []map[string]any `json:"createdObjects"`
		DeletedObjects []map[string]any `json:"deletedObjects"`
		Deltas         []struct {
			ObjectType string          `json:"objectType"`
			ObjectID   string          `json:"objectId"`
			Property   string          `json:"property"`
			OldValue   json.RawMessage `json:"oldValue"`
			NewValue   json.RawMessage `json:"newValue"`
		} `json:"deltas"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body: %s", err, w.Body.String())
	}
	if len(resp.EditedObjects) != 1 {
		t.Fatalf("editedObjects len = %d, want 1; body: %s", len(resp.EditedObjects), w.Body.String())
	}
	eo := resp.EditedObjects[0]
	if eo.ObjectType != "Airport" || eo.ObjectID != "JFK" {
		t.Fatalf("editedObjects[0] = %+v, want Airport/JFK", eo)
	}
	if len(eo.Changes) != 1 || eo.Changes[0].Property != "capacity" {
		t.Fatalf("editedObjects[0].Changes = %+v, want one capacity change", eo.Changes)
	}
	if string(eo.Changes[0].OldValue) != string(raw(100)) {
		t.Fatalf("oldValue = %s, want 100", eo.Changes[0].OldValue)
	}
	if string(eo.Changes[0].NewValue) != string(raw(150)) {
		t.Fatalf("newValue = %s, want 150", eo.Changes[0].NewValue)
	}
	if len(resp.Deltas) != 1 || resp.Deltas[0].Property != "capacity" {
		t.Fatalf("deltas = %+v, want one capacity delta", resp.Deltas)
	}
	// Empty buckets must serialize as empty arrays, not null — clients iterate.
	if resp.CreatedObjects == nil || resp.DeletedObjects == nil {
		t.Fatalf("created/deleted should serialize as [], got created=%v deleted=%v", resp.CreatedObjects, resp.DeletedObjects)
	}
}

// TestHandler_Given_UnknownScenarioRID_When_GETDiff_Then_404 confirms the
// ErrScenarioNotFound sentinel is mapped to a 404, not a 500.
func TestHandler_Given_UnknownScenarioRID_When_GETDiff_Then_404(t *testing.T) {
	reader := &editsReader{edits: map[string][]scenarios.ScenarioEdit{}}
	r := newTestRouter(t, reader, newStubBaseLoader())

	req := httptest.NewRequest(http.MethodGet, "/api/vertex/v1/scenarios/ri.vertex.main.scenario.missing/diff", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

// TestHandler_Given_RepoError_When_GETDiff_Then_500 maps a generic repo error
// (anything not ErrScenarioNotFound) to a 500 with a descriptive name.
func TestHandler_Given_RepoError_When_GETDiff_Then_500(t *testing.T) {
	reader := &editsReader{err: errors.New("db unreachable")}
	r := newTestRouter(t, reader, newStubBaseLoader())

	req := httptest.NewRequest(http.MethodGet, "/api/vertex/v1/scenarios/anything/diff", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

// TestHandler_Given_NoEdits_When_GETDiff_Then_AllBucketsEmpty validates the
// well-known "scenario with no edits" case still returns 200 with [] buckets.
func TestHandler_Given_NoEdits_When_GETDiff_Then_AllBucketsEmpty(t *testing.T) {
	reader := &editsReader{edits: map[string][]scenarios.ScenarioEdit{"ri.vertex.main.scenario.s1": nil}}
	r := newTestRouter(t, reader, newStubBaseLoader())

	req := httptest.NewRequest(http.MethodGet, "/api/vertex/v1/scenarios/ri.vertex.main.scenario.s1/diff", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		EditedObjects  []any `json:"editedObjects"`
		CreatedObjects []any `json:"createdObjects"`
		DeletedObjects []any `json:"deletedObjects"`
		Deltas         []any `json:"deltas"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body: %s", err, w.Body.String())
	}
	if len(resp.EditedObjects) != 0 || len(resp.CreatedObjects) != 0 || len(resp.DeletedObjects) != 0 || len(resp.Deltas) != 0 {
		t.Fatalf("all buckets should be empty; body: %s", w.Body.String())
	}
}
