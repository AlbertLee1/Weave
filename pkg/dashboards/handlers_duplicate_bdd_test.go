package dashboards

import (
	"context"
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
//	POST /api/v2/dashboards/{id}/duplicate
//	  201 + Dashboard wire object  → new dashboard owned by caller,
//	                                 same Definition, name = "<source>
//	                                 (dup)" (auto-suffixed "(copy 2)"
//	                                 / "(copy 3)" on name collision)
//	  401 + Unauthorized            → caller not authenticated
//	  404 + DashboardNotFound       → source missing OR private &
//	                                  not owned by caller
//
// Scenarios:
//   - Owner duplicates own dashboard: new id, "(copy)" name, same
//     Definition body, IsPublic reset to false on the dup.
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
		if err := store.Create(context.TODO(), row); err != nil {
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

	t.Run("Owner duplicates own dashboard with (dup) suffix", func(t *testing.T) {
		store := NewMemoryStore()
		srcDef := `{"widgets":[{"id":"w1","type":"chart"}]}`
		id := mkSource(t, store, "Sales", false, srcDef)

		rec := doDuplicate(t, store, owner, id)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status=%d, want 201; body=%s", rec.Code, rec.Body.String())
		}
		var dup Dashboard
		if err := json.Unmarshal(rec.Body.Bytes(), &dup); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if dup.ID == id {
			t.Errorf("copy.ID = source ID — must be a fresh id")
		}
		if dup.Name != "Sales (copy)" {
			t.Errorf("copy.Name = %q, want %q", dup.Name, "Sales (copy)")
		}
		if dup.CreatedBy != owner.ID {
			t.Errorf("copy.CreatedBy = %q, want %q", dup.CreatedBy, owner.ID)
		}
		if string(dup.Definition) != srcDef {
			t.Errorf("copy.Definition = %s, want %s", string(dup.Definition), srcDef)
		}
		// Privacy resets: copies start private even if source was public.
		if dup.IsPublic {
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
		var dup Dashboard
		_ = json.Unmarshal(rec.Body.Bytes(), &dup)
		if dup.CreatedBy != other.ID {
			t.Errorf("copy.CreatedBy = %q, want caller %q", dup.CreatedBy, other.ID)
		}
		if !strings.HasSuffix(dup.Name, "(copy)") {
			t.Errorf("copy.Name = %q, want suffix \"(dup)\"", dup.Name)
		}
	})

	t.Run("Other user copylicating a PRIVATE dashboard gets 404", func(t *testing.T) {
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

	t.Run("Repeat duplicate auto-suffixes (dup 2), (dup 3)", func(t *testing.T) {
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
		var dup Dashboard
		_ = json.Unmarshal(rec.Body.Bytes(), &dup)
		// Compare via canonical re-marshal so whitespace doesn't matter.
		var want, got interface{}
		_ = json.Unmarshal([]byte(deep), &want)
		_ = json.Unmarshal(dup.Definition, &got)
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

// TestBDD_DashboardDuplicate_LongName covers the round-65 fix for
// the dashboard length-CHECK drift introduced by round 62. The
// dashboards.MaxNameLength constant + migration 000076's
// `CHECK (length(name) BETWEEN 1 AND 128)` constraint both cap
// names at 128 chars. Round 62's Duplicate handler appends
// " (copy)" / " (copy N)" without checking the result fits the
// cap — a source name near 128 chars would silently produce a 132+
// char clone name that violates the PG CHECK on real deployments.
// MemoryStore-backed round-62 BDDs missed it because in-memory
// has no constraint; the only protection was the Go-side
// ValidateName called on Create — which the duplicate path skipped
// when assembling the clone row.
//
// Round 65 truncates the source name so suffix + base fit
// MaxNameLength. Scenarios:
//   - Source name exactly MaxNameLength: clone uses truncated
//     prefix + " (copy)" and length <= MaxNameLength.
//   - Source name MaxNameLength - 5 chars: " (copy)" suffix is 7
//     chars so total would be MaxNameLength+2 without truncation;
//     fix must trim source to (MaxNameLength - len(" (copy)")).
//   - Auto-suffix climbing past " (copy 9)" (which adds 10 chars)
//     must also fit.
func TestBDD_DashboardDuplicate_LongName(t *testing.T) {
	owner := &auth.User{ID: "u-owner"}

	seed := func(t *testing.T, store *MemoryStore, name string) string {
		t.Helper()
		row := &Dashboard{
			ID:         "src-long-" + name[:1],
			Name:       name,
			CreatedBy:  owner.ID,
			Definition: json.RawMessage("{}"),
		}
		if err := store.Create(context.TODO(), row); err != nil {
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

	t.Run("Source name at exactly MaxNameLength clones under cap", func(t *testing.T) {
		store := NewMemoryStore()
		longName := strings.Repeat("A", MaxNameLength)
		id := seed(t, store, longName)

		rec := doDuplicate(t, store, owner, id)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status=%d, want 201; body=%s", rec.Code, rec.Body.String())
		}
		var clone Dashboard
		_ = json.Unmarshal(rec.Body.Bytes(), &clone)
		if len(clone.Name) > MaxNameLength {
			t.Errorf("clone name length=%d, want <= %d (PG CHECK would reject)",
				len(clone.Name), MaxNameLength)
		}
		// The clone must still end with " (copy)" so the user can
		// tell it apart from the source in the list view.
		if !strings.HasSuffix(clone.Name, " (copy)") {
			t.Errorf("clone name %q does not end with \" (copy)\"", clone.Name)
		}
	})

	t.Run("Source name near limit + (copy) fits", func(t *testing.T) {
		// "<127 chars> (copy)" would be 134 chars without truncation.
		store := NewMemoryStore()
		long := strings.Repeat("B", MaxNameLength-1)
		id := seed(t, store, long)

		rec := doDuplicate(t, store, owner, id)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status=%d, want 201; body=%s", rec.Code, rec.Body.String())
		}
		var clone Dashboard
		_ = json.Unmarshal(rec.Body.Bytes(), &clone)
		if len(clone.Name) > MaxNameLength {
			t.Errorf("clone name length=%d > %d", len(clone.Name), MaxNameLength)
		}
	})

	t.Run("Auto-suffix walk past (copy 9) still fits cap", func(t *testing.T) {
		// Seed source + manually create N existing copies so the
		// duplicate handler has to climb past " (copy 9)" (10 chars).
		store := NewMemoryStore()
		long := strings.Repeat("C", MaxNameLength)
		id := seed(t, store, long)

		// Pre-create 9 clones via the same handler. Each one is a
		// fresh suffix attempt.
		for i := 0; i < 9; i++ {
			rec := doDuplicate(t, store, owner, id)
			if rec.Code != http.StatusCreated {
				t.Fatalf("pre-clone %d status=%d", i+1, rec.Code)
			}
		}
		// The 10th clone needs suffix " (copy 10)" (10 chars). Total
		// length after truncation MUST still be <= MaxNameLength.
		rec := doDuplicate(t, store, owner, id)
		if rec.Code != http.StatusCreated {
			t.Fatalf("10th clone status=%d, want 201; body=%s", rec.Code, rec.Body.String())
		}
		var clone Dashboard
		_ = json.Unmarshal(rec.Body.Bytes(), &clone)
		if len(clone.Name) > MaxNameLength {
			t.Errorf("10th clone name length=%d > %d (deeper suffix needs more truncation)",
				len(clone.Name), MaxNameLength)
		}
		// The candidate suffix shape stays recognizable.
		if !strings.Contains(clone.Name, " (copy ") {
			t.Errorf("10th clone name %q lost the (copy N) suffix shape", clone.Name)
		}
	})

	t.Run("Short source name keeps full source + suffix unchanged", func(t *testing.T) {
		// Regression: round-65 truncation must NOT kick in for
		// short-source-name cases — round-62's "Sales (copy)" output
		// must keep working byte-for-byte.
		store := NewMemoryStore()
		id := seed(t, store, "Z-Sales")
		rec := doDuplicate(t, store, owner, id)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status=%d", rec.Code)
		}
		var clone Dashboard
		_ = json.Unmarshal(rec.Body.Bytes(), &clone)
		if clone.Name != "Z-Sales (copy)" {
			t.Errorf("short-source clone name=%q, want %q", clone.Name, "Z-Sales (copy)")
		}
	})
}
