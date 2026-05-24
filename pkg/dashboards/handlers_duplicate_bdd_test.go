package dashboards

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/auth"
)

// TestBDD_DashboardDuplicate covers the round-62 fix for the
// "Duplicate dashboard" Foundry-parity gap. Foundry surfaces a
// Duplicate menu action prominently on every dashboard (so users
// can clone a colleague's public dashboard and tweak it without
// touching the source). Weave's pkg/dashboards had Create / List /
// Get / Update / Delete but no Duplicate — power users had to GET
// the source body, generate a new id client-side, and POST a fresh
// row, working around per-(owner, name) uniqueness manually.
//
// Wire shape:
//
//   POST /api/v2/dashboards/{id}/duplicate
//     201 + Dashboard wire object  → new dashboard owned by caller,
//                                    same Definition, name = "<source>
//                                    (copy)" (auto-suffixed "(copy 2)"
//                                    / "(copy 3)" on name collision)
//     401 + Unauthorized            → caller not authenticated
//     404 + DashboardNotFound       → source missing OR private &
//                                     not owned by caller
//
// Scenarios:
//   - Owner duplicates own dashboard: new id, "(copy)" name, same
//     Definition body, IsPublic reset to false on the copy.
//   - Caller duplicates a PUBLIC dashboard owned by someone else:
//     duplicate is owned by caller (not the original owner) and
//     IsPublic defaults to false.
//   - PRIVATE dashboard owned by someone else: 404 (the get-or-public
//     visibility rule applies before duplicate runs).
//   - Name-collision retries auto-suffix "(copy 2)" → "(copy 3)" so
//     a power user can click Duplicate multiple times without
//     hitting 409 manually.
//   - Definition payload is round-tripped verbatim (deep widget
//     trees survive the clone).
func TestBDD_DashboardDuplicate(t *testing.T) {
	owner := &auth.User{ID: "u-owner"}
	other := &auth.User{ID: "u-other"}

	idCounter := 0
	mkSource := func(t *testing.T, store *MemoryStore, name string, public bool, def string) string {
		t.Helper()
		idCounter++
		row := &Dashboard{
			ID:         "src-" + string(rune('a'+idCounter-1)),
			Name:       name,
			CreatedBy:  owner.ID,
			IsPublic:   public,
			Definition: json.RawMessage(def),
		}
		if err := store.Create(nil, row); err != nil {
			t.Fatalf("seed: %v", err)
		}
		return row.ID
	}

	doDuplicate := func(t *testing.T, store Store, user *auth.User, id string) *httptest.ResponseRecorder {
		t.Helper()
		r := newTestRouter(store, user)
		req := httptest.NewRequest(http.MethodPost, "/api/v2/dashboards/"+id+"/duplicate", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("Owner duplicates own dashboard with (copy) suffix", func(t *testing.T) {
		store := NewMemoryStore()
		srcDef := `{"widgets":[{"id":"w1","type":"chart"}]}`
		id := mkSource(t, store, "Sales", false, srcDef)

		rec := doDuplicate(t, store, owner, id)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status=%d, want 201; body=%s", rec.Code, rec.Body.String())
		}
		var copy Dashboard
		if err := json.Unmarshal(rec.Body.Bytes(), &copy); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if copy.ID == id {
			t.Errorf("copy.ID = source ID — must be a fresh id")
		}
		if copy.Name != "Sales (copy)" {
			t.Errorf("copy.Name = %q, want %q", copy.Name, "Sales (copy)")
		}
		if copy.CreatedBy != owner.ID {
			t.Errorf("copy.CreatedBy = %q, want %q", copy.CreatedBy, owner.ID)
		}
		if string(copy.Definition) != srcDef {
			t.Errorf("copy.Definition = %s, want %s", string(copy.Definition), srcDef)
		}
		// Privacy resets: copies start private even if source was public.
		if copy.IsPublic {
			t.Errorf("copy.IsPublic = true, want false (privacy must reset on clone)")
		}
	})

	t.Run("Other user duplicates a PUBLIC dashboard under their own name", func(t *testing.T) {
		store := NewMemoryStore()
		id := mkSource(t, store, "Shared Report", true, `{"widgets":[]}`)

		rec := doDuplicate(t, store, other, id)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status=%d, want 201; body=%s", rec.Code, rec.Body.String())
		}
		var copy Dashboard
		_ = json.Unmarshal(rec.Body.Bytes(), &copy)
		if copy.CreatedBy != other.ID {
			t.Errorf("copy.CreatedBy = %q, want caller %q", copy.CreatedBy, other.ID)
		}
		if !strings.HasSuffix(copy.Name, "(copy)") {
			t.Errorf("copy.Name = %q, want suffix \"(copy)\"", copy.Name)
		}
	})

	t.Run("Other user duplicating a PRIVATE dashboard gets 404", func(t *testing.T) {
		store := NewMemoryStore()
		id := mkSource(t, store, "Private", false, `{}`)

		rec := doDuplicate(t, store, other, id)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "DashboardNotFound" {
			t.Errorf("errorName=%v, want DashboardNotFound", body["errorName"])
		}
	})

	t.Run("Repeat duplicate auto-suffixes (copy 2), (copy 3)", func(t *testing.T) {
		store := NewMemoryStore()
		id := mkSource(t, store, "Inventory", false, `{}`)

		// First clone → "Inventory (copy)"
		rec1 := doDuplicate(t, store, owner, id)
		if rec1.Code != http.StatusCreated {
			t.Fatalf("first clone status=%d, want 201", rec1.Code)
		}
		// Second clone of the SAME source → must NOT 409.
		rec2 := doDuplicate(t, store, owner, id)
		if rec2.Code != http.StatusCreated {
			t.Fatalf("second clone status=%d, want 201 (auto-suffix); body=%s",
				rec2.Code, rec2.Body.String())
		}
		var c2 Dashboard
		_ = json.Unmarshal(rec2.Body.Bytes(), &c2)
		if c2.Name != "Inventory (copy 2)" {
			t.Errorf("second clone Name=%q, want %q", c2.Name, "Inventory (copy 2)")
		}
		// Third clone → "(copy 3)"
		rec3 := doDuplicate(t, store, owner, id)
		var c3 Dashboard
		_ = json.Unmarshal(rec3.Body.Bytes(), &c3)
		if c3.Name != "Inventory (copy 3)" {
			t.Errorf("third clone Name=%q, want %q", c3.Name, "Inventory (copy 3)")
		}
	})

	t.Run("Definition deep tree round-trips verbatim", func(t *testing.T) {
		store := NewMemoryStore()
		deep := `{"widgets":[{"id":"w1","type":"chart","config":{"axes":[{"x":"date","y":"sum"}],"colors":["#fff","#000"]}},{"id":"w2","type":"map"}]}`
		id := mkSource(t, store, "Complex", false, deep)

		rec := doDuplicate(t, store, owner, id)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status=%d", rec.Code)
		}
		var copy Dashboard
		_ = json.Unmarshal(rec.Body.Bytes(), &copy)
		// Compare via canonical re-marshal so whitespace doesn't matter.
		var want, got interface{}
		_ = json.Unmarshal([]byte(deep), &want)
		_ = json.Unmarshal(copy.Definition, &got)
		wantJSON, _ := json.Marshal(want)
		gotJSON, _ := json.Marshal(got)
		if string(wantJSON) != string(gotJSON) {
			t.Errorf("definition not round-tripped:\n want: %s\n got:  %s", wantJSON, gotJSON)
		}
	})

	t.Run("Unauthenticated caller gets 401", func(t *testing.T) {
		store := NewMemoryStore()
		id := mkSource(t, store, "Anything", true, `{}`)

		// nil user → no auth.WithUser → handler.requireAuth fails.
		rec := doDuplicate(t, store, nil, id)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status=%d, want 401", rec.Code)
		}
	})
}
