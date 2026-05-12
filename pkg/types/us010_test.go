package types

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// US-010 — pkg/types 基础测试
//
// Covers in one suite:
//   * 23 BaseType coerce + validate matrix
//   * ValueType reference expansion + cycle / depth / not-found rejection
//   * Interface ↔ ObjectType property merge with conflict aggregation
//
// All sub-tests are table-driven; each top-level Test function provides a
// related cluster so coverage gaps surface quickly when a new BaseType is added.

// ---------------------------------------------------------------------------
// Section A — BaseType matrix (validate + coerce against representative inputs)
// ---------------------------------------------------------------------------

func TestUS010_BaseType_ValidateMatrix(t *testing.T) {
	tomorrow := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	cases := []struct {
		name    string
		dt      DataType
		good    []interface{}
		bad     []interface{}
		skipBad bool
	}{
		{name: "string", dt: DataType{Type: String}, good: []interface{}{"x", ""}, bad: []interface{}{1, true, nil}, skipBad: false},
		{name: "integer", dt: DataType{Type: Integer}, good: []interface{}{int(1), int32(2), int64(3), float64(4)}, bad: []interface{}{"abc", true}},
		{name: "short", dt: DataType{Type: Short}, good: []interface{}{int(1), int16(2), int32(3), float64(4)}, bad: []interface{}{"abc", true}},
		{name: "long", dt: DataType{Type: Long}, good: []interface{}{int(1), int32(2), int64(3), float64(4), "12345"}, bad: []interface{}{true, "not-a-number"}},
		{name: "float", dt: DataType{Type: Float}, good: []interface{}{float32(1.5), float64(2.5), int(3)}, bad: []interface{}{"abc", true}},
		{name: "double", dt: DataType{Type: Double}, good: []interface{}{float64(2.5), int64(3)}, bad: []interface{}{"abc", true}},
		{name: "boolean", dt: DataType{Type: Boolean}, good: []interface{}{true, false}, bad: []interface{}{"true", 1}},
		{name: "byte", dt: DataType{Type: Byte}, good: []interface{}{int(1), int8(2), byte(3), float64(4)}, bad: []interface{}{"abc", true}},
		{name: "date", dt: DataType{Type: Date}, good: []interface{}{"2026-01-15"}, bad: []interface{}{"15/01/2026", 1}},
		{name: "timestamp", dt: DataType{Type: Timestamp}, good: []interface{}{tomorrow, "2026-01-15T10:30:00Z"}, bad: []interface{}{"not-a-ts", 1}},
		{name: "decimal", dt: DataType{Type: Decimal}, good: []interface{}{float64(2.5), int(3), "1.23"}, bad: []interface{}{true, []interface{}{1}}},
		{name: "array", dt: DataType{Type: Array, SubType: &DataType{Type: String}}, good: []interface{}{[]interface{}{"a"}, []interface{}{}}, bad: []interface{}{"abc", 1}},
		{name: "struct", dt: DataType{Type: Struct, Fields: map[string]DataType{"a": {Type: String}}}, good: []interface{}{map[string]interface{}{"a": "x"}}, bad: []interface{}{[]interface{}{1}, "abc"}},
		// The following permissive types accept any value at validate-time;
		// validate path is exercised, but no "bad" inputs are expected.
		{name: "vector", dt: DataType{Type: Vector}, good: []interface{}{[]interface{}{0.1}}, skipBad: true},
		{name: "geopoint", dt: DataType{Type: Geopoint}, good: []interface{}{map[string]interface{}{"lat": 0, "lon": 0}}, skipBad: true},
		{name: "geoshape", dt: DataType{Type: Geoshape}, good: []interface{}{map[string]interface{}{"type": "Point"}}, skipBad: true},
		{name: "attachment", dt: DataType{Type: Attachment}, good: []interface{}{"att-rid-1"}, skipBad: true},
		{name: "timeseries", dt: DataType{Type: TimeSeries}, good: []interface{}{"ts-1"}, skipBad: true},
		{name: "mediaReference", dt: DataType{Type: MediaReference}, good: []interface{}{"media-rid"}, skipBad: true},
		{name: "media", dt: DataType{Type: Media}, good: []interface{}{"media-bytes"}, skipBad: true},
		{name: "marking", dt: DataType{Type: Marking}, good: []interface{}{"PUBLIC"}, skipBad: true},
		{name: "cipher", dt: DataType{Type: Cipher}, good: []interface{}{"ciphertext"}, skipBad: true},
		{name: "union", dt: DataType{Type: Union, Variants: []DataType{{Type: String}, {Type: Integer}}}, good: []interface{}{"x", 42}, bad: []interface{}{true, map[string]interface{}{"k": "v"}}},
	}
	if len(cases) != 23 {
		t.Fatalf("expected 23 BaseTypes in matrix, got %d — update when adding new BaseType", len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, g := range tc.good {
				if err := Validate(g, tc.dt, false); err != nil {
					t.Errorf("Validate(%v, %s) = %v, want nil", g, tc.name, err)
				}
			}
			if tc.skipBad {
				return
			}
			for _, b := range tc.bad {
				if err := Validate(b, tc.dt, false); err == nil {
					t.Errorf("Validate(%v, %s) = nil, want error", b, tc.name)
				}
			}
		})
	}
}

func TestUS010_BaseType_CoerceMatrix(t *testing.T) {
	cases := []struct {
		name string
		dt   DataType
		in   interface{}
		want interface{}
		err  bool
	}{
		{name: "string_from_int", dt: DataType{Type: String}, in: 42, want: "42"},
		{name: "string_passthrough", dt: DataType{Type: String}, in: "hi", want: "hi"},
		{name: "integer_from_float", dt: DataType{Type: Integer}, in: float64(7), want: int32(7)},
		{name: "integer_rejects_fractional", dt: DataType{Type: Integer}, in: float64(3.14), err: true},
		{name: "short_from_int", dt: DataType{Type: Short}, in: int(5), want: int16(5)},
		{name: "long_from_string", dt: DataType{Type: Long}, in: "9223372036854775807", want: int64(9223372036854775807)},
		{name: "long_rejects_bad_string", dt: DataType{Type: Long}, in: "not-a-long", err: true},
		{name: "float_from_string", dt: DataType{Type: Float}, in: "1.5", want: float64(1.5)},
		{name: "double_from_int", dt: DataType{Type: Double}, in: int64(8), want: float64(8)},
		{name: "boolean_passthrough", dt: DataType{Type: Boolean}, in: true, want: true},
		{name: "boolean_rejects_string", dt: DataType{Type: Boolean}, in: "true", err: true},
		{name: "date_from_string", dt: DataType{Type: Date}, in: "2026-01-15", want: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)},
		{name: "date_rejects_bad_format", dt: DataType{Type: Date}, in: "15/01/2026", err: true},
		{name: "timestamp_from_rfc3339", dt: DataType{Type: Timestamp}, in: "2026-01-15T10:30:00Z", want: time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)},
		{name: "timestamp_rejects_bad", dt: DataType{Type: Timestamp}, in: "bad-ts", err: true},
		{name: "array_subtype_propagates", dt: DataType{Type: Array, SubType: &DataType{Type: Integer}}, in: []interface{}{float64(1), float64(2)}, want: []interface{}{int32(1), int32(2)}},
		{name: "array_rejects_non_array", dt: DataType{Type: Array}, in: "abc", err: true},
		{name: "struct_coerces_known_fields", dt: DataType{Type: Struct, Fields: map[string]DataType{"a": {Type: Integer}}}, in: map[string]interface{}{"a": float64(3), "extra": "kept"}, want: map[string]interface{}{"a": int32(3), "extra": "kept"}},
		{name: "struct_rejects_non_map", dt: DataType{Type: Struct}, in: 1, err: true},
		// pass-through paths return the raw value when no coercion exists.
		{name: "vector_passthrough", dt: DataType{Type: Vector}, in: []interface{}{0.1}, want: []interface{}{0.1}},
		{name: "marking_passthrough", dt: DataType{Type: Marking}, in: "PUBLIC", want: "PUBLIC"},
		{name: "nil_short_circuits", dt: DataType{Type: String}, in: nil, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Coerce(tc.in, tc.dt)
			if tc.err {
				if err == nil {
					t.Fatalf("Coerce(%v, %s) = (%v, nil), want error", tc.in, tc.dt.Type, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Coerce(%v, %s) error: %v", tc.in, tc.dt.Type, err)
			}
			// time values compare via .Equal; other Go values via deep equality.
			if a, ok := got.(time.Time); ok {
				if b, _ := tc.want.(time.Time); !a.Equal(b) {
					t.Fatalf("Coerce time mismatch: got %v want %v", a, b)
				}
				return
			}
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(tc.want)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("Coerce mismatch: got %s want %s", gotJSON, wantJSON)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Section B — ValueType reference expansion + cycle / depth / not-found
// ---------------------------------------------------------------------------

func TestUS010_ValueType_Resolve(t *testing.T) {
	t.Run("DirectBaseType", func(t *testing.T) {
		reg := map[string]ValueTypeDef{
			"Email": {APIName: "Email", BaseType: "string"},
		}
		got, err := ResolveValueType("Email", reg, 0)
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if got.BaseType != String {
			t.Fatalf("BaseType = %s, want string", got.BaseType)
		}
	})

	t.Run("AliasChain", func(t *testing.T) {
		reg := map[string]ValueTypeDef{
			"OfficeEmail":  {APIName: "OfficeEmail", BaseType: "CompanyEmail"},
			"CompanyEmail": {APIName: "CompanyEmail", BaseType: "Email"},
			"Email":        {APIName: "Email", BaseType: "string"},
		}
		got, err := ResolveValueType("OfficeEmail", reg, 0)
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if got.BaseType != String {
			t.Fatalf("BaseType = %s, want string", got.BaseType)
		}
	})

	t.Run("AccumulatesConstraintsOutermostFirst", func(t *testing.T) {
		reg := map[string]ValueTypeDef{
			"OfficeEmail":  {APIName: "OfficeEmail", BaseType: "CompanyEmail", Constraints: []byte(`{"regex":"@office\\."}`)},
			"CompanyEmail": {APIName: "CompanyEmail", BaseType: "Email", Constraints: []byte(`{"regex":"@company\\."}`)},
			"Email":        {APIName: "Email", BaseType: "string", Constraints: []byte(`{"maxLength":255}`)},
		}
		got, err := ResolveValueType("OfficeEmail", reg, 0)
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if len(got.Constraints) != 3 {
			t.Fatalf("Constraints len = %d, want 3", len(got.Constraints))
		}
		if !strings.Contains(string(got.Constraints[0]), "@office") {
			t.Fatalf("first constraint should be outermost (OfficeEmail), got %s", got.Constraints[0])
		}
		if !strings.Contains(string(got.Constraints[2]), "maxLength") {
			t.Fatalf("last constraint should be innermost (Email), got %s", got.Constraints[2])
		}
	})

	t.Run("DirectCycle", func(t *testing.T) {
		reg := map[string]ValueTypeDef{
			"A": {APIName: "A", BaseType: "B"},
			"B": {APIName: "B", BaseType: "A"},
		}
		_, err := ResolveValueType("A", reg, 0)
		if !errors.Is(err, ErrValueTypeCycle) {
			t.Fatalf("expected ErrValueTypeCycle, got %v", err)
		}
		if !strings.Contains(err.Error(), "A -> B -> A") {
			t.Fatalf("cycle path missing from error: %v", err)
		}
	})

	t.Run("SelfReferentialCycle", func(t *testing.T) {
		reg := map[string]ValueTypeDef{
			"Loop": {APIName: "Loop", BaseType: "Loop"},
		}
		_, err := ResolveValueType("Loop", reg, 0)
		if !errors.Is(err, ErrValueTypeCycle) {
			t.Fatalf("expected ErrValueTypeCycle, got %v", err)
		}
	})

	t.Run("ThreeNodeCycle", func(t *testing.T) {
		reg := map[string]ValueTypeDef{
			"A": {APIName: "A", BaseType: "B"},
			"B": {APIName: "B", BaseType: "C"},
			"C": {APIName: "C", BaseType: "A"},
		}
		_, err := ResolveValueType("A", reg, 0)
		if !errors.Is(err, ErrValueTypeCycle) {
			t.Fatalf("expected ErrValueTypeCycle, got %v", err)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		_, err := ResolveValueType("Ghost", map[string]ValueTypeDef{}, 0)
		if !errors.Is(err, ErrValueTypeNotFound) {
			t.Fatalf("expected ErrValueTypeNotFound, got %v", err)
		}
	})

	t.Run("NotFoundMidChain", func(t *testing.T) {
		reg := map[string]ValueTypeDef{
			"A": {APIName: "A", BaseType: "B"},
			// B intentionally absent.
		}
		_, err := ResolveValueType("A", reg, 0)
		if !errors.Is(err, ErrValueTypeNotFound) {
			t.Fatalf("expected ErrValueTypeNotFound, got %v", err)
		}
	})

	t.Run("EmptyBaseTypeRejected", func(t *testing.T) {
		reg := map[string]ValueTypeDef{
			"Empty": {APIName: "Empty", BaseType: ""},
		}
		_, err := ResolveValueType("Empty", reg, 0)
		if err == nil || !strings.Contains(err.Error(), "empty baseType") {
			t.Fatalf("expected empty baseType error, got %v", err)
		}
	})

	t.Run("DepthExceeded", func(t *testing.T) {
		reg := map[string]ValueTypeDef{
			"A": {APIName: "A", BaseType: "B"},
			"B": {APIName: "B", BaseType: "C"},
			"C": {APIName: "C", BaseType: "D"},
			"D": {APIName: "D", BaseType: "string"},
		}
		_, err := ResolveValueType("A", reg, 2)
		if !errors.Is(err, ErrValueTypeDepthExceeded) {
			t.Fatalf("expected ErrValueTypeDepthExceeded, got %v", err)
		}
	})

	t.Run("DefaultDepthAccepts32Hops", func(t *testing.T) {
		reg := make(map[string]ValueTypeDef, 33)
		for i := 0; i < 32; i++ {
			reg["v"+itoa(i)] = ValueTypeDef{APIName: "v" + itoa(i), BaseType: "v" + itoa(i+1)}
		}
		reg["v32"] = ValueTypeDef{APIName: "v32", BaseType: "string"}
		got, err := ResolveValueType("v0", reg, 0)
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if got.BaseType != String {
			t.Fatalf("BaseType = %s, want string", got.BaseType)
		}
	})
}

// ---------------------------------------------------------------------------
// Section C — Interface ↔ ObjectType property merge
// ---------------------------------------------------------------------------

func TestUS010_InterfaceMerge(t *testing.T) {
	stringProp := func(name string) PropertyDef {
		return PropertyDef{APIName: name, DataType: DataType{Type: String}, Nullable: false}
	}
	intProp := func(name string) PropertyDef {
		return PropertyDef{APIName: name, DataType: DataType{Type: Integer}, Nullable: false}
	}

	t.Run("InterfaceOnlyAndObjectOnlyCombine", func(t *testing.T) {
		iface := []PropertyDef{stringProp("name")}
		obj := []PropertyDef{stringProp("local")}
		got, err := MergeInterfaceProperties(iface, obj)
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("merged len = %d, want 2", len(got))
		}
		if got[0].APIName != "local" || got[0].Source != "object-only" {
			t.Fatalf("expected local/object-only first, got %+v", got[0])
		}
		if got[1].APIName != "name" || got[1].Source != "interface-only" {
			t.Fatalf("expected name/interface-only second, got %+v", got[1])
		}
	})

	t.Run("CompatiblePropertyMerged", func(t *testing.T) {
		iface := []PropertyDef{stringProp("name")}
		obj := []PropertyDef{stringProp("name")}
		got, err := MergeInterfaceProperties(iface, obj)
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if len(got) != 1 || got[0].Source != "merged" {
			t.Fatalf("expected single merged entry, got %+v", got)
		}
	})

	t.Run("BaseTypeConflict", func(t *testing.T) {
		iface := []PropertyDef{stringProp("score")}
		obj := []PropertyDef{intProp("score")}
		_, err := MergeInterfaceProperties(iface, obj)
		if !errors.Is(err, ErrInterfaceConflict) {
			t.Fatalf("expected ErrInterfaceConflict, got %v", err)
		}
		if !strings.Contains(err.Error(), "score") {
			t.Fatalf("conflict error should name 'score', got: %v", err)
		}
	})

	t.Run("NullableConflict", func(t *testing.T) {
		iface := []PropertyDef{{APIName: "email", DataType: DataType{Type: String}, Nullable: false}}
		obj := []PropertyDef{{APIName: "email", DataType: DataType{Type: String}, Nullable: true}}
		_, err := MergeInterfaceProperties(iface, obj)
		if !errors.Is(err, ErrInterfaceConflict) {
			t.Fatalf("expected ErrInterfaceConflict on nullable mismatch, got %v", err)
		}
	})

	t.Run("ArraySubTypeConflict", func(t *testing.T) {
		iface := []PropertyDef{{APIName: "tags", DataType: DataType{Type: Array, SubType: &DataType{Type: String}}}}
		obj := []PropertyDef{{APIName: "tags", DataType: DataType{Type: Array, SubType: &DataType{Type: Integer}}}}
		_, err := MergeInterfaceProperties(iface, obj)
		if !errors.Is(err, ErrInterfaceConflict) {
			t.Fatalf("expected ErrInterfaceConflict on array sub-type mismatch, got %v", err)
		}
	})

	t.Run("StructFieldConflict", func(t *testing.T) {
		iface := []PropertyDef{{APIName: "addr", DataType: DataType{Type: Struct, Fields: map[string]DataType{"city": {Type: String}}}}}
		obj := []PropertyDef{{APIName: "addr", DataType: DataType{Type: Struct, Fields: map[string]DataType{"city": {Type: Integer}}}}}
		_, err := MergeInterfaceProperties(iface, obj)
		if !errors.Is(err, ErrInterfaceConflict) {
			t.Fatalf("expected ErrInterfaceConflict on struct field mismatch, got %v", err)
		}
	})

	t.Run("DecimalPrecisionConflict", func(t *testing.T) {
		p1, s1 := 10, 2
		p2, s2 := 12, 2
		iface := []PropertyDef{{APIName: "amount", DataType: DataType{Type: Decimal, Precision: &p1, Scale: &s1}}}
		obj := []PropertyDef{{APIName: "amount", DataType: DataType{Type: Decimal, Precision: &p2, Scale: &s2}}}
		_, err := MergeInterfaceProperties(iface, obj)
		if !errors.Is(err, ErrInterfaceConflict) {
			t.Fatalf("expected ErrInterfaceConflict on decimal precision mismatch, got %v", err)
		}
	})

	t.Run("AggregatesMultipleConflicts", func(t *testing.T) {
		iface := []PropertyDef{stringProp("a"), stringProp("b"), stringProp("c")}
		obj := []PropertyDef{intProp("a"), intProp("b")}
		_, err := MergeInterfaceProperties(iface, obj)
		if !errors.Is(err, ErrInterfaceConflict) {
			t.Fatalf("expected ErrInterfaceConflict, got %v", err)
		}
		if !strings.Contains(err.Error(), "a") || !strings.Contains(err.Error(), "b") {
			t.Fatalf("expected both a and b in error, got %v", err)
		}
	})

	t.Run("DeterministicOrder", func(t *testing.T) {
		iface := []PropertyDef{stringProp("z"), stringProp("a")}
		obj := []PropertyDef{stringProp("m")}
		got, _ := MergeInterfaceProperties(iface, obj)
		if len(got) != 3 {
			t.Fatalf("len = %d, want 3", len(got))
		}
		if got[0].APIName != "a" || got[1].APIName != "m" || got[2].APIName != "z" {
			t.Fatalf("expected sorted [a m z], got [%s %s %s]", got[0].APIName, got[1].APIName, got[2].APIName)
		}
	})

	t.Run("EmptyInputsReturnEmpty", func(t *testing.T) {
		got, err := MergeInterfaceProperties(nil, nil)
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected empty merge, got %+v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
