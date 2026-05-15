package graphsvc

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/liyang/weave/pkg/rid"
)

// MemRepo is an in-memory Repo for tests and degraded-mode boots. It mirrors
// PGRepo's semantics: Create seeds v=1 history (when versioned=true), Update
// bumps version + writes history, UpdateLayout patches positions without
// touching version.
type MemRepo struct {
	mu       sync.Mutex
	graphs   map[string]*Graph
	versions map[string][]GraphVersion
}

// NewMemRepo returns an empty MemRepo.
func NewMemRepo() *MemRepo {
	return &MemRepo{
		graphs:   map[string]*Graph{},
		versions: map[string][]GraphVersion{},
	}
}

func (r *MemRepo) Create(ctx context.Context, ontologyRID, name, createdBy string, payload json.RawMessage, versioned bool) (*Graph, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	if len(payload) == 0 {
		payload = json.RawMessage(`{"layers":[],"edges":[]}`)
	}
	clone := append(json.RawMessage(nil), payload...)
	g := &Graph{
		RID:         rid.New("vertex", "main", "graph"),
		OntologyRID: ontologyRID,
		Name:        name,
		Version:     1,
		Versioned:   versioned,
		Payload:     clone,
		CreatedBy:   createdBy,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	r.graphs[g.RID] = g
	if versioned {
		r.versions[g.RID] = []GraphVersion{{
			GraphRID:  g.RID,
			Version:   1,
			Payload:   append(json.RawMessage(nil), clone...),
			CreatedAt: now,
		}}
	}
	return g, nil
}

func (r *MemRepo) Get(ctx context.Context, ridStr string) (*Graph, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, ok := r.graphs[ridStr]
	if !ok {
		return nil, ErrGraphNotFound
	}
	return cloneGraph(g), nil
}

func (r *MemRepo) Update(ctx context.Context, ridStr string, payload json.RawMessage) (*Graph, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, ok := r.graphs[ridStr]
	if !ok {
		return nil, ErrGraphNotFound
	}
	g.Version++
	g.Payload = append(json.RawMessage(nil), payload...)
	g.UpdatedAt = time.Now().UTC()
	if g.Versioned {
		r.versions[ridStr] = append(r.versions[ridStr], GraphVersion{
			GraphRID:  ridStr,
			Version:   g.Version,
			Payload:   append(json.RawMessage(nil), g.Payload...),
			CreatedAt: g.UpdatedAt,
		})
	}
	return cloneGraph(g), nil
}

func (r *MemRepo) UpdateLayout(ctx context.Context, ridStr string, positions json.RawMessage) error {
	if len(positions) == 0 {
		return ErrInvalidPositions
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	g, ok := r.graphs[ridStr]
	if !ok {
		return ErrGraphNotFound
	}
	// VTX-024 — merge per-node positions into payload.positions. A drag
	// fires PATCH with just the dragged node's coords; the request must
	// NOT clobber sibling positions, so we read the existing map and
	// overwrite key-by-key. Falls back to a wholesale replacement when
	// the existing payload is opaque / not an object (degraded fixtures).
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(g.Payload, &obj); err != nil {
		obj = map[string]json.RawMessage{}
	}
	obj["positions"] = mergePositionsBytes(obj["positions"], positions)
	merged, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	g.Payload = merged
	g.UpdatedAt = time.Now().UTC()
	return nil
}

// mergePositionsBytes returns a JSON object whose keys are the union of
// existing and patch; values from patch override matching keys in existing.
// Either argument may be empty/non-object; falls through to the patch alone.
func mergePositionsBytes(existing, patch json.RawMessage) json.RawMessage {
	var existingMap map[string]json.RawMessage
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &existingMap)
	}
	var patchMap map[string]json.RawMessage
	if err := json.Unmarshal(patch, &patchMap); err != nil {
		return append(json.RawMessage(nil), patch...)
	}
	if existingMap == nil {
		existingMap = map[string]json.RawMessage{}
	}
	for k, v := range patchMap {
		existingMap[k] = append(json.RawMessage(nil), v...)
	}
	out, err := json.Marshal(existingMap)
	if err != nil {
		return append(json.RawMessage(nil), patch...)
	}
	return out
}

func (r *MemRepo) Duplicate(ctx context.Context, sourceRID string) (*Graph, error) {
	r.mu.Lock()
	src, ok := r.graphs[sourceRID]
	if !ok {
		r.mu.Unlock()
		return nil, ErrGraphNotFound
	}
	srcCopy := cloneGraph(src)
	r.mu.Unlock()
	return r.Create(ctx, srcCopy.OntologyRID, srcCopy.Name+" (copy)", srcCopy.CreatedBy,
		srcCopy.Payload, srcCopy.Versioned)
}

func (r *MemRepo) GetVersion(ctx context.Context, ridStr string, version int) (*Graph, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, ok := r.graphs[ridStr]
	if !ok {
		return nil, ErrGraphNotFound
	}
	for _, v := range r.versions[ridStr] {
		if v.Version == version {
			out := cloneGraph(g)
			out.Version = v.Version
			out.Payload = append(json.RawMessage(nil), v.Payload...)
			out.UpdatedAt = v.CreatedAt
			return out, nil
		}
	}
	return nil, ErrVersionNotFound
}

func (r *MemRepo) ListVersions(ctx context.Context, ridStr string) ([]GraphVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	src := r.versions[ridStr]
	out := make([]GraphVersion, len(src))
	copy(out, src)
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

func cloneGraph(g *Graph) *Graph {
	c := *g
	c.Payload = append(json.RawMessage(nil), g.Payload...)
	return &c
}

// MemTemplateStore is an in-memory TemplateStore for tests.
type MemTemplateStore struct {
	mu sync.Mutex
	m  map[string]*GraphTemplate
}

// NewMemTemplateStore returns an empty MemTemplateStore.
func NewMemTemplateStore() *MemTemplateStore {
	return &MemTemplateStore{m: map[string]*GraphTemplate{}}
}

func (s *MemTemplateStore) Create(ctx context.Context, t *GraphTemplate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[t.RID] = t
	return nil
}

func (s *MemTemplateStore) Get(ctx context.Context, ridStr string) (*GraphTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.m[ridStr]
	if !ok {
		return nil, ErrTemplateNotFound
	}
	return t, nil
}

// Count returns the number of stored templates (for test assertions).
func (s *MemTemplateStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.m)
}

var _ Repo = (*MemRepo)(nil)
var _ TemplateStore = (*MemTemplateStore)(nil)
