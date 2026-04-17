package sdkgen_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/liyang/weave/pkg/sdkgen"
	"github.com/liyang/weave/pkg/types"
)

// --- helpers ---

func testSchema() sdkgen.OntologySchema {
	return sdkgen.OntologySchema{
		Ontology: sdkgen.OntologyMeta{
			RID:         "ri.ontology.main.ontology.1",
			APIName:     "myOntology",
			DisplayName: "My Ontology",
			Version:     3,
		},
		ObjectTypes: []sdkgen.ObjectTypeSchema{
			{
				RID:         "ri.ontology.main.objectType.1",
				APIName:     "Employee",
				DisplayName: "Employee",
				PrimaryKey:  "employeeId",
				Properties: []sdkgen.PropertySchema{
					{APIName: "employeeId", BaseType: "string"},
					{APIName: "firstName", BaseType: "string"},
					{APIName: "age", BaseType: "integer"},
					{APIName: "salary", BaseType: "double"},
					{APIName: "active", BaseType: "boolean"},
					{APIName: "hireDate", BaseType: "date"},
					{APIName: "tags", BaseType: "string", IsArray: true},
				},
			},
			{
				RID:         "ri.ontology.main.objectType.2",
				APIName:     "Department",
				DisplayName: "Department",
				PrimaryKey:  "departmentId",
				Properties: []sdkgen.PropertySchema{
					{APIName: "departmentId", BaseType: "string"},
					{APIName: "name", BaseType: "string"},
				},
			},
		},
		LinkTypes: []sdkgen.LinkTypeSchema{
			{
				APIName:          "employeeDepartment",
				SourceObjectType: "Employee",
				TargetObjectType: "Department",
				Cardinality:      "MANY_TO_ONE",
			},
		},
		ActionTypes: []sdkgen.ActionTypeSchema{
			{
				APIName:     "createEmployee",
				DisplayName: "Create Employee",
				Parameters: []sdkgen.ActionParamSchema{
					{ID: "firstName", BaseType: "string", Required: true},
					{ID: "age", BaseType: "integer", Required: false},
				},
			},
		},
		Interfaces: []sdkgen.InterfaceSchema{
			{APIName: "HasName", DisplayName: "Has Name"},
		},
	}
}

// --- OntologySchema tests ---

func TestOntologySchema_Structure(t *testing.T) {
	schema := testSchema()

	if schema.Ontology.APIName != "myOntology" {
		t.Errorf("expected ontology apiName 'myOntology', got %q", schema.Ontology.APIName)
	}
	if len(schema.ObjectTypes) != 2 {
		t.Errorf("expected 2 object types, got %d", len(schema.ObjectTypes))
	}
	if len(schema.LinkTypes) != 1 {
		t.Errorf("expected 1 link type, got %d", len(schema.LinkTypes))
	}
	if len(schema.ActionTypes) != 1 {
		t.Errorf("expected 1 action type, got %d", len(schema.ActionTypes))
	}
	if len(schema.Interfaces) != 1 {
		t.Errorf("expected 1 interface, got %d", len(schema.Interfaces))
	}
}

func TestOntologySchema_ObjectTypeProperties(t *testing.T) {
	schema := testSchema()

	emp := schema.ObjectTypes[0]
	if emp.APIName != "Employee" {
		t.Fatalf("expected first objectType to be Employee, got %q", emp.APIName)
	}
	if len(emp.Properties) != 7 {
		t.Errorf("expected 7 properties on Employee, got %d", len(emp.Properties))
	}
	if emp.PrimaryKey != "employeeId" {
		t.Errorf("expected primaryKey 'employeeId', got %q", emp.PrimaryKey)
	}
}

func TestParseActionParameters(t *testing.T) {
	raw := json.RawMessage(`[{"id":"firstName","type":"string","required":true},{"id":"age","type":"integer","required":false}]`)
	params := sdkgen.ParseActionParameters(raw)

	if len(params) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(params))
	}
	if params[0].ID != "firstName" || params[0].BaseType != "string" || !params[0].Required {
		t.Errorf("unexpected first param: %+v", params[0])
	}
	if params[1].ID != "age" || params[1].BaseType != "integer" || params[1].Required {
		t.Errorf("unexpected second param: %+v", params[1])
	}
}

func TestParseActionParameters_Empty(t *testing.T) {
	cases := []json.RawMessage{
		nil,
		json.RawMessage("null"),
		json.RawMessage("[]"),
	}
	for _, raw := range cases {
		params := sdkgen.ParseActionParameters(raw)
		if len(params) != 0 {
			t.Errorf("expected 0 params for %q, got %d", string(raw), len(params))
		}
	}
}

// --- Type mapping tests ---

func TestTypeMapForLanguage(t *testing.T) {
	tests := []struct {
		lang string
		ok   bool
	}{
		{"ts", true},
		{"python", true},
		{"go", true},
		{"java", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			m, err := sdkgen.TypeMapForLanguage(tt.lang)
			if tt.ok {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if m == nil {
					t.Fatal("expected non-nil map")
				}
			} else {
				if err == nil {
					t.Fatal("expected error for unsupported language")
				}
			}
		})
	}
}

func TestTypeMapForLanguage_AllBaseTypes(t *testing.T) {
	allTypes := []types.BaseType{
		types.String, types.Integer, types.Short, types.Long,
		types.Float, types.Double, types.Boolean, types.Byte,
		types.Date, types.Timestamp, types.Decimal,
		types.Geopoint, types.Geoshape, types.Attachment,
		types.TimeSeries, types.MediaReference, types.Marking, types.Cipher,
	}

	langs := []string{"ts", "python", "go"}
	for _, lang := range langs {
		t.Run(lang, func(t *testing.T) {
			m, err := sdkgen.TypeMapForLanguage(lang)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, bt := range allTypes {
				mapped := m[bt]
				if mapped == "" {
					t.Errorf("base type %q not mapped for lang %q", bt, lang)
				}
			}
		})
	}
}

func TestTypeMapForLanguage_TSMappings(t *testing.T) {
	m, _ := sdkgen.TypeMapForLanguage("ts")

	checks := map[types.BaseType]string{
		types.String:  "string",
		types.Integer: "number",
		types.Boolean: "boolean",
		types.Long:    "string",
		types.Double:  "number",
		types.Date:    "string",
	}
	for bt, expected := range checks {
		if got := m[bt]; got != expected {
			t.Errorf("TS mapping for %q: expected %q, got %q", bt, expected, got)
		}
	}
}

func TestTypeMapForLanguage_PythonMappings(t *testing.T) {
	m, _ := sdkgen.TypeMapForLanguage("python")

	checks := map[types.BaseType]string{
		types.String:  "str",
		types.Integer: "int",
		types.Boolean: "bool",
		types.Double:  "float",
	}
	for bt, expected := range checks {
		if got := m[bt]; got != expected {
			t.Errorf("Python mapping for %q: expected %q, got %q", bt, expected, got)
		}
	}
}

func TestTypeMapForLanguage_GoMappings(t *testing.T) {
	m, _ := sdkgen.TypeMapForLanguage("go")

	checks := map[types.BaseType]string{
		types.String:  "string",
		types.Integer: "int32",
		types.Long:    "int64",
		types.Boolean: "bool",
		types.Double:  "float64",
	}
	for bt, expected := range checks {
		if got := m[bt]; got != expected {
			t.Errorf("Go mapping for %q: expected %q, got %q", bt, expected, got)
		}
	}
}

// --- Generator interface tests ---

func TestGeneratorRegistry(t *testing.T) {
	langs := []string{"ts", "python", "go"}
	for _, lang := range langs {
		t.Run(lang, func(t *testing.T) {
			g, err := sdkgen.GetGenerator(lang)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if g == nil {
				t.Fatal("expected non-nil generator")
			}
		})
	}
}

func TestGeneratorRegistry_Unknown(t *testing.T) {
	_, err := sdkgen.GetGenerator("java")
	if err == nil {
		t.Fatal("expected error for unsupported language")
	}
}

func TestGenerator_GenerateReturnsFiles(t *testing.T) {
	schema := testSchema()

	langs := []string{"ts", "python", "go"}
	for _, lang := range langs {
		t.Run(lang, func(t *testing.T) {
			g, err := sdkgen.GetGenerator(lang)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			files, err := g.Generate(context.Background(), schema)
			if err != nil {
				t.Fatalf("Generate failed: %v", err)
			}
			if len(files) == 0 {
				t.Fatal("expected at least one generated file")
			}

			for _, f := range files {
				if f.Path == "" {
					t.Error("generated file has empty path")
				}
				if len(f.Content) == 0 {
					t.Errorf("generated file %q has empty content", f.Path)
				}
			}
		})
	}
}
