package aip

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func newToolCatalogTestHandler() (*ToolCatalogHandler, *MemoryToolCatalog, *ToolRegistry) {
	cat := NewMemoryToolCatalog()
	reg := NewToolRegistry()
	inv := &stubFunctionInvoker{result: "ok"}
	return NewToolCatalogHandler(cat, reg, inv), cat, reg
}

func newToolCatalogRouter(h *ToolCatalogHandler) *chi.Mux {
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

func TestToolCatalogHandler_CreateAndList(t *testing.T) {
	h, cat, reg := newToolCatalogTestHandler()
	r := newToolCatalogRouter(h)

	body := `{"name":"lookup","description":"look up","handlerFunctionRid":"ri.functions.main.fn.a","parameters":{"type":"object"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/aip/tools", strings.NewReader(body))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var rec ToolRecord
	if err := json.Unmarshal(w.Body.Bytes(), &rec); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if rec.Name != "lookup" || rec.HandlerFunctionRID != "ri.functions.main.fn.a" {
		t.Errorf("created rec = %+v", rec)
	}
	if !rec.Enabled {
		t.Errorf("expected default Enabled=true, got %+v", rec)
	}
	if rec.CreatedBy != "user:alice" {
		t.Errorf("CreatedBy = %q", rec.CreatedBy)
	}

	// Registry should now have the new tool.
	names := reg.Names()
	found := false
	for _, n := range names {
		if n == "lookup" {
			found = true
		}
	}
	if !found {
		t.Errorf("registry missing 'lookup' after create, names=%v", names)
	}

	// And list should return one record.
	got, err := cat.ListTools(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("ListTools err=%v len=%d", err, len(got))
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v2/aip/tools", nil)
	listReq = withAuthContext(listReq, "user:alice")
	listW := httptest.NewRecorder()
	r.ServeHTTP(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("list status = %d", listW.Code)
	}
	var resp listToolsResponse
	if err := json.Unmarshal(listW.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(resp.Tools) != 1 || resp.Tools[0].Name != "lookup" {
		t.Errorf("list = %+v", resp.Tools)
	}
}

func TestToolCatalogHandler_CreateRejectsInvalidName(t *testing.T) {
	h, _, _ := newToolCatalogTestHandler()
	r := newToolCatalogRouter(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/aip/tools",
		strings.NewReader(`{"name":"bad-name"}`))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestToolCatalogHandler_CreateDuplicate(t *testing.T) {
	h, _, _ := newToolCatalogTestHandler()
	r := newToolCatalogRouter(h)
	body := `{"name":"echo"}`

	req := httptest.NewRequest(http.MethodPost, "/api/v2/aip/tools", strings.NewReader(body))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("first create status = %d", w.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/v2/aip/tools", strings.NewReader(body))
	req2 = withAuthContext(req2, "user:alice")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("dup status = %d body = %s", w2.Code, w2.Body.String())
	}
}

func TestToolCatalogHandler_NoAuth(t *testing.T) {
	h, _, _ := newToolCatalogTestHandler()
	r := newToolCatalogRouter(h)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/aip/tools", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestToolCatalogHandler_GetMissing(t *testing.T) {
	h, _, _ := newToolCatalogTestHandler()
	r := newToolCatalogRouter(h)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/aip/tools/nope", nil)
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestToolCatalogHandler_UpdateDisablesInRegistry(t *testing.T) {
	h, cat, reg := newToolCatalogTestHandler()
	r := newToolCatalogRouter(h)
	_ = cat.CreateTool(context.Background(), &ToolRecord{
		Name:               "echo",
		HandlerFunctionRID: "ri.functions.main.fn.x",
		Enabled:            true,
	})
	reg.Register(NewFunctionToolHandler(&ToolRecord{Name: "echo", HandlerFunctionRID: "ri.functions.main.fn.x"}, nil))

	disabled := false
	body, _ := json.Marshal(map[string]interface{}{"enabled": disabled})
	req := httptest.NewRequest(http.MethodPut, "/api/v2/aip/tools/echo", strings.NewReader(string(body)))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}

	for _, n := range reg.Names() {
		if n == "echo" {
			t.Errorf("echo should be unregistered after disable, registry has %v", reg.Names())
		}
	}
}

func TestToolCatalogHandler_Delete(t *testing.T) {
	h, cat, reg := newToolCatalogTestHandler()
	r := newToolCatalogRouter(h)
	_ = cat.CreateTool(context.Background(), &ToolRecord{Name: "echo", Enabled: true})
	reg.Register(NewFunctionToolHandler(&ToolRecord{Name: "echo"}, nil))

	req := httptest.NewRequest(http.MethodDelete, "/api/v2/aip/tools/echo", nil)
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d", w.Code)
	}

	if _, err := cat.GetTool(context.Background(), "echo"); err == nil {
		t.Error("expected ErrToolRecordNotFound after delete")
	}
	for _, n := range reg.Names() {
		if n == "echo" {
			t.Errorf("registry still has echo after delete, names=%v", reg.Names())
		}
	}
}

func TestToolCatalogHandler_NoCatalog(t *testing.T) {
	h := NewToolCatalogHandler(nil, nil, nil)
	r := newToolCatalogRouter(h)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/aip/tools", nil)
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}
