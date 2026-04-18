package oms_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/oms"
)

// US-210 link-property admin CRUD tests. The mock stores below only
// implement oms.LinkPropertyStore / oms.LinkEdgeStore — both are narrow
// interfaces deliberately kept out of oms.Repository, so these tests do not
// need to touch mockRepo.

// --- in-memory LinkPropertyStore ---

type memLinkPropertyStore struct {
	byRID map[string]*oms.LinkProperty
}

func newMemLinkPropertyStore() *memLinkPropertyStore {
	return &memLinkPropertyStore{byRID: map[string]*oms.LinkProperty{}}
}

func (s *memLinkPropertyStore) CreateLinkProperty(_ context.Context, lp *oms.LinkProperty) error {
	if err := lp.Validate(); err != nil {
		return err
	}
	for _, existing := range s.byRID {
		if existing.LinkTypeRID == lp.LinkTypeRID && existing.APIName == lp.APIName {
			return oms.ErrDuplicate
		}
	}
	cp := *lp
	s.byRID[lp.RID] = &cp
	return nil
}

func (s *memLinkPropertyStore) GetLinkProperty(_ context.Context, rid string) (*oms.LinkProperty, error) {
	lp, ok := s.byRID[rid]
	if !ok {
		return nil, oms.ErrNotFound
	}
	cp := *lp
	return &cp, nil
}

func (s *memLinkPropertyStore) ListLinkProperties(_ context.Context, linkTypeRID string) ([]oms.LinkProperty, error) {
	var out []oms.LinkProperty
	for _, lp := range s.byRID {
		if lp.LinkTypeRID == linkTypeRID {
			out = append(out, *lp)
		}
	}
	return out, nil
}

func (s *memLinkPropertyStore) UpdateLinkProperty(_ context.Context, lp *oms.LinkProperty) error {
	if _, ok := s.byRID[lp.RID]; !ok {
		return oms.ErrNotFound
	}
	cp := *lp
	s.byRID[lp.RID] = &cp
	return nil
}

func (s *memLinkPropertyStore) DeleteLinkProperty(_ context.Context, rid string) error {
	if _, ok := s.byRID[rid]; !ok {
		return oms.ErrNotFound
	}
	delete(s.byRID, rid)
	return nil
}

// --- in-memory LinkEdgeStore ---

type memLinkEdgeStore struct {
	edges []oms.LinkEdge
}

func newMemLinkEdgeStore() *memLinkEdgeStore { return &memLinkEdgeStore{} }

func (s *memLinkEdgeStore) GetLinkEdge(_ context.Context, linkTypeRID, sourcePK, targetPK string) (*oms.LinkEdge, error) {
	for i := range s.edges {
		e := &s.edges[i]
		if e.LinkTypeRID == linkTypeRID && e.SourceObjectPK == sourcePK && e.TargetObjectPK == targetPK {
			cp := *e
			return &cp, nil
		}
	}
	return nil, oms.ErrNotFound
}

func (s *memLinkEdgeStore) UpsertLinkEdge(_ context.Context, edge *oms.LinkEdge) error {
	for i := range s.edges {
		e := &s.edges[i]
		if e.LinkTypeRID == edge.LinkTypeRID && e.SourceObjectPK == edge.SourceObjectPK && e.TargetObjectPK == edge.TargetObjectPK {
			e.EdgeProperties = edge.EdgeProperties
			return nil
		}
	}
	s.edges = append(s.edges, *edge)
	return nil
}

func (s *memLinkEdgeStore) DeleteLinkEdge(_ context.Context, linkTypeRID, sourcePK, targetPK string) error {
	for i := range s.edges {
		e := &s.edges[i]
		if e.LinkTypeRID == linkTypeRID && e.SourceObjectPK == sourcePK && e.TargetObjectPK == targetPK {
			s.edges = append(s.edges[:i], s.edges[i+1:]...)
			return nil
		}
	}
	return oms.ErrNotFound
}

func (s *memLinkEdgeStore) ListLinkEdgesWithProperties(_ context.Context, linkTypeRID string, sourcePKs []string) ([]oms.LinkEdge, error) {
	set := map[string]bool{}
	for _, pk := range sourcePKs {
		set[pk] = true
	}
	var out []oms.LinkEdge
	for _, e := range s.edges {
		if e.LinkTypeRID == linkTypeRID && set[e.SourceObjectPK] {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *memLinkEdgeStore) ListLinkEdgesWithPropertiesByTarget(_ context.Context, linkTypeRID string, targetPKs []string) ([]oms.LinkEdge, error) {
	set := map[string]bool{}
	for _, pk := range targetPKs {
		set[pk] = true
	}
	var out []oms.LinkEdge
	for _, e := range s.edges {
		if e.LinkTypeRID == linkTypeRID && set[e.TargetObjectPK] {
			out = append(out, e)
		}
	}
	return out, nil
}

// --- helpers ---

func newLinkPropRouter(handler *oms.OMSHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/links/{linkTypeRid}/properties", handler.ListLinkProperties)
	r.Post("/api/v2/ontologies/{ontologyApiName}/links/{linkTypeRid}/properties", handler.CreateLinkProperty)
	r.Put("/api/v2/ontologies/{ontologyApiName}/links/properties/byRid/{linkPropertyRid}", handler.UpdateLinkProperty)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/links/properties/byRid/{linkPropertyRid}", handler.DeleteLinkProperty)
	r.Put("/api/v2/ontologies/{ontologyApiName}/links/{linkTypeRid}/edges/{sourcePk}/{targetPk}/properties", handler.PutLinkEdgeProperties)
	return r
}

func seedMembershipLinkType(repo *mockRepo) (ontRID, ltRID string) {
	ontRID = seedOntology(repo)
	ltRID = "ri.ontology.main.link-type.user-group"
	repo.linkTypes = append(repo.linkTypes, oms.LinkType{
		RID:              ltRID,
		OntologyRID:      ontRID,
		APIName:          "membership",
		SourceObjectType: "user",
		TargetObjectType: "group",
		Cardinality:      "MANY_TO_MANY",
	})
	return
}

func wireLinkPropHandler(repo *mockRepo) (*oms.OMSHandler, *memLinkPropertyStore, *memLinkEdgeStore) {
	h := oms.NewOMSHandler(repo)
	propStore := newMemLinkPropertyStore()
	edgeStore := newMemLinkEdgeStore()
	h.SetLinkPropertyStore(propStore)
	h.SetLinkEdgeStore(edgeStore)
	return h, propStore, edgeStore
}

// --- schema CRUD ---

func TestCreateLinkProperty_Success(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ltRID := seedMembershipLinkType(repo)
	h, propStore, _ := wireLinkPropHandler(repo)
	r := newLinkPropRouter(h)

	body := map[string]interface{}{
		"apiName":     "role",
		"displayName": "Role",
		"baseType":    "string",
		"isNullable":  true,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/"+ontRID+"/links/"+ltRID+"/properties", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if resp["apiName"] != "role" || resp["baseType"] != "string" {
		t.Errorf("unexpected response: %v", resp)
	}
	rid, _ := resp["rid"].(string)
	if rid == "" {
		t.Fatalf("expected non-empty rid")
	}
	if _, err := propStore.GetLinkProperty(context.Background(), rid); err != nil {
		t.Fatalf("row not persisted: %v", err)
	}
}

func TestCreateLinkProperty_Duplicate(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ltRID := seedMembershipLinkType(repo)
	h, _, _ := wireLinkPropHandler(repo)
	r := newLinkPropRouter(h)

	create := func() int {
		body := map[string]interface{}{"apiName": "role", "baseType": "string"}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/"+ontRID+"/links/"+ltRID+"/properties", strings.NewReader(string(b)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	if got := create(); got != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d", got)
	}
	if got := create(); got != http.StatusConflict {
		t.Fatalf("second create: expected 409, got %d", got)
	}
}

func TestCreateLinkProperty_UnknownLinkType(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	h, _, _ := wireLinkPropHandler(repo)
	r := newLinkPropRouter(h)

	body := map[string]interface{}{"apiName": "role", "baseType": "string"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/"+ontRID+"/links/ri.nope/properties", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateLinkProperty_InvalidBaseType(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ltRID := seedMembershipLinkType(repo)
	h, _, _ := wireLinkPropHandler(repo)
	r := newLinkPropRouter(h)

	body := map[string]interface{}{"apiName": "role", "baseType": "bogus-type"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/"+ontRID+"/links/"+ltRID+"/properties", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListLinkProperties_Empty(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ltRID := seedMembershipLinkType(repo)
	h, _, _ := wireLinkPropHandler(repo)
	r := newLinkPropRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/"+ontRID+"/links/"+ltRID+"/properties", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := parseJSON(t, w.Body.Bytes())
	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatalf("expected data array, got %v", resp)
	}
	if len(data) != 0 {
		t.Errorf("expected empty list, got %d", len(data))
	}
}

func TestUpdateLinkProperty_Success(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ltRID := seedMembershipLinkType(repo)
	h, propStore, _ := wireLinkPropHandler(repo)

	lp := &oms.LinkProperty{
		RID:         "ri.ontology.main.link-property.role",
		LinkTypeRID: ltRID,
		APIName:     "role",
		BaseType:    "string",
		IsNullable:  true,
	}
	if err := propStore.CreateLinkProperty(context.Background(), lp); err != nil {
		t.Fatalf("seed: %v", err)
	}

	r := newLinkPropRouter(h)
	body := map[string]interface{}{"displayName": "Assigned Role"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/v2/ontologies/"+ontRID+"/links/properties/byRid/"+lp.RID, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	if resp["displayName"] != "Assigned Role" {
		t.Errorf("unexpected displayName: %v", resp)
	}
}

func TestDeleteLinkProperty_Success(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ltRID := seedMembershipLinkType(repo)
	h, propStore, _ := wireLinkPropHandler(repo)

	lp := &oms.LinkProperty{
		RID:         "ri.ontology.main.link-property.role",
		LinkTypeRID: ltRID,
		APIName:     "role",
		BaseType:    "string",
	}
	if err := propStore.CreateLinkProperty(context.Background(), lp); err != nil {
		t.Fatalf("seed: %v", err)
	}

	r := newLinkPropRouter(h)
	req := httptest.NewRequest(http.MethodDelete, "/api/v2/ontologies/"+ontRID+"/links/properties/byRid/"+lp.RID, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if _, err := propStore.GetLinkProperty(context.Background(), lp.RID); !errors.Is(err, oms.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

// --- edge-value endpoint ---

func TestPutLinkEdgeProperties_Success(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ltRID := seedMembershipLinkType(repo)
	h, propStore, edgeStore := wireLinkPropHandler(repo)

	_ = propStore.CreateLinkProperty(context.Background(), &oms.LinkProperty{
		RID: "ri.ontology.main.link-property.role", LinkTypeRID: ltRID,
		APIName: "role", BaseType: "string", IsNullable: true,
	})

	body := map[string]interface{}{"values": map[string]interface{}{"role": "admin"}}
	b, _ := json.Marshal(body)
	r := newLinkPropRouter(h)
	req := httptest.NewRequest(http.MethodPut,
		"/api/v2/ontologies/"+ontRID+"/links/"+ltRID+"/edges/u1/g1/properties",
		strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	edge, err := edgeStore.GetLinkEdge(context.Background(), ltRID, "u1", "g1")
	if err != nil {
		t.Fatalf("edge not written: %v", err)
	}
	var props map[string]interface{}
	_ = json.Unmarshal(edge.EdgeProperties, &props)
	if props["role"] != "admin" {
		t.Errorf("unexpected edge props: %v", props)
	}
}

func TestPutLinkEdgeProperties_UnknownProperty(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ltRID := seedMembershipLinkType(repo)
	h, _, _ := wireLinkPropHandler(repo)

	body := map[string]interface{}{"values": map[string]interface{}{"bogus": "x"}}
	b, _ := json.Marshal(body)
	r := newLinkPropRouter(h)
	req := httptest.NewRequest(http.MethodPut,
		"/api/v2/ontologies/"+ontRID+"/links/"+ltRID+"/edges/u1/g1/properties",
		strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseJSON(t, w.Body.Bytes())
	params, _ := resp["parameters"].(map[string]interface{})
	if params["parameter"] != "bogus" {
		t.Errorf("expected parameter=bogus in error params, got %v", params)
	}
}

func TestPutLinkEdgeProperties_RequiredMissing(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ltRID := seedMembershipLinkType(repo)
	h, propStore, _ := wireLinkPropHandler(repo)

	// role is NOT nullable → required.
	_ = propStore.CreateLinkProperty(context.Background(), &oms.LinkProperty{
		RID: "ri.ontology.main.link-property.role", LinkTypeRID: ltRID,
		APIName: "role", BaseType: "string", IsNullable: false,
	})

	// Empty values map: missing role should be rejected.
	body := map[string]interface{}{"values": map[string]interface{}{}}
	b, _ := json.Marshal(body)
	r := newLinkPropRouter(h)
	req := httptest.NewRequest(http.MethodPut,
		"/api/v2/ontologies/"+ontRID+"/links/"+ltRID+"/edges/u1/g1/properties",
		strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPutLinkEdgeProperties_UnknownLinkType(t *testing.T) {
	repo := &mockRepo{}
	ontRID := seedOntology(repo)
	h, _, _ := wireLinkPropHandler(repo)

	body := map[string]interface{}{"values": map[string]interface{}{}}
	b, _ := json.Marshal(body)
	r := newLinkPropRouter(h)
	req := httptest.NewRequest(http.MethodPut,
		"/api/v2/ontologies/"+ontRID+"/links/ri.nope/edges/u1/g1/properties",
		strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestPutLinkEdgeProperties_ReplacesPriorValues(t *testing.T) {
	repo := &mockRepo{}
	ontRID, ltRID := seedMembershipLinkType(repo)
	h, propStore, edgeStore := wireLinkPropHandler(repo)

	_ = propStore.CreateLinkProperty(context.Background(), &oms.LinkProperty{
		RID: "ri.ontology.main.link-property.role", LinkTypeRID: ltRID,
		APIName: "role", BaseType: "string", IsNullable: true,
	})

	put := func(role string) int {
		body := map[string]interface{}{"values": map[string]interface{}{"role": role}}
		b, _ := json.Marshal(body)
		r := newLinkPropRouter(h)
		req := httptest.NewRequest(http.MethodPut,
			"/api/v2/ontologies/"+ontRID+"/links/"+ltRID+"/edges/u1/g1/properties",
			strings.NewReader(string(b)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	if got := put("admin"); got != http.StatusOK {
		t.Fatalf("first PUT: expected 200, got %d", got)
	}
	if got := put("member"); got != http.StatusOK {
		t.Fatalf("second PUT: expected 200, got %d", got)
	}
	edge, err := edgeStore.GetLinkEdge(context.Background(), ltRID, "u1", "g1")
	if err != nil {
		t.Fatalf("edge not present: %v", err)
	}
	var props map[string]interface{}
	_ = json.Unmarshal(edge.EdgeProperties, &props)
	if props["role"] != "member" {
		t.Errorf("expected role=member after update, got %v", props)
	}
}
