package objectset_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/oss/objectset"
)

// TestBDD_SnapshotHandler_DownstreamErrorsReturnHTTP500 covers the
// PersistedSnapshotStore downstream-failure path (rounds 24-26 closed
// the same antipattern in pkg/oms, pkg/oss/objectset/handler.go, and
// pkg/oss/handlers*.go). Two call sites in snapshot_persisted.go used
// `apierror.NewInvalidParameter("Snapshot…Failed", …)` for what are
// actually downstream PG-store failures (disk full, connection lost,
// etc.). That returned HTTP 400 INVALID_ARGUMENT — telling Foundry
// SDK clients "you sent a bad request, fix your input" when the
// caller's input (ObjectSet RID from URL) was already validated and
// the actual failure was server-side.
//
// Sites fixed (file:line pre-commit):
//
//	snapshot_persisted.go:156   SnapshotPersistFailed
//	snapshot_persisted.go:203   SnapshotLookupFailed
//	snapshot_persisted.go:226   PropertyFilterFailed (GetSnapshot path)
//	handler_objectset.go:138    PropertyFilterFailed (LoadLinks path)
//
// Site NOT changed in this round: handler_objectset.go:70
// LoadLinksFailed — the same executor.Execute() mixed-bag site that
// round 25 deferred at handler.go:200 ObjectSetFailed. Needs sentinel
// error types in the executor before it can be safely promoted to
// HTTP 500.
//
// The pre-existing TestCreateSnapshot_PropagatesStoreError (round 24
// of the snapshot suite) enshrines the bug at HTTP 400 — its stated
// reasoning "configuration mistakes stay visible" is equally
// satisfied by HTTP 500 INTERNAL and more accurate (these ARE
// server-side failures). Updated as part of this commit.
func TestBDD_SnapshotHandler_DownstreamErrorsReturnHTTP500(t *testing.T) {
	t.Run("CreatePersistedSnapshot error returns HTTP 500 INTERNAL", func(t *testing.T) {
		h, store := setupSnapshotHandlerTest(t)
		pstore := newFakePersistedSnapshotStore()
		pstore.createEr = errors.New("postgres: disk full")
		h.SetPersistedSnapshotStore(pstore)

		def := &objectset.Definition{Type: "base", ObjectType: "employee"}
		rid := store.Put(def)
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/myOntology/objectSets/"+rid+"/snapshot", nil)
		rec := httptest.NewRecorder()
		newSnapshotRouter(t, h).ServeHTTP(rec, req)

		assertSnapshotInternalError(t, rec, "SnapshotPersistFailed", "postgres: disk full")
	})

	t.Run("GetPersistedSnapshot non-sentinel error returns HTTP 500 INTERNAL", func(t *testing.T) {
		h, _ := setupSnapshotHandlerTest(t)
		pstore := newFakePersistedSnapshotStore()
		pstore.getEr = errors.New("postgres: connection lost")
		h.SetPersistedSnapshotStore(pstore)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/myOntology/objectSets/snapshots/ri.objectsets.main.snapshot.fake", nil)
		rec := httptest.NewRecorder()
		newSnapshotRouter(t, h).ServeHTTP(rec, req)

		assertSnapshotInternalError(t, rec, "SnapshotLookupFailed", "postgres: connection lost")
	})

	t.Run("GetPersistedSnapshot genuine ErrSnapshotNotFound regression guard still returns HTTP 404", func(t *testing.T) {
		// Sentinel path stays unchanged: a real "snapshot does not
		// exist" returns 404 NOT_FOUND with errorName SnapshotNotFound.
		// Only the non-sentinel error branch changes to 500.
		h, _ := setupSnapshotHandlerTest(t)
		pstore := newFakePersistedSnapshotStore()
		// no rows seeded; lookup returns ErrSnapshotNotFound
		h.SetPersistedSnapshotStore(pstore)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/myOntology/objectSets/snapshots/ri.objectsets.main.snapshot.missing", nil)
		rec := httptest.NewRecorder()
		newSnapshotRouter(t, h).ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("genuine ErrSnapshotNotFound: status = %d, want 404; body = %s", rec.Code, rec.Body.String())
		}
		var env struct {
			ErrorCode string `json:"errorCode"`
			ErrorName string `json:"errorName"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&env)
		if env.ErrorCode != "NOT_FOUND" {
			t.Errorf("errorCode = %q, want NOT_FOUND", env.ErrorCode)
		}
		if env.ErrorName != "SnapshotNotFound" {
			t.Errorf("errorName = %q, want SnapshotNotFound", env.ErrorName)
		}
	})
}

func assertSnapshotInternalError(t *testing.T, rec *httptest.ResponseRecorder, wantErrorName, wantErrorSubstring string) {
	t.Helper()
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body = %s", rec.Code, rec.Body.String())
	}
	var env struct {
		ErrorCode  string            `json:"errorCode"`
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&env)
	if env.ErrorCode != "INTERNAL" {
		t.Errorf("errorCode = %q, want INTERNAL (400 INVALID_ARGUMENT would mislead the SDK into a 'fix your input' branch when the caller's input is fine and the persisted-snapshot store failed)", env.ErrorCode)
	}
	if env.ErrorName != wantErrorName {
		t.Errorf("errorName = %q, want %q", env.ErrorName, wantErrorName)
	}
	if wantErrorSubstring != "" {
		got := env.Parameters["error"]
		if !strings.Contains(got, wantErrorSubstring) {
			t.Errorf("parameters.error = %q, want it to mention %q", got, wantErrorSubstring)
		}
	}
}
