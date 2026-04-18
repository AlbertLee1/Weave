package cellsec

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
	"github.com/liyang/weave/pkg/masking"
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
		PrimaryKey:      "c-100",
		PropertyAPIName: "ssn",
		MaskRule:        masking.MaskRuleHash,
		AppliesTo:       masking.AppliesTo{Roles: []string{"finance"}},
		Description:     "Hash c-100's ssn for non-finance",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/cell-masks", bytes.NewReader(body))
	req = withAdminUser(req)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CellMask
	decodeBody(t, w.Body, &resp)
	if resp.RID == "" {
		t.Fatalf("expected RID populated")
	}
	if !strings.HasPrefix(resp.RID, "ri.cellsec.main.cell-mask.") {
		t.Fatalf("unexpected RID prefix: %s", resp.RID)
	}
	if resp.PrimaryKey != "c-100" {
		t.Fatalf("expected PrimaryKey=c-100, got %q", resp.PrimaryKey)
	}

	got, err := store.Get(context.Background(), resp.RID)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if got.MaskRule != masking.MaskRuleHash {
		t.Fatalf("expected MaskRule=hash, got %v", got.MaskRule)
	}
}

func TestHandler_Create_MissingUser_401(t *testing.T) {
	store := NewMemoryStore()
	router, _ := mountHandler(t, store)
	body := mustMarshal(t, CreateRequest{
		ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
		PrimaryKey:      "c-100",
		PropertyAPIName: "ssn",
		MaskRule:        masking.MaskRuleHash,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/cell-masks", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandler_Create_Validation_MissingPK(t *testing.T) {
	store := NewMemoryStore()
	router, _ := mountHandler(t, store)
	body := mustMarshal(t, CreateRequest{
		// missing PrimaryKey
		ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
		PropertyAPIName: "ssn",
		MaskRule:        masking.MaskRuleHash,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/cell-masks", bytes.NewReader(body))
	req = withAdminUser(req)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandler_Create_Validation_UnknownRule(t *testing.T) {
	store := NewMemoryStore()
	router, _ := mountHandler(t, store)
	body := mustMarshal(t, CreateRequest{
		ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
		PrimaryKey:      "c-100",
		PropertyAPIName: "ssn",
		MaskRule:        masking.MaskRule("shrug"),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/cell-masks", bytes.NewReader(body))
	req = withAdminUser(req)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandler_List_FilterByObjectType(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.Create(ctx, &CellMask{
		RID:             "ri.cellsec.main.cell-mask.one",
		ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
		PrimaryKey:      "c-100",
		PropertyAPIName: "ssn",
		MaskRule:        masking.MaskRuleHash,
	})
	_ = store.Create(ctx, &CellMask{
		RID:             "ri.cellsec.main.cell-mask.two",
		ObjectTypeRID:   "ri.ontology.main.object-type.Order",
		PrimaryKey:      "o-200",
		PropertyAPIName: "total",
		MaskRule:        masking.MaskRuleRedact,
	})

	router, _ := mountHandler(t, store)
	req := httptest.NewRequest(http.MethodGet,
		"/api/admin/cell-masks?objectType=ri.ontology.main.object-type.Customer", nil)
	req = withAdminUser(req)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp ListResponse
	decodeBody(t, w.Body, &resp)
	if len(resp.Masks) != 1 {
		t.Fatalf("expected 1 filtered mask, got %d", len(resp.Masks))
	}
	if resp.Masks[0].RID != "ri.cellsec.main.cell-mask.one" {
		t.Fatalf("unexpected RID: %s", resp.Masks[0].RID)
	}
}

func TestHandler_GetUpdateDelete(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	rid := "ri.cellsec.main.cell-mask.ssn"
	_ = store.Create(ctx, &CellMask{
		RID:             rid,
		ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
		PrimaryKey:      "c-100",
		PropertyAPIName: "ssn",
		MaskRule:        masking.MaskRuleHash,
		Description:     "initial",
	})

	router, _ := mountHandler(t, store)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/cell-masks/"+rid, nil)
	req = withAdminUser(req)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Get: expected 200, got %d", w.Code)
	}

	newDesc := "updated description"
	newRule := masking.MaskRuleRedact
	upd := mustMarshal(t, CellMaskUpdate{Description: &newDesc, MaskRule: &newRule})
	req = httptest.NewRequest(http.MethodPatch, "/api/admin/cell-masks/"+rid, bytes.NewReader(upd))
	req = withAdminUser(req)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Update: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got CellMask
	decodeBody(t, w.Body, &got)
	if got.Description != newDesc {
		t.Fatalf("description not persisted, got %q", got.Description)
	}
	if got.MaskRule != masking.MaskRuleRedact {
		t.Fatalf("rule not updated, got %v", got.MaskRule)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/admin/cell-masks/"+rid, nil)
	req = withAdminUser(req)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("Delete: expected 204, got %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/cell-masks/"+rid, nil)
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
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cell-masks/ri.cellsec.main.cell-mask.ghost", nil)
	req = withAdminUser(req)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandler_Update_InvalidRule(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	rid := "ri.cellsec.main.cell-mask.x"
	_ = store.Create(ctx, &CellMask{
		RID:             rid,
		ObjectTypeRID:   "ri.ontology.main.object-type.Customer",
		PrimaryKey:      "c-100",
		PropertyAPIName: "ssn",
		MaskRule:        masking.MaskRuleHash,
	})
	router, _ := mountHandler(t, store)
	badRule := masking.MaskRule("nope")
	body := mustMarshal(t, CellMaskUpdate{MaskRule: &badRule})
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/cell-masks/"+rid, bytes.NewReader(body))
	req = withAdminUser(req)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown rule on update, got %d", w.Code)
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
		PrimaryKey:      "c-100",
		PropertyAPIName: "ssn",
		MaskRule:        masking.MaskRuleHash,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/cell-masks", bytes.NewReader(body))
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
