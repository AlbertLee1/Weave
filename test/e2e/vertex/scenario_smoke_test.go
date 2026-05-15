// Package vertex_test holds the Phase 1 end-to-end smoke for the Vertex
// scenario read-overlay stack (VTX-001 ~ VTX-005). The smoke proves the
// migration (scenario_edits), the scenarios.Repo, the FoldObject algorithm,
// and the OSS X-Scenario-Id handler integration line up through a real
// chi HTTP router. No PostgreSQL: an in-memory scenarios.Repo and an
// in-memory oss.Service stub seed Customer/ALFKI by hand.
package vertex_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/rid"
	"github.com/liyang/weave/pkg/scenarios"
)

// ---------------------------------------------------------------------------
// In-memory scenarios.Repo — only the methods the smoke exercises.
// ---------------------------------------------------------------------------

type memScenarioRepo struct {
	mu          sync.Mutex
	caseStudies map[string]*scenarios.CaseStudy
	scenarios   map[string]*scenarios.Scenario
	edits       map[string][]scenarios.ScenarioEdit
	nextSeq     int64
}

func newMemScenarioRepo() *memScenarioRepo {
	return &memScenarioRepo{
		caseStudies: map[string]*scenarios.CaseStudy{},
		scenarios:   map[string]*scenarios.Scenario{},
		edits:       map[string][]scenarios.ScenarioEdit{},
	}
}

func (r *memScenarioRepo) CreateCaseStudy(_ context.Context, name, ontologyRID, createdBy string) (*scenarios.CaseStudy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cs := &scenarios.CaseStudy{
		RID:         rid.New("vertex", "main", "case-study"),
		Name:        name,
		OntologyRID: ontologyRID,
		CreatedBy:   createdBy,
		CreatedAt:   time.Now(),
	}
	r.caseStudies[cs.RID] = cs
	return cs, nil
}

func (r *memScenarioRepo) GetCaseStudy(_ context.Context, rid string) (*scenarios.CaseStudy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cs, ok := r.caseStudies[rid]
	if !ok {
		return nil, scenarios.ErrScenarioNotFound
	}
	cp := *cs
	return &cp, nil
}

func (r *memScenarioRepo) CreateScenario(_ context.Context, caseStudyRID, name, parentCommit, createdBy string) (*scenarios.Scenario, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := &scenarios.Scenario{
		RID:                  rid.New("vertex", "main", "scenario"),
		CaseStudyRID:         caseStudyRID,
		Name:                 name,
		ParentOntologyCommit: parentCommit,
		Status:               "draft",
		Immutable:            false,
		CreatedBy:            createdBy,
		CreatedAt:            time.Now(),
	}
	r.scenarios[s.RID] = s
	return s, nil
}

func (r *memScenarioRepo) GetScenario(_ context.Context, sid string) (*scenarios.Scenario, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.scenarios[sid]
	if !ok {
		return nil, scenarios.ErrScenarioNotFound
	}
	cp := *s
	return &cp, nil
}

func (r *memScenarioRepo) AppendEdit(_ context.Context, sid string, edit scenarios.ScenarioEdit) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.scenarios[sid]
	if !ok {
		return scenarios.ErrScenarioNotFound
	}
	if s.Immutable {
		return scenarios.ErrScenarioImmutable
	}
	r.nextSeq++
	edit.ScenarioRID = sid
	edit.Seq = r.nextSeq
	edit.CreatedAt = time.Now()
	r.edits[sid] = append(r.edits[sid], edit)
	return nil
}

func (r *memScenarioRepo) ListEdits(_ context.Context, sid string) ([]scenarios.ScenarioEdit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.scenarios[sid]; !ok {
		return nil, scenarios.ErrScenarioNotFound
	}
	out := make([]scenarios.ScenarioEdit, len(r.edits[sid]))
	copy(out, r.edits[sid])
	return out, nil
}

// Unused methods — present only to satisfy the full scenarios.Repo contract.
func (r *memScenarioRepo) Freeze(_ context.Context, _ string) error { return nil }
func (r *memScenarioRepo) UpsertOverride(_ context.Context, _ scenarios.ScenarioOverride) error {
	return nil
}
func (r *memScenarioRepo) ListOverrides(_ context.Context, _ string) ([]scenarios.ScenarioOverride, error) {
	return nil, nil
}

var _ scenarios.Repo = (*memScenarioRepo)(nil)

// ---------------------------------------------------------------------------
// Minimal in-memory oss.Service for one object: Customer/ALFKI.
// ---------------------------------------------------------------------------

type memOSSService struct {
	objects map[string]*oss.WireObject
}

func (s *memOSSService) GetObject(_ context.Context, req oss.GetObjectRequest) (*oss.WireObject, error) {
	key := req.ObjectType + "/" + req.PrimaryKey
	obj, ok := s.objects[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return obj, nil
}

func (s *memOSSService) ListObjects(_ context.Context, req oss.ListObjectsRequest) (*oss.ObjectPage, error) {
	var rows []*oss.WireObject
	for k, v := range s.objects {
		// keys are "Type/PK"
		if len(k) > len(req.ObjectType)+1 && k[:len(req.ObjectType)+1] == req.ObjectType+"/" {
			rows = append(rows, v)
		}
	}
	return &oss.ObjectPage{Data: rows}, nil
}
func (s *memOSSService) SearchObjects(_ context.Context, _ oss.SearchObjectsRequest) (*oss.ObjectPage, error) {
	panic("not used")
}
func (s *memOSSService) ListLinkedObjects(_ context.Context, _ oss.LinkedObjectsRequest) (*oss.ObjectPage, error) {
	panic("not used")
}
func (s *memOSSService) GetLinkedObject(_ context.Context, _ oss.GetLinkedObjectRequest) (*oss.WireObject, error) {
	panic("not used")
}
func (s *memOSSService) CountObjects(_ context.Context, _ oss.CountObjectsRequest) (*oss.CountObjectsResponse, error) {
	panic("not used")
}

var _ oss.Service = (*memOSSService)(nil)

// ---------------------------------------------------------------------------
// Smoke test
// ---------------------------------------------------------------------------

func TestScenarioSmoke_Given_ModifyEdit_When_ReadWithOverlay_Then_ReflectsEdit(t *testing.T) {
	ctx := context.Background()

	// Seed: Customer/ALFKI baseline (northwind canonical row).
	const ontologyRID = "ri.ontology.main.ontology.northwind"
	svc := &memOSSService{
		objects: map[string]*oss.WireObject{
			"Customer/ALFKI": {
				PrimaryKey: "ALFKI",
				APIName:    "Customer",
				Properties: map[string]any{
					"companyName": "Alfreds Futterkiste",
					"country":     "Germany",
				},
			},
		},
	}

	// Seed: case study + scenario + a single modifyProperty edit.
	scenRepo := newMemScenarioRepo()
	cs, err := scenRepo.CreateCaseStudy(ctx, "alfki-rebrand", ontologyRID, "albert")
	if err != nil {
		t.Fatalf("CreateCaseStudy: %v", err)
	}
	scen, err := scenRepo.CreateScenario(ctx, cs.RID, "Rebrand ALFKI", ontologyRID, "albert")
	if err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}
	editValue, _ := json.Marshal("Acme Futterkiste")
	if err := scenRepo.AppendEdit(ctx, scen.RID, scenarios.ScenarioEdit{
		Op:         "modifyProperty",
		ObjectType: "Customer",
		ObjectID:   "ALFKI",
		Property:   "companyName",
		NewValue:   editValue,
	}); err != nil {
		t.Fatalf("AppendEdit: %v", err)
	}

	// Wire the HTTP stack: chi router + handler + scenario reader.
	h := oss.NewHandler(svc)
	h.SetScenarioReader(scenRepo)
	router := chi.NewRouter()
	h.RegisterRoutes(router)

	// --- Step 1: header-less request returns base (互不污染). ---
	t.Run("without header returns base", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontologyRID+"/objects/Customer/ALFKI", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status: got %d want 200; body=%s", rec.Code, rec.Body.String())
		}
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got["companyName"].(string) != "Alfreds Futterkiste" {
			t.Errorf("companyName: got %v want Alfreds Futterkiste (base, no overlay leak)", got["companyName"])
		}
		if got["country"].(string) != "Germany" {
			t.Errorf("country: got %v want Germany", got["country"])
		}
	})

	// --- Step 2: same request with X-Scenario-Id returns overlay. ---
	t.Run("with X-Scenario-Id returns overlay", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontologyRID+"/objects/Customer/ALFKI", nil)
		req.Header.Set("X-Scenario-Id", scen.RID)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status: got %d want 200; body=%s", rec.Code, rec.Body.String())
		}
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got["companyName"].(string) != "Acme Futterkiste" {
			t.Errorf("companyName: got %v want Acme Futterkiste (overlay)", got["companyName"])
		}
		if got["country"].(string) != "Germany" {
			t.Errorf("country: got %v want Germany (untouched property carried through)", got["country"])
		}
	})

	// --- Step 3: header-less request AFTER the overlay request still
	// returns base — no leakage between requests, no state mutation. ---
	t.Run("base read after overlay read is unaffected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontologyRID+"/objects/Customer/ALFKI", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var got map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if got["companyName"].(string) != "Alfreds Futterkiste" {
			t.Errorf("base companyName mutated after overlay read: got %v", got["companyName"])
		}
	})
}
