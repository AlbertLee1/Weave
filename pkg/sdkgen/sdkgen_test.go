package sdkgen_test

import (
	"context"
	"encoding/json"
	"strings"
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

// --- TypeScript SDK structure tests (US-137) ---

func filesByPath(files []sdkgen.GeneratedFile) map[string]string {
	out := make(map[string]string, len(files))
	for _, f := range files {
		out[f.Path] = string(f.Content)
	}
	return out
}

func TestTSGenerator_PackageJSONName(t *testing.T) {
	g, _ := sdkgen.GetGenerator("ts")
	files, err := g.Generate(context.Background(), testSchema())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	by := filesByPath(files)
	pkg, ok := by["package.json"]
	if !ok {
		t.Fatal("expected package.json in output")
	}
	if !strings.Contains(pkg, `"name": "@weave/myOntology-sdk"`) {
		t.Errorf("package.json missing expected name: %s", pkg)
	}
}

func TestTSGenerator_TSConfig(t *testing.T) {
	g, _ := sdkgen.GetGenerator("ts")
	files, err := g.Generate(context.Background(), testSchema())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	by := filesByPath(files)
	if _, ok := by["tsconfig.json"]; !ok {
		t.Fatal("expected tsconfig.json in output")
	}
}

func TestTSGenerator_ObjectInterfaces(t *testing.T) {
	g, _ := sdkgen.GetGenerator("ts")
	files, err := g.Generate(context.Background(), testSchema())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	by := filesByPath(files)
	models, ok := by["src/models.ts"]
	if !ok {
		t.Fatal("expected src/models.ts in output")
	}

	wantSubstrings := []string{
		"export interface Employee {",
		"employeeId: string;",
		"firstName: string;",
		"age: number;",
		"salary: number;",
		"active: boolean;",
		"hireDate: string;",
		"tags: string[];",
		"export interface Department {",
		"departmentId: string;",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(models, s) {
			t.Errorf("models.ts missing %q", s)
		}
	}
}

func TestTSGenerator_ActionParamInterface(t *testing.T) {
	g, _ := sdkgen.GetGenerator("ts")
	files, err := g.Generate(context.Background(), testSchema())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	by := filesByPath(files)
	models := by["src/models.ts"]

	if !strings.Contains(models, "export interface CreateEmployeeParams {") {
		t.Errorf("expected CreateEmployeeParams interface, got: %s", models)
	}
	if !strings.Contains(models, "firstName: string;") {
		t.Errorf("expected required param firstName: string;")
	}
	if !strings.Contains(models, "age?: number;") {
		t.Errorf("expected optional param age?: number;")
	}
}

func TestTSGenerator_ClientPerObjectTypeRepository(t *testing.T) {
	g, _ := sdkgen.GetGenerator("ts")
	files, err := g.Generate(context.Background(), testSchema())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	by := filesByPath(files)
	client, ok := by["src/client.ts"]
	if !ok {
		t.Fatal("expected src/client.ts in output")
	}

	// Must declare a Repository class for each ObjectType.
	for _, want := range []string{
		"export class EmployeeRepository",
		"export class DepartmentRepository",
		"export class WeaveClient",
	} {
		if !strings.Contains(client, want) {
			t.Errorf("client.ts missing %q\n%s", want, client)
		}
	}

	// Each repository exposes get/list/search methods (not name-suffixed).
	for _, want := range []string{
		"async get(pk: string): Promise<Employee>",
		"async list(opts?: ListOptions): Promise<ListResult<Employee>>",
		"async search(where: Record<string, unknown>, opts?: ListOptions): Promise<ListResult<Employee>>",
		"async get(pk: string): Promise<Department>",
	} {
		if !strings.Contains(client, want) {
			t.Errorf("client.ts missing %q", want)
		}
	}

	// WeaveClient exposes per-type repos as readonly properties.
	for _, want := range []string{
		"readonly Employee: EmployeeRepository",
		"readonly Department: DepartmentRepository",
	} {
		if !strings.Contains(client, want) {
			t.Errorf("client.ts missing %q", want)
		}
	}
}

func TestTSGenerator_LinkedObjectsTraversal(t *testing.T) {
	g, _ := sdkgen.GetGenerator("ts")
	files, err := g.Generate(context.Background(), testSchema())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	client := filesByPath(files)["src/client.ts"]

	// Per AC: {objectType}.linkedObjects.{linkType}(pk) — Employee → Department
	for _, want := range []string{
		"export class EmployeeLinkedObjects",
		"readonly linkedObjects: EmployeeLinkedObjects",
		"async employeeDepartment(pk: string): Promise<{ data: Department[] }>",
	} {
		if !strings.Contains(client, want) {
			t.Errorf("client.ts missing %q\n%s", want, client)
		}
	}
}

func TestTSGenerator_ApplyActionMethod(t *testing.T) {
	g, _ := sdkgen.GetGenerator("ts")
	files, err := g.Generate(context.Background(), testSchema())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	client := filesByPath(files)["src/client.ts"]

	if !strings.Contains(client, "async applyCreateEmployee(params: CreateEmployeeParams)") {
		t.Errorf("client.ts missing apply method for createEmployee\n%s", client)
	}
}

func TestTSGenerator_IndexReExports(t *testing.T) {
	g, _ := sdkgen.GetGenerator("ts")
	files, err := g.Generate(context.Background(), testSchema())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	idx, ok := filesByPath(files)["src/index.ts"]
	if !ok {
		t.Fatal("expected src/index.ts in output")
	}
	for _, want := range []string{"export * from './models';", "export * from './client';"} {
		if !strings.Contains(idx, want) {
			t.Errorf("index.ts missing %q", want)
		}
	}
}

// --- Python SDK structure tests (US-138) ---

func TestPythonGenerator_PyprojectName(t *testing.T) {
	g, _ := sdkgen.GetGenerator("python")
	files, err := g.Generate(context.Background(), testSchema())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	by := filesByPath(files)
	proj, ok := by["pyproject.toml"]
	if !ok {
		t.Fatal("expected pyproject.toml in output")
	}
	for _, want := range []string{
		`name = "weave-myOntology-sdk"`,
		`"httpx`,
		`"pydantic`,
	} {
		if !strings.Contains(proj, want) {
			t.Errorf("pyproject.toml missing %q", want)
		}
	}
}

func TestPythonGenerator_PydanticModels(t *testing.T) {
	g, _ := sdkgen.GetGenerator("python")
	files, err := g.Generate(context.Background(), testSchema())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	by := filesByPath(files)
	models, ok := by["weave_sdk/models.py"]
	if !ok {
		t.Fatal("expected weave_sdk/models.py in output")
	}
	for _, want := range []string{
		"from pydantic import BaseModel",
		"class Employee(BaseModel):",
		"employeeId: str",
		"firstName: str",
		"age: int",
		"salary: float",
		"active: bool",
		"hireDate: datetime.date",
		"tags: list[str]",
		"class Department(BaseModel):",
		"departmentId: str",
	} {
		if !strings.Contains(models, want) {
			t.Errorf("models.py missing %q", want)
		}
	}
}

func TestPythonGenerator_ActionParamModel(t *testing.T) {
	g, _ := sdkgen.GetGenerator("python")
	files, err := g.Generate(context.Background(), testSchema())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	by := filesByPath(files)
	models := by["weave_sdk/models.py"]

	for _, want := range []string{
		"class CreateEmployeeParams(BaseModel):",
		"firstName: str",
		"age: Optional[int]",
	} {
		if !strings.Contains(models, want) {
			t.Errorf("models.py missing %q\n%s", want, models)
		}
	}
}

func TestPythonGenerator_ClientMethods(t *testing.T) {
	g, _ := sdkgen.GetGenerator("python")
	files, err := g.Generate(context.Background(), testSchema())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	by := filesByPath(files)
	client, ok := by["weave_sdk/client.py"]
	if !ok {
		t.Fatal("expected weave_sdk/client.py in output")
	}
	for _, want := range []string{
		"import httpx",
		"class WeaveClient:",
		"def get_employee(self, pk: str) -> Employee:",
		"def list_employee(",
		"def search_employee(",
		"def get_department(self, pk: str) -> Department:",
		"def apply_create_employee(self, params: CreateEmployeeParams)",
	} {
		if !strings.Contains(client, want) {
			t.Errorf("client.py missing %q", want)
		}
	}
}

func TestPythonGenerator_InitExports(t *testing.T) {
	g, _ := sdkgen.GetGenerator("python")
	files, err := g.Generate(context.Background(), testSchema())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	by := filesByPath(files)
	init, ok := by["weave_sdk/__init__.py"]
	if !ok {
		t.Fatal("expected weave_sdk/__init__.py in output")
	}
	for _, want := range []string{
		"from .client import WeaveClient",
		"from .models import (",
		"Employee,",
		"Department,",
		"CreateEmployeeParams,",
		"__all__",
	} {
		if !strings.Contains(init, want) {
			t.Errorf("__init__.py missing %q", want)
		}
	}
}

func TestPythonGenerator_PyTypedMarker(t *testing.T) {
	g, _ := sdkgen.GetGenerator("python")
	files, err := g.Generate(context.Background(), testSchema())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	by := filesByPath(files)
	if _, ok := by["weave_sdk/py.typed"]; !ok {
		t.Error("expected weave_sdk/py.typed marker file for PEP 561 compliance")
	}
}
