package fixtures

import (
	"encoding/json"
	"regexp"
	"strconv"
	"testing"
	"time"
)

func ptrInt(v int) *int       { return &v }
func ptrFloat(v float64) *any { x := any(v); return &x }
func fptr(v float64) *float64 { return &v }

func TestGenerate_RespectsCount(t *testing.T) {
	props := []PropertyDef{
		{APIName: "id", BaseType: "long", IsPrimary: true},
		{APIName: "name", BaseType: "string"},
	}
	rows, err := Generate(props, 25, Options{Seed: 1})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(rows) != 25 {
		t.Fatalf("got %d rows, want 25", len(rows))
	}
}

func TestGenerate_PrimaryKeysAreUnique(t *testing.T) {
	props := []PropertyDef{
		{APIName: "id", BaseType: "string", IsPrimary: true, MinLength: ptrInt(3), MaxLength: ptrInt(8)},
	}
	rows, err := Generate(props, 200, Options{Seed: 42})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	seen := map[string]struct{}{}
	for _, r := range rows {
		v, ok := r["id"].(string)
		if !ok {
			t.Fatalf("id is not string: %T", r["id"])
		}
		if _, dup := seen[v]; dup {
			t.Fatalf("duplicate primary key: %q", v)
		}
		seen[v] = struct{}{}
	}
}

func TestGenerate_PrimaryKeyLong_Sequential(t *testing.T) {
	props := []PropertyDef{
		{APIName: "id", BaseType: "long", IsPrimary: true, Min: fptr(1000), Max: fptr(2000)},
	}
	rows, err := Generate(props, 5, Options{Seed: 7})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	want := []int64{1000, 1001, 1002, 1003, 1004}
	for i, r := range rows {
		got, ok := r["id"].(int64)
		if !ok {
			t.Fatalf("row %d id type = %T", i, r["id"])
		}
		if got != want[i] {
			t.Errorf("row %d id = %d, want %d", i, got, want[i])
		}
	}
}

func TestGenerate_RegexConstraintRespected(t *testing.T) {
	pattern := "^[A-Z]{3}[0-9]{2}$"
	props := []PropertyDef{
		{APIName: "code", BaseType: "string", Regex: pattern, MinLength: ptrInt(5), MaxLength: ptrInt(5)},
	}
	rows, err := Generate(props, 30, Options{Seed: 11})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	re := regexp.MustCompile(pattern)
	for i, r := range rows {
		v, _ := r["code"].(string)
		if !re.MatchString(v) {
			t.Errorf("row %d code %q does not match %s", i, v, pattern)
		}
	}
}

func TestGenerate_NumericMinMaxRespected(t *testing.T) {
	props := []PropertyDef{
		{APIName: "age", BaseType: "integer", Min: fptr(18), Max: fptr(65)},
		{APIName: "score", BaseType: "double", Min: fptr(0), Max: fptr(1)},
	}
	rows, err := Generate(props, 100, Options{Seed: 123})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for i, r := range rows {
		age, ok := r["age"].(int)
		if !ok {
			t.Fatalf("row %d age type = %T", i, r["age"])
		}
		if age < 18 || age > 65 {
			t.Errorf("row %d age = %d out of range", i, age)
		}
		score, _ := r["score"].(float64)
		if score < 0 || score > 1 {
			t.Errorf("row %d score = %v out of range", i, score)
		}
	}
}

func TestGenerate_EnumPicksAllowedValues(t *testing.T) {
	props := []PropertyDef{
		{APIName: "status", BaseType: "string", Enum: []any{"ACTIVE", "DEPRECATED", "EXPERIMENTAL"}},
	}
	rows, err := Generate(props, 50, Options{Seed: 5})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	allowed := map[string]bool{"ACTIVE": true, "DEPRECATED": true, "EXPERIMENTAL": true}
	for i, r := range rows {
		v, _ := r["status"].(string)
		if !allowed[v] {
			t.Errorf("row %d status %q not in enum", i, v)
		}
	}
}

func TestGenerate_StringLengthBounds(t *testing.T) {
	props := []PropertyDef{
		{APIName: "name", BaseType: "string", MinLength: ptrInt(5), MaxLength: ptrInt(10)},
	}
	rows, err := Generate(props, 50, Options{Seed: 99})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for i, r := range rows {
		v, _ := r["name"].(string)
		if len(v) < 5 || len(v) > 10 {
			t.Errorf("row %d name %q length %d out of [5,10]", i, v, len(v))
		}
	}
}

func TestGenerate_ArrayProperty(t *testing.T) {
	props := []PropertyDef{
		{APIName: "tags", BaseType: "string", IsArray: true},
	}
	rows, err := Generate(props, 10, Options{Seed: 1})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for i, r := range rows {
		v, ok := r["tags"].([]any)
		if !ok {
			t.Fatalf("row %d tags is not []any: %T", i, r["tags"])
		}
		if len(v) < 1 || len(v) > 3 {
			t.Errorf("row %d array length %d outside [1,3]", i, len(v))
		}
	}
}

func TestGenerate_NullableEmitsNulls(t *testing.T) {
	props := []PropertyDef{
		{APIName: "maybe", BaseType: "string", IsNullable: true},
	}
	rows, err := Generate(props, 200, Options{Seed: 31, NullRatio: 0.5})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	nulls := 0
	for _, r := range rows {
		if r["maybe"] == nil {
			nulls++
		}
	}
	if nulls == 0 || nulls == len(rows) {
		t.Errorf("expected mix of nulls and values, got %d/%d nulls", nulls, len(rows))
	}
}

func TestGenerate_PrimaryNeverEmitsNullEvenIfNullable(t *testing.T) {
	props := []PropertyDef{
		{APIName: "id", BaseType: "string", IsPrimary: true, IsNullable: true},
	}
	rows, err := Generate(props, 50, Options{Seed: 1, NullRatio: 1.0})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for i, r := range rows {
		if r["id"] == nil {
			t.Fatalf("row %d primary key emitted as null", i)
		}
	}
}

func TestGenerate_DateAndTimestampParse(t *testing.T) {
	props := []PropertyDef{
		{APIName: "born", BaseType: "date"},
		{APIName: "created", BaseType: "timestamp"},
	}
	rows, err := Generate(props, 5, Options{Seed: 13})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for i, r := range rows {
		if _, err := time.Parse("2006-01-02", r["born"].(string)); err != nil {
			t.Errorf("row %d date %q: %v", i, r["born"], err)
		}
		if _, err := time.Parse(time.RFC3339, r["created"].(string)); err != nil {
			t.Errorf("row %d timestamp %q: %v", i, r["created"], err)
		}
	}
}

func TestGenerate_BooleanType(t *testing.T) {
	props := []PropertyDef{{APIName: "active", BaseType: "boolean"}}
	rows, err := Generate(props, 10, Options{Seed: 1})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for i, r := range rows {
		if _, ok := r["active"].(bool); !ok {
			t.Errorf("row %d active is not bool: %T", i, r["active"])
		}
	}
}

func TestGenerate_DeterministicWithSeed(t *testing.T) {
	props := []PropertyDef{
		{APIName: "id", BaseType: "long", IsPrimary: true},
		{APIName: "name", BaseType: "string", MinLength: ptrInt(4), MaxLength: ptrInt(10)},
		{APIName: "score", BaseType: "double", Min: fptr(0), Max: fptr(100)},
	}
	a, _ := Generate(props, 20, Options{Seed: 4242})
	b, _ := Generate(props, 20, Options{Seed: 4242})
	bufA, _ := json.Marshal(a)
	bufB, _ := json.Marshal(b)
	if string(bufA) != string(bufB) {
		t.Fatalf("same seed produced different rows:\nA=%s\nB=%s", bufA, bufB)
	}
}

func TestGenerate_DifferentSeedsProduceDifferentRows(t *testing.T) {
	props := []PropertyDef{
		{APIName: "name", BaseType: "string"},
	}
	a, _ := Generate(props, 10, Options{Seed: 1})
	b, _ := Generate(props, 10, Options{Seed: 2})
	bufA, _ := json.Marshal(a)
	bufB, _ := json.Marshal(b)
	if string(bufA) == string(bufB) {
		t.Fatalf("seeds 1 and 2 produced identical rows")
	}
}

func TestGenerate_NegativeCountErrors(t *testing.T) {
	if _, err := Generate(nil, -1, Options{}); err == nil {
		t.Fatal("expected error for negative count")
	}
}

func TestGenerate_BadNullRatioErrors(t *testing.T) {
	if _, err := Generate(nil, 1, Options{NullRatio: 1.5}); err == nil {
		t.Fatal("expected error for bad nullRatio")
	}
}

func TestGenerate_RegexUnsatisfiableErrors(t *testing.T) {
	props := []PropertyDef{
		{APIName: "code", BaseType: "string", Regex: "^XYZ-[0-9]{8}-[A-F]{4}$", MinLength: ptrInt(1), MaxLength: ptrInt(2)},
	}
	if _, err := Generate(props, 1, Options{Seed: 1}); err == nil {
		t.Fatal("expected error for unsatisfiable regex / length combo")
	}
}

func TestPropertyDefsFromWire_BasicShape(t *testing.T) {
	wire := map[string]any{
		"name": map[string]any{
			"dataType": map[string]any{
				"type":      "string",
				"minLength": float64(2),
				"maxLength": float64(20),
			},
		},
		"age": map[string]any{
			"dataType": map[string]any{
				"type": "integer",
				"min":  float64(0),
				"max":  float64(150),
			},
		},
		"tags": map[string]any{
			"dataType": map[string]any{
				"type":    "array",
				"subType": map[string]any{"type": "string"},
			},
		},
	}
	defs, err := PropertyDefsFromWire(wire, []string{"name"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	byName := map[string]PropertyDef{}
	for _, d := range defs {
		byName[d.APIName] = d
	}
	if !byName["name"].IsPrimary {
		t.Error("expected name to be primary key")
	}
	if got := *byName["name"].MinLength; got != 2 {
		t.Errorf("name minLength = %d", got)
	}
	if !byName["tags"].IsArray || byName["tags"].BaseType != "string" {
		t.Errorf("tags = %+v", byName["tags"])
	}
	if got := *byName["age"].Max; got != 150 {
		t.Errorf("age max = %v", got)
	}
}

func TestPropertyDefsFromWire_EnumPropagated(t *testing.T) {
	wire := map[string]any{
		"status": map[string]any{
			"dataType": map[string]any{
				"type": "string",
				"enum": []any{"A", "B", "C"},
			},
		},
	}
	defs, err := PropertyDefsFromWire(wire, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(defs) != 1 || len(defs[0].Enum) != 3 {
		t.Fatalf("enum not propagated: %+v", defs)
	}
}

func TestPropertyDefsFromWire_RejectsNonObjectEntry(t *testing.T) {
	wire := map[string]any{"x": "not-a-map"}
	if _, err := PropertyDefsFromWire(wire, nil); err == nil {
		t.Fatal("expected error for non-object property entry")
	}
}

func TestHashSeed_StableAcrossCalls(t *testing.T) {
	a := HashSeed("northwind/Customer")
	b := HashSeed("northwind/Customer")
	if a != b {
		t.Fatalf("HashSeed not stable: %d vs %d", a, b)
	}
	if a == 0 {
		t.Fatalf("HashSeed produced zero — would collide with the time-based fallback")
	}
	if HashSeed("") != 0 {
		t.Fatal("empty label should hash to 0 (let caller fall through to time-based seed)")
	}
}

// stress: large primary-key pool exercises ensureUnique + uniqueAlnum fallback.
func TestGenerate_LargePrimaryKeyPoolStaysUnique(t *testing.T) {
	props := []PropertyDef{
		{APIName: "id", BaseType: "string", IsPrimary: true, MinLength: ptrInt(2), MaxLength: ptrInt(2)},
	}
	// alphabet=62 char, length=2 ⇒ 62*62 = 3844 unique slots.
	// 800 rows is well within capacity but heavy on collisions.
	rows, err := Generate(props, 800, Options{Seed: 777})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	seen := map[string]struct{}{}
	for _, r := range rows {
		v := r["id"].(string)
		if _, dup := seen[v]; dup {
			t.Fatalf("duplicate primary key %q", v)
		}
		seen[v] = struct{}{}
	}
	if len(seen) != 800 {
		t.Fatalf("expected 800 unique keys, got %d", len(seen))
	}
}

func TestGenerate_PrimaryKeyEnumErrors(t *testing.T) {
	props := []PropertyDef{
		{APIName: "id", BaseType: "string", IsPrimary: true, Enum: []any{"only-one"}},
	}
	if _, err := Generate(props, 5, Options{Seed: 1}); err == nil {
		t.Fatal("expected error: enum cannot supply unique primary keys")
	}
}

// guard against silently widening the supported BaseType list — the documented
// pure-passthrough fallback for unknown types is "treat as string".
func TestGenerate_UnknownBaseTypeFallsBackToString(t *testing.T) {
	props := []PropertyDef{
		{APIName: "weird", BaseType: "vector"},
	}
	rows, err := Generate(props, 3, Options{Seed: 1})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for i, r := range rows {
		if _, ok := r["weird"].(string); !ok {
			t.Fatalf("row %d unknown type emitted %T", i, r["weird"])
		}
	}
}

func TestGenerate_LongIntFitsBitWidth(t *testing.T) {
	// guarantees the long path actually returns int64, not int — important
	// for downstream JSON consumers expecting JS-precision numerics.
	props := []PropertyDef{{APIName: "n", BaseType: "long", Min: fptr(1), Max: fptr(1000)}}
	rows, _ := Generate(props, 10, Options{Seed: 1})
	for i, r := range rows {
		if _, ok := r["n"].(int64); !ok {
			t.Errorf("row %d long emitted as %T", i, r["n"])
		}
	}
	// And 32-bit kinds emit `int`.
	props = []PropertyDef{{APIName: "n", BaseType: "integer", Min: fptr(1), Max: fptr(100)}}
	rows, _ = Generate(props, 10, Options{Seed: 1})
	for i, r := range rows {
		if _, ok := r["n"].(int); !ok {
			t.Errorf("row %d integer emitted as %T", i, r["n"])
		}
	}
}

// sanity: ensure we cap an absurdly small max so the bounds-narrowing
// short-circuit doesn't divide by zero.
func TestGenerate_MinEqualsMaxIsValidConstantValue(t *testing.T) {
	props := []PropertyDef{{APIName: "k", BaseType: "integer", Min: fptr(7), Max: fptr(7)}}
	rows, err := Generate(props, 5, Options{Seed: 1})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, r := range rows {
		if r["k"] != 7 {
			t.Fatalf("expected constant 7, got %v (%T)", r["k"], r["k"])
		}
	}
}

// A very tight alphabet boundary: int64 width ⇒ uniqueAlnum's 50-attempt loop
// triggers the suffix fallback. We just want the call to not panic and to
// still produce N unique values.
func TestGenerate_PrimaryKeyExhaustionFallback(t *testing.T) {
	props := []PropertyDef{
		{APIName: "id", BaseType: "string", IsPrimary: true, MinLength: ptrInt(1), MaxLength: ptrInt(1)},
	}
	// 62 single-char alphanums available — request fewer.
	rows, err := Generate(props, 30, Options{Seed: 12345})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	seen := map[string]struct{}{}
	for _, r := range rows {
		seen[r["id"].(string)] = struct{}{}
	}
	if len(seen) != 30 {
		t.Fatalf("got %d unique keys, want 30", len(seen))
	}
}

// silence import lint when ptrFloat unused
var _ = ptrFloat
var _ = strconv.Itoa
