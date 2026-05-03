package oms

import (
	"context"
	"sync"
	"time"
)

// CachedRepository wraps an oms.Repository with in-memory TTL caching on hot
// read paths. Metadata rarely changes, and most requests hit GetOntology,
// GetObjectTypeByAPIName, GetLinkType, ListOutgoingLinkTypes, and friends --
// caching them avoids round-trips to PostgreSQL on every request.
//
// Only successful reads are cached; errors (including ErrNotFound) always fall
// through to the inner repository so that a write that later creates the
// resource becomes immediately visible without explicit invalidation.
//
// Writes through the decorator's own methods invalidate all caches.
// External invalidation (e.g. from a NATS subscriber reacting to a remote
// metadata change) is exposed via InvalidateAll.
type CachedRepository struct {
	Repository

	ttl     time.Duration
	nowFunc func() time.Time

	mu              sync.RWMutex
	ontology        map[string]cacheEntry[*Ontology]
	ontologyList    *cacheEntry[[]Ontology]
	objectType      map[string]cacheEntry[*ObjectType]
	objectTypeAPI   map[string]cacheEntry[*ObjectType]
	objectTypeList  map[string]cacheEntry[[]ObjectType]
	propertyList    map[string]cacheEntry[[]Property]
	linkType        map[string]cacheEntry[*LinkType]
	linkTypeAPI     map[string]cacheEntry[*LinkType]
	outgoingLink    map[string]cacheEntry[[]LinkType]
	incomingLink    map[string]cacheEntry[[]LinkType]
	linkTypeList    map[string]cacheEntry[[]LinkType]
	actionType      map[string]cacheEntry[*ActionType]
	actionTypeAPI   map[string]cacheEntry[*ActionType]
	actionTypeList  map[string]cacheEntry[[]ActionType]
	interfaceByRID  map[string]cacheEntry[*Interface]
	interfaceAPI    map[string]cacheEntry[*Interface]
	interfaceList   map[string]cacheEntry[[]Interface]
	valueTypeAPI    map[string]cacheEntry[*ValueType]
	valueTypeList   *cacheEntry[[]ValueType]
}

type cacheEntry[V any] struct {
	value   V
	expires time.Time
}

// NewCachedRepository returns a CachedRepository wrapping inner with the given
// TTL. A TTL of 0 disables caching (all calls fall through).
func NewCachedRepository(inner Repository, ttl time.Duration) *CachedRepository {
	return &CachedRepository{
		Repository:     inner,
		ttl:            ttl,
		nowFunc:        time.Now,
		ontology:       map[string]cacheEntry[*Ontology]{},
		objectType:     map[string]cacheEntry[*ObjectType]{},
		objectTypeAPI:  map[string]cacheEntry[*ObjectType]{},
		objectTypeList: map[string]cacheEntry[[]ObjectType]{},
		propertyList:   map[string]cacheEntry[[]Property]{},
		linkType:       map[string]cacheEntry[*LinkType]{},
		linkTypeAPI:    map[string]cacheEntry[*LinkType]{},
		outgoingLink:   map[string]cacheEntry[[]LinkType]{},
		incomingLink:   map[string]cacheEntry[[]LinkType]{},
		linkTypeList:   map[string]cacheEntry[[]LinkType]{},
		actionType:     map[string]cacheEntry[*ActionType]{},
		actionTypeAPI:  map[string]cacheEntry[*ActionType]{},
		actionTypeList: map[string]cacheEntry[[]ActionType]{},
		interfaceByRID: map[string]cacheEntry[*Interface]{},
		interfaceAPI:   map[string]cacheEntry[*Interface]{},
		interfaceList:  map[string]cacheEntry[[]Interface]{},
		valueTypeAPI:   map[string]cacheEntry[*ValueType]{},
	}
}

// SetNowFunc installs a test clock. Production code should not call this.
func (c *CachedRepository) SetNowFunc(f func() time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nowFunc = f
}

// InvalidateAll drops every cached entry. Call this when an external source
// (e.g. a NATS invalidation message) tells the process that metadata changed.
func (c *CachedRepository) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ontology = map[string]cacheEntry[*Ontology]{}
	c.ontologyList = nil
	c.objectType = map[string]cacheEntry[*ObjectType]{}
	c.objectTypeAPI = map[string]cacheEntry[*ObjectType]{}
	c.objectTypeList = map[string]cacheEntry[[]ObjectType]{}
	c.propertyList = map[string]cacheEntry[[]Property]{}
	c.linkType = map[string]cacheEntry[*LinkType]{}
	c.linkTypeAPI = map[string]cacheEntry[*LinkType]{}
	c.outgoingLink = map[string]cacheEntry[[]LinkType]{}
	c.incomingLink = map[string]cacheEntry[[]LinkType]{}
	c.linkTypeList = map[string]cacheEntry[[]LinkType]{}
	c.actionType = map[string]cacheEntry[*ActionType]{}
	c.actionTypeAPI = map[string]cacheEntry[*ActionType]{}
	c.actionTypeList = map[string]cacheEntry[[]ActionType]{}
	c.interfaceByRID = map[string]cacheEntry[*Interface]{}
	c.interfaceAPI = map[string]cacheEntry[*Interface]{}
	c.interfaceList = map[string]cacheEntry[[]Interface]{}
	c.valueTypeAPI = map[string]cacheEntry[*ValueType]{}
	c.valueTypeList = nil
}

// entryFresh reports whether e has not yet expired at the current clock.
func (c *CachedRepository) entryFresh(expires time.Time) bool {
	return c.nowFunc().Before(expires)
}

func (c *CachedRepository) newExpiry() time.Time {
	return c.nowFunc().Add(c.ttl)
}

// --- Ontology ---

func (c *CachedRepository) GetOntology(ctx context.Context, rid string) (*Ontology, error) {
	c.mu.RLock()
	if e, ok := c.ontology[rid]; ok && c.entryFresh(e.expires) {
		c.mu.RUnlock()
		return e.value, nil
	}
	c.mu.RUnlock()
	v, err := c.Repository.GetOntology(ctx, rid)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.ontology[rid] = cacheEntry[*Ontology]{value: v, expires: c.newExpiry()}
	// Keep an alias entry under the ontology's apiName so the dual-accept
	// lookup is served from a single primed read.
	if v != nil && v.APIName != "" && v.APIName != rid {
		c.ontology[v.APIName] = cacheEntry[*Ontology]{value: v, expires: c.newExpiry()}
	}
	c.mu.Unlock()
	return v, nil
}

func (c *CachedRepository) ListOntologies(ctx context.Context) ([]Ontology, error) {
	c.mu.RLock()
	if c.ontologyList != nil && c.entryFresh(c.ontologyList.expires) {
		v := c.ontologyList.value
		c.mu.RUnlock()
		return v, nil
	}
	c.mu.RUnlock()
	v, err := c.Repository.ListOntologies(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	e := cacheEntry[[]Ontology]{value: v, expires: c.newExpiry()}
	c.ontologyList = &e
	c.mu.Unlock()
	return v, nil
}

func (c *CachedRepository) CreateOntology(ctx context.Context, o *Ontology) error {
	err := c.Repository.CreateOntology(ctx, o)
	c.InvalidateAll()
	return err
}

func (c *CachedRepository) UpdateOntology(ctx context.Context, o *Ontology) error {
	err := c.Repository.UpdateOntology(ctx, o)
	c.InvalidateAll()
	return err
}

// --- ObjectType ---

func (c *CachedRepository) GetObjectType(ctx context.Context, rid string) (*ObjectType, error) {
	c.mu.RLock()
	if e, ok := c.objectType[rid]; ok && c.entryFresh(e.expires) {
		c.mu.RUnlock()
		return e.value, nil
	}
	c.mu.RUnlock()
	v, err := c.Repository.GetObjectType(ctx, rid)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.objectType[rid] = cacheEntry[*ObjectType]{value: v, expires: c.newExpiry()}
	c.mu.Unlock()
	return v, nil
}

func (c *CachedRepository) GetObjectTypeByAPIName(ctx context.Context, ontologyRID, apiName string) (*ObjectType, error) {
	key := ontologyRID + "|" + apiName
	c.mu.RLock()
	if e, ok := c.objectTypeAPI[key]; ok && c.entryFresh(e.expires) {
		c.mu.RUnlock()
		return e.value, nil
	}
	c.mu.RUnlock()
	v, err := c.Repository.GetObjectTypeByAPIName(ctx, ontologyRID, apiName)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.objectTypeAPI[key] = cacheEntry[*ObjectType]{value: v, expires: c.newExpiry()}
	c.mu.Unlock()
	return v, nil
}

func (c *CachedRepository) ListObjectTypes(ctx context.Context, ontologyRID string) ([]ObjectType, error) {
	c.mu.RLock()
	if e, ok := c.objectTypeList[ontologyRID]; ok && c.entryFresh(e.expires) {
		c.mu.RUnlock()
		return e.value, nil
	}
	c.mu.RUnlock()
	v, err := c.Repository.ListObjectTypes(ctx, ontologyRID)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.objectTypeList[ontologyRID] = cacheEntry[[]ObjectType]{value: v, expires: c.newExpiry()}
	c.mu.Unlock()
	return v, nil
}

func (c *CachedRepository) CreateObjectType(ctx context.Context, ot *ObjectType) error {
	err := c.Repository.CreateObjectType(ctx, ot)
	c.InvalidateAll()
	return err
}

func (c *CachedRepository) UpdateObjectType(ctx context.Context, ot *ObjectType) error {
	err := c.Repository.UpdateObjectType(ctx, ot)
	c.InvalidateAll()
	return err
}

func (c *CachedRepository) DeleteObjectType(ctx context.Context, rid string) error {
	err := c.Repository.DeleteObjectType(ctx, rid)
	c.InvalidateAll()
	return err
}

// --- Property ---

func (c *CachedRepository) ListProperties(ctx context.Context, objectTypeRID string) ([]Property, error) {
	c.mu.RLock()
	if e, ok := c.propertyList[objectTypeRID]; ok && c.entryFresh(e.expires) {
		c.mu.RUnlock()
		return e.value, nil
	}
	c.mu.RUnlock()
	v, err := c.Repository.ListProperties(ctx, objectTypeRID)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.propertyList[objectTypeRID] = cacheEntry[[]Property]{value: v, expires: c.newExpiry()}
	c.mu.Unlock()
	return v, nil
}

func (c *CachedRepository) CreateProperty(ctx context.Context, p *Property) error {
	err := c.Repository.CreateProperty(ctx, p)
	c.InvalidateAll()
	return err
}

func (c *CachedRepository) UpdateProperty(ctx context.Context, p *Property) error {
	err := c.Repository.UpdateProperty(ctx, p)
	c.InvalidateAll()
	return err
}

func (c *CachedRepository) DeleteProperty(ctx context.Context, rid string) error {
	err := c.Repository.DeleteProperty(ctx, rid)
	c.InvalidateAll()
	return err
}

// --- LinkType ---

func (c *CachedRepository) GetLinkType(ctx context.Context, rid string) (*LinkType, error) {
	c.mu.RLock()
	if e, ok := c.linkType[rid]; ok && c.entryFresh(e.expires) {
		c.mu.RUnlock()
		return e.value, nil
	}
	c.mu.RUnlock()
	v, err := c.Repository.GetLinkType(ctx, rid)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.linkType[rid] = cacheEntry[*LinkType]{value: v, expires: c.newExpiry()}
	c.mu.Unlock()
	return v, nil
}

func (c *CachedRepository) GetLinkTypeByAPIName(ctx context.Context, ontologyRID, apiName string) (*LinkType, error) {
	key := ontologyRID + "|" + apiName
	c.mu.RLock()
	if e, ok := c.linkTypeAPI[key]; ok && c.entryFresh(e.expires) {
		c.mu.RUnlock()
		return e.value, nil
	}
	c.mu.RUnlock()
	v, err := c.Repository.GetLinkTypeByAPIName(ctx, ontologyRID, apiName)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.linkTypeAPI[key] = cacheEntry[*LinkType]{value: v, expires: c.newExpiry()}
	c.mu.Unlock()
	return v, nil
}

func (c *CachedRepository) ListOutgoingLinkTypes(ctx context.Context, objectTypeRID string) ([]LinkType, error) {
	c.mu.RLock()
	if e, ok := c.outgoingLink[objectTypeRID]; ok && c.entryFresh(e.expires) {
		c.mu.RUnlock()
		return e.value, nil
	}
	c.mu.RUnlock()
	v, err := c.Repository.ListOutgoingLinkTypes(ctx, objectTypeRID)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.outgoingLink[objectTypeRID] = cacheEntry[[]LinkType]{value: v, expires: c.newExpiry()}
	c.mu.Unlock()
	return v, nil
}

func (c *CachedRepository) ListIncomingLinkTypes(ctx context.Context, objectTypeRID string) ([]LinkType, error) {
	c.mu.RLock()
	if e, ok := c.incomingLink[objectTypeRID]; ok && c.entryFresh(e.expires) {
		c.mu.RUnlock()
		return e.value, nil
	}
	c.mu.RUnlock()
	v, err := c.Repository.ListIncomingLinkTypes(ctx, objectTypeRID)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.incomingLink[objectTypeRID] = cacheEntry[[]LinkType]{value: v, expires: c.newExpiry()}
	c.mu.Unlock()
	return v, nil
}

func (c *CachedRepository) ListLinkTypes(ctx context.Context, ontologyRID string) ([]LinkType, error) {
	c.mu.RLock()
	if e, ok := c.linkTypeList[ontologyRID]; ok && c.entryFresh(e.expires) {
		c.mu.RUnlock()
		return e.value, nil
	}
	c.mu.RUnlock()
	v, err := c.Repository.ListLinkTypes(ctx, ontologyRID)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.linkTypeList[ontologyRID] = cacheEntry[[]LinkType]{value: v, expires: c.newExpiry()}
	c.mu.Unlock()
	return v, nil
}

func (c *CachedRepository) CreateLinkType(ctx context.Context, lt *LinkType) error {
	err := c.Repository.CreateLinkType(ctx, lt)
	c.InvalidateAll()
	return err
}

func (c *CachedRepository) UpdateLinkType(ctx context.Context, lt *LinkType) error {
	err := c.Repository.UpdateLinkType(ctx, lt)
	c.InvalidateAll()
	return err
}

func (c *CachedRepository) DeleteLinkType(ctx context.Context, rid string) error {
	err := c.Repository.DeleteLinkType(ctx, rid)
	c.InvalidateAll()
	return err
}

// --- ActionType ---

func (c *CachedRepository) GetActionType(ctx context.Context, rid string) (*ActionType, error) {
	c.mu.RLock()
	if e, ok := c.actionType[rid]; ok && c.entryFresh(e.expires) {
		c.mu.RUnlock()
		return e.value, nil
	}
	c.mu.RUnlock()
	v, err := c.Repository.GetActionType(ctx, rid)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.actionType[rid] = cacheEntry[*ActionType]{value: v, expires: c.newExpiry()}
	c.mu.Unlock()
	return v, nil
}

func (c *CachedRepository) GetActionTypeByAPIName(ctx context.Context, ontologyRID, apiNameOrRID string) (*ActionType, error) {
	// Cache key includes the branch scope so a branch-aware lookup never
	// returns the main row from a sibling request that warmed the cache
	// without a branch (US-384).
	key := BranchScopeFromContext(ctx) + "|" + ontologyRID + "|" + apiNameOrRID
	c.mu.RLock()
	if e, ok := c.actionTypeAPI[key]; ok && c.entryFresh(e.expires) {
		c.mu.RUnlock()
		return e.value, nil
	}
	c.mu.RUnlock()
	v, err := c.Repository.GetActionTypeByAPIName(ctx, ontologyRID, apiNameOrRID)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.actionTypeAPI[key] = cacheEntry[*ActionType]{value: v, expires: c.newExpiry()}
	c.mu.Unlock()
	return v, nil
}

func (c *CachedRepository) ListActionTypes(ctx context.Context, ontologyRID string) ([]ActionType, error) {
	key := BranchScopeFromContext(ctx) + "|" + ontologyRID
	c.mu.RLock()
	if e, ok := c.actionTypeList[key]; ok && c.entryFresh(e.expires) {
		c.mu.RUnlock()
		return e.value, nil
	}
	c.mu.RUnlock()
	v, err := c.Repository.ListActionTypes(ctx, ontologyRID)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.actionTypeList[key] = cacheEntry[[]ActionType]{value: v, expires: c.newExpiry()}
	c.mu.Unlock()
	return v, nil
}

func (c *CachedRepository) CreateActionType(ctx context.Context, at *ActionType) error {
	err := c.Repository.CreateActionType(ctx, at)
	c.InvalidateAll()
	return err
}

func (c *CachedRepository) UpdateActionType(ctx context.Context, at *ActionType) error {
	err := c.Repository.UpdateActionType(ctx, at)
	c.InvalidateAll()
	return err
}

func (c *CachedRepository) DeleteActionType(ctx context.Context, rid string) error {
	err := c.Repository.DeleteActionType(ctx, rid)
	c.InvalidateAll()
	return err
}

// --- Interface ---

func (c *CachedRepository) GetInterface(ctx context.Context, rid string) (*Interface, error) {
	c.mu.RLock()
	if e, ok := c.interfaceByRID[rid]; ok && c.entryFresh(e.expires) {
		c.mu.RUnlock()
		return e.value, nil
	}
	c.mu.RUnlock()
	v, err := c.Repository.GetInterface(ctx, rid)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.interfaceByRID[rid] = cacheEntry[*Interface]{value: v, expires: c.newExpiry()}
	c.mu.Unlock()
	return v, nil
}

func (c *CachedRepository) GetInterfaceByAPIName(ctx context.Context, ontologyRID, apiName string) (*Interface, error) {
	key := ontologyRID + "|" + apiName
	c.mu.RLock()
	if e, ok := c.interfaceAPI[key]; ok && c.entryFresh(e.expires) {
		c.mu.RUnlock()
		return e.value, nil
	}
	c.mu.RUnlock()
	v, err := c.Repository.GetInterfaceByAPIName(ctx, ontologyRID, apiName)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.interfaceAPI[key] = cacheEntry[*Interface]{value: v, expires: c.newExpiry()}
	c.mu.Unlock()
	return v, nil
}

func (c *CachedRepository) ListInterfaces(ctx context.Context, ontologyRID string) ([]Interface, error) {
	c.mu.RLock()
	if e, ok := c.interfaceList[ontologyRID]; ok && c.entryFresh(e.expires) {
		c.mu.RUnlock()
		return e.value, nil
	}
	c.mu.RUnlock()
	v, err := c.Repository.ListInterfaces(ctx, ontologyRID)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.interfaceList[ontologyRID] = cacheEntry[[]Interface]{value: v, expires: c.newExpiry()}
	c.mu.Unlock()
	return v, nil
}

func (c *CachedRepository) CreateInterface(ctx context.Context, iface *Interface) error {
	err := c.Repository.CreateInterface(ctx, iface)
	c.InvalidateAll()
	return err
}

func (c *CachedRepository) UpdateInterface(ctx context.Context, iface *Interface) error {
	err := c.Repository.UpdateInterface(ctx, iface)
	c.InvalidateAll()
	return err
}

func (c *CachedRepository) DeleteInterface(ctx context.Context, rid string) error {
	err := c.Repository.DeleteInterface(ctx, rid)
	c.InvalidateAll()
	return err
}

func (c *CachedRepository) AttachInterface(ctx context.Context, oti *ObjectTypeInterface) error {
	err := c.Repository.AttachInterface(ctx, oti)
	c.InvalidateAll()
	return err
}

func (c *CachedRepository) DetachInterface(ctx context.Context, objectTypeRID, interfaceRID string) error {
	err := c.Repository.DetachInterface(ctx, objectTypeRID, interfaceRID)
	c.InvalidateAll()
	return err
}

// --- ValueType ---

func (c *CachedRepository) GetValueTypeByAPIName(ctx context.Context, ridOrApiName string) (*ValueType, error) {
	c.mu.RLock()
	if e, ok := c.valueTypeAPI[ridOrApiName]; ok && c.entryFresh(e.expires) {
		c.mu.RUnlock()
		return e.value, nil
	}
	c.mu.RUnlock()
	v, err := c.Repository.GetValueTypeByAPIName(ctx, ridOrApiName)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.valueTypeAPI[ridOrApiName] = cacheEntry[*ValueType]{value: v, expires: c.newExpiry()}
	c.mu.Unlock()
	return v, nil
}

func (c *CachedRepository) ListValueTypes(ctx context.Context) ([]ValueType, error) {
	c.mu.RLock()
	if c.valueTypeList != nil && c.entryFresh(c.valueTypeList.expires) {
		v := c.valueTypeList.value
		c.mu.RUnlock()
		return v, nil
	}
	c.mu.RUnlock()
	v, err := c.Repository.ListValueTypes(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	e := cacheEntry[[]ValueType]{value: v, expires: c.newExpiry()}
	c.valueTypeList = &e
	c.mu.Unlock()
	return v, nil
}

func (c *CachedRepository) CreateValueType(ctx context.Context, vt *ValueType) error {
	err := c.Repository.CreateValueType(ctx, vt)
	c.InvalidateAll()
	return err
}

func (c *CachedRepository) UpdateValueType(ctx context.Context, vt *ValueType) error {
	err := c.Repository.UpdateValueType(ctx, vt)
	c.InvalidateAll()
	return err
}

func (c *CachedRepository) DeleteValueType(ctx context.Context, rid string) error {
	err := c.Repository.DeleteValueType(ctx, rid)
	c.InvalidateAll()
	return err
}
