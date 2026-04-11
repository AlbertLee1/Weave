package objectset_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// ---------- static ----------

func TestDefinition_ValidateStatic_Valid(t *testing.T) {
	def := &objectset.Definition{
		Type:        "static",
		ObjectType:  "employee",
		PrimaryKeys: []string{"e1", "e2"},
	}
	if err := def.Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestDefinition_ValidateStatic_MissingObjectType(t *testing.T) {
	def := &objectset.Definition{
		Type:        "static",
		PrimaryKeys: []string{"e1"},
	}
	if err := def.Validate(); err == nil {
		t.Fatal("expected error for missing objectType")
	}
}

func TestParseDefinition_Static(t *testing.T) {
	data := []byte(`{"type":"static","objectType":"employee","primaryKeys":["e1","e3"]}`)
	def, err := objectset.ParseDefinition(data)
	if err != nil {
		t.Fatalf("ParseDefinition: %v", err)
	}
	if def.Type != "static" || def.ObjectType != "employee" {
		t.Errorf("unexpected definition: %+v", def)
	}
	if len(def.PrimaryKeys) != 2 || def.PrimaryKeys[0] != "e1" || def.PrimaryKeys[1] != "e3" {
		t.Errorf("unexpected PrimaryKeys: %v", def.PrimaryKeys)
	}
}

func TestExecute_Static(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	def := &objectset.Definition{
		Type:        "static",
		ObjectType:  "employee",
		PrimaryKeys: []string{"e1", "e3"},
	}
	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.ObjectType != "employee" {
		t.Errorf("expected ObjectType 'employee', got %q", result.ObjectType)
	}
	pks := sorted(result.PrimaryKeys)
	expected := []string{"e1", "e3"}
	if len(pks) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, pks)
	}
	for i, pk := range pks {
		if pk != expected[i] {
			t.Errorf("PK[%d]: expected %s, got %s", i, expected[i], pk)
		}
	}
}

func TestExecute_Static_Empty(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	def := &objectset.Definition{
		Type:        "static",
		ObjectType:  "employee",
		PrimaryKeys: []string{},
	}
	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.PrimaryKeys) != 0 {
		t.Errorf("expected 0 PKs, got %d", len(result.PrimaryKeys))
	}
}

// ---------- asType ----------

func TestDefinition_ValidateAsType_Valid(t *testing.T) {
	def := &objectset.Definition{
		Type:       "asType",
		ObjectType: "person",
		ObjectSet:  &objectset.Definition{Type: "base", ObjectType: "employee"},
	}
	if err := def.Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestDefinition_ValidateAsType_MissingObjectType(t *testing.T) {
	def := &objectset.Definition{
		Type:      "asType",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
	}
	if err := def.Validate(); err == nil {
		t.Fatal("expected error for missing objectType")
	}
}

func TestDefinition_ValidateAsType_MissingObjectSet(t *testing.T) {
	def := &objectset.Definition{
		Type:       "asType",
		ObjectType: "person",
	}
	if err := def.Validate(); err == nil {
		t.Fatal("expected error for missing objectSet")
	}
}

func TestExecute_AsType(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	def := &objectset.Definition{
		Type:       "asType",
		ObjectType: "person",
		ObjectSet:  &objectset.Definition{Type: "base", ObjectType: "employee"},
	}
	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.ObjectType != "person" {
		t.Errorf("expected ObjectType 'person' (relabeled), got %q", result.ObjectType)
	}
	// PKs should match the inner base (4 employees)
	if len(result.PrimaryKeys) != 4 {
		t.Errorf("expected 4 PKs, got %d", len(result.PrimaryKeys))
	}
}

// ---------- asBaseObjectTypes ----------

func TestDefinition_ValidateAsBaseObjectTypes_Valid(t *testing.T) {
	def := &objectset.Definition{
		Type:      "asBaseObjectTypes",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
	}
	if err := def.Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestDefinition_ValidateAsBaseObjectTypes_MissingObjectSet(t *testing.T) {
	def := &objectset.Definition{Type: "asBaseObjectTypes"}
	if err := def.Validate(); err == nil {
		t.Fatal("expected error for missing objectSet")
	}
}

func TestExecute_AsBaseObjectTypes(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	def := &objectset.Definition{
		Type:      "asBaseObjectTypes",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
	}
	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// asBaseObjectTypes is a pass-through in the single-type execution model.
	if result.ObjectType != "employee" {
		t.Errorf("expected ObjectType 'employee', got %q", result.ObjectType)
	}
	if len(result.PrimaryKeys) != 4 {
		t.Errorf("expected 4 PKs, got %d", len(result.PrimaryKeys))
	}
}

// ---------- interfaceBase ----------

func TestDefinition_ValidateInterfaceBase_Valid(t *testing.T) {
	def := &objectset.Definition{
		Type:          "interfaceBase",
		InterfaceType: "Named",
	}
	if err := def.Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestDefinition_ValidateInterfaceBase_MissingInterfaceType(t *testing.T) {
	def := &objectset.Definition{Type: "interfaceBase"}
	if err := def.Validate(); err == nil {
		t.Fatal("expected error for missing interfaceType")
	}
}

func TestExecute_InterfaceBase_NoResolver(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	def := &objectset.Definition{
		Type:          "interfaceBase",
		InterfaceType: "Named",
	}
	_, err := executor.Execute(ctx, def)
	if err == nil {
		t.Fatal("expected error when interface resolver is not configured")
	}
	if !strings.Contains(err.Error(), "interface resolver") {
		t.Errorf("expected 'interface resolver' error, got: %v", err)
	}
}

func TestExecute_InterfaceBase_WithResolver(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("employee", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if _, err := mgr.EnsureIndex("contractor", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if err := mgr.IndexDocument("employee", "e1", map[string]interface{}{"id": "e1", "name": "alice"}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}
	if err := mgr.IndexDocument("contractor", "c1", map[string]interface{}{"id": "c1", "name": "bob"}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	store := objectset.NewStore(1 * time.Hour)
	executor := objectset.NewExecutor(mgr, nil, store)
	executor.SetInterfaceResolver(&fakeInterfaceResolver{
		types: map[string][]string{"Named": {"employee", "contractor"}},
	})

	def := &objectset.Definition{Type: "interfaceBase", InterfaceType: "Named"}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	pks := sorted(result.PrimaryKeys)
	expected := []string{"c1", "e1"}
	if len(pks) != len(expected) || pks[0] != expected[0] || pks[1] != expected[1] {
		t.Errorf("expected %v, got %v", expected, pks)
	}
}

type fakeInterfaceResolver struct {
	types map[string][]string
}

func (f *fakeInterfaceResolver) ResolveInterfaceObjectTypes(ctx context.Context, interfaceAPIName string) ([]string, error) {
	return f.types[interfaceAPIName], nil
}

// ---------- interfaceLinkSearchAround ----------

func TestDefinition_ValidateInterfaceLinkSearchAround_Valid(t *testing.T) {
	def := &objectset.Definition{
		Type:          "interfaceLinkSearchAround",
		ObjectSet:     &objectset.Definition{Type: "base", ObjectType: "employee"},
		InterfaceLink: "worksFor",
	}
	if err := def.Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestDefinition_ValidateInterfaceLinkSearchAround_MissingObjectSet(t *testing.T) {
	def := &objectset.Definition{
		Type:          "interfaceLinkSearchAround",
		InterfaceLink: "worksFor",
	}
	if err := def.Validate(); err == nil {
		t.Fatal("expected error for missing objectSet")
	}
}

func TestDefinition_ValidateInterfaceLinkSearchAround_MissingInterfaceLink(t *testing.T) {
	def := &objectset.Definition{
		Type:      "interfaceLinkSearchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
	}
	if err := def.Validate(); err == nil {
		t.Fatal("expected error for missing interfaceLink")
	}
}

func TestExecute_InterfaceLinkSearchAround_DelegatesToLinkResolver(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	def := &objectset.Definition{
		Type:          "interfaceLinkSearchAround",
		ObjectSet:     &objectset.Definition{Type: "base", ObjectType: "employee"},
		InterfaceLink: "employeeDept",
	}
	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	pks := sorted(result.PrimaryKeys)
	expected := []string{"d1", "d2"}
	if len(pks) != len(expected) || pks[0] != expected[0] || pks[1] != expected[1] {
		t.Errorf("expected %v, got %v", expected, pks)
	}
}

// ---------- methodInput ----------

func TestDefinition_ValidateMethodInput_Valid(t *testing.T) {
	def := &objectset.Definition{
		Type:  "methodInput",
		Input: "parameterName",
	}
	if err := def.Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestDefinition_ValidateMethodInput_MissingInput(t *testing.T) {
	def := &objectset.Definition{Type: "methodInput"}
	if err := def.Validate(); err == nil {
		t.Fatal("expected error for missing input")
	}
}

func TestExecute_MethodInput_NotSupported(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	def := &objectset.Definition{Type: "methodInput", Input: "x"}
	_, err := executor.Execute(ctx, def)
	if err == nil {
		t.Fatal("expected error for methodInput, got nil")
	}
	if !strings.Contains(err.Error(), "methodInput") {
		t.Errorf("expected 'methodInput' error, got: %v", err)
	}
}

// ---------- Parse round-trip ----------

func TestParseDefinition_US028_AllVariants(t *testing.T) {
	cases := map[string]string{
		"static":                    `{"type":"static","objectType":"employee","primaryKeys":["e1"]}`,
		"asType":                    `{"type":"asType","objectType":"person","objectSet":{"type":"base","objectType":"employee"}}`,
		"asBaseObjectTypes":         `{"type":"asBaseObjectTypes","objectSet":{"type":"base","objectType":"employee"}}`,
		"interfaceBase":             `{"type":"interfaceBase","interfaceType":"Named"}`,
		"interfaceLinkSearchAround": `{"type":"interfaceLinkSearchAround","objectSet":{"type":"base","objectType":"employee"},"interfaceLink":"worksFor"}`,
		"methodInput":               `{"type":"methodInput","input":"paramName"}`,
	}
	for name, data := range cases {
		def, err := objectset.ParseDefinition([]byte(data))
		if err != nil {
			t.Fatalf("%s: ParseDefinition: %v", name, err)
		}
		if def.Type != name {
			t.Errorf("%s: got type %q", name, def.Type)
		}
		// Round-trip serialize and verify the type field is preserved.
		out, err := json.Marshal(def)
		if err != nil {
			t.Fatalf("%s: Marshal: %v", name, err)
		}
		if !strings.Contains(string(out), `"type":"`+name+`"`) {
			t.Errorf("%s: round-trip output missing type field: %s", name, string(out))
		}
	}
}
