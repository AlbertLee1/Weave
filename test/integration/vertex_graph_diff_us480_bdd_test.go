//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/vertex/graphsvc"
)

// US-480 BDD — RFC 6902 JSON Patch diff endpoint over a real PG-backed
// PGRepo. The scenarios cover the four PRD-literal acceptance shapes
// (add node, remove node, modify edge, modify layer property) over a
// production-equivalent wire path: real testcontainers PG + real
// migrations + real graphsvc.PGRepo + real chi router.
//
// Negative control on top: an identical from==to pair must produce
// `ops: []`, proving the endpoint isn't blindly returning a fixed
// patch on every call.

type us480DiffEnvelope struct {
	RID  string             `json:"rid"`
	From int                `json:"from"`
	To   int                `json:"to"`
	Ops  []graphsvc.PatchOp `json:"ops"`
}

func setupUS480Fixture(t *testing.T) (*chi.Mux, *graphsvc.PGRepo) {
	t.Helper()
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	// system_graphs.ontology_rid carries a FK to ontologies(rid); seed a
	// row so the graph inserts in the scenarios below succeed.
	if _, err := pg.Pool.Exec(context.Background(),
		`INSERT INTO ontologies (rid, api_name, display_name) VALUES ($1, $2, $3)`,
		"ri.ontology.main.ontology.us480", "us480", "US-480 BDD"); err != nil {
		t.Fatalf("seed ontology: %v", err)
	}
	repo := graphsvc.NewPGRepo(pg.Pool)
	h := graphsvc.NewHandler(repo, graphsvc.NewMemTemplateStore())
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r, repo
}

// seedTwoVersions creates a graph at v1 then updates to v2, returning rid.
func seedTwoVersions(t *testing.T, repo *graphsvc.PGRepo, v1, v2 json.RawMessage) string {
	t.Helper()
	ctx := context.Background()
	g, err := repo.Create(ctx, "ri.ontology.main.ontology.us480", "us480 graph", "tester", v1, true)
	if err != nil {
		t.Fatalf("seed v1: %v", err)
	}
	if _, err := repo.Update(ctx, g.RID, v2); err != nil {
		t.Fatalf("seed v2: %v", err)
	}
	return g.RID
}

func doDiff(t *testing.T, r *chi.Mux, ridStr, query string) us480DiffEnvelope {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/vertex/v1/graphs/"+ridStr+"/diff?"+query, bytes.NewReader(nil))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp us480DiffEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, w.Body.String())
	}
	return resp
}

func TestBDD_US480_AddNode_EmitsAddOpAtIDKeyedPath(t *testing.T) {
	router, repo := setupUS480Fixture(t)
	rid := seedTwoVersions(t, repo,
		json.RawMessage(`{"layers":[{"id":"L1","objectType":"Customer"}],"edges":[]}`),
		json.RawMessage(`{"layers":[{"id":"L1","objectType":"Customer"},{"id":"L2","objectType":"Order"}],"edges":[]}`))

	resp := doDiff(t, router, rid, "from=1&to=2")
	if len(resp.Ops) != 1 {
		t.Fatalf("ops=%+v, want one add op", resp.Ops)
	}
	if resp.Ops[0].Op != "add" || resp.Ops[0].Path != "/layers/L2" {
		t.Errorf("ops[0]={op:%s path:%s}, want {add /layers/L2}", resp.Ops[0].Op, resp.Ops[0].Path)
	}
}

func TestBDD_US480_RemoveNode_EmitsRemoveOp(t *testing.T) {
	router, repo := setupUS480Fixture(t)
	rid := seedTwoVersions(t, repo,
		json.RawMessage(`{"layers":[{"id":"L1","objectType":"Customer"},{"id":"L2","objectType":"Order"}],"edges":[]}`),
		json.RawMessage(`{"layers":[{"id":"L1","objectType":"Customer"}],"edges":[]}`))

	resp := doDiff(t, router, rid, "from=1&to=2")
	if len(resp.Ops) != 1 {
		t.Fatalf("ops=%+v, want one remove op", resp.Ops)
	}
	if resp.Ops[0].Op != "remove" || resp.Ops[0].Path != "/layers/L2" {
		t.Errorf("ops[0]={op:%s path:%s}, want {remove /layers/L2}", resp.Ops[0].Op, resp.Ops[0].Path)
	}
	if resp.Ops[0].Value != nil {
		t.Errorf("remove ops must not carry a value (RFC 6902); got %v", resp.Ops[0].Value)
	}
}

func TestBDD_US480_ModifyEdge_EmitsReplaceOpAtNestedPath(t *testing.T) {
	router, repo := setupUS480Fixture(t)
	rid := seedTwoVersions(t, repo,
		json.RawMessage(`{"layers":[],"edges":[{"id":"E1","source":"L1","target":"L2","linkTypeRid":"ri.link.a"}]}`),
		json.RawMessage(`{"layers":[],"edges":[{"id":"E1","source":"L1","target":"L3","linkTypeRid":"ri.link.a"}]}`))

	resp := doDiff(t, router, rid, "from=1&to=2")
	if len(resp.Ops) != 1 {
		t.Fatalf("ops=%+v, want one replace op", resp.Ops)
	}
	got := resp.Ops[0]
	if got.Op != "replace" || got.Path != "/edges/E1/target" || got.Value != "L3" {
		t.Errorf("ops[0]=%+v, want {replace /edges/E1/target L3}", got)
	}
}

func TestBDD_US480_ModifyLayerProperty_EmitsReplaceOp(t *testing.T) {
	router, repo := setupUS480Fixture(t)
	rid := seedTwoVersions(t, repo,
		json.RawMessage(`{"layers":[{"id":"L1","objectType":"Customer","filter":{"status":"active"}}]}`),
		json.RawMessage(`{"layers":[{"id":"L1","objectType":"Customer","filter":{"status":"inactive"}}]}`))

	resp := doDiff(t, router, rid, "from=1&to=2")
	if len(resp.Ops) != 1 {
		t.Fatalf("ops=%+v, want one replace op", resp.Ops)
	}
	got := resp.Ops[0]
	if got.Op != "replace" || got.Path != "/layers/L1/filter/status" || got.Value != "inactive" {
		t.Errorf("ops[0]=%+v, want {replace /layers/L1/filter/status inactive}", got)
	}
}

func TestBDD_US480_IdenticalVersions_EmitsEmptyOps(t *testing.T) {
	// Negative control: a regression that always returns a hardcoded patch
	// would pass the four shape scenarios above. This one nails it shut by
	// diffing v1 against itself (from==to is allowed) and asserts ops=[].
	router, repo := setupUS480Fixture(t)
	g, err := repo.Create(context.Background(), "ri.ontology.main.ontology.us480",
		"us480 graph", "tester",
		json.RawMessage(`{"layers":[{"id":"L1","objectType":"Customer"}],"edges":[]}`), true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	resp := doDiff(t, router, g.RID, "from=1&to=1")
	if len(resp.Ops) != 0 {
		t.Errorf("ops=%+v, want empty for identical versions", resp.Ops)
	}
	// Wire shape must be `[]` not `null` — verify the marshal path keeps it.
	req := httptest.NewRequest(http.MethodGet, "/api/vertex/v1/graphs/"+g.RID+"/diff?from=1&to=1", bytes.NewReader(nil))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if !bytes.Contains(w.Body.Bytes(), []byte(`"ops":[]`)) {
		t.Errorf("body must contain `\"ops\":[]`; got %s", w.Body.String())
	}
}

func TestBDD_US480_UnknownVersion_Returns404(t *testing.T) {
	router, repo := setupUS480Fixture(t)
	g, _ := repo.Create(context.Background(), "ri.ontology.main.ontology.us480",
		"us480 graph", "tester",
		json.RawMessage(`{"layers":[]}`), true)

	req := httptest.NewRequest(http.MethodGet, "/api/vertex/v1/graphs/"+g.RID+"/diff?from=1&to=42", bytes.NewReader(nil))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", w.Code, w.Body.String())
	}
}
