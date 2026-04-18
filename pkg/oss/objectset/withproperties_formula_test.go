package objectset_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// setupPersonExecutor stages three people with first/last names and a salary so
// we can exercise string-concat and arithmetic formulas in withProperties
// (US-201).
func setupPersonExecutor(t *testing.T) (*objectset.Executor, *index.Manager) {
	t.Helper()
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "firstName", BaseType: "string", IsSearchable: true},
		{APIName: "lastName", BaseType: "string", IsSearchable: true},
		{APIName: "salary", BaseType: "double", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("person", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	people := []struct {
		id, first, last string
		salary          float64
	}{
		{"p1", "Ada", "Lovelace", 100.0},
		{"p2", "Grace", "Hopper", 250.0},
		{"p3", "Alan", "Turing", 500.0},
	}
	for _, p := range people {
		if err := mgr.IndexDocument("person", p.id, map[string]interface{}{
			"id":        p.id,
			"firstName": p.first,
			"lastName":  p.last,
			"salary":    p.salary,
		}); err != nil {
			t.Fatalf("IndexDocument %s: %v", p.id, err)
		}
	}

	resolver := &perPKLinkResolver{}
	store := objectset.NewStore(time.Hour)
	return objectset.NewExecutor(mgr, resolver, store), mgr
}

// TestWithPropertiesFormula_FullName drives the PRD's canonical example:
// define fullName = this.firstName + ' ' + this.lastName and verify each
// base object resolves to the expected concatenation (US-201).
func TestWithPropertiesFormula_FullName(t *testing.T) {
	exec, _ := setupPersonExecutor(t)

	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "person"},
		DerivedProperties: []objectset.DerivedPropertyDef{
			{
				Name:    "fullName",
				Metric:  "formula",
				Formula: "this.firstName + ' ' + this.lastName",
			},
		},
	}

	result, err := exec.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.DerivedValues == nil {
		t.Fatalf("expected DerivedValues populated")
	}
	expected := map[string]string{
		"p1": "Ada Lovelace",
		"p2": "Grace Hopper",
		"p3": "Alan Turing",
	}
	for pk, want := range expected {
		raw, ok := result.DerivedValues[pk]["fullName"]
		if !ok {
			t.Errorf("fullName missing for %s", pk)
			continue
		}
		got, ok := raw.(string)
		if !ok {
			t.Errorf("%s fullName: want string, got %T (%v)", pk, raw, raw)
			continue
		}
		if got != want {
			t.Errorf("%s fullName: got %q, want %q", pk, got, want)
		}
	}
}

// TestWithPropertiesFormula_Arithmetic verifies numeric expressions work and
// round-trip through the JS VM as float64 — matching the Bleve coercion path
// already exercised by sum/avg/min/max.
func TestWithPropertiesFormula_Arithmetic(t *testing.T) {
	exec, _ := setupPersonExecutor(t)
	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "person"},
		DerivedProperties: []objectset.DerivedPropertyDef{
			{Name: "doubled", Metric: "formula", Formula: "this.salary * 2"},
		},
	}
	result, err := exec.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	expected := map[string]float64{
		"p1": 200,
		"p2": 500,
		"p3": 1000,
	}
	for pk, want := range expected {
		got, ok := toFloat64(result.DerivedValues[pk]["doubled"])
		if !ok {
			t.Errorf("%s doubled: want numeric, got %T (%v)", pk, result.DerivedValues[pk]["doubled"], result.DerivedValues[pk]["doubled"])
			continue
		}
		if got != want {
			t.Errorf("%s doubled: got %v, want %v", pk, got, want)
		}
	}
}

// toFloat64 accepts either int64 or float64 — Goja exports whole-number JS
// arithmetic results as int64 and fractional ones as float64, and either is a
// legitimate wire representation of "numeric derived value".
func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	}
	return 0, false
}

// TestWithPropertiesFormula_EmptyBaseSet asserts an empty inner ObjectSet
// short-circuits cleanly — no base-fields query, non-nil DerivedValues.
func TestWithPropertiesFormula_EmptyBaseSet(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })
	if _, err := mgr.EnsureIndex("person", []index.Property{{APIName: "id", BaseType: "string", IsSearchable: true}}); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	exec := objectset.NewExecutor(mgr, &perPKLinkResolver{}, objectset.NewStore(time.Hour))

	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "person"},
		DerivedProperties: []objectset.DerivedPropertyDef{
			{Name: "fullName", Metric: "formula", Formula: "this.firstName + this.lastName"},
		},
	}
	result, err := exec.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.PrimaryKeys) != 0 {
		t.Errorf("expected 0 rows, got %d", len(result.PrimaryKeys))
	}
	if result.DerivedValues == nil {
		t.Errorf("expected non-nil DerivedValues")
	}
}

// TestWithPropertiesFormula_MultipleDPs demonstrates two formula DPs over one
// base load and verifies both land on every row.
func TestWithPropertiesFormula_MultipleDPs(t *testing.T) {
	exec, _ := setupPersonExecutor(t)
	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "person"},
		DerivedProperties: []objectset.DerivedPropertyDef{
			{Name: "fullName", Metric: "formula", Formula: "this.firstName + ' ' + this.lastName"},
			{Name: "initial", Metric: "formula", Formula: "this.firstName.charAt(0)"},
		},
	}
	result, err := exec.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.DerivedValues["p1"]["fullName"] != "Ada Lovelace" {
		t.Errorf("p1 fullName: got %v", result.DerivedValues["p1"]["fullName"])
	}
	if result.DerivedValues["p1"]["initial"] != "A" {
		t.Errorf("p1 initial: got %v", result.DerivedValues["p1"]["initial"])
	}
}

// TestWithPropertiesFormula_ValidationMissingFormula rejects a metric=formula
// DP that forgets the formula source — otherwise the compile step would fail
// with a less clear error deeper in execution.
func TestWithPropertiesFormula_ValidationMissingFormula(t *testing.T) {
	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "person"},
		DerivedProperties: []objectset.DerivedPropertyDef{
			{Name: "fullName", Metric: "formula"},
		},
	}
	if err := def.Validate(); err == nil {
		t.Fatal("expected validation error for missing formula")
	}
}

// TestWithPropertiesFormula_ValidationCompileError surfaces syntactically
// invalid JS as an error at Execute time — the evaluator's ErrCompile is
// wrapped into the executor's return so callers get a pointed message.
func TestWithPropertiesFormula_ValidationCompileError(t *testing.T) {
	exec, _ := setupPersonExecutor(t)
	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "person"},
		DerivedProperties: []objectset.DerivedPropertyDef{
			{Name: "bad", Metric: "formula", Formula: "((("},
		},
	}
	_, err := exec.Execute(context.Background(), def)
	if err == nil {
		t.Fatal("expected compile error for invalid JS")
	}
	if !strings.Contains(err.Error(), "compile") {
		t.Errorf("expected compile-error wording, got %v", err)
	}
}

// TestWithPropertiesFormula_FormulaWithoutMetric lets the caller omit metric
// and infer it from the presence of a formula. Either metric="formula" OR
// supplying a non-empty Formula should be sufficient.
func TestWithPropertiesFormula_FormulaWithoutMetric(t *testing.T) {
	exec, _ := setupPersonExecutor(t)
	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "person"},
		DerivedProperties: []objectset.DerivedPropertyDef{
			{Name: "fullName", Formula: "this.firstName + ' ' + this.lastName"},
		},
	}
	result, err := exec.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.DerivedValues["p1"]["fullName"] != "Ada Lovelace" {
		t.Errorf("fullName: got %v", result.DerivedValues["p1"]["fullName"])
	}
}

// TestLoadObjects_DerivedFormula drives the PRD's E2E acceptance criterion:
// POST a loadObjectSet request with a withProperties + formula DP and verify
// the returned data carries the computed value as a top-level property.
func TestLoadObjects_DerivedFormula(t *testing.T) {
	exec, mgr := setupPersonExecutor(t)
	store := objectset.NewStore(time.Hour)
	h := objectset.NewHandler(exec, mgr, store)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", h.LoadObjects)

	body := map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type": "withProperties",
			"objectSet": map[string]interface{}{
				"type":       "base",
				"objectType": "person",
			},
			"derivedProperties": []map[string]interface{}{
				{
					"name":    "fullName",
					"metric":  "formula",
					"formula": "this.firstName + ' ' + this.lastName",
				},
			},
		},
		"select": []string{"id", "firstName", "lastName", "fullName"},
	}
	raw, _ := json.Marshal(body)

	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/test/objectSets/loadObjects",
		bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	// V2 wire flattens property fields to the row's top level alongside
	// __primaryKey / __apiName, so we decode rows as plain maps.
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, rr.Body.String())
	}
	if len(resp.Data) == 0 {
		t.Fatalf("no rows returned: %s", rr.Body.String())
	}
	gotFullNames := map[string]string{}
	for _, row := range resp.Data {
		id, _ := row["id"].(string)
		full, _ := row["fullName"].(string)
		gotFullNames[id] = full
	}
	for id, want := range map[string]string{
		"p1": "Ada Lovelace",
		"p2": "Grace Hopper",
		"p3": "Alan Turing",
	} {
		if gotFullNames[id] != want {
			t.Errorf("%s fullName: got %q, want %q", id, gotFullNames[id], want)
		}
	}
}

// TestWithPropertiesFormula_HundredObjects keeps the batch-load path honest
// against larger inner ObjectSets so we don't regress into a per-row Bleve
// query.
func TestWithPropertiesFormula_HundredObjects(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })
	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "value", BaseType: "double", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("item", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("i%03d", i)
		if err := mgr.IndexDocument("item", id, map[string]interface{}{
			"id":    id,
			"value": float64(i),
		}); err != nil {
			t.Fatalf("IndexDocument %s: %v", id, err)
		}
	}
	exec := objectset.NewExecutor(mgr, &perPKLinkResolver{}, objectset.NewStore(time.Hour))
	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "item"},
		DerivedProperties: []objectset.DerivedPropertyDef{
			{Name: "plusOne", Metric: "formula", Formula: "this.value + 1"},
		},
	}
	result, err := exec.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.PrimaryKeys) != 100 {
		t.Fatalf("expected 100 rows, got %d", len(result.PrimaryKeys))
	}
	for _, pk := range result.PrimaryKeys {
		raw := result.DerivedValues[pk]["plusOne"]
		if _, ok := toFloat64(raw); !ok {
			t.Errorf("%s plusOne: want numeric, got %T (%v)", pk, raw, raw)
		}
	}
}
