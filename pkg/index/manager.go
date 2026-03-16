package index

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
)

// Manager manages Bleve indexes for object types.
type Manager struct {
	mu      sync.RWMutex
	indexes map[string]bleve.Index // objectTypeApiName -> index
	dataDir string
}

// NewManager creates a new index manager.
func NewManager(dataDir string) *Manager {
	return &Manager{
		indexes: make(map[string]bleve.Index),
		dataDir: dataDir,
	}
}

// Property describes a property for index mapping purposes.
type Property struct {
	APIName      string
	BaseType     string
	IsSearchable bool
	IsArray      bool
}

// EnsureIndex creates or opens an index for the given object type.
// properties defines the field mappings based on the type's schema.
func (m *Manager) EnsureIndex(objectType string, properties []Property) (bleve.Index, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if idx, ok := m.indexes[objectType]; ok {
		return idx, nil
	}

	indexPath := filepath.Join(m.dataDir, "indexes", objectType)

	// Try to open existing index first
	idx, err := bleve.Open(indexPath)
	if err == nil {
		m.indexes[objectType] = idx
		return idx, nil
	}

	// Create new index with mapping
	indexMapping := m.buildMapping(properties)
	idx, err = bleve.New(indexPath, indexMapping)
	if err != nil {
		return nil, fmt.Errorf("create index %q: %w", objectType, err)
	}

	m.indexes[objectType] = idx
	return idx, nil
}

// GetIndex returns the index for the given object type, or nil if not loaded.
func (m *Manager) GetIndex(objectType string) bleve.Index {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.indexes[objectType]
}

// DropIndex closes and removes the index for the given object type.
func (m *Manager) DropIndex(objectType string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx, ok := m.indexes[objectType]
	if !ok {
		return nil
	}

	if err := idx.Close(); err != nil {
		return err
	}
	delete(m.indexes, objectType)

	indexPath := filepath.Join(m.dataDir, "indexes", objectType)
	return os.RemoveAll(indexPath)
}

// Close closes all open indexes.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var lastErr error
	for name, idx := range m.indexes {
		if err := idx.Close(); err != nil {
			lastErr = err
		}
		delete(m.indexes, name)
	}
	return lastErr
}

// IndexDocument indexes a single document in the specified object type's index.
func (m *Manager) IndexDocument(objectType, docID string, doc map[string]interface{}) error {
	m.mu.RLock()
	idx, ok := m.indexes[objectType]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("index not found for object type %q", objectType)
	}
	return idx.Index(docID, doc)
}

// DeleteDocument removes a document from the specified object type's index.
func (m *Manager) DeleteDocument(objectType, docID string) error {
	m.mu.RLock()
	idx, ok := m.indexes[objectType]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("index not found for object type %q", objectType)
	}
	return idx.Delete(docID)
}

// Search executes a search on the specified object type's index.
func (m *Manager) Search(objectType string, req *bleve.SearchRequest) (*bleve.SearchResult, error) {
	m.mu.RLock()
	idx, ok := m.indexes[objectType]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("index not found for object type %q", objectType)
	}
	return idx.Search(req)
}

// DocCount returns the number of documents in the specified index.
func (m *Manager) DocCount(objectType string) (uint64, error) {
	m.mu.RLock()
	idx, ok := m.indexes[objectType]
	m.mu.RUnlock()

	if !ok {
		return 0, fmt.Errorf("index not found for object type %q", objectType)
	}
	return idx.DocCount()
}

func (m *Manager) buildMapping(properties []Property) mapping.IndexMapping {
	indexMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()

	for _, prop := range properties {
		fm := FieldMappingForBaseType(prop.BaseType, prop.IsSearchable)
		docMapping.AddFieldMappingsAt(prop.APIName, fm)
	}

	indexMapping.DefaultMapping = docMapping
	return indexMapping
}
