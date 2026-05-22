package masking

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
)

func mustMarshal(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func withAdminUser(r *http.Request) *http.Request {
	u := &auth.User{ID: "user:admin@ex.com", Email: "admin@ex.com", Roles: []string{auth.RoleAdmin}}
	return r.WithContext(auth.WithUser(r.Context(), u))
}

func mountHandler(t *testing.T, store Store) (*chi.Mux, *Handler) {
	t.Helper()
	handler := NewHandler(store, nil, nil)
	r := chi.NewRouter()
	handler.RegisterRoutes(r)
	return r, handler
}

func decodeBody(t *testing.T, body io.Reader, into interface{}) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(into); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestHandler_Create(t *testing.T) {
	store := NewMemoryStore()
	router, _ := mountHandler(t, store)

	body := mustMarshal(t, CreateRequest{
		ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
		PropertyAPIName: "ssn",
		MaskRule:        MaskRuleHash,
		AppliesTo:       AppliesTo{Roles: []string{"finance"}},
		Description:     "Hash ssn for non-finance",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/column-masks", bytes.NewReader(body))
	req = withAdminUser(req)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp ColumnMask
	decodeBody(t, w.Body, &resp)
	if resp.RID == "" {
		t.Fatalf("expected RID populated")
	}
	if !strings.HasPrefix(resp.RID, "ri.masking.main.column-mask.") {
		t.Fatalf("unexpected RID prefix: %s", resp.RID)
	}

	got, err := store.Get(context.Background(), resp.RID)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if got.MaskRule != MaskRuleHash {
		t.Fatalf("expected MaskRule=hash, got %v", got.MaskRule)
	}
}

func TestHandler_Create_MissingUser_401(t *testing.T) {
	store := NewMemoryStore()
	router, _ := mountHandler(t, store)
	body := mustMarshal(t, CreateRequest{
		ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
		PropertyAPIName: "ssn",
		MaskRule:        MaskRuleHash,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/column-masks", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandler_Create_Validation(t *testing.T) {
	store := NewMemoryStore()
	router, _ := mountHandler(t, store)
	body := mustMarshal(t, CreateRequest{
		// missing MaskRule
		ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
		PropertyAPIName: "ssn",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/column-masks", bytes.NewReader(body))
	req = withAdminUser(req)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandler_List(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.Create(ctx, &ColumnMask{
		RID:             "ri.masking.main.column-mask.one",
		ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
		PropertyAPIName: "ssn",
		MaskRule:        MaskRuleHash,
	})
	_ = store.Create(ctx, &ColumnMask{
		RID:             "ri.masking.main.column-mask.two",
		ObjectTypeRID:   "ri.ontology.main.object-type.Order",
		PropertyAPIName: "total",
		MaskRule:        MaskRuleRedact,
	})

	router, _ := mountHandler(t, store)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/column-masks", nil)
	req = withAdminUser(req)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp ListResponse
	decodeBody(t, w.Body, &resp)
	if len(resp.Masks) != 2 {
		t.Fatalf("expected 2 masks, got %d", len(resp.Masks))
	}
}

func TestHandler_List_FilterByObjectType(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.Create(ctx, &ColumnMask{
		RID:             "ri.masking.main.column-mask.one",
		ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
		PropertyAPIName: "ssn",
		MaskRule:        MaskRuleHash,
	})
	_ = store.Create(ctx, &ColumnMask{
		RID:             "ri.masking.main.column-mask.two",
		ObjectTypeRID:   "ri.ontology.main.object-type.Order",
		PropertyAPIName: "total",
		MaskRule:        MaskRuleRedact,
	})
	router, _ := mountHandler(t, store)
	req := httptest.NewRequest(http.MethodGet,
		"/api/admin/column-masks?objectType=ri.ontology.main.object-type.Customer", nil)
	req = withAdminUser(req)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp ListResponse
	decodeBody(t, w.Body, &resp)
	if len(resp.Masks) != 1 {
		t.Fatalf("expected 1 mask filtered, got %d", len(resp.Masks))
	}
	if resp.Masks[0].RID != "ri.masking.main.column-mask.one" {
		t.Fatalf("unexpected RID: %s", resp.Masks[0].RID)
	}
}

func TestHandler_GetUpdateDelete(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	rid := "ri.masking.main.column-mask.ssn"
	_ = store.Create(ctx, &ColumnMask{
		RID:             rid,
		ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
		PropertyAPIName: "ssn",
		MaskRule:        MaskRuleHash,
		Description:     "initial",
	})

	router, _ := mountHandler(t, store)

	// Get
	req := httptest.NewRequest(http.MethodGet, "/api/admin/column-masks/"+rid, nil)
	req = withAdminUser(req)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Get: expected 200, got %d", w.Code)
	}

	// Update
	newDesc := "updated description"
	newRule := MaskRuleRedact
	upd := mustMarshal(t, ColumnMaskUpdate{Description: &newDesc, MaskRule: &newRule})
	req = httptest.NewRequest(http.MethodPatch, "/api/admin/column-masks/"+rid, bytes.NewReader(upd))
	req = withAdminUser(req)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Update: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got ColumnMask
	decodeBody(t, w.Body, &got)
	if got.Description != newDesc {
		t.Fatalf("description not persisted, got %q", got.Description)
	}
	if got.MaskRule != MaskRuleRedact {
		t.Fatalf("rule not updated, got %v", got.MaskRule)
	}

	// Delete
	req = httptest.NewRequest(http.MethodDelete, "/api/admin/column-masks/"+rid, nil)
	req = withAdminUser(req)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("Delete: expected 204, got %d", w.Code)
	}

	// Get again → 404
	req = httptest.NewRequest(http.MethodGet, "/api/admin/column-masks/"+rid, nil)
	req = withAdminUser(req)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("Get after Delete: expected 404, got %d", w.Code)
	}
}

func TestHandler_Get_Unknown_404(t *testing.T) {
	store := NewMemoryStore()
	router, _ := mountHandler(t, store)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/column-masks/ri.masking.main.column-mask.ghost", nil)
	req = withAdminUser(req)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandler_WriteRefreshesEngine(t *testing.T) {
	store := NewMemoryStore()
	engine := New(store, nil)
	_ = engine.Reload(context.Background())
	handler := NewHandler(store, nil, engine)
	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := mustMarshal(t, CreateRequest{
		ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
		PropertyAPIName: "ssn",
		MaskRule:        MaskRuleHash,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/column-masks", bytes.NewReader(body))
	req = withAdminUser(req)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if engine.Size("ri.ontology.main.object-type.Customer") != 1 {
		t.Fatalf("engine should see 1 mask after create, got %d",
			engine.Size("ri.ontology.main.object-type.Customer"))
	}
}

func TestBDD_HandlerRejectsAmbiguousJSONBodies_RSI001(t *testing.T) {
	t.Run("create rejects a valid column mask followed by another JSON value", func(t *testing.T) {
		store := NewMemoryStore()
		router, _ := mountHandler(t, store)

		first := string(mustMarshal(t, CreateRequest{
			ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
			PropertyAPIName: "ssn",
			MaskRule:        MaskRuleHash,
			AppliesTo:       AppliesTo{Roles: []string{"finance"}},
			Description:     "hash ssn",
		}))
		req := httptest.NewRequest(
			http.MethodPost,
			"/api/admin/column-masks",
			strings.NewReader(first+`{"smuggled":true}`),
		)
		req = withAdminUser(req)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assertRSI001BadRequest(t, w, "InvalidColumnMaskRequest")
		rows, err := store.List(context.Background())
		if err != nil {
			t.Fatalf("store.List: %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("ambiguous create persisted %d column masks", len(rows))
		}
	})

	t.Run("update rejects a valid patch followed by another JSON value", func(t *testing.T) {
		store := NewMemoryStore()
		const rid = "ri.masking.main.column-mask.ssn"
		if err := store.Create(context.Background(), &ColumnMask{
			RID:             rid,
			ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
			PropertyAPIName: "ssn",
			MaskRule:        MaskRuleHash,
			Description:     "original",
		}); err != nil {
			t.Fatalf("seed mask: %v", err)
		}
		router, _ := mountHandler(t, store)

		nextDescription := "mutated"
		first := string(mustMarshal(t, ColumnMaskUpdate{Description: &nextDescription}))
		req := httptest.NewRequest(
			http.MethodPatch,
			"/api/admin/column-masks/"+rid,
			strings.NewReader(first+`{"smuggled":true}`),
		)
		req = withAdminUser(req)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assertRSI001BadRequest(t, w, "InvalidColumnMaskUpdate")
		got, err := store.Get(context.Background(), rid)
		if err != nil {
			t.Fatalf("store.Get: %v", err)
		}
		if got.Description != "original" {
			t.Fatalf("ambiguous update mutated description to %q", got.Description)
		}
	})
}

func assertRSI001BadRequest(t *testing.T, w *httptest.ResponseRecorder, errorName string) {
	t.Helper()
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), errorName) {
		t.Fatalf("expected error %q in response body: %s", errorName, w.Body.String())
	}
}
