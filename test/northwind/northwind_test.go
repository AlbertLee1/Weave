package northwind_test

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
// In-memory OMS Repository (shared test infrastructure, same as e2e/chinook)
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
// Test environment
// ---------------------------------------------------------------------------

type northwindEnv struct {
	server      *httptest.Server
	router      http.Handler
	repo        *inMemoryOmsRepo
	indexMgr    *index.Manager
	ontologyRID string
}

var (
	sharedEnv     *northwindEnv
	envOnce       sync.Once
	ontologyOnce  sync.Once
	indexDataOnce sync.Once
	envErr        error
	ontologyErr   error
	indexDataErr  error
)

func northwindCSVPath(filename string) string {
	return filepath.Join("..", "..", "testdata", "northwind", filename)
}

func setupEnv(t *testing.T) *northwindEnv {
	t.Helper()

	envOnce.Do(func() {
		tmpDir, err := os.MkdirTemp("", "weave-northwind-*")
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

		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		})

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

		ossHandler := oss.NewHandler(ossSvc)
		ossHandler.RegisterRoutes(r)

		actionHandler := actions.NewHandler(actionExecutor)
		r.Post("/api/v2/ontologies/{ontologyApiName}/actions/{action}/apply", actionHandler.Apply)
		r.Post("/api/v2/ontologies/{ontologyApiName}/actions/{action}/applyBatch", actionHandler.ApplyBatch)

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

		objSetHandler := objectset.NewHandler(objSetExecutor, indexMgr, objSetStore)
		r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", objSetHandler.LoadObjects)
		r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/aggregate", objSetHandler.Aggregate)
		r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/createTemporary", objSetHandler.CreateTemporary)

		srv := httptest.NewServer(r)

		sharedEnv = &northwindEnv{
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

// ---------------------------------------------------------------------------
// Northwind ontology definitions: 10 object types with diverse base types
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

var northwindObjectTypes = []objectTypeDef{
	{
		apiName: "category", display: "Category", primaryKey: "categoryId", titleProp: "categoryName",
		csvFile: "categories.csv",
		properties: []propDef{
			{"categoryId", "string"},
			{"categoryName", "string"},
			{"description", "string"},
		},
	},
	{
		apiName: "customer", display: "Customer", primaryKey: "customerId", titleProp: "companyName",
		csvFile: "customers.csv",
		properties: []propDef{
			{"customerId", "string"},
			{"companyName", "string"},
			{"contactName", "string"},
			{"contactTitle", "string"},
			{"address", "string"},
			{"city", "string"},
			{"region", "string"},
			{"postalCode", "string"},
			{"country", "string"},
			{"phone", "string"},
			{"fax", "string"},
		},
	},
	{
		apiName: "employee", display: "Employee", primaryKey: "employeeId", titleProp: "lastName",
		csvFile: "employees.csv",
		properties: []propDef{
			{"employeeId", "string"},
			{"lastName", "string"},
			{"firstName", "string"},
			{"title", "string"},
			{"titleOfCourtesy", "string"},
			{"birthDate", "timestamp"},
			{"hireDate", "timestamp"},
			{"address", "string"},
			{"city", "string"},
			{"region", "string"},
			{"postalCode", "string"},
			{"country", "string"},
			{"homePhone", "string"},
			{"extension", "string"},
			{"notes", "string"},
			{"reportsTo", "integer"},
		},
	},
	{
		apiName: "order", display: "Order", primaryKey: "orderId", titleProp: "orderId",
		csvFile: "orders.csv",
		properties: []propDef{
			{"orderId", "string"},
			{"customerId", "string"},
			{"employeeId", "integer"},
			{"orderDate", "timestamp"},
			{"requiredDate", "timestamp"},
			{"shippedDate", "timestamp"},
			{"shipVia", "integer"},
			{"freight", "double"},
			{"shipName", "string"},
			{"shipAddress", "string"},
			{"shipCity", "string"},
			{"shipRegion", "string"},
			{"shipPostalCode", "string"},
			{"shipCountry", "string"},
		},
	},
	{
		apiName: "orderDetail", display: "Order Detail", primaryKey: "orderDetailId", titleProp: "orderDetailId",
		csvFile: "order_details.csv",
		properties: []propDef{
			{"orderDetailId", "string"},
			{"orderId", "string"},
			{"productId", "string"},
			{"unitPrice", "double"},
			{"quantity", "integer"},
			{"discount", "double"},
		},
	},
	{
		apiName: "product", display: "Product", primaryKey: "productId", titleProp: "productName",
		csvFile: "products.csv",
		properties: []propDef{
			{"productId", "string"},
			{"productName", "string"},
			{"supplierId", "integer"},
			{"categoryId", "integer"},
			{"quantityPerUnit", "string"},
			{"unitPrice", "double"},
			{"unitsInStock", "integer"},
			{"unitsOnOrder", "integer"},
			{"reorderLevel", "integer"},
			{"discontinued", "boolean"},
		},
	},
	{
		apiName: "region", display: "Region", primaryKey: "regionId", titleProp: "regionDescription",
		csvFile: "regions.csv",
		properties: []propDef{
			{"regionId", "string"},
			{"regionDescription", "string"},
		},
	},
	{
		apiName: "shipper", display: "Shipper", primaryKey: "shipperId", titleProp: "companyName",
		csvFile: "shippers.csv",
		properties: []propDef{
			{"shipperId", "string"},
			{"companyName", "string"},
			{"phone", "string"},
		},
	},
	{
		apiName: "supplier", display: "Supplier", primaryKey: "supplierId", titleProp: "companyName",
		csvFile: "suppliers.csv",
		properties: []propDef{
			{"supplierId", "string"},
			{"companyName", "string"},
			{"contactName", "string"},
			{"contactTitle", "string"},
			{"address", "string"},
			{"city", "string"},
			{"region", "string"},
			{"postalCode", "string"},
			{"country", "string"},
			{"phone", "string"},
			{"fax", "string"},
			{"homePage", "string"},
		},
	},
	{
		apiName: "territory", display: "Territory", primaryKey: "territoryId", titleProp: "territoryDescription",
		csvFile: "territories.csv",
		properties: []propDef{
			{"territoryId", "string"},
			{"territoryDescription", "string"},
			{"regionId", "integer"},
		},
	},
}

// CSV header -> property apiName mapping for each CSV file.
var csvColumnMap = map[string]map[string]string{
	"categories.csv": {
		"categoryID":   "categoryId",
		"categoryName": "categoryName",
		"description":  "description",
	},
	"customers.csv": {
		"customerID":   "customerId",
		"companyName":  "companyName",
		"contactName":  "contactName",
		"contactTitle": "contactTitle",
		"address":      "address",
		"city":         "city",
		"region":       "region",
		"postalCode":   "postalCode",
		"country":      "country",
		"phone":        "phone",
		"fax":          "fax",
	},
	"employees.csv": {
		"employeeID":      "employeeId",
		"lastName":        "lastName",
		"firstName":       "firstName",
		"title":           "title",
		"titleOfCourtesy": "titleOfCourtesy",
		"birthDate":       "birthDate",
		"hireDate":        "hireDate",
		"address":         "address",
		"city":            "city",
		"region":          "region",
		"postalCode":      "postalCode",
		"country":         "country",
		"homePhone":       "homePhone",
		"extension":       "extension",
		"notes":           "notes",
		"reportsTo":       "reportsTo",
	},
	"orders.csv": {
		"orderID":        "orderId",
		"customerID":     "customerId",
		"employeeID":     "employeeId",
		"orderDate":      "orderDate",
		"requiredDate":   "requiredDate",
		"shippedDate":    "shippedDate",
		"shipVia":        "shipVia",
		"freight":        "freight",
		"shipName":       "shipName",
		"shipAddress":    "shipAddress",
		"shipCity":       "shipCity",
		"shipRegion":     "shipRegion",
		"shipPostalCode": "shipPostalCode",
		"shipCountry":    "shipCountry",
	},
	"order_details.csv": {
		"orderID":   "orderId",
		"productID": "productId",
		"unitPrice": "unitPrice",
		"quantity":  "quantity",
		"discount":  "discount",
	},
	"products.csv": {
		"productID":       "productId",
		"productName":     "productName",
		"supplierID":      "supplierId",
		"categoryID":      "categoryId",
		"quantityPerUnit": "quantityPerUnit",
		"unitPrice":       "unitPrice",
		"unitsInStock":    "unitsInStock",
		"unitsOnOrder":    "unitsOnOrder",
		"reorderLevel":    "reorderLevel",
		"discontinued":    "discontinued",
	},
	"regions.csv": {
		"regionID":          "regionId",
		"regionDescription": "regionDescription",
	},
	"shippers.csv": {
		"shipperID":   "shipperId",
		"companyName": "companyName",
		"phone":       "phone",
	},
	"suppliers.csv": {
		"supplierID":   "supplierId",
		"companyName":  "companyName",
		"contactName":  "contactName",
		"contactTitle": "contactTitle",
		"address":      "address",
		"city":         "city",
		"region":       "region",
		"postalCode":   "postalCode",
		"country":      "country",
		"phone":        "phone",
		"fax":          "fax",
		"homePage":     "homePage",
	},
	"territories.csv": {
		"territoryID":          "territoryId",
		"territoryDescription": "territoryDescription",
		"regionID":             "regionId",
	},
}

// primaryKeyColumn maps objectType apiName -> CSV column name of the PK.
var primaryKeyColumn = map[string]string{
	"category":    "categoryID",
	"customer":    "customerID",
	"employee":    "employeeID",
	"order":       "orderID",
	"orderDetail": "", // composite: handled specially
	"product":     "productID",
	"region":      "regionID",
	"shipper":     "shipperID",
	"supplier":    "supplierID",
	"territory":   "territoryID",
}

// expectedCounts is the expected document count per object type.
var expectedCounts = map[string]uint64{
	"category":    8,
	"customer":    91,
	"employee":    9,
	"order":       830,
	"orderDetail": 2155,
	"product":     77,
	"region":      4,
	"shipper":     3,
	"supplier":    29,
	"territory":   53,
}

// propertyBaseTypes maps property apiName -> baseType for type-aware coercion.
var propertyBaseTypes map[string]string

func init() {
	propertyBaseTypes = make(map[string]string)
	for _, ot := range northwindObjectTypes {
		for _, p := range ot.properties {
			propertyBaseTypes[p.apiName] = p.baseType
		}
	}
}

// Link type definitions.
type linkDef struct {
	apiName     string
	display     string
	source      string // objectType apiName
	target      string // objectType apiName
	cardinality string
}

var northwindLinkTypes = []linkDef{
	// ONE_TO_MANY
	{"category-products", "Category Products", "category", "product", "ONE_TO_MANY"},
	{"supplier-products", "Supplier Products", "supplier", "product", "ONE_TO_MANY"},
	{"customer-orders", "Customer Orders", "customer", "order", "ONE_TO_MANY"},
	{"employee-orders", "Employee Orders", "employee", "order", "ONE_TO_MANY"},
	{"shipper-orders", "Shipper Orders", "shipper", "order", "ONE_TO_MANY"},
	{"order-orderDetails", "Order Details", "order", "orderDetail", "ONE_TO_MANY"},
	{"region-territories", "Region Territories", "region", "territory", "ONE_TO_MANY"},
	// Self-referencing
	{"employee-reportsTo", "Reports To", "employee", "employee", "ONE_TO_MANY"},
	// MANY_TO_MANY
	{"employee-territories", "Employee Territories", "employee", "territory", "MANY_TO_MANY"},
	{"order-products", "Order Products", "order", "product", "MANY_TO_MANY"},
}

// ---------------------------------------------------------------------------
// Setup functions
// ---------------------------------------------------------------------------

func objectTypeRID(apiName string) string {
	return fmt.Sprintf("ri.ontology.main.object-type.northwind-%s", apiName)
}

func setupOntology(t *testing.T) *northwindEnv {
	t.Helper()
	env := setupEnv(t)

	ontologyOnce.Do(func() {
		ctx := context.Background()

		ont := &oms.Ontology{
			RID:         "ri.ontology.main.ontology.northwind",
			APIName:     "northwind",
			DisplayName: "Northwind Traders",
		}
		if err := env.repo.CreateOntology(ctx, ont); err != nil {
			ontologyErr = fmt.Errorf("create ontology: %w", err)
			return
		}
		env.ontologyRID = ont.RID

		for _, otDef := range northwindObjectTypes {
			otRID := objectTypeRID(otDef.apiName)
			ot := &oms.ObjectType{
				RID:           otRID,
				OntologyRID:   ont.RID,
				APIName:       otDef.apiName,
				DisplayName:   otDef.display,
				PrimaryKey:    otDef.primaryKey,
				TitleProperty: otDef.titleProp,
				Status:        "ACTIVE",
				Visibility:    "NORMAL",
			}
			if err := env.repo.CreateObjectType(ctx, ot); err != nil {
				ontologyErr = fmt.Errorf("create object type %s: %w", otDef.apiName, err)
				return
			}

			for _, prop := range otDef.properties {
				p := &oms.Property{
					RID:           fmt.Sprintf("ri.ontology.main.property.northwind-%s-%s", otDef.apiName, prop.apiName),
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
			}
		}

		// Create link types with proper RID-based source/target
		for _, ld := range northwindLinkTypes {
			lt := &oms.LinkType{
				RID:              fmt.Sprintf("ri.ontology.main.link-type.northwind-%s", ld.apiName),
				OntologyRID:      ont.RID,
				APIName:          ld.apiName,
				DisplayName:      ld.display,
				SourceObjectType: objectTypeRID(ld.source),
				TargetObjectType: objectTypeRID(ld.target),
				Cardinality:      ld.cardinality,
			}
			if err := env.repo.CreateLinkType(ctx, lt); err != nil {
				ontologyErr = fmt.Errorf("create link type %s: %w", ld.apiName, err)
				return
			}
		}

		// Create action type: createProduct
		createProductParams, _ := json.Marshal([]map[string]interface{}{
			{"id": "productName", "type": "string", "required": true, "description": "Product name"},
			{"id": "unitPrice", "type": "double", "required": true, "description": "Unit price"},
			{"id": "discontinued", "type": "boolean", "required": false, "description": "Is discontinued"},
		})
		createProductRules, _ := json.Marshal([]map[string]interface{}{
			{
				"type":       "createObject",
				"objectType": "product",
				"propertyBindings": map[string]interface{}{
					"productName":  map[string]interface{}{"type": "parameter", "value": "productName"},
					"unitPrice":    map[string]interface{}{"type": "parameter", "value": "unitPrice"},
					"discontinued": map[string]interface{}{"type": "parameter", "value": "discontinued"},
				},
			},
		})
		at := &oms.ActionType{
			RID:         "ri.ontology.main.action-type.northwind-createProduct",
			OntologyRID: ont.RID,
			APIName:     "createProduct",
			DisplayName: "Create Product",
			Status:      "ACTIVE",
			Parameters:  createProductParams,
			Rules:       createProductRules,
		}
		if err := env.repo.CreateActionType(ctx, at); err != nil {
			ontologyErr = fmt.Errorf("create action type createProduct: %w", err)
			return
		}
	})

	if ontologyErr != nil {
		t.Fatalf("ontology setup failed: %v", ontologyErr)
	}
	return env
}

// coerceCSVValue converts a raw CSV string value to the appropriate Go type
// based on the property's baseType for indexing into Bleve.
func coerceCSVValue(val, baseType string) (interface{}, bool) {
	if val == "" || val == "NULL" {
		return nil, false
	}
	switch baseType {
	case "string":
		return val, true
	case "integer", "short", "long":
		if fv, err := strconv.ParseFloat(val, 64); err == nil {
			return fv, true
		}
		return nil, false
	case "double", "float":
		if fv, err := strconv.ParseFloat(val, 64); err == nil {
			return fv, true
		}
		return nil, false
	case "boolean":
		return val == "1" || strings.EqualFold(val, "true"), true
	case "timestamp":
		if t, err := time.Parse("2006-01-02 15:04:05.000", val); err == nil {
			return t, true
		}
		if t, err := time.Parse("2006-01-02 15:04:05", val); err == nil {
			return t, true
		}
		if t, err := time.Parse("2006-01-02", val); err == nil {
			return t, true
		}
		return val, true // fallback: store as string
	case "date":
		if t, err := time.Parse("2006-01-02", val); err == nil {
			return t, true
		}
		return val, true
	default:
		return val, true
	}
}

func setupIndexData(t *testing.T) *northwindEnv {
	t.Helper()
	env := setupOntology(t)

	indexDataOnce.Do(func() {
		for _, otDef := range northwindObjectTypes {
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

			f, err := os.Open(northwindCSVPath(otDef.csvFile))
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
				var docID string

				// Handle composite PK for orderDetail
				if otDef.apiName == "orderDetail" {
					docID = rowMap["orderID"] + "-" + rowMap["productID"]
					doc["orderDetailId"] = docID
				} else {
					docID = rowMap[pkCol]
				}

				for csvCol, propName := range colMap {
					val := rowMap[csvCol]
					bt := propertyBaseTypes[propName]
					if coerced, ok := coerceCSVValue(val, bt); ok {
						doc[propName] = coerced
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

func doRequest(t *testing.T, env *northwindEnv, method, path string, body interface{}) *httptest.ResponseRecorder {
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

func parseJSON(t *testing.T, rr *httptest.ResponseRecorder, target interface{}) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), target); err != nil {
		t.Fatalf("parse response JSON: %v\nbody: %s", err, rr.Body.String())
	}
}

// ===========================================================================
// Phase 1: Schema Definition — OMS read-back verification
// ===========================================================================

func TestNorthwind_Phase1_Schema(t *testing.T) {
	env := setupOntology(t)

	t.Run("GetOntology", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp map[string]interface{}
		parseJSON(t, rr, &resp)
		if resp["apiName"] != "northwind" {
			t.Errorf("expected apiName=northwind, got %v", resp["apiName"])
		}
	})

	t.Run("ListObjectTypes_Returns10", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objectTypes", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Data []json.RawMessage `json:"data"`
		}
		parseJSON(t, rr, &resp)
		if len(resp.Data) != 10 {
			t.Errorf("expected 10 object types, got %d", len(resp.Data))
		}
	})

	t.Run("GetProductObjectType_HasBooleanProperty", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objectTypes/product", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp map[string]interface{}
		parseJSON(t, rr, &resp)
		if resp["apiName"] != "product" {
			t.Errorf("expected apiName=product, got %v", resp["apiName"])
		}
		props, ok := resp["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("expected properties map in response")
		}
		disc, ok := props["discontinued"]
		if !ok {
			t.Fatal("expected 'discontinued' property on product")
		}
		discMap, ok := disc.(map[string]interface{})
		if !ok {
			t.Fatal("expected discontinued to be a map")
		}
		// WireJSON format: properties.{name}.dataType.type = baseType
		dt, _ := discMap["dataType"].(map[string]interface{})
		if dt == nil || dt["type"] != "boolean" {
			t.Errorf("expected discontinued.dataType.type=boolean, got %v", dt)
		}
	})

	t.Run("GetOrderObjectType_HasTimestampProperties", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objectTypes/order", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp map[string]interface{}
		parseJSON(t, rr, &resp)
		props, ok := resp["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("expected properties map")
		}
		for _, tsField := range []string{"orderDate", "requiredDate", "shippedDate"} {
			p, ok := props[tsField]
			if !ok {
				t.Errorf("expected '%s' property on order", tsField)
				continue
			}
			pm, _ := p.(map[string]interface{})
			dt, _ := pm["dataType"].(map[string]interface{})
			if dt == nil || dt["type"] != "timestamp" {
				t.Errorf("expected %s.dataType.type=timestamp, got %v", tsField, dt)
			}
		}
	})

	t.Run("GetProductObjectType_HasIntegerProperties", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objectTypes/product", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp map[string]interface{}
		parseJSON(t, rr, &resp)
		props := resp["properties"].(map[string]interface{})
		for _, intField := range []string{"unitsInStock", "unitsOnOrder", "reorderLevel"} {
			p, ok := props[intField]
			if !ok {
				t.Errorf("expected '%s' property on product", intField)
				continue
			}
			pm, _ := p.(map[string]interface{})
			dt, _ := pm["dataType"].(map[string]interface{})
			if dt == nil || dt["type"] != "integer" {
				t.Errorf("expected %s.dataType.type=integer, got %v", intField, dt)
			}
		}
	})

	t.Run("ListOutgoingLinkTypes_Employee", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objectTypes/employee/outgoingLinkTypes", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Data []json.RawMessage `json:"data"`
		}
		parseJSON(t, rr, &resp)
		// employee has 3 outgoing: employee-orders, employee-reportsTo, employee-territories
		if len(resp.Data) != 3 {
			t.Errorf("expected 3 outgoing link types for employee, got %d", len(resp.Data))
		}

		// Verify MANY_TO_MANY link exists
		var foundM2M bool
		for _, raw := range resp.Data {
			var lt map[string]interface{}
			json.Unmarshal(raw, &lt)
			if lt["cardinality"] == "MANY_TO_MANY" {
				foundM2M = true
			}
		}
		if !foundM2M {
			t.Error("expected at least one MANY_TO_MANY link type for employee")
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
			t.Error("expected at least 1 action type (createProduct)")
		}
	})
}

// ===========================================================================
// Phase 2: Data Indexing Verification
// ===========================================================================

func TestNorthwind_Phase2_IndexData(t *testing.T) {
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
// Phase 3: Single Object Read — verify typed properties
// ===========================================================================

func TestNorthwind_Phase3_GetObject(t *testing.T) {
	env := setupIndexData(t)

	t.Run("GetProduct_Chai", func(t *testing.T) {
		// Product #1: Chai, unitPrice=18.00, unitsInStock=39, discontinued=0
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/product/1", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var obj map[string]interface{}
		parseJSON(t, rr, &obj)

		if obj["productName"] != "Chai" {
			t.Errorf("expected productName=Chai, got %v", obj["productName"])
		}

		// unitPrice should be numeric
		if up, ok := obj["unitPrice"].(float64); !ok || up != 18.0 {
			t.Errorf("expected unitPrice=18.0 (float64), got %v (%T)", obj["unitPrice"], obj["unitPrice"])
		}

		// unitsInStock should be numeric (Bleve stores as float64)
		if us, ok := obj["unitsInStock"].(float64); !ok || us != 39.0 {
			t.Errorf("expected unitsInStock=39.0 (float64), got %v (%T)", obj["unitsInStock"], obj["unitsInStock"])
		}

		// discontinued should be boolean false
		if disc, ok := obj["discontinued"].(bool); !ok || disc != false {
			t.Errorf("expected discontinued=false (bool), got %v (%T)", obj["discontinued"], obj["discontinued"])
		}
	})

	t.Run("GetProduct_Discontinued", func(t *testing.T) {
		// Product #5: Chef Anton's Gumbo Mix, discontinued=1
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/product/5", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var obj map[string]interface{}
		parseJSON(t, rr, &obj)

		if disc, ok := obj["discontinued"].(bool); !ok || disc != true {
			t.Errorf("expected discontinued=true (bool), got %v (%T)", obj["discontinued"], obj["discontinued"])
		}
	})

	t.Run("GetOrder_10248", func(t *testing.T) {
		// Order #10248: orderDate=1996-07-04, freight=32.38
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/order/10248", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var obj map[string]interface{}
		parseJSON(t, rr, &obj)

		// freight should be numeric
		if fr, ok := obj["freight"].(float64); !ok || fr != 32.38 {
			t.Errorf("expected freight=32.38 (float64), got %v (%T)", obj["freight"], obj["freight"])
		}

		// orderDate should be present (format depends on Bleve datetime serialization)
		if obj["orderDate"] == nil {
			t.Error("expected orderDate to be present")
		}
	})

	t.Run("GetEmployee_1", func(t *testing.T) {
		// Employee #1: Nancy Davolio, birthDate=1948-12-08, hireDate=1992-05-01
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/employee/1", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var obj map[string]interface{}
		parseJSON(t, rr, &obj)

		if obj["lastName"] != "Davolio" {
			t.Errorf("expected lastName=Davolio, got %v", obj["lastName"])
		}
		if obj["firstName"] != "Nancy" {
			t.Errorf("expected firstName=Nancy, got %v", obj["firstName"])
		}
		// birthDate and hireDate should be present
		if obj["birthDate"] == nil {
			t.Error("expected birthDate to be present")
		}
		if obj["hireDate"] == nil {
			t.Error("expected hireDate to be present")
		}
	})

	t.Run("GetCategory_1", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/category/1", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var obj map[string]interface{}
		parseJSON(t, rr, &obj)
		if obj["categoryName"] != "Beverages" {
			t.Errorf("expected categoryName=Beverages, got %v", obj["categoryName"])
		}
	})

	t.Run("GetCustomer_ALFKI", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/customer/ALFKI", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var obj map[string]interface{}
		parseJSON(t, rr, &obj)
		if obj["companyName"] != "Alfreds Futterkiste" {
			t.Errorf("expected companyName=Alfreds Futterkiste, got %v", obj["companyName"])
		}
		if obj["country"] != "Germany" {
			t.Errorf("expected country=Germany, got %v", obj["country"])
		}
	})
}

// ===========================================================================
// Phase 4: Search with type-specific filters
// ===========================================================================

func TestNorthwind_Phase4_Search(t *testing.T) {
	env := setupIndexData(t)

	t.Run("SearchProducts_DiscontinuedTrue", func(t *testing.T) {
		body := map[string]interface{}{
			"where": map[string]interface{}{
				"type":  "eq",
				"field": "discontinued",
				"value": true,
			},
			"select": []string{"productId", "discontinued"},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/product/search", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Data []map[string]interface{} `json:"data"`
		}
		parseJSON(t, rr, &resp)

		// Northwind has 8 discontinued products
		if len(resp.Data) == 0 {
			t.Fatal("expected some discontinued products, got 0")
		}
		// Verify all returned products are discontinued
		for _, p := range resp.Data {
			if disc, ok := p["discontinued"].(bool); !ok || !disc {
				t.Errorf("expected discontinued=true, got %v (%T)", p["discontinued"], p["discontinued"])
			}
		}
	})

	t.Run("SearchProducts_HighPrice", func(t *testing.T) {
		body := map[string]interface{}{
			"where": map[string]interface{}{
				"type":  "gt",
				"field": "unitPrice",
				"value": 100.0,
			},
			"select": []string{"productId", "unitPrice"},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/product/search", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Data []map[string]interface{} `json:"data"`
		}
		parseJSON(t, rr, &resp)

		// Should find products with unitPrice > 100
		for _, p := range resp.Data {
			if up, ok := p["unitPrice"].(float64); ok && up <= 100.0 {
				t.Errorf("expected unitPrice > 100, got %v", up)
			}
		}
	})

	t.Run("SearchOrders_HighFreight", func(t *testing.T) {
		body := map[string]interface{}{
			"where": map[string]interface{}{
				"type":  "gt",
				"field": "freight",
				"value": 500.0,
			},
			"select": []string{"orderId", "freight"},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/order/search", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Data []map[string]interface{} `json:"data"`
		}
		parseJSON(t, rr, &resp)

		for _, o := range resp.Data {
			if fr, ok := o["freight"].(float64); ok && fr <= 500.0 {
				t.Errorf("expected freight > 500, got %v", fr)
			}
		}
	})

	t.Run("SearchCustomers_ByCountry", func(t *testing.T) {
		body := map[string]interface{}{
			"where": map[string]interface{}{
				"type":  "eq",
				"field": "country",
				"value": "Germany",
			},
			"select": []string{"customerId", "country"},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/customer/search", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Data []map[string]interface{} `json:"data"`
		}
		parseJSON(t, rr, &resp)

		if len(resp.Data) == 0 {
			t.Fatal("expected some German customers, got 0")
		}
	})

	t.Run("SearchProducts_InStock", func(t *testing.T) {
		body := map[string]interface{}{
			"where": map[string]interface{}{
				"type":  "gt",
				"field": "unitsInStock",
				"value": 0,
			},
			"select": []string{"productId", "unitsInStock"},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/product/search", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Data []map[string]interface{} `json:"data"`
		}
		parseJSON(t, rr, &resp)

		if len(resp.Data) == 0 {
			t.Fatal("expected products in stock, got 0")
		}
	})
}

// ===========================================================================
// Phase 5: Aggregation on diverse types
// ===========================================================================

func TestNorthwind_Phase5_Aggregation(t *testing.T) {
	env := setupIndexData(t)

	t.Run("AggregateProducts_ByCategory", func(t *testing.T) {
		body := map[string]interface{}{
			"groupBy": []map[string]interface{}{
				{"field": "categoryId", "type": "exact"},
			},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/product/aggregate", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp interface{}
		parseJSON(t, rr, &resp)
		// Just verify we got a valid response (aggregation shape varies)
		if resp == nil {
			t.Fatal("expected non-nil aggregation response")
		}
	})

	t.Run("AggregateOrders_ByShipCountry", func(t *testing.T) {
		body := map[string]interface{}{
			"groupBy": []map[string]interface{}{
				{"field": "shipCountry", "type": "exact"},
			},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objects/order/aggregate", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp interface{}
		parseJSON(t, rr, &resp)
		if resp == nil {
			t.Fatal("expected non-nil aggregation response")
		}
	})
}

// ===========================================================================
// Phase 6: Link types — verify MANY_TO_MANY and self-referencing
// ===========================================================================

func TestNorthwind_Phase6_Links(t *testing.T) {
	env := setupOntology(t)

	t.Run("ListOutgoingLinkTypes_Category_ONE_TO_MANY", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objectTypes/category/outgoingLinkTypes", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Data []map[string]interface{} `json:"data"`
		}
		parseJSON(t, rr, &resp)
		if len(resp.Data) != 1 {
			t.Fatalf("expected 1 outgoing link type for category, got %d", len(resp.Data))
		}
		if resp.Data[0]["cardinality"] != "ONE_TO_MANY" {
			t.Errorf("expected cardinality=ONE_TO_MANY, got %v", resp.Data[0]["cardinality"])
		}
	})

	t.Run("ListOutgoingLinkTypes_Employee_SelfRef", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objectTypes/employee/outgoingLinkTypes", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Data []map[string]interface{} `json:"data"`
		}
		parseJSON(t, rr, &resp)

		var foundSelfRef, foundM2M bool
		for _, lt := range resp.Data {
			src := lt["sourceObjectTypeRid"]
			tgt := lt["targetObjectTypeRid"]
			if src == tgt {
				foundSelfRef = true
			}
			if lt["cardinality"] == "MANY_TO_MANY" {
				foundM2M = true
			}
		}
		if !foundSelfRef {
			t.Error("expected self-referencing link type for employee (reportsTo)")
		}
		if !foundM2M {
			t.Error("expected MANY_TO_MANY link type for employee (territories)")
		}
	})

	t.Run("ListOutgoingLinkTypes_Order", func(t *testing.T) {
		rr := doRequest(t, env, http.MethodGet,
			fmt.Sprintf("/api/v2/ontologies/%s/objectTypes/order/outgoingLinkTypes", env.ontologyRID), nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Data []map[string]interface{} `json:"data"`
		}
		parseJSON(t, rr, &resp)
		// order has: order-orderDetails (ONE_TO_MANY), order-products (MANY_TO_MANY)
		if len(resp.Data) != 2 {
			t.Errorf("expected 2 outgoing link types for order, got %d", len(resp.Data))
		}
	})

	t.Run("VerifyAllCardinalities", func(t *testing.T) {
		// Verify that the system supports all three cardinalities
		cardinalitySeen := make(map[string]bool)
		for _, ld := range northwindLinkTypes {
			cardinalitySeen[ld.cardinality] = true
		}
		for _, c := range []string{"ONE_TO_MANY", "MANY_TO_MANY"} {
			if !cardinalitySeen[c] {
				t.Errorf("expected cardinality %s in link type definitions", c)
			}
		}
	})
}

// ===========================================================================
// Phase 7: ObjectSet operations — loadObjects with filters
// ===========================================================================

func TestNorthwind_Phase7_ObjectSet(t *testing.T) {
	env := setupIndexData(t)

	t.Run("LoadObjects_Products", func(t *testing.T) {
		body := map[string]interface{}{
			"objectSet": map[string]interface{}{
				"type":       "base",
				"objectType": "product",
			},
			"select": []string{"productId", "productName"},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objectSets/loadObjects", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Data []map[string]interface{} `json:"data"`
		}
		parseJSON(t, rr, &resp)
		if len(resp.Data) == 0 {
			t.Fatal("expected products in objectSet, got 0")
		}
	})

	t.Run("LoadObjects_FilteredProducts", func(t *testing.T) {
		body := map[string]interface{}{
			"objectSet": map[string]interface{}{
				"type": "filter",
				"objectSet": map[string]interface{}{
					"type":       "base",
					"objectType": "product",
				},
				"where": map[string]interface{}{
					"type":  "eq",
					"field": "discontinued",
					"value": true,
				},
			},
			"select": []string{"productId", "discontinued"},
		}
		rr := doRequest(t, env, http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/objectSets/loadObjects", env.ontologyRID), body)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Data []map[string]interface{} `json:"data"`
		}
		parseJSON(t, rr, &resp)
		if len(resp.Data) == 0 {
			t.Fatal("expected discontinued products in filtered objectSet, got 0")
		}
		for _, p := range resp.Data {
			if disc, ok := p["discontinued"].(bool); !ok || !disc {
				t.Errorf("expected all filtered products to be discontinued, got %v (%T)", p["discontinued"], p["discontinued"])
			}
		}
	})
}

func (r *inMemoryOmsRepo) CreateSecurityPolicy(_ context.Context, _ *oms.SecurityPolicy) error { return nil }
func (r *inMemoryOmsRepo) GetSecurityPolicy(_ context.Context, _ string) (*oms.SecurityPolicy, error) { return nil, nil }
func (r *inMemoryOmsRepo) ListSecurityPolicies(_ context.Context, _ string) ([]oms.SecurityPolicy, error) { return nil, nil }
func (r *inMemoryOmsRepo) UpdateSecurityPolicy(_ context.Context, _ *oms.SecurityPolicy) error { return nil }
func (r *inMemoryOmsRepo) DeleteSecurityPolicy(_ context.Context, _ string) error { return nil }
