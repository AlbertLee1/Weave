package objectset_test

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// mockLinkResolver for testing
type mockLinkResolver struct {
	results map[string][]string // linkAPIName -> target PKs
}

func (m *mockLinkResolver) ResolveLinked(ctx context.Context, linkTypeKey string, sourcePKs []string, dir links.Direction) ([]string, error) {
	return m.results[linkTypeKey], nil
}

func (m *mockLinkResolver) ResolveLinkedObjects(ctx context.Context, linkTypeRID string, sourcePKs []string) ([]string, error) {
	return nil, nil
}

func (m *mockLinkResolver) ResolveLinkedObjectsByAPIName(ctx context.Context, sourceOTAPIName, linkAPIName string, sourcePKs []string) ([]string, error) {
	return m.results[linkAPIName], nil
}

func setupExecutorTest(t *testing.T) (*objectset.Executor, *index.Manager) {
	t.Helper()
	dir := t.TempDir()
	mgr := index.NewManager(dir)

	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
		{APIName: "age", BaseType: "integer", IsSearchable: true},
		{APIName: "dept", BaseType: "string", IsSearchable: true},
	}
	_, err := mgr.EnsureIndex("employee", props)
	if err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	// Seed test data
	docs := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"e1", map[string]interface{}{"id": "e1", "name": "alice", "age": float64(30), "dept": "eng"}},
		{"e2", map[string]interface{}{"id": "e2", "name": "bob", "age": float64(25), "dept": "eng"}},
		{"e3", map[string]interface{}{"id": "e3", "name": "charlie", "age": float64(35), "dept": "sales"}},
		{"e4", map[string]interface{}{"id": "e4", "name": "diana", "age": float64(28), "dept": "hr"}},
	}
	for _, d := range docs {
		if err := mgr.IndexDocument("employee", d.id, d.doc); err != nil {
			t.Fatalf("IndexDocument %s: %v", d.id, err)
		}
	}

	linkResolver := &mockLinkResolver{
		results: map[string][]string{
			"employeeDept": {"d1", "d2"},
		},
	}

	store := objectset.NewStore(1 * time.Hour)
	executor := objectset.NewExecutor(mgr, linkResolver, store)
	t.Cleanup(func() { mgr.Close() })
	return executor, mgr
}

// sorted returns a sorted copy of the slice for deterministic comparison.
func sorted(pks []string) []string {
	out := make([]string, len(pks))
	copy(out, pks)
	sort.Strings(out)
	return out
}

// --- Definition Validation Tests (4) ---

func TestDefinition_ValidateBase_Valid(t *testing.T) {
	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	if err := def.Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestDefinition_ValidateBase_MissingObjectType(t *testing.T) {
	def := &objectset.Definition{Type: "base"}
	err := def.Validate()
	if err == nil {
		t.Fatal("expected error for missing objectType")
	}
}

func TestDefinition_ValidateFilter_MissingWhere(t *testing.T) {
	def := &objectset.Definition{
		Type:      "filter",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
	}
	err := def.Validate()
	if err == nil {
		t.Fatal("expected error for missing where clause")
	}
}

func TestDefinition_ValidateUnknown(t *testing.T) {
	def := &objectset.Definition{Type: "bogus"}
	err := def.Validate()
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

// --- ParseDefinition Tests (2) ---

func TestParseDefinition_Base(t *testing.T) {
	data := []byte(`{"type":"base","objectType":"employee"}`)
	def, err := objectset.ParseDefinition(data)
	if err != nil {
		t.Fatalf("ParseDefinition: %v", err)
	}
	if def.Type != "base" {
		t.Errorf("expected type base, got %s", def.Type)
	}
	if def.ObjectType != "employee" {
		t.Errorf("expected objectType employee, got %s", def.ObjectType)
	}
}

func TestParseDefinition_Filter(t *testing.T) {
	data := []byte(`{
		"type": "filter",
		"objectSet": {"type":"base","objectType":"employee"},
		"where": {"type":"eq","field":"name","value":"alice"}
	}`)
	def, err := objectset.ParseDefinition(data)
	if err != nil {
		t.Fatalf("ParseDefinition: %v", err)
	}
	if def.Type != "filter" {
		t.Errorf("expected type filter, got %s", def.Type)
	}
	if def.ObjectSet == nil {
		t.Fatal("expected nested objectSet")
	}
	if def.ObjectSet.Type != "base" {
		t.Errorf("expected nested type base, got %s", def.ObjectSet.Type)
	}
}

// --- Execute Base Tests (2) ---

func TestExecute_Base_AllObjects(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.ObjectType != "employee" {
		t.Errorf("expected objectType employee, got %s", result.ObjectType)
	}

	pks := sorted(result.PrimaryKeys)
	expected := []string{"e1", "e2", "e3", "e4"}
	if len(pks) != len(expected) {
		t.Fatalf("expected %d PKs, got %d: %v", len(expected), len(pks), pks)
	}
	for i, pk := range pks {
		if pk != expected[i] {
			t.Errorf("PK[%d]: expected %s, got %s", i, expected[i], pk)
		}
	}
}

func TestExecute_Base_EmptyIndex(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
	}
	_, err := mgr.EnsureIndex("empty_type", props)
	if err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	store := objectset.NewStore(1 * time.Hour)
	executor := objectset.NewExecutor(mgr, nil, store)
	ctx := context.Background()

	def := &objectset.Definition{Type: "base", ObjectType: "empty_type"}
	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.PrimaryKeys) != 0 {
		t.Errorf("expected 0 PKs, got %d", len(result.PrimaryKeys))
	}
}

// --- Execute Filter Tests (3) ---

func TestExecute_Filter_EqString(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	whereJSON := json.RawMessage(`{"type":"eq","field":"name","value":"alice"}`)
	def := &objectset.Definition{
		Type:      "filter",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Where:     whereJSON,
	}

	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.PrimaryKeys) != 1 {
		t.Fatalf("expected 1 PK, got %d: %v", len(result.PrimaryKeys), result.PrimaryKeys)
	}
	if result.PrimaryKeys[0] != "e1" {
		t.Errorf("expected e1, got %s", result.PrimaryKeys[0])
	}
}

func TestExecute_Filter_GtNumber(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	whereJSON := json.RawMessage(`{"type":"gt","field":"age","value":30}`)
	def := &objectset.Definition{
		Type:      "filter",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Where:     whereJSON,
	}

	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Only charlie (age 35) is > 30
	if len(result.PrimaryKeys) != 1 {
		t.Fatalf("expected 1 PK, got %d: %v", len(result.PrimaryKeys), result.PrimaryKeys)
	}
	if result.PrimaryKeys[0] != "e3" {
		t.Errorf("expected e3, got %s", result.PrimaryKeys[0])
	}
}

func TestExecute_Filter_And(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	// Filter: dept == "eng" AND age > 24 -> alice(30) and bob(25)
	whereJSON := json.RawMessage(`{
		"type":"and",
		"value":[
			{"type":"eq","field":"dept","value":"eng"},
			{"type":"gt","field":"age","value":24}
		]
	}`)
	def := &objectset.Definition{
		Type:      "filter",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Where:     whereJSON,
	}

	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	pks := sorted(result.PrimaryKeys)
	expected := []string{"e1", "e2"}
	if len(pks) != len(expected) {
		t.Fatalf("expected %d PKs, got %d: %v", len(expected), len(pks), pks)
	}
	for i, pk := range pks {
		if pk != expected[i] {
			t.Errorf("PK[%d]: expected %s, got %s", i, expected[i], pk)
		}
	}
}

// --- Execute Union Tests (2) ---

func TestExecute_Union(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	// Union of: name=alice, name=bob
	def := &objectset.Definition{
		Type: "union",
		ObjectSets: []*objectset.Definition{
			{
				Type:      "filter",
				ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
				Where:     json.RawMessage(`{"type":"eq","field":"name","value":"alice"}`),
			},
			{
				Type:      "filter",
				ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
				Where:     json.RawMessage(`{"type":"eq","field":"name","value":"bob"}`),
			},
		},
	}

	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	pks := sorted(result.PrimaryKeys)
	expected := []string{"e1", "e2"}
	if len(pks) != len(expected) {
		t.Fatalf("expected %d PKs, got %d: %v", len(expected), len(pks), pks)
	}
	for i, pk := range pks {
		if pk != expected[i] {
			t.Errorf("PK[%d]: expected %s, got %s", i, expected[i], pk)
		}
	}
}

func TestExecute_Union_Dedup(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	// Both sub-sets include dept=eng (alice, bob). Union should deduplicate.
	def := &objectset.Definition{
		Type: "union",
		ObjectSets: []*objectset.Definition{
			{
				Type:      "filter",
				ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
				Where:     json.RawMessage(`{"type":"eq","field":"dept","value":"eng"}`),
			},
			{
				Type:      "filter",
				ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
				Where:     json.RawMessage(`{"type":"eq","field":"name","value":"alice"}`),
			},
		},
	}

	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// alice is in both sets but should only appear once. Total: alice, bob
	pks := sorted(result.PrimaryKeys)
	expected := []string{"e1", "e2"}
	if len(pks) != len(expected) {
		t.Fatalf("expected %d PKs, got %d: %v", len(expected), len(pks), pks)
	}
}

// --- Execute Intersect Tests (2) ---

func TestExecute_Intersect(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	// Intersect: dept=eng (alice, bob) AND age>26 (alice(30), charlie(35), diana(28))
	// -> alice only
	def := &objectset.Definition{
		Type: "intersect",
		ObjectSets: []*objectset.Definition{
			{
				Type:      "filter",
				ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
				Where:     json.RawMessage(`{"type":"eq","field":"dept","value":"eng"}`),
			},
			{
				Type:      "filter",
				ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
				Where:     json.RawMessage(`{"type":"gt","field":"age","value":26}`),
			},
		},
	}

	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	pks := sorted(result.PrimaryKeys)
	// alice (e1) is eng and age 30 > 26; bob (e2) is eng but age 25 not > 26
	// diana (e4) is hr, age 28 > 26 but not eng
	// charlie (e3) is sales, age 35 > 26 but not eng
	expected := []string{"e1"}
	if len(pks) != len(expected) {
		t.Fatalf("expected %d PKs, got %d: %v", len(expected), len(pks), pks)
	}
	if pks[0] != "e1" {
		t.Errorf("expected e1, got %s", pks[0])
	}
}

func TestExecute_Intersect_NoOverlap(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	// Intersect: dept=eng (alice, bob) AND dept=sales (charlie)
	// -> empty
	def := &objectset.Definition{
		Type: "intersect",
		ObjectSets: []*objectset.Definition{
			{
				Type:      "filter",
				ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
				Where:     json.RawMessage(`{"type":"eq","field":"dept","value":"eng"}`),
			},
			{
				Type:      "filter",
				ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
				Where:     json.RawMessage(`{"type":"eq","field":"dept","value":"sales"}`),
			},
		},
	}

	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.PrimaryKeys) != 0 {
		t.Errorf("expected 0 PKs, got %d: %v", len(result.PrimaryKeys), result.PrimaryKeys)
	}
}

// --- Execute Subtract Tests (2) ---

func TestExecute_Subtract(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	// Subtract: all employees minus dept=eng (alice, bob)
	// -> charlie, diana
	def := &objectset.Definition{
		Type: "subtract",
		ObjectSets: []*objectset.Definition{
			{Type: "base", ObjectType: "employee"},
			{
				Type:      "filter",
				ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
				Where:     json.RawMessage(`{"type":"eq","field":"dept","value":"eng"}`),
			},
		},
	}

	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	pks := sorted(result.PrimaryKeys)
	expected := []string{"e3", "e4"}
	if len(pks) != len(expected) {
		t.Fatalf("expected %d PKs, got %d: %v", len(expected), len(pks), pks)
	}
	for i, pk := range pks {
		if pk != expected[i] {
			t.Errorf("PK[%d]: expected %s, got %s", i, expected[i], pk)
		}
	}
}

func TestExecute_Subtract_All(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	// Subtract all from all -> empty
	def := &objectset.Definition{
		Type: "subtract",
		ObjectSets: []*objectset.Definition{
			{Type: "base", ObjectType: "employee"},
			{Type: "base", ObjectType: "employee"},
		},
	}

	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.PrimaryKeys) != 0 {
		t.Errorf("expected 0 PKs, got %d: %v", len(result.PrimaryKeys), result.PrimaryKeys)
	}
}

// --- Execute SearchAround Test (1) ---

func TestExecute_SearchAround(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Link:      "employeeDept",
	}

	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	pks := sorted(result.PrimaryKeys)
	expected := []string{"d1", "d2"}
	if len(pks) != len(expected) {
		t.Fatalf("expected %d PKs, got %d: %v", len(expected), len(pks), pks)
	}
	for i, pk := range pks {
		if pk != expected[i] {
			t.Errorf("PK[%d]: expected %s, got %s", i, expected[i], pk)
		}
	}
}

func TestExecute_SearchAround_ObjectType(t *testing.T) {
	// The searchAround result should have a non-empty ObjectType from the link resolver.
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
	}
	_, err := mgr.EnsureIndex("employee", props)
	if err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if err := mgr.IndexDocument("employee", "e1", map[string]interface{}{"id": "e1", "name": "alice"}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	linkResolver := &mockLinkResolverWithType{
		results:    map[string][]string{"employeeDept": {"d1"}},
		targetType: map[string]string{"employeeDept": "department"},
	}

	store := objectset.NewStore(1 * time.Hour)
	executor := objectset.NewExecutor(mgr, linkResolver, store)

	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Link:      "employeeDept",
	}

	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result.ObjectType != "department" {
		t.Errorf("expected ObjectType 'department', got %q", result.ObjectType)
	}
}

// mockLinkResolverWithType implements LinkResolver and LinkTargetTypeResolver.
type mockLinkResolverWithType struct {
	results    map[string][]string // linkAPIName -> target PKs
	targetType map[string]string   // linkAPIName -> target object type API name
}

func (m *mockLinkResolverWithType) ResolveLinkedObjects(ctx context.Context, linkTypeRID string, sourcePKs []string) ([]string, error) {
	return nil, nil
}

func (m *mockLinkResolverWithType) ResolveLinkedObjectsByAPIName(ctx context.Context, sourceOTAPIName, linkAPIName string, sourcePKs []string) ([]string, error) {
	return m.results[linkAPIName], nil
}

func (m *mockLinkResolverWithType) ResolveLinked(ctx context.Context, linkTypeKey string, sourcePKs []string, dir links.Direction) ([]string, error) {
	return m.results[linkTypeKey], nil
}

func (m *mockLinkResolverWithType) ResolveTargetObjectType(ctx context.Context, sourceObjectType, linkTypeAPIName string) (string, error) {
	if tt, ok := m.targetType[linkTypeAPIName]; ok {
		return tt, nil
	}
	return "", nil
}

// --- Store Tests (3) ---

func TestStore_PutGet(t *testing.T) {
	store := objectset.NewStore(1 * time.Hour)
	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	id := store.Put(def)

	got, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Type != "base" || got.ObjectType != "employee" {
		t.Errorf("unexpected definition: %+v", got)
	}
}

func TestStore_GetNotFound(t *testing.T) {
	store := objectset.NewStore(1 * time.Hour)
	_, err := store.Get("nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent ID")
	}
}

func TestStore_Expired(t *testing.T) {
	store := objectset.NewStore(1 * time.Millisecond)
	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	id := store.Put(def)

	time.Sleep(5 * time.Millisecond)

	_, err := store.Get(id)
	if err == nil {
		t.Fatal("expected error for expired entry")
	}
}

// --- Store Cleanup Test (1) ---

func TestStore_Cleanup(t *testing.T) {
	store := objectset.NewStore(1 * time.Millisecond)
	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	store.Put(def)
	store.Put(def)

	if store.Count() != 2 {
		t.Fatalf("expected 2 entries, got %d", store.Count())
	}

	time.Sleep(5 * time.Millisecond)
	store.Cleanup()

	if store.Count() != 0 {
		t.Errorf("expected 0 entries after cleanup, got %d", store.Count())
	}
}

func TestStore_BackgroundCleanup(t *testing.T) {
	// Store should automatically clean up expired entries via background goroutine.
	store := objectset.NewStoreWithCleanup(10*time.Millisecond, 20*time.Millisecond)
	defer store.Stop()

	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	store.Put(def)
	store.Put(def)

	if store.Count() != 2 {
		t.Fatalf("expected 2 entries, got %d", store.Count())
	}

	// Wait for entries to expire and cleanup goroutine to run.
	time.Sleep(100 * time.Millisecond)

	if store.Count() != 0 {
		t.Errorf("expected 0 entries after background cleanup, got %d", store.Count())
	}
}

func TestStore_Stop(t *testing.T) {
	// Stop should be safe to call multiple times.
	store := objectset.NewStoreWithCleanup(1*time.Hour, 50*time.Millisecond)
	store.Stop()
	store.Stop() // should not panic
}

// --- Execute Reference Test (1) ---

func TestExecute_Reference(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	// Store a base definition and execute via reference
	store := objectset.NewStore(1 * time.Hour)
	baseDef := &objectset.Definition{Type: "base", ObjectType: "employee"}
	refID := store.Put(baseDef)

	// Create a new executor with this store
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	// Re-use the existing executor's index by creating a fresh setup
	// Actually, let's just use the executor from setup — but we need to use its store
	// So we'll create the reference def and pass it to the existing executor
	// The existing executor has its own store, so let's store in that store instead.
	_ = executor // use the executor from setup

	// Use the store from the setup executor by putting the def in it
	// Since we can't access the internal store, let's create a standalone test
	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
	}
	_, err := mgr.EnsureIndex("employee", props)
	if err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if err := mgr.IndexDocument("employee", "e1", map[string]interface{}{"id": "e1", "name": "alice"}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	refExecutor := objectset.NewExecutor(mgr, nil, store)

	refDef := &objectset.Definition{Type: "reference", Reference: refID}
	result, err := refExecutor.Execute(ctx, refDef)
	if err != nil {
		t.Fatalf("Execute reference: %v", err)
	}
	if len(result.PrimaryKeys) != 1 {
		t.Fatalf("expected 1 PK, got %d: %v", len(result.PrimaryKeys), result.PrimaryKeys)
	}
	if result.PrimaryKeys[0] != "e1" {
		t.Errorf("expected e1, got %s", result.PrimaryKeys[0])
	}
}

// --- NearestNeighbors Tests (2) ---

func TestDefinition_ValidateNearestNeighbors_Valid(t *testing.T) {
	def := &objectset.Definition{
		Type:      "nearestNeighbors",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		PropertyIdentifier: &objectset.PropertyIdentifier{
			Property: struct {
				APIName string `json:"apiName"`
			}{APIName: "embedding"},
		},
	}
	if err := def.Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestExecute_NearestNeighbors_NotSupported(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	def := &objectset.Definition{
		Type:      "nearestNeighbors",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		PropertyIdentifier: &objectset.PropertyIdentifier{
			Property: struct {
				APIName string `json:"apiName"`
			}{APIName: "embedding"},
		},
	}

	_, err := executor.Execute(ctx, def)
	if err == nil {
		t.Fatal("expected error for nearestNeighbors, got nil")
	}
	if !strings.Contains(err.Error(), "not yet supported") {
		t.Errorf("expected 'not yet supported' error, got: %v", err)
	}
}

// --- WithProperties Tests (2) ---

func TestDefinition_ValidateWithProperties_Valid(t *testing.T) {
	def := &objectset.Definition{
		Type:       "withProperties",
		ObjectSet:  &objectset.Definition{Type: "base", ObjectType: "employee"},
		Properties: []string{"name", "age"},
	}
	if err := def.Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestExecute_WithProperties_DelegatesToInnerObjectSet(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	def := &objectset.Definition{
		Type:       "withProperties",
		ObjectSet:  &objectset.Definition{Type: "base", ObjectType: "employee"},
		Properties: []string{"name", "age"},
	}

	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// withProperties should delegate to inner objectSet and return all 4 employees
	if result.ObjectType != "employee" {
		t.Errorf("expected objectType employee, got %s", result.ObjectType)
	}
	pks := sorted(result.PrimaryKeys)
	expected := []string{"e1", "e2", "e3", "e4"}
	if len(pks) != len(expected) {
		t.Fatalf("expected %d PKs, got %d: %v", len(expected), len(pks), pks)
	}
	for i, pk := range pks {
		if pk != expected[i] {
			t.Errorf("PK[%d]: expected %s, got %s", i, expected[i], pk)
		}
	}
}

// --- Truncation marker tests (Fix 4) ---

// TestExecute_Base_NotTruncated verifies the Truncated flag is false when the
// base ObjectSet query returns fewer rows than the executor's hard cap.
func TestExecute_Base_NotTruncated(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Truncated {
		t.Errorf("expected Truncated=false for small result, got true")
	}
}

// TestExecute_Base_Truncated verifies that when the base query hits the
// executor's hard cap of 10000 rows the Truncated/Approximate flag is set so
// callers can warn the user that the result is partial.
func TestExecute_Base_Truncated(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping truncation test in short mode (seeds 10001 docs)")
	}

	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("bigtype", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	// Seed > 10000 docs in batches via ApplyBatch — single-doc IndexDocument
	// is too slow because it forces a fsync per call.
	const total = 10001
	const batchSize = 500
	ops := make([]index.BatchOp, 0, batchSize)
	for i := 0; i < total; i++ {
		id := "obj-" + strconv.Itoa(i)
		ops = append(ops, index.BatchOp{
			Type:       index.BatchOpIndex,
			PrimaryKey: id,
			Document: map[string]interface{}{
				"id":   id,
				"name": "name-" + strconv.Itoa(i),
			},
		})
		if len(ops) == batchSize {
			if err := mgr.ApplyBatch("bigtype", ops); err != nil {
				t.Fatalf("ApplyBatch %d: %v", i, err)
			}
			ops = ops[:0]
		}
	}
	if len(ops) > 0 {
		if err := mgr.ApplyBatch("bigtype", ops); err != nil {
			t.Fatalf("ApplyBatch tail: %v", err)
		}
	}

	store := objectset.NewStore(1 * time.Hour)
	executor := objectset.NewExecutor(mgr, nil, store)
	def := &objectset.Definition{Type: "base", ObjectType: "bigtype"}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Truncated {
		t.Errorf("expected Truncated=true when result hits the 10000 cap, got false (returned %d PKs)", len(result.PrimaryKeys))
	}
	if len(result.PrimaryKeys) != 10000 {
		t.Errorf("expected exactly 10000 PKs at the cap, got %d", len(result.PrimaryKeys))
	}
}
