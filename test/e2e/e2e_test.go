package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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
// In-memory OMS Repository
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
	// Load properties
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
func (r *inMemoryOmsRepo) CountPropertiesUsingSharedProperty(_ context.Context, _ string) (int, error) {
	return 0, nil
}

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
func (r *inMemoryOmsRepo) CountObjectTypesInTypeGroup(_ context.Context, _ string) (int, error) {
	return 0, nil
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
func (r *inMemoryOmsRepo) UpdateActionLogSideEffectStatus(_ context.Context, _ int64, _ json.RawMessage) error {
	return nil
}
func (r *inMemoryOmsRepo) InsertSideEffectDLQRow(_ context.Context, _ *oms.SideEffectDLQRow) error {
	return nil
}
func (r *inMemoryOmsRepo) ListSideEffectDLQByActionLog(_ context.Context, _ int64) ([]oms.SideEffectDLQRow, error) {
	return nil, nil
}
func (r *inMemoryOmsRepo) ListPendingSideEffectDLQRows(_ context.Context, _ int) ([]oms.SideEffectDLQRow, error) {
	return nil, nil
}
func (r *inMemoryOmsRepo) MarkSideEffectDLQAbandoned(_ context.Context, _ int64) error { return nil }
func (r *inMemoryOmsRepo) GetSideEffectDLQRow(_ context.Context, _ int64) (*oms.SideEffectDLQRow, error) {
	return nil, oms.ErrNotFound
}
func (r *inMemoryOmsRepo) UpdateSideEffectDLQAfterReplay(_ context.Context, _ int64, _ json.RawMessage, _ bool) error {
	return nil
}

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
// Test helper: build full router with in-memory deps
// ---------------------------------------------------------------------------

type testEnv struct {
	server   *httptest.Server
	repo     *inMemoryOmsRepo
	indexMgr *index.Manager
}

func setupTestServer(t *testing.T) *testEnv {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "weave-e2e-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	repo := newInMemoryOmsRepo()
	indexMgr := index.NewManager(tmpDir)
	t.Cleanup(func() { indexMgr.Close() })

	linkResolver := links.NewResolver(repo, indexMgr)
	ossSvc := oss.NewService(repo, indexMgr, linkResolver)
	aggEngine := aggregation.NewEngine()
	objSetStore := objectset.NewStore(1 * time.Hour)
	objSetExecutor := objectset.NewExecutor(indexMgr, linkResolver, objSetStore)
	actionExecutor := actions.NewExecutor(repo, nil) // no NATS publisher in unit E2E

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
	// V2 read-only routes
	r.Get("/api/v2/ontologies", omsHandler.ListOntologies)
	r.Get("/api/v2/ontologies/{ontologyApiName}", omsHandler.GetOntology)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes", omsHandler.ListObjectTypes)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}", omsHandler.GetObjectType)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/outgoingLinkTypes", omsHandler.ListOutgoingLinkTypes)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes", omsHandler.ListActionTypes)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes/{actionTypeRid}", omsHandler.GetActionType)
	// Admin routes
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

	// Aggregation
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
	t.Cleanup(srv.Close)

	return &testEnv{
		server:   srv,
		repo:     repo,
		indexMgr: indexMgr,
	}
}

// seedOntology creates an ontology and an object type with properties and index.
// Returns (ontologyRID, objectTypeRID).
func seedOntology(t *testing.T, env *testEnv) (string, string) {
	t.Helper()
	ctx := context.Background()

	ont := &oms.Ontology{
		RID:         "ri.ontology.main.ontology.test-1",
		APIName:     "testOntology",
		DisplayName: "Test Ontology",
	}
	if err := env.repo.CreateOntology(ctx, ont); err != nil {
		t.Fatalf("seed ontology: %v", err)
	}

	ot := &oms.ObjectType{
		RID:         "ri.ontology.main.object-type.employee",
		OntologyRID: ont.RID,
		APIName:     "Employee",
		DisplayName: "Employee",
		PrimaryKey:  "employeeId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := env.repo.CreateObjectType(ctx, ot); err != nil {
		t.Fatalf("seed object type: %v", err)
	}

	props := []oms.Property{
		{RID: "ri.ontology.main.property.empId", ObjectTypeRID: ot.RID, APIName: "employeeId", BaseType: "string", IsSearchable: true},
		{RID: "ri.ontology.main.property.name", ObjectTypeRID: ot.RID, APIName: "name", BaseType: "string", IsSearchable: true},
		{RID: "ri.ontology.main.property.age", ObjectTypeRID: ot.RID, APIName: "age", BaseType: "integer", IsSearchable: true},
		{RID: "ri.ontology.main.property.dept", ObjectTypeRID: ot.RID, APIName: "department", BaseType: "string", IsSearchable: true},
	}
	for i := range props {
		if err := env.repo.CreateProperty(ctx, &props[i]); err != nil {
			t.Fatalf("seed property %s: %v", props[i].APIName, err)
		}
	}

	// Create Bleve index
	indexProps := make([]index.Property, len(props))
	for i, p := range props {
		indexProps[i] = index.Property{
			APIName:      p.APIName,
			BaseType:     p.BaseType,
			IsSearchable: p.IsSearchable,
		}
	}
	if _, err := env.indexMgr.EnsureIndex("Employee", indexProps); err != nil {
		t.Fatalf("ensure index: %v", err)
	}

	return ont.RID, ot.RID
}

// indexEmployees indexes sample employee documents.
func indexEmployees(t *testing.T, env *testEnv) {
	t.Helper()
	employees := []struct {
		id   string
		doc  map[string]interface{}
	}{
		{"emp1", map[string]interface{}{"employeeId": "emp1", "name": "alice", "age": float64(30), "department": "engineering"}},
		{"emp2", map[string]interface{}{"employeeId": "emp2", "name": "bob", "age": float64(25), "department": "engineering"}},
		{"emp3", map[string]interface{}{"employeeId": "emp3", "name": "charlie", "age": float64(35), "department": "sales"}},
		{"emp4", map[string]interface{}{"employeeId": "emp4", "name": "diana", "age": float64(28), "department": "sales"}},
		{"emp5", map[string]interface{}{"employeeId": "emp5", "name": "eve", "age": float64(32), "department": "marketing"}},
	}
	for _, e := range employees {
		if err := env.indexMgr.IndexDocument("Employee", e.id, e.doc); err != nil {
			t.Fatalf("index employee %s: %v", e.id, err)
		}
	}
}

func doGet(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

func doPost(t *testing.T, url string, body interface{}) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, v interface{}) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// findE2EMetric is a test helper that looks up a metric by name from
// a JSON metrics array ([]interface{} of {name, value} objects).
func findE2EMetric(t *testing.T, metricsArr []interface{}, name string) interface{} {
	t.Helper()
	for _, m := range metricsArr {
		mv := m.(map[string]interface{})
		if mv["name"] == name {
			return mv["value"]
		}
	}
	t.Fatalf("metric %q not found in %v", name, metricsArr)
	return nil
}

// ---------------------------------------------------------------------------
// E2E Tests
// ---------------------------------------------------------------------------

// 1. Health endpoint
func TestE2E_HealthEndpoint(t *testing.T) {
	env := setupTestServer(t)
	resp := doGet(t, env.server.URL+"/health")
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]string
	decodeJSON(t, resp, &body)
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %q", body["status"])
	}
}

// 2. Create Ontology
func TestE2E_CreateOntology(t *testing.T) {
	env := setupTestServer(t)
	resp := doPost(t, env.server.URL+"/api/admin/ontologies", map[string]string{
		"apiName":     "testOnt",
		"displayName": "Test Ontology",
	})
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	if body["apiName"] != "testOnt" {
		t.Errorf("expected apiName=testOnt, got %v", body["apiName"])
	}
	if body["rid"] == nil || body["rid"] == "" {
		t.Error("expected non-empty rid")
	}
}

// 3. List Ontologies
func TestE2E_ListOntologies(t *testing.T) {
	env := setupTestServer(t)

	// Create two ontologies
	doPost(t, env.server.URL+"/api/admin/ontologies", map[string]string{
		"apiName": "ont1", "displayName": "Ont 1",
	}).Body.Close()
	doPost(t, env.server.URL+"/api/admin/ontologies", map[string]string{
		"apiName": "ont2", "displayName": "Ont 2",
	}).Body.Close()

	resp := doGet(t, env.server.URL+"/api/v2/ontologies")
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatalf("expected data to be array, got %T", body["data"])
	}
	if len(data) != 2 {
		t.Errorf("expected 2 ontologies, got %d", len(data))
	}
}

// 4. Create ObjectType
func TestE2E_CreateObjectType(t *testing.T) {
	env := setupTestServer(t)

	// Create ontology first
	resp := doPost(t, env.server.URL+"/api/admin/ontologies", map[string]string{
		"apiName": "myOnt", "displayName": "My Ontology",
	})
	var ont map[string]interface{}
	decodeJSON(t, resp, &ont)
	ontRid := ont["rid"].(string)

	// Create object type
	resp = doPost(t, env.server.URL+"/api/admin/ontologies/"+ontRid+"/objectTypes", map[string]interface{}{
		"apiName":     "Employee",
		"displayName": "Employee",
		"primaryKey":  "employeeId",
	})
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	if body["apiName"] != "Employee" {
		t.Errorf("expected apiName=Employee, got %v", body["apiName"])
	}
	if body["primaryKey"] != "employeeId" {
		t.Errorf("expected primaryKey=employeeId, got %v", body["primaryKey"])
	}
}

// 5. Get ObjectType
func TestE2E_GetObjectType(t *testing.T) {
	env := setupTestServer(t)
	ontRid, _ := seedOntology(t, env)

	resp := doGet(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/objectTypes/Employee")
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	if body["apiName"] != "Employee" {
		t.Errorf("expected apiName=Employee, got %v", body["apiName"])
	}
	if body["primaryKey"] != "employeeId" {
		t.Errorf("expected primaryKey=employeeId, got %v", body["primaryKey"])
	}
	// Should have properties
	props, ok := body["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties map, got %T", body["properties"])
	}
	if len(props) == 0 {
		t.Error("expected non-empty properties")
	}
}

// 6. Create LinkType
func TestE2E_CreateLinkType(t *testing.T) {
	env := setupTestServer(t)
	ontRid, otRid := seedOntology(t, env)

	resp := doPost(t, env.server.URL+"/api/admin/ontologies/"+ontRid+"/linkTypes", map[string]interface{}{
		"apiName":                  "manages",
		"displayName":             "Manages",
		"objectTypeApiName":       otRid,
		"linkedObjectTypeApiName": otRid,
		"cardinality":             "MANY",
	})
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	if body["apiName"] != "manages" {
		t.Errorf("expected apiName=manages, got %v", body["apiName"])
	}
}

// 7. Create ActionType
func TestE2E_CreateActionType(t *testing.T) {
	env := setupTestServer(t)
	ontRid, _ := seedOntology(t, env)

	resp := doPost(t, env.server.URL+"/api/admin/ontologies/"+ontRid+"/actionTypes", map[string]interface{}{
		"apiName":     "createEmployee",
		"displayName": "Create Employee",
		"parameters":  json.RawMessage(`[{"id":"name","type":"string","required":true}]`),
		"rules":       json.RawMessage(`[{"type":"createObject","objectType":"Employee","propertyBindings":{"name":{"type":"parameter","value":"name"}}}]`),
	})
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	if body["apiName"] != "createEmployee" {
		t.Errorf("expected apiName=createEmployee, got %v", body["apiName"])
	}
}

// 8. List Objects - Empty
func TestE2E_ListObjects_Empty(t *testing.T) {
	env := setupTestServer(t)
	ontRid, _ := seedOntology(t, env)

	resp := doGet(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/objects/Employee")
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	data := body["data"].([]interface{})
	if len(data) != 0 {
		t.Errorf("expected 0 objects, got %d", len(data))
	}
}

// 9. Index and Get Object
func TestE2E_IndexAndGetObject(t *testing.T) {
	env := setupTestServer(t)
	ontRid, _ := seedOntology(t, env)
	indexEmployees(t, env)

	resp := doGet(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/objects/Employee/emp1")
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	if body["__primaryKey"] != "emp1" {
		t.Errorf("expected primaryKey=emp1, got %v", body["__primaryKey"])
	}
	if body["__apiName"] != "Employee" {
		t.Errorf("expected apiName=Employee, got %v", body["__apiName"])
	}
}

// 10. List Objects with data
func TestE2E_ListObjects_WithData(t *testing.T) {
	env := setupTestServer(t)
	ontRid, _ := seedOntology(t, env)
	indexEmployees(t, env)

	resp := doGet(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/objects/Employee")
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	data := body["data"].([]interface{})
	if len(data) != 5 {
		t.Errorf("expected 5 objects, got %d", len(data))
	}
}

// 11. Search Objects with eq
func TestE2E_SearchObjects_Eq(t *testing.T) {
	env := setupTestServer(t)
	ontRid, _ := seedOntology(t, env)
	indexEmployees(t, env)

	resp := doPost(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/objects/Employee/search", map[string]interface{}{
		"where": map[string]interface{}{
			"type":  "eq",
			"field": "department",
			"value": "engineering",
		},
		"select": []string{"employeeId", "name", "department"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	data := body["data"].([]interface{})
	if len(data) != 2 {
		t.Errorf("expected 2 objects matching department=engineering, got %d", len(data))
	}
}

// 12. Search Objects with and
func TestE2E_SearchObjects_And(t *testing.T) {
	env := setupTestServer(t)
	ontRid, _ := seedOntology(t, env)
	indexEmployees(t, env)

	resp := doPost(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/objects/Employee/search", map[string]interface{}{
		"where": map[string]interface{}{
			"type": "and",
			"value": []map[string]interface{}{
				{"type": "eq", "field": "department", "value": "engineering"},
				{"type": "gte", "field": "age", "value": 30},
			},
		},
		"select": []string{"employeeId", "name", "age", "department"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	data := body["data"].([]interface{})
	// alice: eng, 30. bob: eng, 25. Only alice matches >=30 AND engineering
	if len(data) != 1 {
		t.Errorf("expected 1 object matching department=engineering AND age>=30, got %d", len(data))
	}
}

// 13. List Objects with pagination
func TestE2E_ListObjects_Pagination(t *testing.T) {
	env := setupTestServer(t)
	ontRid, _ := seedOntology(t, env)
	indexEmployees(t, env)

	// Page 1: size=2
	resp := doGet(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/objects/Employee?pageSize=2")
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	data := body["data"].([]interface{})
	if len(data) != 2 {
		t.Errorf("expected 2 objects in page 1, got %d", len(data))
	}
	nextToken, ok := body["nextPageToken"].(string)
	if !ok || nextToken == "" {
		t.Fatal("expected nextPageToken to be non-empty")
	}

	// Page 2
	resp2 := doGet(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/objects/Employee?pageSize=2&pageToken="+nextToken)
	defer resp2.Body.Close()
	var body2 map[string]interface{}
	decodeJSON(t, resp2, &body2)
	data2 := body2["data"].([]interface{})
	if len(data2) != 2 {
		t.Errorf("expected 2 objects in page 2, got %d", len(data2))
	}
}

// 14. Aggregate count
func TestE2E_Aggregate_Count(t *testing.T) {
	env := setupTestServer(t)
	ontRid, _ := seedOntology(t, env)
	indexEmployees(t, env)

	resp := doPost(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/objects/Employee/aggregate", map[string]interface{}{
		"aggregation": []map[string]interface{}{
			{"type": "count", "name": "total"},
		},
	})
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	data := body["data"].([]interface{})
	if len(data) == 0 {
		t.Fatal("expected at least one row")
	}
	row := data[0].(map[string]interface{})
	metrics := row["metrics"].([]interface{})
	count := findE2EMetric(t, metrics, "total")
	// count should be 5
	if count.(float64) != 5 {
		t.Errorf("expected count=5, got %v", count)
	}
}

// 15. Aggregate min/max
func TestE2E_Aggregate_MinMax(t *testing.T) {
	env := setupTestServer(t)
	ontRid, _ := seedOntology(t, env)
	indexEmployees(t, env)

	resp := doPost(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/objects/Employee/aggregate", map[string]interface{}{
		"aggregation": []map[string]interface{}{
			{"type": "min", "field": "age", "name": "minAge"},
			{"type": "max", "field": "age", "name": "maxAge"},
		},
	})
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	data := body["data"].([]interface{})
	if len(data) == 0 {
		t.Fatal("expected at least one row")
	}
	row := data[0].(map[string]interface{})
	metrics := row["metrics"].([]interface{})

	minAge := findE2EMetric(t, metrics, "minAge").(float64)
	maxAge := findE2EMetric(t, metrics, "maxAge").(float64)
	if minAge != 25 {
		t.Errorf("expected minAge=25, got %v", minAge)
	}
	if maxAge != 35 {
		t.Errorf("expected maxAge=35, got %v", maxAge)
	}
}

// 16. Aggregate with groupBy
func TestE2E_Aggregate_GroupBy(t *testing.T) {
	env := setupTestServer(t)
	ontRid, _ := seedOntology(t, env)
	indexEmployees(t, env)

	resp := doPost(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/objects/Employee/aggregate", map[string]interface{}{
		"aggregation": []map[string]interface{}{
			{"type": "count", "name": "count"},
		},
		"groupBy": []map[string]interface{}{
			{"type": "exact", "field": "department"},
		},
	})
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	data := body["data"].([]interface{})
	// Should have 3 groups: engineering(2), sales(2), marketing(1)
	if len(data) != 3 {
		t.Errorf("expected 3 groups, got %d", len(data))
	}
}

// seedEventOntology creates an ontology + "Event" ObjectType whose startDate
// property is a numeric (epoch-seconds) field, then ensures its Bleve index.
// Returns the ontology RID. Used by the duration-bucketing BDD scenarios.
func seedEventOntology(t *testing.T, env *testEnv) string {
	t.Helper()
	ctx := context.Background()

	ont := &oms.Ontology{
		RID:         "ri.ontology.main.ontology.duration-bdd",
		APIName:     "durationBddOntology",
		DisplayName: "Duration BDD Ontology",
	}
	if err := env.repo.CreateOntology(ctx, ont); err != nil {
		t.Fatalf("seed ontology: %v", err)
	}

	ot := &oms.ObjectType{
		RID:         "ri.ontology.main.object-type.event",
		OntologyRID: ont.RID,
		APIName:     "Event",
		DisplayName: "Event",
		PrimaryKey:  "eventId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := env.repo.CreateObjectType(ctx, ot); err != nil {
		t.Fatalf("seed object type: %v", err)
	}

	props := []oms.Property{
		{RID: "ri.ontology.main.property.eventId", ObjectTypeRID: ot.RID, APIName: "eventId", BaseType: "string", IsSearchable: true},
		{RID: "ri.ontology.main.property.startDate", ObjectTypeRID: ot.RID, APIName: "startDate", BaseType: "long", IsSearchable: true},
	}
	for i := range props {
		if err := env.repo.CreateProperty(ctx, &props[i]); err != nil {
			t.Fatalf("seed property %s: %v", props[i].APIName, err)
		}
	}

	indexProps := make([]index.Property, len(props))
	for i, p := range props {
		indexProps[i] = index.Property{APIName: p.APIName, BaseType: p.BaseType, IsSearchable: p.IsSearchable}
	}
	if _, err := env.indexMgr.EnsureIndex("Event", indexProps); err != nil {
		t.Fatalf("ensure Event index: %v", err)
	}
	return ont.RID
}

const dayEpoch = float64(86400)

// TestBDD_Aggregate_GroupByDuration_Quarter is the BDD contract for the P3M
// (byQuarter) ISO 8601 groupBy shortcut, exercised through the chi HTTP router.
//
//	Given an Event ObjectType with startDate epochs spanning two 90-day windows
//	When the client POSTs an aggregate with groupBy duration "P3M"
//	Then the endpoint returns 200 with one quarter bucket per 90-day window.
func TestBDD_Aggregate_GroupByDuration_Quarter(t *testing.T) {
	env := setupTestServer(t)
	ontRid := seedEventOntology(t, env)

	// day 0 + day 45 share the [0,90d) quarter; day 120 falls in [90d,180d).
	events := map[string]float64{"e1": 0, "e2": 45 * dayEpoch, "e3": 120 * dayEpoch}
	for id, epoch := range events {
		doc := map[string]interface{}{"eventId": id, "startDate": epoch}
		if err := env.indexMgr.IndexDocument("Event", id, doc); err != nil {
			t.Fatalf("index event %s: %v", id, err)
		}
	}

	resp := doPost(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/objects/Event/aggregate", map[string]interface{}{
		"aggregation": []map[string]interface{}{
			{"type": "count", "name": "count"},
		},
		"groupBy": []map[string]interface{}{
			{"type": "duration", "field": "startDate", "duration": "P3M"},
		},
	})
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 for P3M groupBy, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	data := body["data"].([]interface{})
	if len(data) != 2 {
		t.Fatalf("expected 2 quarter buckets, got %d (data=%v)", len(data), data)
	}
	total := 0.0
	for _, row := range data {
		metrics := row.(map[string]interface{})["metrics"].([]interface{})
		total += findE2EMetric(t, metrics, "count").(float64)
	}
	if total != 3 {
		t.Errorf("total count across quarter buckets = %v, want 3", total)
	}
}

// TestBDD_Aggregate_GroupByDuration_Hour is the BDD contract for the PT1H
// (byHours) ISO 8601 groupBy shortcut, exercised through the chi HTTP router.
func TestBDD_Aggregate_GroupByDuration_Hour(t *testing.T) {
	env := setupTestServer(t)
	ontRid := seedEventOntology(t, env)

	// epoch 0 + 1800 share the [0,3600) hour; epoch 7200 falls in [7200,10800).
	events := map[string]float64{"h1": 0, "h2": 1800, "h3": 7200}
	for id, epoch := range events {
		doc := map[string]interface{}{"eventId": id, "startDate": epoch}
		if err := env.indexMgr.IndexDocument("Event", id, doc); err != nil {
			t.Fatalf("index event %s: %v", id, err)
		}
	}

	resp := doPost(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/objects/Event/aggregate", map[string]interface{}{
		"aggregation": []map[string]interface{}{
			{"type": "count", "name": "count"},
		},
		"groupBy": []map[string]interface{}{
			{"type": "duration", "field": "startDate", "duration": "PT1H"},
		},
	})
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 for PT1H groupBy, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	data := body["data"].([]interface{})
	if len(data) != 2 {
		t.Fatalf("expected 2 hour buckets, got %d (data=%v)", len(data), data)
	}
}

// 17. ObjectSet base
func TestE2E_ObjectSet_Base(t *testing.T) {
	env := setupTestServer(t)
	ontRid, _ := seedOntology(t, env)
	indexEmployees(t, env)

	resp := doPost(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/objectSets/loadObjects", map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type":       "base",
			"objectType": "Employee",
		},
		"select": []string{"employeeId", "name", "age", "department"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	data := body["data"].([]interface{})
	if len(data) != 5 {
		t.Errorf("expected 5 objects, got %d", len(data))
	}
	if body["totalCount"] != "5" {
		t.Errorf("expected totalCount '5', got %v", body["totalCount"])
	}
}

// 18. ObjectSet filter
func TestE2E_ObjectSet_Filter(t *testing.T) {
	env := setupTestServer(t)
	ontRid, _ := seedOntology(t, env)
	indexEmployees(t, env)

	resp := doPost(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/objectSets/loadObjects", map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type": "filter",
			"objectSet": map[string]interface{}{
				"type":       "base",
				"objectType": "Employee",
			},
			"where": map[string]interface{}{
				"type":  "eq",
				"field": "department",
				"value": "engineering",
			},
		},
		"select": []string{"employeeId", "name", "department"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	data := body["data"].([]interface{})
	if len(data) != 2 {
		t.Errorf("expected 2 objects (engineering dept), got %d", len(data))
	}
}

// 19. ObjectSet union
func TestE2E_ObjectSet_Union(t *testing.T) {
	env := setupTestServer(t)
	ontRid, _ := seedOntology(t, env)
	indexEmployees(t, env)

	resp := doPost(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/objectSets/loadObjects", map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type": "union",
			"objectSets": []map[string]interface{}{
				{
					"type": "filter",
					"objectSet": map[string]interface{}{
						"type":       "base",
						"objectType": "Employee",
					},
					"where": map[string]interface{}{
						"type":  "eq",
						"field": "department",
						"value": "engineering",
					},
				},
				{
					"type": "filter",
					"objectSet": map[string]interface{}{
						"type":       "base",
						"objectType": "Employee",
					},
					"where": map[string]interface{}{
						"type":  "eq",
						"field": "department",
						"value": "marketing",
					},
				},
			},
		},
		"select": []string{"employeeId", "name", "department"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	data := body["data"].([]interface{})
	// engineering(2) + marketing(1) = 3
	if len(data) != 3 {
		t.Errorf("expected 3 objects (engineering + marketing), got %d", len(data))
	}
}

// 20. ObjectSet intersect
func TestE2E_ObjectSet_Intersect(t *testing.T) {
	env := setupTestServer(t)
	ontRid, _ := seedOntology(t, env)
	indexEmployees(t, env)

	resp := doPost(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/objectSets/loadObjects", map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type": "intersect",
			"objectSets": []map[string]interface{}{
				{
					"type": "filter",
					"objectSet": map[string]interface{}{
						"type":       "base",
						"objectType": "Employee",
					},
					"where": map[string]interface{}{
						"type":  "eq",
						"field": "department",
						"value": "engineering",
					},
				},
				{
					"type": "filter",
					"objectSet": map[string]interface{}{
						"type":       "base",
						"objectType": "Employee",
					},
					"where": map[string]interface{}{
						"type":  "gte",
						"field": "age",
						"value": 30,
					},
				},
			},
		},
		"select": []string{"employeeId", "name", "age", "department"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	data := body["data"].([]interface{})
	// engineering AND age>=30 => alice(30) only
	if len(data) != 1 {
		t.Errorf("expected 1 object (alice, engineering, age=30), got %d", len(data))
	}
}

// 21. Action apply - create object
func TestE2E_Action_Apply_CreateObject(t *testing.T) {
	env := setupTestServer(t)
	ontRid, _ := seedOntology(t, env)
	indexEmployees(t, env)

	// Create action type
	ctx := context.Background()
	at := &oms.ActionType{
		RID:         "ri.ontology.main.action-type.create-emp",
		OntologyRID: ontRid,
		APIName:     "createEmployee",
		DisplayName: "Create Employee",
		Status:      "ACTIVE",
		Parameters:  json.RawMessage(`[{"id":"name","type":"string","required":true}]`),
		Rules:       json.RawMessage(`[{"type":"createObject","objectType":"Employee","propertyBindings":{"name":{"type":"parameter","value":"name"}}}]`),
	}
	if err := env.repo.CreateActionType(ctx, at); err != nil {
		t.Fatalf("create action type: %v", err)
	}

	resp := doPost(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/actions/createEmployee/apply", map[string]interface{}{
		"parameters": map[string]interface{}{
			"name": "frank",
		},
	})
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	edits, ok := body["edits"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected edits object (ActionResults), got %T", body["edits"])
	}
	if edits["type"] != "edits" {
		t.Errorf("expected edits.type=\"edits\", got %v", edits["type"])
	}
	if edits["addedObjectCount"] != float64(1) {
		t.Errorf("expected addedObjectCount=1, got %v", edits["addedObjectCount"])
	}
}

// 22. Action apply - modify object
func TestE2E_Action_Apply_ModifyObject(t *testing.T) {
	env := setupTestServer(t)
	ontRid, _ := seedOntology(t, env)
	indexEmployees(t, env)

	ctx := context.Background()
	at := &oms.ActionType{
		RID:         "ri.ontology.main.action-type.modify-emp",
		OntologyRID: ontRid,
		APIName:     "modifyEmployee",
		DisplayName: "Modify Employee",
		Status:      "ACTIVE",
		Parameters:  json.RawMessage(`[{"id":"primaryKey","type":"string","required":true},{"id":"name","type":"string","required":true}]`),
		Rules:       json.RawMessage(`[{"type":"modifyObject","objectType":"Employee","propertyBindings":{"name":{"type":"parameter","value":"name"}}}]`),
	}
	if err := env.repo.CreateActionType(ctx, at); err != nil {
		t.Fatalf("create action type: %v", err)
	}

	resp := doPost(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/actions/modifyEmployee/apply", map[string]interface{}{
		"parameters": map[string]interface{}{
			"primaryKey": "emp1",
			"name":       "alice-updated",
		},
	})
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	edits, ok := body["edits"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected edits object (ActionResults), got %T", body["edits"])
	}
	if edits["type"] != "edits" {
		t.Errorf("expected edits.type=\"edits\", got %v", edits["type"])
	}
	if edits["modifiedObjectCount"] != float64(1) {
		t.Errorf("expected modifiedObjectCount=1, got %v", edits["modifiedObjectCount"])
	}
}

// 23. Action apply - delete object
func TestE2E_Action_Apply_DeleteObject(t *testing.T) {
	env := setupTestServer(t)
	ontRid, _ := seedOntology(t, env)
	indexEmployees(t, env)

	ctx := context.Background()
	at := &oms.ActionType{
		RID:         "ri.ontology.main.action-type.delete-emp",
		OntologyRID: ontRid,
		APIName:     "deleteEmployee",
		DisplayName: "Delete Employee",
		Status:      "ACTIVE",
		Parameters:  json.RawMessage(`[{"id":"primaryKey","type":"string","required":true}]`),
		Rules:       json.RawMessage(`[{"type":"deleteObject","objectType":"Employee"}]`),
	}
	if err := env.repo.CreateActionType(ctx, at); err != nil {
		t.Fatalf("create action type: %v", err)
	}

	resp := doPost(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/actions/deleteEmployee/apply", map[string]interface{}{
		"parameters": map[string]interface{}{
			"primaryKey": "emp1",
		},
	})
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	edits, ok := body["edits"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected edits object (ActionResults), got %T", body["edits"])
	}
	if edits["type"] != "edits" {
		t.Errorf("expected edits.type=\"edits\", got %v", edits["type"])
	}
	if edits["deletedObjectCount"] != float64(1) {
		t.Errorf("expected deletedObjectCount=1, got %v", edits["deletedObjectCount"])
	}
}

// 24. Action applyBatch
func TestE2E_Action_ApplyBatch(t *testing.T) {
	env := setupTestServer(t)
	ontRid, _ := seedOntology(t, env)
	indexEmployees(t, env)

	ctx := context.Background()
	at := &oms.ActionType{
		RID:         "ri.ontology.main.action-type.create-emp",
		OntologyRID: ontRid,
		APIName:     "createEmployee",
		DisplayName: "Create Employee",
		Status:      "ACTIVE",
		Parameters:  json.RawMessage(`[{"id":"name","type":"string","required":true}]`),
		Rules:       json.RawMessage(`[{"type":"createObject","objectType":"Employee","propertyBindings":{"name":{"type":"parameter","value":"name"}}}]`),
	}
	if err := env.repo.CreateActionType(ctx, at); err != nil {
		t.Fatalf("create action type: %v", err)
	}

	resp := doPost(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/actions/createEmployee/applyBatch", map[string]interface{}{
		"actions": []map[string]interface{}{
			{"parameters": map[string]interface{}{"name": "frank"}},
			{"parameters": map[string]interface{}{"name": "grace"}},
		},
	})
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	edits, ok := body["edits"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected edits object (ActionResults), got %T", body["edits"])
	}
	if edits["addedObjectCount"] != float64(2) {
		t.Errorf("expected addedObjectCount=2, got %v", edits["addedObjectCount"])
	}
}

// 25. Auth dev mode
func TestE2E_Auth_DevMode(t *testing.T) {
	// AUTH_MODE defaults to "" (dev mode), so requests pass through.
	env := setupTestServer(t)

	resp := doGet(t, env.server.URL+"/health")
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 in dev mode, got %d", resp.StatusCode)
	}
}

// 26. Auth prod mode unauthorized
func TestE2E_Auth_ProdMode_Unauthorized(t *testing.T) {
	// Set AUTH_MODE to "token" for this test.
	original := os.Getenv("AUTH_MODE")
	os.Setenv("AUTH_MODE", "token")
	defer os.Setenv("AUTH_MODE", original)

	env := setupTestServer(t)

	// Request without Authorization header
	resp := doGet(t, env.server.URL+"/health")
	defer resp.Body.Close()

	if resp.StatusCode != 401 {
		t.Fatalf("expected 401 in prod mode without token, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	if body["errorCode"] != "UNAUTHORIZED" {
		t.Errorf("expected errorCode=UNAUTHORIZED, got %v", body["errorCode"])
	}
}

// 27. Not found 404
func TestE2E_NotFound_404(t *testing.T) {
	env := setupTestServer(t)
	ontRid, _ := seedOntology(t, env)
	indexEmployees(t, env)

	resp := doGet(t, env.server.URL+"/api/v2/ontologies/"+ontRid+"/objects/Employee/nonexistent-key")
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	if body["errorCode"] != "NOT_FOUND" {
		t.Errorf("expected errorCode=NOT_FOUND, got %v", body["errorCode"])
	}
	if body["errorName"] != "ObjectNotFound" {
		t.Errorf("expected errorName=ObjectNotFound, got %v", body["errorName"])
	}
}

// 28. Invalid request 400
func TestE2E_InvalidRequest_400(t *testing.T) {
	env := setupTestServer(t)

	// POST to admin ontologies without required fields
	resp := doPost(t, env.server.URL+"/api/admin/ontologies", map[string]string{
		"displayName": "No API Name",
	})
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	if body["errorCode"] != "INVALID_ARGUMENT" {
		t.Errorf("expected errorCode=INVALID_ARGUMENT, got %v", body["errorCode"])
	}
}

func (r *inMemoryOmsRepo) CreateSecurityPolicy(_ context.Context, _ *oms.SecurityPolicy) error { return nil }
func (r *inMemoryOmsRepo) GetSecurityPolicy(_ context.Context, _ string) (*oms.SecurityPolicy, error) { return nil, nil }
func (r *inMemoryOmsRepo) ListSecurityPolicies(_ context.Context, _ string) ([]oms.SecurityPolicy, error) { return nil, nil }
func (r *inMemoryOmsRepo) UpdateSecurityPolicy(_ context.Context, _ *oms.SecurityPolicy) error { return nil }
func (r *inMemoryOmsRepo) DeleteSecurityPolicy(_ context.Context, _ string) error { return nil }
