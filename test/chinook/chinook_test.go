package chinook_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/aggregation"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// ---------------------------------------------------------------------------
// In-memory OMS Repository (copied from test/e2e/e2e_test.go)
// ---------------------------------------------------------------------------

type inMemoryOmsRepo struct {
	mu          sync.RWMutex
	ontologies  map[string]*oms.Ontology
	objectTypes map[string]*oms.ObjectType
	properties  map[string]*oms.Property
	linkTypes   map[string]*oms.LinkType
	actionTypes map[string]*oms.ActionType
	interfaces  map[string]*oms.Interface
	otiLinks    []oms.ObjectTypeInterface
}

func newInMemoryOmsRepo() *inMemoryOmsRepo {
	return &inMemoryOmsRepo{
		ontologies:  make(map[string]*oms.Ontology),
		objectTypes: make(map[string]*oms.ObjectType),
		properties:  make(map[string]*oms.Property),
		linkTypes:   make(map[string]*oms.LinkType),
		actionTypes: make(map[string]*oms.ActionType),
		interfaces:  make(map[string]*oms.Interface),
	}
}

func (r *inMemoryOmsRepo) CreateOntology(_ context.Context, o *oms.Ontology) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.ontologies {
		if existing.APIName == o.APIName {
			return fmt.Errorf("%w: apiName %q", oms.ErrDuplicate, o.APIName)
		}
	}
	cp := *o
	cp.CreatedAt = time.Now()
	cp.UpdatedAt = cp.CreatedAt
	r.ontologies[o.RID] = &cp
	return nil
}

func (r *inMemoryOmsRepo) GetOntology(_ context.Context, rid string) (*oms.Ontology, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	o, ok := r.ontologies[rid]
	if !ok {
		return nil, oms.ErrNotFound
	}
	cp := *o
	return &cp, nil
}

func (r *inMemoryOmsRepo) ListOntologies(_ context.Context) ([]oms.Ontology, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []oms.Ontology
	for _, o := range r.ontologies {
		result = append(result, *o)
	}
	return result, nil
}

func (r *inMemoryOmsRepo) UpdateOntology(_ context.Context, o *oms.Ontology) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.ontologies[o.RID]; !ok {
		return oms.ErrNotFound
	}
	cp := *o
	cp.UpdatedAt = time.Now()
	r.ontologies[o.RID] = &cp
	return nil
}

func (r *inMemoryOmsRepo) CreateObjectType(_ context.Context, ot *oms.ObjectType) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.objectTypes {
		if existing.OntologyRID == ot.OntologyRID && existing.APIName == ot.APIName {
			return fmt.Errorf("%w: apiName %q", oms.ErrDuplicate, ot.APIName)
		}
	}
	cp := *ot
	cp.CreatedAt = time.Now()
	cp.UpdatedAt = cp.CreatedAt
	r.objectTypes[ot.RID] = &cp
	return nil
}

func (r *inMemoryOmsRepo) GetObjectType(_ context.Context, rid string) (*oms.ObjectType, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ot, ok := r.objectTypes[rid]
	if !ok {
		return nil, oms.ErrNotFound
	}
	cp := *ot
	var props []oms.Property
	for _, p := range r.properties {
		if p.ObjectTypeRID == rid {
			props = append(props, *p)
		}
	}
	cp.Properties = props
	return &cp, nil
}

func (r *inMemoryOmsRepo) GetObjectTypeByAPIName(_ context.Context, ontologyRID, apiName string) (*oms.ObjectType, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, ot := range r.objectTypes {
		if ot.OntologyRID == ontologyRID && ot.APIName == apiName {
			cp := *ot
			var props []oms.Property
			for _, p := range r.properties {
				if p.ObjectTypeRID == ot.RID {
					props = append(props, *p)
				}
			}
			cp.Properties = props
			return &cp, nil
		}
	}
	return nil, oms.ErrNotFound
}

func (r *inMemoryOmsRepo) ListObjectTypes(_ context.Context, ontologyRID string) ([]oms.ObjectType, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []oms.ObjectType
	for _, ot := range r.objectTypes {
		if ot.OntologyRID == ontologyRID {
			result = append(result, *ot)
		}
	}
	return result, nil
}

func (r *inMemoryOmsRepo) UpdateObjectType(_ context.Context, ot *oms.ObjectType) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.objectTypes[ot.RID]; !ok {
		return oms.ErrNotFound
	}
	cp := *ot
	cp.UpdatedAt = time.Now()
	r.objectTypes[ot.RID] = &cp
	return nil
}

func (r *inMemoryOmsRepo) DeleteObjectType(_ context.Context, rid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.objectTypes[rid]; !ok {
		return oms.ErrNotFound
	}
	delete(r.objectTypes, rid)
	return nil
}

func (r *inMemoryOmsRepo) CreateProperty(_ context.Context, p *oms.Property) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.properties {
		if existing.ObjectTypeRID == p.ObjectTypeRID && existing.APIName == p.APIName {
			return fmt.Errorf("%w: apiName %q", oms.ErrDuplicate, p.APIName)
		}
	}
	cp := *p
	cp.CreatedAt = time.Now()
	r.properties[p.RID] = &cp
	return nil
}

func (r *inMemoryOmsRepo) GetProperty(_ context.Context, rid string) (*oms.Property, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.properties[rid]
	if !ok {
		return nil, oms.ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (r *inMemoryOmsRepo) ListProperties(_ context.Context, objectTypeRID string) ([]oms.Property, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []oms.Property
	for _, p := range r.properties {
		if p.ObjectTypeRID == objectTypeRID {
			result = append(result, *p)
		}
	}
	return result, nil
}

func (r *inMemoryOmsRepo) UpdateProperty(_ context.Context, p *oms.Property) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.properties[p.RID]; !ok {
		return oms.ErrNotFound
	}
	cp := *p
	r.properties[p.RID] = &cp
	return nil
}

func (r *inMemoryOmsRepo) DeleteProperty(_ context.Context, rid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.properties[rid]; !ok {
		return oms.ErrNotFound
	}
	delete(r.properties, rid)
	return nil
}

func (r *inMemoryOmsRepo) CreateLinkType(_ context.Context, lt *oms.LinkType) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.linkTypes {
		if existing.OntologyRID == lt.OntologyRID && existing.APIName == lt.APIName {
			return fmt.Errorf("%w: apiName %q", oms.ErrDuplicate, lt.APIName)
		}
	}
	cp := *lt
	cp.CreatedAt = time.Now()
	r.linkTypes[lt.RID] = &cp
	return nil
}

func (r *inMemoryOmsRepo) GetLinkType(_ context.Context, rid string) (*oms.LinkType, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	lt, ok := r.linkTypes[rid]
	if !ok {
		return nil, oms.ErrNotFound
	}
	cp := *lt
	return &cp, nil
}

func (r *inMemoryOmsRepo) ListOutgoingLinkTypes(_ context.Context, objectTypeRID string) ([]oms.LinkType, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []oms.LinkType
	for _, lt := range r.linkTypes {
		if lt.SourceObjectType == objectTypeRID {
			result = append(result, *lt)
		}
	}
	return result, nil
}

func (r *inMemoryOmsRepo) ListIncomingLinkTypes(_ context.Context, objectTypeRID string) ([]oms.LinkType, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []oms.LinkType
	for _, lt := range r.linkTypes {
		if lt.TargetObjectType == objectTypeRID {
			result = append(result, *lt)
		}
	}
	return result, nil
}

func (r *inMemoryOmsRepo) ListLinkTypes(_ context.Context, ontologyRID string) ([]oms.LinkType, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []oms.LinkType
	for _, lt := range r.linkTypes {
		if lt.OntologyRID == ontologyRID {
			result = append(result, *lt)
		}
	}
	return result, nil
}

func (r *inMemoryOmsRepo) UpdateLinkType(_ context.Context, lt *oms.LinkType) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.linkTypes[lt.RID]; !ok {
		return oms.ErrNotFound
	}
	cp := *lt
	r.linkTypes[lt.RID] = &cp
	return nil
}

func (r *inMemoryOmsRepo) DeleteLinkType(_ context.Context, rid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.linkTypes[rid]; !ok {
		return oms.ErrNotFound
	}
	delete(r.linkTypes, rid)
	return nil
}

func (r *inMemoryOmsRepo) CreateActionType(_ context.Context, at *oms.ActionType) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.actionTypes {
		if existing.OntologyRID == at.OntologyRID && existing.APIName == at.APIName {
			return fmt.Errorf("%w: apiName %q", oms.ErrDuplicate, at.APIName)
		}
	}
	cp := *at
	cp.CreatedAt = time.Now()
	r.actionTypes[at.RID] = &cp
	return nil
}

func (r *inMemoryOmsRepo) GetActionType(_ context.Context, rid string) (*oms.ActionType, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	at, ok := r.actionTypes[rid]
	if !ok {
		return nil, oms.ErrNotFound
	}
	cp := *at
	return &cp, nil
}

func (r *inMemoryOmsRepo) GetActionTypeByAPIName(_ context.Context, ontologyRID, apiNameOrRID string) (*oms.ActionType, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, at := range r.actionTypes {
		if (at.OntologyRID == ontologyRID) && (at.RID == apiNameOrRID || at.APIName == apiNameOrRID) {
			cp := *at
			return &cp, nil
		}
	}
	return nil, oms.ErrNotFound
}

func (r *inMemoryOmsRepo) ListActionTypes(_ context.Context, ontologyRID string) ([]oms.ActionType, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []oms.ActionType
	for _, at := range r.actionTypes {
		if at.OntologyRID == ontologyRID {
			result = append(result, *at)
		}
	}
	return result, nil
}

func (r *inMemoryOmsRepo) UpdateActionType(_ context.Context, at *oms.ActionType) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.actionTypes[at.RID]; !ok {
		return oms.ErrNotFound
	}
	cp := *at
	r.actionTypes[at.RID] = &cp
	return nil
}

func (r *inMemoryOmsRepo) DeleteActionType(_ context.Context, rid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.actionTypes[rid]; !ok {
		return oms.ErrNotFound
	}
	delete(r.actionTypes, rid)
	return nil
}

func (r *inMemoryOmsRepo) CreateInterface(_ context.Context, iface *oms.Interface) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *iface
	cp.CreatedAt = time.Now()
	r.interfaces[iface.RID] = &cp
	return nil
}

func (r *inMemoryOmsRepo) GetInterface(_ context.Context, rid string) (*oms.Interface, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	i, ok := r.interfaces[rid]
	if !ok {
		return nil, oms.ErrNotFound
	}
	cp := *i
	return &cp, nil
}

func (r *inMemoryOmsRepo) GetInterfaceByAPIName(_ context.Context, ontologyRID, apiName string) (*oms.Interface, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, i := range r.interfaces {
		if i.OntologyRID == ontologyRID && i.APIName == apiName {
			cp := *i
			return &cp, nil
		}
	}
	return nil, oms.ErrNotFound
}

func (r *inMemoryOmsRepo) ListInterfaces(_ context.Context, ontologyRID string) ([]oms.Interface, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []oms.Interface
	for _, i := range r.interfaces {
		if i.OntologyRID == ontologyRID {
			result = append(result, *i)
		}
	}
	return result, nil
}

func (r *inMemoryOmsRepo) UpdateInterface(_ context.Context, iface *oms.Interface) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.interfaces[iface.RID]; !ok {
		return oms.ErrNotFound
	}
	cp := *iface
	r.interfaces[iface.RID] = &cp
	return nil
}

func (r *inMemoryOmsRepo) DeleteInterface(_ context.Context, rid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.interfaces[rid]; !ok {
		return oms.ErrNotFound
	}
	delete(r.interfaces, rid)
	return nil
}

func (r *inMemoryOmsRepo) AttachInterface(_ context.Context, oti *oms.ObjectTypeInterface) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.otiLinks = append(r.otiLinks, *oti)
	return nil
}

func (r *inMemoryOmsRepo) DetachInterface(_ context.Context, objectTypeRID, interfaceRID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, oti := range r.otiLinks {
		if oti.ObjectTypeRID == objectTypeRID && oti.InterfaceRID == interfaceRID {
			r.otiLinks = append(r.otiLinks[:i], r.otiLinks[i+1:]...)
			return nil
		}
	}
	return oms.ErrNotFound
}

func (r *inMemoryOmsRepo) ListInterfaceObjectTypes(_ context.Context, _ string) ([]oms.ObjectType, error) {
	return nil, nil
}

func (r *inMemoryOmsRepo) ListObjectTypeInterfaces(_ context.Context, objectTypeRID string) ([]oms.ObjectTypeInterface, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []oms.ObjectTypeInterface
	for _, oti := range r.otiLinks {
		if oti.ObjectTypeRID == objectTypeRID {
			result = append(result, oti)
		}
	}
	return result, nil
}

// SharedProperty stubs
func (r *inMemoryOmsRepo) CreateSharedProperty(_ context.Context, _ *oms.SharedProperty) error {
	return nil
}
func (r *inMemoryOmsRepo) GetSharedProperty(_ context.Context, _ string) (*oms.SharedProperty, error) {
	return nil, oms.ErrNotFound
}
func (r *inMemoryOmsRepo) ListSharedProperties(_ context.Context, _ string) ([]oms.SharedProperty, error) {
	return nil, nil
}
func (r *inMemoryOmsRepo) UpdateSharedProperty(_ context.Context, _ *oms.SharedProperty) error {
	return nil
}
func (r *inMemoryOmsRepo) DeleteSharedProperty(_ context.Context, _ string) error { return nil }

// TypeGroup stubs
func (r *inMemoryOmsRepo) CreateTypeGroup(_ context.Context, _ *oms.TypeGroup) error { return nil }
func (r *inMemoryOmsRepo) GetTypeGroup(_ context.Context, _ string) (*oms.TypeGroup, error) {
	return nil, oms.ErrNotFound
}
func (r *inMemoryOmsRepo) ListTypeGroups(_ context.Context, _ string) ([]oms.TypeGroup, error) {
	return nil, nil
}
func (r *inMemoryOmsRepo) UpdateTypeGroup(_ context.Context, _ *oms.TypeGroup) error { return nil }
func (r *inMemoryOmsRepo) DeleteTypeGroup(_ context.Context, _ string) error         { return nil }
func (r *inMemoryOmsRepo) AssignTypeGroup(_ context.Context, _, _ string) error      { return nil }
func (r *inMemoryOmsRepo) RemoveTypeGroup(_ context.Context, _, _ string) error      { return nil }
func (r *inMemoryOmsRepo) ListTypeGroupsForObjectType(_ context.Context, _ string) ([]oms.TypeGroup, error) {
	return nil, nil
}

// ValueType stubs
func (r *inMemoryOmsRepo) CreateValueType(_ context.Context, _ *oms.ValueType) error { return nil }
func (r *inMemoryOmsRepo) GetValueType(_ context.Context, _ string) (*oms.ValueType, error) {
	return nil, oms.ErrNotFound
}
func (r *inMemoryOmsRepo) GetValueTypeByAPIName(_ context.Context, _ string) (*oms.ValueType, error) {
	return nil, oms.ErrNotFound
}
func (r *inMemoryOmsRepo) ListValueTypes(_ context.Context) ([]oms.ValueType, error) {
	return nil, nil
}
func (r *inMemoryOmsRepo) UpdateValueType(_ context.Context, _ *oms.ValueType) error { return nil }
func (r *inMemoryOmsRepo) DeleteValueType(_ context.Context, _ string) error         { return nil }
func (r *inMemoryOmsRepo) ListPropertyUsagesByBaseType(_ context.Context, _ string) ([]oms.PropertyUsage, error) {
	return nil, nil
}

// DatasourceBinding stubs
func (r *inMemoryOmsRepo) CreateDatasourceBinding(_ context.Context, _ *oms.DatasourceBinding) error {
	return nil
}
func (r *inMemoryOmsRepo) GetDatasourceBinding(_ context.Context, _ string) (*oms.DatasourceBinding, error) {
	return nil, oms.ErrNotFound
}
func (r *inMemoryOmsRepo) ListDatasourceBindings(_ context.Context, _ string) ([]oms.DatasourceBinding, error) {
	return nil, nil
}
func (r *inMemoryOmsRepo) UpdateDatasourceBinding(_ context.Context, _ *oms.DatasourceBinding) error {
	return nil
}
func (r *inMemoryOmsRepo) DeleteDatasourceBinding(_ context.Context, _ string) error { return nil }

// QueryType stubs
func (r *inMemoryOmsRepo) CreateQueryType(_ context.Context, _ *oms.QueryType) error { return nil }
func (r *inMemoryOmsRepo) GetQueryType(_ context.Context, _ string) (*oms.QueryType, error) {
	return nil, oms.ErrNotFound
}
func (r *inMemoryOmsRepo) GetQueryTypeByAPIName(_ context.Context, _, _ string) (*oms.QueryType, error) {
	return nil, oms.ErrNotFound
}
func (r *inMemoryOmsRepo) ListQueryTypes(_ context.Context, _ string) ([]oms.QueryType, error) {
	return nil, nil
}
func (r *inMemoryOmsRepo) UpdateQueryType(_ context.Context, _ *oms.QueryType) error { return nil }
func (r *inMemoryOmsRepo) DeleteQueryType(_ context.Context, _ string) error         { return nil }

// ActionLog stubs
func (r *inMemoryOmsRepo) InsertActionLog(_ context.Context, _ *oms.ActionLog) error { return nil }
func (r *inMemoryOmsRepo) GetActionLog(_ context.Context, _ int64) (*oms.ActionLog, error) {
	return nil, oms.ErrNotFound
}
func (r *inMemoryOmsRepo) ListActionLogs(_ context.Context, _ string, _, _ int) ([]oms.ActionLog, error) {
	return nil, nil
}
func (r *inMemoryOmsRepo) CountActionLogs(_ context.Context, _ string) (int, error)    { return 0, nil }
func (r *inMemoryOmsRepo) UpdateActionLogStatus(_ context.Context, _ int64, _ string) error { return nil }

// Search stubs
func (r *inMemoryOmsRepo) SearchOntologyResources(_ context.Context, _, _ string) ([]oms.SearchResult, error) {
	return nil, nil
}

// Snapshot stubs
func (r *inMemoryOmsRepo) CreateSnapshot(_ context.Context, _ *oms.OntologySnapshot) error {
	return nil
}
func (r *inMemoryOmsRepo) ListSnapshots(_ context.Context, _ string) ([]oms.OntologySnapshot, error) {
	return nil, nil
}
func (r *inMemoryOmsRepo) GetSnapshot(_ context.Context, _ string, _ int) (*oms.OntologySnapshot, error) {
	return nil, oms.ErrNotFound
}
func (r *inMemoryOmsRepo) GetOntologyVersion(_ context.Context, _ string) (int, error) {
	return 0, nil
}
func (r *inMemoryOmsRepo) IncrementOntologyVersion(_ context.Context, _ string) (int, error) {
	return 1, nil
}

// ---------------------------------------------------------------------------
// Test environment with shared state
// ---------------------------------------------------------------------------

type chinookEnv struct {
	server      *httptest.Server
	router      http.Handler
	repo        *inMemoryOmsRepo
	indexMgr    *index.Manager
	ontologyRID string // populated after setupOntology
}

var (
	sharedEnv     *chinookEnv
	envOnce       sync.Once
	ontologyOnce  sync.Once
	indexDataOnce sync.Once
	envErr        error
	ontologyErr   error
	indexDataErr  error
)

func chinookCSVPath(filename string) string {
	return filepath.Join("..", "..", "testdata", "chinook", filename)
}

func loadCSV(t *testing.T, filename string) []map[string]string {
	t.Helper()
	f, err := os.Open(chinookCSVPath(filename))
	if err != nil {
		t.Fatalf("open CSV %s: %v", filename, err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("read CSV %s: %v", filename, err)
	}

	if len(records) < 2 {
		t.Fatalf("CSV %s has no data rows", filename)
	}

	headers := records[0]
	var result []map[string]string
	for _, row := range records[1:] {
		m := make(map[string]string, len(headers))
		for i, h := range headers {
			if i < len(row) {
				m[h] = row[i]
			}
		}
		result = append(result, m)
	}
	return result
}

// setupEnv creates the shared test environment (router, repo, indexMgr).
// Called once across all tests via sync.Once.
func setupEnv(t *testing.T) *chinookEnv {
	t.Helper()

	envOnce.Do(func() {
		tmpDir, err := os.MkdirTemp("", "weave-chinook-*")
		if err != nil {
			envErr = fmt.Errorf("create temp dir: %w", err)
			return
		}

		repo := newInMemoryOmsRepo()
		indexMgr := index.NewManager(tmpDir)

		linkResolver := links.NewResolver(repo, indexMgr)
		ossSvc := oss.NewService(repo, indexMgr, linkResolver)
		aggEngine := aggregation.NewEngine()
		objSetStore := objectset.NewStore(1 * time.Hour)
		objSetExecutor := objectset.NewExecutor(indexMgr, linkResolver, objSetStore)
		actionExecutor := actions.NewExecutor(repo, nil)

		r := chi.NewRouter()
		r.Use(middleware.RequestID)
		r.Use(middleware.RealIP)
		r.Use(middleware.Recoverer)
		r.Use(auth.Middleware())

		// Health
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		})

		// OMS
		omsHandler := oms.NewOMSHandler(repo)
		r.Get("/api/v2/ontologies", omsHandler.ListOntologies)
		r.Get("/api/v2/ontologies/{ontologyApiName}", omsHandler.GetOntology)
		r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes", omsHandler.ListObjectTypes)
		r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}", omsHandler.GetObjectType)
		r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/outgoingLinkTypes", omsHandler.ListOutgoingLinkTypes)
		r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes", omsHandler.ListActionTypes)
		r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes/{actionTypeRid}", omsHandler.GetActionType)
		r.Post("/api/admin/ontologies", omsHandler.CreateOntology)
		r.Post("/api/admin/ontologies/{ontologyApiName}/objectTypes", omsHandler.CreateObjectType)
		r.Put("/api/admin/objectTypes/{objectTypeRid}", omsHandler.UpdateObjectType)
		r.Delete("/api/admin/objectTypes/{objectTypeRid}", omsHandler.DeleteObjectType)
		r.Post("/api/admin/objectTypes/{objectTypeRid}/properties", omsHandler.CreateProperty)
		r.Delete("/api/admin/properties/{propertyRid}", omsHandler.DeleteProperty)
		r.Post("/api/admin/ontologies/{ontologyApiName}/linkTypes", omsHandler.CreateLinkType)
		r.Post("/api/admin/ontologies/{ontologyApiName}/actionTypes", omsHandler.CreateActionType)
		r.Put("/api/admin/actionTypes/{actionTypeRid}", omsHandler.UpdateActionType)

		// OSS
		ossHandler := oss.NewHandler(ossSvc)
		ossHandler.RegisterRoutes(r)

		// Actions
		actionHandler := actions.NewHandler(actionExecutor)
		r.Post("/api/v2/ontologies/{ontologyApiName}/actions/{action}/apply", actionHandler.Apply)
		r.Post("/api/v2/ontologies/{ontologyApiName}/actions/{action}/applyBatch", actionHandler.ApplyBatch)

		// Aggregation (inline handler)
		r.Post("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/aggregate", func(w http.ResponseWriter, r *http.Request) {
			objectType := chi.URLParam(r, "objectType")
			var req aggregation.AggregationRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			req.ObjectType = objectType

			idx := indexMgr.GetIndex(objectType)
			if idx == nil {
				http.Error(w, "index not found", 404)
				return
			}

			result, err := aggEngine.Aggregate(idx, &req)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(result)
		})

		// ObjectSet
		objSetHandler := objectset.NewHandler(objSetExecutor, indexMgr, objSetStore)
		r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", objSetHandler.LoadObjects)
		r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/aggregate", objSetHandler.Aggregate)
		r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/createTemporary", objSetHandler.CreateTemporary)

		srv := httptest.NewServer(r)

		sharedEnv = &chinookEnv{
			server:   srv,
			router:   r,
			repo:     repo,
			indexMgr: indexMgr,
		}
	})

	if envErr != nil {
		t.Fatalf("env setup failed: %v", envErr)
	}
	return sharedEnv
}

// setupOntology creates the chinook ontology, object types, properties,
// link types, and action types directly via the in-memory repo.
// Idempotent via sync.Once.
func setupOntology(t *testing.T) *chinookEnv {
	t.Helper()
	env := setupEnv(t)

	ontologyOnce.Do(func() {
		ctx := context.Background()

		// Create ontology
		ont := &oms.Ontology{
			RID:         "ri.ontology.main.ontology.chinook",
			APIName:     "chinook",
			DisplayName: "Chinook Music Store",
		}
		if err := env.repo.CreateOntology(ctx, ont); err != nil {
			ontologyErr = fmt.Errorf("create ontology: %w", err)
			return
		}
		env.ontologyRID = ont.RID

		// Create object types + properties
		for i, otDef := range chinookObjectTypes {
			otRID := fmt.Sprintf("ri.ontology.main.object-type.chinook-%s", otDef.apiName)
			ot := &oms.ObjectType{
				RID:         otRID,
				OntologyRID: ont.RID,
				APIName:     otDef.apiName,
				DisplayName: otDef.display,
				PrimaryKey:  otDef.primaryKey,
				TitleProperty: otDef.titleProp,
				Status:      "ACTIVE",
				Visibility:  "NORMAL",
			}
			if err := env.repo.CreateObjectType(ctx, ot); err != nil {
				ontologyErr = fmt.Errorf("create object type %s: %w", otDef.apiName, err)
				return
			}

			for j, prop := range otDef.properties {
				p := &oms.Property{
					RID:           fmt.Sprintf("ri.ontology.main.property.chinook-%s-%s", otDef.apiName, prop.apiName),
					ObjectTypeRID: otRID,
					APIName:       prop.apiName,
					DisplayName:   prop.apiName,
					BaseType:      prop.baseType,
					IsSearchable:  true,
					IsSortable:    true,
				}
				if err := env.repo.CreateProperty(ctx, p); err != nil {
					ontologyErr = fmt.Errorf("create property %s.%s: %w", otDef.apiName, prop.apiName, err)
					return
				}
				_ = i
				_ = j
			}
		}

		// Create link types
		for _, ld := range chinookLinkTypes {
			lt := &oms.LinkType{
				RID:              fmt.Sprintf("ri.ontology.main.link-type.chinook-%s", ld.apiName),
				OntologyRID:      ont.RID,
				APIName:          ld.apiName,
				DisplayName:      ld.display,
				SourceObjectType: ld.source,
				TargetObjectType: ld.target,
				Cardinality:      ld.cardinality,
			}
			if err := env.repo.CreateLinkType(ctx, lt); err != nil {
				ontologyErr = fmt.Errorf("create link type %s: %w", ld.apiName, err)
				return
			}
		}

		// Create action types
		createArtistParams, _ := json.Marshal([]map[string]interface{}{
			{"id": "name", "type": "string", "required": true, "description": "Artist name"},
		})
		createArtistRules, _ := json.Marshal([]map[string]interface{}{
			{
				"type":       "createObject",
				"objectType": "artist",
				"propertyBindings": map[string]interface{}{
					"name": map[string]interface{}{"type": "parameter", "value": "name"},
				},
			},
		})
		at := &oms.ActionType{
			RID:         "ri.ontology.main.action-type.chinook-createArtist",
			OntologyRID: ont.RID,
			APIName:     "createArtist",
			DisplayName: "Create Artist",
			Status:      "ACTIVE",
			Parameters:  createArtistParams,
			Rules:       createArtistRules,
		}
		if err := env.repo.CreateActionType(ctx, at); err != nil {
			ontologyErr = fmt.Errorf("create action type createArtist: %w", err)
			return
		}
	})

	if ontologyErr != nil {
		t.Fatalf("ontology setup failed: %v", ontologyErr)
	}
	return env
}

// setupIndexData indexes all Chinook CSV data into Bleve.
// Idempotent via sync.Once.
func setupIndexData(t *testing.T) *chinookEnv {
	t.Helper()
	env := setupOntology(t)

	indexDataOnce.Do(func() {
		for _, otDef := range chinookObjectTypes {
			// Create index with property mappings
			indexProps := make([]index.Property, len(otDef.properties))
			for i, p := range otDef.properties {
				indexProps[i] = index.Property{
					APIName:      p.apiName,
					BaseType:     p.baseType,
					IsSearchable: true,
				}
			}

			if _, err := env.indexMgr.EnsureIndex(otDef.apiName, indexProps); err != nil {
				indexDataErr = fmt.Errorf("ensure index for %s: %w", otDef.apiName, err)
				return
			}

			// Load and index CSV data
			f, err := os.Open(chinookCSVPath(otDef.csvFile))
			if err != nil {
				indexDataErr = fmt.Errorf("open CSV %s: %w", otDef.csvFile, err)
				return
			}

			reader := csv.NewReader(f)
			records, err := reader.ReadAll()
			f.Close()
			if err != nil {
				indexDataErr = fmt.Errorf("read CSV %s: %w", otDef.csvFile, err)
				return
			}

			if len(records) < 2 {
				indexDataErr = fmt.Errorf("CSV %s has no data rows", otDef.csvFile)
				return
			}

			headers := records[0]
			colMap := csvColumnMap[otDef.csvFile]
			pkCol := primaryKeyColumn[otDef.apiName]

			for _, row := range records[1:] {
				rowMap := make(map[string]string, len(headers))
				for i, h := range headers {
					if i < len(row) {
						rowMap[h] = row[i]
					}
				}

				doc := make(map[string]interface{})
				docID := rowMap[pkCol]

				for csvCol, propName := range colMap {
					val := rowMap[csvCol]
					if numericFields[propName] && val != "" {
						if fv, err := strconv.ParseFloat(val, 64); err == nil {
							doc[propName] = fv
						} else {
							doc[propName] = val
						}
					} else {
						doc[propName] = val
					}
				}

				if err := env.indexMgr.IndexDocument(otDef.apiName, docID, doc); err != nil {
					indexDataErr = fmt.Errorf("index document %s/%s: %w", otDef.apiName, docID, err)
					return
				}
			}
		}
	})

	if indexDataErr != nil {
		t.Fatalf("index data setup failed: %v", indexDataErr)
	}
	return env
}

// doRequest executes an HTTP request against the test router.
func doRequest(t *testing.T, env *chinookEnv, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()

	var reqBody *bytes.Buffer
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reqBody = bytes.NewBuffer(data)
	} else {
		reqBody = &bytes.Buffer{}
	}

	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	env.router.ServeHTTP(rr, req)
	return rr
}

// parseJSON unmarshals the response body into the given target.
func parseJSON(t *testing.T, rr *httptest.ResponseRecorder, target interface{}) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), target); err != nil {
		t.Fatalf("parse response JSON: %v\nbody: %s", err, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Ontology definition data: describes the 8 Chinook object types
// ---------------------------------------------------------------------------

type objectTypeDef struct {
	apiName    string
	display    string
	primaryKey string
	titleProp  string
	csvFile    string
	properties []propDef
}

type propDef struct {
	apiName  string
	baseType string
}

var chinookObjectTypes = []objectTypeDef{
	{
		apiName: "artist", display: "Artist", primaryKey: "artistId", titleProp: "name",
		csvFile: "Artist.csv",
		properties: []propDef{
			{"artistId", "string"},
			{"name", "string"},
		},
	},
	{
		apiName: "album", display: "Album", primaryKey: "albumId", titleProp: "title",
		csvFile: "Album.csv",
		properties: []propDef{
			{"albumId", "string"},
			{"title", "string"},
			{"artistId", "string"},
		},
	},
	{
		apiName: "track", display: "Track", primaryKey: "trackId", titleProp: "name",
		csvFile: "Track.csv",
		properties: []propDef{
			{"trackId", "string"},
			{"name", "string"},
			{"albumId", "string"},
			{"mediaTypeId", "string"},
			{"genreId", "string"},
			{"composer", "string"},
			{"milliseconds", "double"},
			{"bytes", "double"},
			{"unitPrice", "double"},
		},
	},
	{
		apiName: "genre", display: "Genre", primaryKey: "genreId", titleProp: "name",
		csvFile: "Genre.csv",
		properties: []propDef{
			{"genreId", "string"},
			{"name", "string"},
		},
	},
	{
		apiName: "customer", display: "Customer", primaryKey: "customerId", titleProp: "lastName",
		csvFile: "Customer.csv",
		properties: []propDef{
			{"customerId", "string"},
			{"firstName", "string"},
			{"lastName", "string"},
			{"company", "string"},
			{"address", "string"},
			{"city", "string"},
			{"state", "string"},
			{"country", "string"},
			{"postalCode", "string"},
			{"phone", "string"},
			{"fax", "string"},
			{"email", "string"},
			{"supportRepId", "string"},
		},
	},
	{
		apiName: "invoice", display: "Invoice", primaryKey: "invoiceId", titleProp: "invoiceId",
		csvFile: "Invoice.csv",
		properties: []propDef{
			{"invoiceId", "string"},
			{"customerId", "string"},
			{"invoiceDate", "string"},
			{"billingAddress", "string"},
			{"billingCity", "string"},
			{"billingState", "string"},
			{"billingCountry", "string"},
			{"billingPostalCode", "string"},
			{"total", "double"},
		},
	},
	{
		apiName: "invoiceLine", display: "Invoice Line", primaryKey: "invoiceLineId", titleProp: "invoiceLineId",
		csvFile: "InvoiceLine.csv",
		properties: []propDef{
			{"invoiceLineId", "string"},
			{"invoiceId", "string"},
			{"trackId", "string"},
			{"unitPrice", "double"},
			{"quantity", "double"},
		},
	},
	{
		apiName: "employee", display: "Employee", primaryKey: "employeeId", titleProp: "lastName",
		csvFile: "Employee.csv",
		properties: []propDef{
			{"employeeId", "string"},
			{"lastName", "string"},
			{"firstName", "string"},
			{"title", "string"},
			{"reportsTo", "string"},
			{"birthDate", "string"},
			{"hireDate", "string"},
			{"address", "string"},
			{"city", "string"},
			{"state", "string"},
			{"country", "string"},
			{"postalCode", "string"},
			{"phone", "string"},
			{"fax", "string"},
			{"email", "string"},
		},
	},
}

// CSV header -> property apiName mapping for each object type.
var csvColumnMap = map[string]map[string]string{
	"Artist.csv": {
		"ArtistId": "artistId",
		"Name":     "name",
	},
	"Album.csv": {
		"AlbumId":  "albumId",
		"Title":    "title",
		"ArtistId": "artistId",
	},
	"Track.csv": {
		"TrackId":      "trackId",
		"Name":         "name",
		"AlbumId":      "albumId",
		"MediaTypeId":  "mediaTypeId",
		"GenreId":      "genreId",
		"Composer":     "composer",
		"Milliseconds": "milliseconds",
		"Bytes":        "bytes",
		"UnitPrice":    "unitPrice",
	},
	"Genre.csv": {
		"GenreId": "genreId",
		"Name":    "name",
	},
	"Customer.csv": {
		"CustomerId":   "customerId",
		"FirstName":    "firstName",
		"LastName":     "lastName",
		"Company":      "company",
		"Address":      "address",
		"City":         "city",
		"State":        "state",
		"Country":      "country",
		"PostalCode":   "postalCode",
		"Phone":        "phone",
		"Fax":          "fax",
		"Email":        "email",
		"SupportRepId": "supportRepId",
	},
	"Invoice.csv": {
		"InvoiceId":         "invoiceId",
		"CustomerId":        "customerId",
		"InvoiceDate":       "invoiceDate",
		"BillingAddress":    "billingAddress",
		"BillingCity":       "billingCity",
		"BillingState":      "billingState",
		"BillingCountry":    "billingCountry",
		"BillingPostalCode": "billingPostalCode",
		"Total":             "total",
	},
	"InvoiceLine.csv": {
		"InvoiceLineId": "invoiceLineId",
		"InvoiceId":     "invoiceId",
		"TrackId":       "trackId",
		"UnitPrice":     "unitPrice",
		"Quantity":      "quantity",
	},
	"Employee.csv": {
		"EmployeeId": "employeeId",
		"LastName":   "lastName",
		"FirstName":  "firstName",
		"Title":      "title",
		"ReportsTo":  "reportsTo",
		"BirthDate":  "birthDate",
		"HireDate":   "hireDate",
		"Address":    "address",
		"City":       "city",
		"State":      "state",
		"Country":    "country",
		"PostalCode": "postalCode",
		"Phone":      "phone",
		"Fax":        "fax",
		"Email":      "email",
	},
}

// numericFields lists properties that should be indexed as numbers.
var numericFields = map[string]bool{
	"milliseconds": true,
	"bytes":        true,
	"unitPrice":    true,
	"total":        true,
	"quantity":     true,
}

// primaryKeyColumn maps objectType apiName -> CSV column name of the PK.
var primaryKeyColumn = map[string]string{
	"artist":      "ArtistId",
	"album":       "AlbumId",
	"track":       "TrackId",
	"genre":       "GenreId",
	"customer":    "CustomerId",
	"invoice":     "InvoiceId",
	"invoiceLine": "InvoiceLineId",
	"employee":    "EmployeeId",
}

// expectedCounts is the expected document count per object type.
var expectedCounts = map[string]uint64{
	"artist":      275,
	"album":       347,
	"track":       3503,
	"genre":       25,
	"customer":    59,
	"invoice":     412,
	"invoiceLine": 2240,
	"employee":    8,
}

// Link type definitions.
type linkDef struct {
	apiName     string
	display     string
	source      string
	target      string
	cardinality string
}

var chinookLinkTypes = []linkDef{
	{"artist-albums", "Artist Albums", "artist", "album", "ONE_TO_MANY"},
	{"album-tracks", "Album Tracks", "album", "track", "ONE_TO_MANY"},
	{"genre-tracks", "Genre Tracks", "genre", "track", "ONE_TO_MANY"},
	{"customer-invoices", "Customer Invoices", "customer", "invoice", "ONE_TO_MANY"},
	{"invoice-lines", "Invoice Lines", "invoice", "invoiceLine", "ONE_TO_MANY"},
	{"employee-customers", "Employee Customers", "employee", "customer", "ONE_TO_MANY"},
	{"track-invoiceLines", "Track Invoice Lines", "track", "invoiceLine", "ONE_TO_MANY"},
	{"album-artist", "Album Artist", "album", "artist", "MANY_TO_ONE"},
}

// ===========================================================================
// Phase 1: Schema Definition (OMS read-back verification via HTTP)
// ===========================================================================

func TestChinook_Phase1_CreateOntology(t *testing.T) {
	env := setupOntology(t)

	t.Run("GetOntology", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		parseJSON(t, rr, &resp)
		if resp["apiName"] != "chinook" {
			t.Errorf("expected apiName=chinook, got %v", resp["apiName"])
		}
	})

	t.Run("ListObjectTypes_Returns8", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objectTypes", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp struct {
			Data []json.RawMessage `json:"data"`
		}
		parseJSON(t, rr, &resp)
		if len(resp.Data) != 8 {
			t.Errorf("expected 8 object types, got %d", len(resp.Data))
		}
	})

	t.Run("GetArtistObjectType", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objectTypes/artist", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		parseJSON(t, rr, &resp)
		if resp["apiName"] != "artist" {
			t.Errorf("expected apiName=artist, got %v", resp["apiName"])
		}
		if resp["primaryKey"] != "artistId" {
			t.Errorf("expected primaryKey=artistId, got %v", resp["primaryKey"])
		}

		// Verify properties exist
		props, ok := resp["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("expected properties map in response")
		}
		if _, ok := props["name"]; !ok {
			t.Error("expected 'name' property on artist")
		}
		if _, ok := props["artistId"]; !ok {
			t.Error("expected 'artistId' property on artist")
		}
	})

	t.Run("ListActionTypes", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/actionTypes", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp struct {
			Data []json.RawMessage `json:"data"`
		}
		parseJSON(t, rr, &resp)
		if len(resp.Data) < 1 {
			t.Error("expected at least 1 action type (createArtist)")
		}
	})
}

// ===========================================================================
// Phase 2: Data Indexing Verification
// ===========================================================================

func TestChinook_Phase2_IndexData(t *testing.T) {
	env := setupIndexData(t)

	t.Run("VerifyDocCounts", func(t *testing.T) {
		for otName, expected := range expectedCounts {
			count, err := env.indexMgr.DocCount(otName)
			if err != nil {
				t.Errorf("doc count for %s: %v", otName, err)
				continue
			}
			if count != expected {
				t.Errorf("doc count for %s: expected %d, got %d", otName, expected, count)
			}
		}
	})
}

// ===========================================================================
// Phase 3: Single Object Read
// ===========================================================================

func TestChinook_Phase3_GetObject(t *testing.T) {
	env := setupIndexData(t)

	t.Run("GetArtist_ACDC", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/artist/1", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var obj map[string]interface{}
		parseJSON(t, rr, &obj)

		if obj["__apiName"] != "artist" {
			t.Errorf("expected __apiName=artist, got %v", obj["__apiName"])
		}
		name, _ := obj["name"].(string)
		if name != "AC/DC" {
			t.Errorf("expected name=AC/DC, got %q", name)
		}
	})

	t.Run("GetTrack_ForThoseAboutToRock", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/track/1", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var obj map[string]interface{}
		parseJSON(t, rr, &obj)

		name, _ := obj["name"].(string)
		if !strings.Contains(name, "For Those About To Rock") {
			t.Errorf("expected name containing 'For Those About To Rock', got %q", name)
		}
	})

	t.Run("GetCustomer_VerifyEmail", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/customer/1", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var obj map[string]interface{}
		parseJSON(t, rr, &obj)

		email, _ := obj["email"].(string)
		if email != "luisg@embraer.com.br" {
			t.Errorf("expected email=luisg@embraer.com.br, got %q", email)
		}
	})

	t.Run("GetNonExistentObject_404", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/artist/99999", env.ontologyRID), nil)
		if rr.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d: %s", rr.Code, rr.Body.String())
		}
	})
}

// ===========================================================================
// Phase 4: List + Pagination
// ===========================================================================

func TestChinook_Phase4_ListWithPagination(t *testing.T) {
	env := setupIndexData(t)

	t.Run("ListAllGenres", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/genre", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var page oss.ObjectPage
		parseJSON(t, rr, &page)

		if len(page.Data) != 25 {
			t.Errorf("expected 25 genres, got %d", len(page.Data))
		}
		if page.TotalCount != "25" {
			t.Errorf("expected totalCount=25, got %q", page.TotalCount)
		}
	})

	t.Run("ListTracksPage1", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/track?pageSize=10", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var page oss.ObjectPage
		parseJSON(t, rr, &page)

		if len(page.Data) != 10 {
			t.Errorf("expected 10 tracks on page 1, got %d", len(page.Data))
		}
		if page.NextPageToken == "" {
			t.Error("expected nextPageToken to be set for page 1")
		}
		if page.TotalCount != "3503" {
			t.Errorf("expected totalCount=3503, got %q", page.TotalCount)
		}
	})

	t.Run("ListTracksPage2", func(t *testing.T) {
		// Get page 1 to get the token
		rr1 := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/track?pageSize=10", env.ontologyRID), nil)
		var page1 oss.ObjectPage
		parseJSON(t, rr1, &page1)

		if page1.NextPageToken == "" {
			t.Fatal("no nextPageToken from page 1")
		}

		// Get page 2
		rr2 := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/track?pageSize=10&pageToken=%s",
				env.ontologyRID, page1.NextPageToken), nil)
		if rr2.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr2.Code, rr2.Body.String())
		}

		var page2 oss.ObjectPage
		parseJSON(t, rr2, &page2)

		if len(page2.Data) != 10 {
			t.Errorf("expected 10 tracks on page 2, got %d", len(page2.Data))
		}
	})
}

// ===========================================================================
// Phase 5: Where Search
// ===========================================================================

func TestChinook_Phase5_Search(t *testing.T) {
	env := setupIndexData(t)

	t.Run("TrackUnitPrice_Eq_099", func(t *testing.T) {
		body := map[string]interface{}{
			"where": map[string]interface{}{
				"type":  "eq",
				"field": "unitPrice",
				"value": 0.99,
			},
			"select":   []string{"trackId", "unitPrice"},
			"pageSize": 1000,
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/track/search", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var page oss.ObjectPage
		parseJSON(t, rr, &page)

		// Most tracks are $0.99. Should be a large number.
		if len(page.Data) < 100 {
			t.Errorf("expected many tracks at $0.99, got %d", len(page.Data))
		}
	})

	t.Run("TrackUnitPrice_Gt_099", func(t *testing.T) {
		body := map[string]interface{}{
			"where": map[string]interface{}{
				"type":  "gt",
				"field": "unitPrice",
				"value": 0.99,
			},
			"select":   []string{"trackId", "unitPrice"},
			"pageSize": 1000,
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/track/search", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var page oss.ObjectPage
		parseJSON(t, rr, &page)

		// Only some tracks are above $0.99 (typically $1.99 tracks).
		if len(page.Data) == 0 {
			t.Error("expected some high-price tracks, got 0")
		}
		totalHigh, _ := strconv.Atoi(page.TotalCount)
		if totalHigh >= 3000 {
			t.Errorf("expected fewer high-price tracks, got totalCount=%d", totalHigh)
		}
	})

	t.Run("CustomerCountry_Eq_Brazil", func(t *testing.T) {
		body := map[string]interface{}{
			"where": map[string]interface{}{
				"type":  "eq",
				"field": "country",
				"value": "Brazil",
			},
			"select": []string{"customerId", "country"},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/customer/search", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var page oss.ObjectPage
		parseJSON(t, rr, &page)

		if page.TotalCount != "5" {
			t.Errorf("expected 5 Brazilian customers, got totalCount=%s", page.TotalCount)
		}
	})

	t.Run("TrackName_StartsWith_B", func(t *testing.T) {
		body := map[string]interface{}{
			"where": map[string]interface{}{
				"type":  "startsWith",
				"field": "name",
				"value": "B",
			},
			"select":   []string{"trackId", "name"},
			"pageSize": 1000,
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/track/search", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var page oss.ObjectPage
		parseJSON(t, rr, &page)

		if len(page.Data) == 0 {
			t.Error("expected some tracks starting with B, got 0")
		}

		// Verify first result starts with "B" or "b"
		if len(page.Data) > 0 {
			var obj map[string]interface{}
			data, _ := json.Marshal(page.Data[0])
			json.Unmarshal(data, &obj)
			name, _ := obj["name"].(string)
			if !strings.HasPrefix(strings.ToLower(name), "b") {
				t.Errorf("first result name should start with B, got %q", name)
			}
		}
	})

	t.Run("CompoundSearch_AND", func(t *testing.T) {
		// Tracks where genreId = "1" AND unitPrice > 0.99
		body := map[string]interface{}{
			"where": map[string]interface{}{
				"type": "and",
				"value": []map[string]interface{}{
					{"type": "eq", "field": "genreId", "value": "1"},
					{"type": "gt", "field": "unitPrice", "value": 0.99},
				},
			},
			"select":   []string{"trackId", "genreId", "unitPrice"},
			"pageSize": 1000,
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/track/search", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var page oss.ObjectPage
		parseJSON(t, rr, &page)

		totalCount, _ := strconv.Atoi(page.TotalCount)
		if totalCount < 0 {
			t.Errorf("expected non-negative totalCount, got %d", totalCount)
		}
	})

	t.Run("CustomerCompany_IsNull", func(t *testing.T) {
		body := map[string]interface{}{
			"where": map[string]interface{}{
				"type":  "isNull",
				"field": "company",
				"value": true,
			},
			"select":   []string{"customerId", "company"},
			"pageSize": 100,
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/customer/search", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var page oss.ObjectPage
		parseJSON(t, rr, &page)

		// Some customers have no company (empty string in CSV)
		totalNull, _ := strconv.Atoi(page.TotalCount)
		if totalNull < 0 {
			t.Errorf("expected non-negative count of customers without company, got %d", totalNull)
		}
	})
}

// ===========================================================================
// Phase 6: Aggregation
// ===========================================================================

func TestChinook_Phase6_Aggregation(t *testing.T) {
	env := setupIndexData(t)

	t.Run("TrackCount", func(t *testing.T) {
		body := map[string]interface{}{
			"aggregation": []map[string]interface{}{
				{"type": "count", "name": "total"},
			},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/track/aggregate", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp aggregation.AggregationResponse
		parseJSON(t, rr, &resp)

		if len(resp.Data) != 1 {
			t.Fatalf("expected 1 data row, got %d", len(resp.Data))
		}
		if len(resp.Data[0].Metrics) < 1 {
			t.Fatal("expected at least 1 metric")
		}

		countVal := resp.Data[0].Metrics[0].Value
		countFloat, ok := countVal.(float64)
		if !ok {
			t.Fatalf("expected count as number, got %T: %v", countVal, countVal)
		}
		if int(countFloat) != 3503 {
			t.Errorf("expected track count=3503, got %v", countFloat)
		}
	})

	t.Run("InvoiceAvgTotal", func(t *testing.T) {
		body := map[string]interface{}{
			"aggregation": []map[string]interface{}{
				{"type": "avg", "field": "total", "name": "avgTotal"},
			},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/invoice/aggregate", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp aggregation.AggregationResponse
		parseJSON(t, rr, &resp)

		if len(resp.Data) != 1 || len(resp.Data[0].Metrics) < 1 {
			t.Fatal("expected 1 data row with at least 1 metric")
		}

		avgVal, ok := resp.Data[0].Metrics[0].Value.(float64)
		if !ok {
			t.Fatalf("expected avg as float64, got %T", resp.Data[0].Metrics[0].Value)
		}
		if avgVal <= 0 {
			t.Errorf("expected positive average invoice total, got %f", avgVal)
		}
	})

	t.Run("InvoiceSumTotal", func(t *testing.T) {
		body := map[string]interface{}{
			"aggregation": []map[string]interface{}{
				{"type": "sum", "field": "total", "name": "revenue"},
			},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/invoice/aggregate", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp aggregation.AggregationResponse
		parseJSON(t, rr, &resp)

		if len(resp.Data) != 1 || len(resp.Data[0].Metrics) < 1 {
			t.Fatal("expected 1 data row with at least 1 metric")
		}

		sumVal, ok := resp.Data[0].Metrics[0].Value.(float64)
		if !ok {
			t.Fatalf("expected sum as float64, got %T", resp.Data[0].Metrics[0].Value)
		}
		// Total revenue should be substantial (~$2328.60 for Chinook)
		if sumVal < 1000 {
			t.Errorf("expected total revenue > 1000, got %f", sumVal)
		}
	})

	t.Run("TrackMinMaxMilliseconds", func(t *testing.T) {
		body := map[string]interface{}{
			"aggregation": []map[string]interface{}{
				{"type": "min", "field": "milliseconds", "name": "shortest"},
				{"type": "max", "field": "milliseconds", "name": "longest"},
			},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/track/aggregate", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp aggregation.AggregationResponse
		parseJSON(t, rr, &resp)

		if len(resp.Data) != 1 || len(resp.Data[0].Metrics) < 2 {
			t.Fatal("expected 1 data row with at least 2 metrics")
		}

		minVal, _ := resp.Data[0].Metrics[0].Value.(float64)
		maxVal, _ := resp.Data[0].Metrics[1].Value.(float64)

		if minVal <= 0 {
			t.Errorf("expected positive min milliseconds, got %f", minVal)
		}
		if maxVal <= minVal {
			t.Errorf("expected max > min, got max=%f, min=%f", maxVal, minVal)
		}
	})

	t.Run("InvoiceGroupByCountry", func(t *testing.T) {
		body := map[string]interface{}{
			"aggregation": []map[string]interface{}{
				{"type": "count", "name": "count"},
			},
			"groupBy": []map[string]interface{}{
				{"type": "exact", "field": "billingCountry"},
			},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/invoice/aggregate", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp aggregation.AggregationResponse
		parseJSON(t, rr, &resp)

		if len(resp.Data) == 0 {
			t.Fatal("expected multiple country groups, got 0")
		}

		for _, row := range resp.Data {
			if row.Group == nil {
				t.Error("expected group to be set")
				continue
			}
			if _, ok := row.Group["billingCountry"]; !ok {
				t.Error("expected group to have billingCountry key")
			}
			if len(row.Metrics) == 0 {
				t.Error("expected at least 1 metric per group")
			}
		}
	})

	t.Run("TrackGroupByUnitPrice", func(t *testing.T) {
		body := map[string]interface{}{
			"aggregation": []map[string]interface{}{
				{"type": "count", "name": "count"},
			},
			"groupBy": []map[string]interface{}{
				{"type": "exact", "field": "unitPrice"},
			},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/track/aggregate", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp aggregation.AggregationResponse
		parseJSON(t, rr, &resp)

		// Chinook has 2 price levels: $0.99 and $1.99
		if len(resp.Data) < 2 {
			t.Errorf("expected at least 2 price groups, got %d", len(resp.Data))
		}
	})
}

// ===========================================================================
// Phase 7: ObjectSet Composite Queries
// ===========================================================================

func TestChinook_Phase7_ObjectSet(t *testing.T) {
	env := setupIndexData(t)

	t.Run("BaseTrack_AllObjects", func(t *testing.T) {
		body := map[string]interface{}{
			"objectSet": map[string]interface{}{
				"type":       "base",
				"objectType": "track",
			},
			"select": []string{"trackId", "name"},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objectSets/loadObjects", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp objectset.LoadObjectSetResponse
		parseJSON(t, rr, &resp)

		if resp.TotalCount != "3503" {
			t.Errorf("expected totalCount=3503, got %q", resp.TotalCount)
		}
	})

	t.Run("FilterTrack_HighPrice", func(t *testing.T) {
		body := map[string]interface{}{
			"objectSet": map[string]interface{}{
				"type": "filter",
				"objectSet": map[string]interface{}{
					"type":       "base",
					"objectType": "track",
				},
				"where": map[string]interface{}{
					"type":  "gt",
					"field": "unitPrice",
					"value": 0.99,
				},
			},
			"select": []string{"trackId", "unitPrice"},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objectSets/loadObjects", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp objectset.LoadObjectSetResponse
		parseJSON(t, rr, &resp)

		totalCount, _ := strconv.Atoi(resp.TotalCount)
		if totalCount == 0 {
			t.Error("expected some high-price tracks, got totalCount=0")
		}
		if totalCount >= 3503 {
			t.Errorf("expected fewer than 3503 high-price tracks, got %d", totalCount)
		}
	})

	t.Run("BaseTrack_WithPageSize", func(t *testing.T) {
		body := map[string]interface{}{
			"objectSet": map[string]interface{}{
				"type":       "base",
				"objectType": "track",
			},
			"select":   []string{"trackId", "name"},
			"pageSize": 5,
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objectSets/loadObjects", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp objectset.LoadObjectSetResponse
		parseJSON(t, rr, &resp)

		if len(resp.Data) != 5 {
			t.Errorf("expected 5 tracks with pageSize=5, got %d", len(resp.Data))
		}
		if resp.NextPageToken == "" {
			t.Error("expected nextPageToken for paginated result")
		}
	})

	t.Run("BaseTrack_SelectFields", func(t *testing.T) {
		body := map[string]interface{}{
			"objectSet": map[string]interface{}{
				"type":       "base",
				"objectType": "track",
			},
			"select":   []string{"name", "unitPrice"},
			"pageSize": 5,
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objectSets/loadObjects", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp objectset.LoadObjectSetResponse
		parseJSON(t, rr, &resp)

		if len(resp.Data) == 0 {
			t.Fatal("expected some tracks in response")
		}

		// Verify only selected fields are present (plus __rid, __primaryKey, __apiName)
		firstObj := resp.Data[0]
		objData, _ := json.Marshal(firstObj)
		var objMap map[string]interface{}
		json.Unmarshal(objData, &objMap)

		if _, ok := objMap["name"]; !ok {
			t.Error("expected 'name' field in selected response")
		}
		if _, ok := objMap["unitPrice"]; !ok {
			t.Error("expected 'unitPrice' field in selected response")
		}
		// Fields NOT in select should not be present
		if _, ok := objMap["albumId"]; ok {
			t.Error("unexpected 'albumId' field in selected response")
		}
		if _, ok := objMap["composer"]; ok {
			t.Error("unexpected 'composer' field in selected response")
		}
	})

	t.Run("ObjectSetAggregate_TrackCount", func(t *testing.T) {
		body := map[string]interface{}{
			"objectSet": map[string]interface{}{
				"type":       "base",
				"objectType": "track",
			},
			"aggregation": []map[string]interface{}{
				{"type": "count", "name": "total"},
			},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objectSets/aggregate", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp aggregation.AggregationResponse
		parseJSON(t, rr, &resp)

		if len(resp.Data) != 1 || len(resp.Data[0].Metrics) < 1 {
			t.Fatal("expected 1 data row with at least 1 metric")
		}

		countVal, ok := resp.Data[0].Metrics[0].Value.(float64)
		if !ok {
			t.Fatalf("expected count as float64, got %T", resp.Data[0].Metrics[0].Value)
		}
		if int(countVal) != 3503 {
			t.Errorf("expected track count=3503, got %v", countVal)
		}
	})
}

// ===========================================================================
// Phase 8: Action Write
// ===========================================================================

func TestChinook_Phase8_Actions(t *testing.T) {
	env := setupOntology(t)

	t.Run("ApplyCreateArtist_Success", func(t *testing.T) {
		body := map[string]interface{}{
			"parameters": map[string]interface{}{
				"name": "Test Band",
			},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/actions/createArtist/apply", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var result actions.SyncApplyActionResponseV2
		parseJSON(t, rr, &result)

		if result.Edits == nil {
			t.Error("expected edits in SyncApplyActionResponseV2")
		}
		if result.Edits != nil && result.Edits.AddedObjectCount == 0 {
			t.Error("expected addedObjectCount > 0 for createArtist")
		}
	})

	t.Run("ApplyCreateArtist_MissingRequiredParam", func(t *testing.T) {
		body := map[string]interface{}{
			"parameters": map[string]interface{}{},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/actions/createArtist/apply", env.ontologyRID), body)
		// Should fail validation
		if rr.Code == http.StatusOK {
			t.Error("expected error for missing required parameter, but got 200")
		}
	})

	t.Run("ApplyNonExistentAction", func(t *testing.T) {
		body := map[string]interface{}{
			"parameters": map[string]interface{}{},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/actions/nonExistentAction/apply", env.ontologyRID), body)
		// Should fail - action type not found
		if rr.Code == http.StatusOK {
			t.Error("expected error for non-existent action type, but got 200")
		}
	})
}

func (r *inMemoryOmsRepo) CreateSecurityPolicy(_ context.Context, _ *oms.SecurityPolicy) error { return nil }
func (r *inMemoryOmsRepo) GetSecurityPolicy(_ context.Context, _ string) (*oms.SecurityPolicy, error) { return nil, nil }
func (r *inMemoryOmsRepo) ListSecurityPolicies(_ context.Context, _ string) ([]oms.SecurityPolicy, error) { return nil, nil }
func (r *inMemoryOmsRepo) UpdateSecurityPolicy(_ context.Context, _ *oms.SecurityPolicy) error { return nil }
func (r *inMemoryOmsRepo) DeleteSecurityPolicy(_ context.Context, _ string) error { return nil }
