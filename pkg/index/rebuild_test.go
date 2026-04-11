package index_test

import (
	"context"
	"errors"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
)

// stubRebuildRepo satisfies index.RebuildRepo with an in-memory snapshot.
type stubRebuildRepo struct {
	ontologies  map[string]oms.Ontology              // apiName/RID -> ontology
	objectTypes map[string]map[string]oms.ObjectType // ontologyRID -> apiName -> ObjectType
	properties  map[string][]oms.Property            // objectTypeRID -> properties

	getOntologyErr error
	getOTErr       error
	listPropsErr   error
}

func (s *stubRebuildRepo) GetOntology(_ context.Context, ridOrAPIName string) (*oms.Ontology, error) {
	if s.getOntologyErr != nil {
		return nil, s.getOntologyErr
	}
	if o, ok := s.ontologies[ridOrAPIName]; ok {
		return &o, nil
	}
	return nil, oms.ErrNotFound
}

func (s *stubRebuildRepo) GetObjectTypeByAPIName(_ context.Context, ontologyRID, apiName string) (*oms.ObjectType, error) {
	if s.getOTErr != nil {
		return nil, s.getOTErr
	}
	if m, ok := s.objectTypes[ontologyRID]; ok {
		if ot, ok := m[apiName]; ok {
			return &ot, nil
		}
	}
	return nil, oms.ErrNotFound
}

func (s *stubRebuildRepo) ListProperties(_ context.Context, objectTypeRID string) ([]oms.Property, error) {
	if s.listPropsErr != nil {
		return nil, s.listPropsErr
	}
	return s.properties[objectTypeRID], nil
}

// stubDocSource feeds a fixed set of latest documents for a given objectType RID.
type stubDocSource struct {
	rows map[string][]index.LatestDocument
	err  error
}

func (s *stubDocSource) LoadLatestDocuments(_ context.Context, objectTypeRID string) ([]index.LatestDocument, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.rows[objectTypeRID], nil
}

func newRebuildFixture() (*stubRebuildRepo, *stubDocSource) {
	repo := &stubRebuildRepo{
		ontologies: map[string]oms.Ontology{
			"northwind":                    {RID: "ri.ontology.main.ontology.nw", APIName: "northwind"},
			"ri.ontology.main.ontology.nw": {RID: "ri.ontology.main.ontology.nw", APIName: "northwind"},
		},
		objectTypes: map[string]map[string]oms.ObjectType{
			"ri.ontology.main.ontology.nw": {
				"Customer": {
					RID:         "ri.ontology.main.objectType.customer",
					OntologyRID: "ri.ontology.main.ontology.nw",
					APIName:     "Customer",
					PrimaryKey:  "customerId",
				},
			},
		},
		properties: map[string][]oms.Property{
			"ri.ontology.main.objectType.customer": {
				{APIName: "customerId", BaseType: "string", IsSearchable: true},
				{
					APIName:      "country",
					BaseType:     "string",
					IsSearchable: true,
					TypeConfig:   []byte(`{"analyzer":"not_analyzed"}`),
				},
			},
		},
	}
	docs := &stubDocSource{
		rows: map[string][]index.LatestDocument{
			"ri.ontology.main.objectType.customer": {
				{PrimaryKey: "ALFKI", Body: map[string]interface{}{"customerId": "ALFKI", "country": "USA"}},
				{PrimaryKey: "ANATR", Body: map[string]interface{}{"customerId": "ANATR", "country": "Mexico"}},
				{PrimaryKey: "ANTON", Body: map[string]interface{}{"customerId": "ANTON", "country": "Mexico"}},
			},
		},
	}
	return repo, docs
}

func TestRebuild_DropsAndReindexesFromSource(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo, src := newRebuildFixture()
	key := index.ScopedKey("northwind", "Customer")

	// Pre-populate with stale data to prove the rebuild wipes it.
	if _, err := mgr.EnsureIndex(key, []index.Property{{APIName: "customerId", BaseType: "string", IsSearchable: true}}); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if err := mgr.IndexDocument(key, "STALE", map[string]interface{}{"customerId": "STALE"}); err != nil {
		t.Fatalf("seed stale doc: %v", err)
	}

	res, err := index.Rebuild(context.Background(), mgr, repo, src, index.RebuildRequest{
		OntologyAPIName:   "northwind",
		ObjectTypeAPIName: "Customer",
	})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if res.IndexedCount != 3 {
		t.Errorf("IndexedCount = %d, want 3", res.IndexedCount)
	}
	if res.ScopedKey != key {
		t.Errorf("ScopedKey = %q, want %q", res.ScopedKey, key)
	}

	count, err := mgr.DocCount(key)
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if count != 3 {
		t.Errorf("DocCount after rebuild = %d, want 3", count)
	}

	// Stale doc should no longer be present.
	stale := bleve.NewDocIDQuery([]string{"STALE"})
	hits, err := mgr.Search(key, bleve.NewSearchRequest(stale))
	if err != nil {
		t.Fatalf("search stale: %v", err)
	}
	if hits.Total != 0 {
		t.Errorf("stale doc still present: total=%d", hits.Total)
	}

	// not_analyzed country should be case-sensitive exact-match post-rebuild.
	usa := bleve.NewTermQuery("USA")
	usa.SetField("country")
	hits, err = mgr.Search(key, bleve.NewSearchRequest(usa))
	if err != nil {
		t.Fatalf("search country USA: %v", err)
	}
	if hits.Total != 1 {
		t.Errorf("country=USA got total=%d, want 1", hits.Total)
	}

	usaLower := bleve.NewTermQuery("usa")
	usaLower.SetField("country")
	hits, err = mgr.Search(key, bleve.NewSearchRequest(usaLower))
	if err != nil {
		t.Fatalf("search country usa: %v", err)
	}
	if hits.Total != 0 {
		t.Errorf("not_analyzed country should NOT match lowercase usa; got total=%d", hits.Total)
	}
}

func TestRebuild_Idempotent(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo, src := newRebuildFixture()
	req := index.RebuildRequest{OntologyAPIName: "northwind", ObjectTypeAPIName: "Customer"}

	for i := 0; i < 3; i++ {
		res, err := index.Rebuild(context.Background(), mgr, repo, src, req)
		if err != nil {
			t.Fatalf("rebuild iteration %d: %v", i, err)
		}
		if res.IndexedCount != 3 {
			t.Errorf("iter %d IndexedCount=%d, want 3", i, res.IndexedCount)
		}
	}
	count, _ := mgr.DocCount(index.ScopedKey("northwind", "Customer"))
	if count != 3 {
		t.Errorf("final DocCount = %d, want 3", count)
	}
}

func TestRebuild_NilSourceYieldsEmptyIndex(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo, _ := newRebuildFixture()
	res, err := index.Rebuild(context.Background(), mgr, repo, nil, index.RebuildRequest{
		OntologyAPIName:   "northwind",
		ObjectTypeAPIName: "Customer",
	})
	if err != nil {
		t.Fatalf("Rebuild with nil source: %v", err)
	}
	if res.IndexedCount != 0 {
		t.Errorf("IndexedCount = %d, want 0", res.IndexedCount)
	}
	// Index shell should still exist.
	if mgr.GetIndex(index.ScopedKey("northwind", "Customer")) == nil {
		t.Error("expected index shell to exist")
	}
}

func TestRebuild_UnknownOntology(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo, src := newRebuildFixture()
	_, err := index.Rebuild(context.Background(), mgr, repo, src, index.RebuildRequest{
		OntologyAPIName:   "doesnotexist",
		ObjectTypeAPIName: "Customer",
	})
	if err == nil {
		t.Fatal("expected error for unknown ontology")
	}
}

func TestRebuild_UnknownObjectType(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo, src := newRebuildFixture()
	_, err := index.Rebuild(context.Background(), mgr, repo, src, index.RebuildRequest{
		OntologyAPIName:   "northwind",
		ObjectTypeAPIName: "Ghost",
	})
	if err == nil {
		t.Fatal("expected error for unknown object type")
	}
}

func TestRebuild_DocSourceError(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo, _ := newRebuildFixture()
	badSrc := &stubDocSource{err: errors.New("boom")}
	_, err := index.Rebuild(context.Background(), mgr, repo, badSrc, index.RebuildRequest{
		OntologyAPIName:   "northwind",
		ObjectTypeAPIName: "Customer",
	})
	if err == nil {
		t.Fatal("expected error from doc source propagation")
	}
}

func TestRebuild_NilManager(t *testing.T) {
	repo, src := newRebuildFixture()
	_, err := index.Rebuild(context.Background(), nil, repo, src, index.RebuildRequest{
		OntologyAPIName:   "northwind",
		ObjectTypeAPIName: "Customer",
	})
	if err == nil {
		t.Fatal("expected nil manager to error")
	}
}
