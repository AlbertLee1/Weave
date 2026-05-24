package types_test

import (
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/types"
)

// TestBDD_Validate_DeepStructAndArray covers PRD-V2 Gap-T2:
// "Struct / Array 深度序列化测试" — the existing Validate()
// only checks the outer container type (is it a map? is it an
// array?) and silently accepts any element / field shape. That
// lets nonsense values like {"a": 42} pass when the schema says
// {"a": String}, leaking through to the persistence layer and
// surfacing later as Bleve indexing errors or downstream NaN
// surprises. Per the Foundry contract, schema-typed properties
// MUST validate every element against the declared SubType / Fields.
//
// Acceptance criteria (Given → When → Then):
//
//   Given an Array(SubType: Integer) and value [true, false]
//   When  types.Validate runs
//   Then  it returns an error mentioning the element type mismatch
//
//   Given a Struct(Fields: {"a": String}) and value {"a": 42}
//   When  types.Validate runs
//   Then  it returns an error mentioning the field type mismatch
//
//   Given a nested Struct({"inner": Struct({"k": Integer})}) and
//         value {"inner": {"k": "wrong"}}
//   When  types.Validate runs
//   Then  it recurses and returns an error mentioning "inner" / "k"
//
//   Given an Array(SubType: Struct({"k": Integer})) and value
//         [{"k": "wrong"}]
//   When  types.Validate runs
//   Then  it recurses and returns an error
//
//   Given a Struct with extra fields not in schema
//   When  types.Validate runs
//   Then  extras are preserved (Foundry round-trips unknown fields)
//
//   Given a Struct with FEWER fields than the schema declares
//   When  types.Validate runs
//   Then  validation passes — declared-but-absent fields are tolerated
//         (partial updates / MODIFY edits supply only changed fields)
//
// The 4 "wrong-type" scenarios fail before the fix (shallow
// validator), pass after. The 2 permissive scenarios stay green
// throughout (regression guards for the existing Foundry-style
// tolerant behavior).
func TestBDD_Validate_DeepStructAndArray(t *testing.T) {
	intT := types.DataType{Type: types.Integer}
	strT := types.DataType{Type: types.String}

	t.Run("Array with wrong element type returns error", func(t *testing.T) {
		dt := types.DataType{Type: types.Array, SubType: &intT}
		bad := []interface{}{true, false}
		err := types.Validate(bad, dt, false)
		if err == nil {
			t.Fatal("Validate([true, false], Array<Integer>) = nil, want error mentioning element type")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "integer") {
			t.Errorf("error = %q, want it to mention the declared element type 'integer'", err.Error())
		}
	})

	t.Run("Array with correct element types passes", func(t *testing.T) {
		dt := types.DataType{Type: types.Array, SubType: &intT}
		good := []interface{}{int32(1), int64(2), float64(3)}
		if err := types.Validate(good, dt, false); err != nil {
			t.Errorf("Validate([1, 2, 3], Array<Integer>) = %v, want nil", err)
		}
	})

	t.Run("Array with no SubType stays permissive (regression guard)", func(t *testing.T) {
		dt := types.DataType{Type: types.Array} // no SubType
		mixed := []interface{}{"a", 1, true}
		if err := types.Validate(mixed, dt, false); err != nil {
			t.Errorf("Validate(mixed, Array<any>) = %v, want nil (untyped arrays stay tolerant)", err)
		}
	})

	t.Run("Struct with wrong field type returns error", func(t *testing.T) {
		dt := types.DataType{Type: types.Struct, Fields: map[string]types.DataType{"a": strT}}
		bad := map[string]interface{}{"a": 42}
		err := types.Validate(bad, dt, false)
		if err == nil {
			t.Fatal(`Validate({"a": 42}, Struct{a: String}) = nil, want error mentioning field type`)
		}
		if !strings.Contains(err.Error(), "a") || !strings.Contains(strings.ToLower(err.Error()), "string") {
			t.Errorf("error = %q, want it to mention the field name 'a' and declared type 'string'", err.Error())
		}
	})

	t.Run("Struct with correct field types passes", func(t *testing.T) {
		dt := types.DataType{Type: types.Struct, Fields: map[string]types.DataType{
			"name":  strT,
			"count": intT,
		}}
		good := map[string]interface{}{"name": "alice", "count": int32(3)}
		if err := types.Validate(good, dt, false); err != nil {
			t.Errorf("Validate good struct = %v, want nil", err)
		}
	})

	t.Run("Nested struct recurses into inner fields", func(t *testing.T) {
		inner := types.DataType{Type: types.Struct, Fields: map[string]types.DataType{"k": intT}}
		outer := types.DataType{Type: types.Struct, Fields: map[string]types.DataType{"inner": inner}}
		bad := map[string]interface{}{"inner": map[string]interface{}{"k": "wrong"}}
		err := types.Validate(bad, outer, false)
		if err == nil {
			t.Fatal(`Validate({inner: {k: "wrong"}}, nested struct) = nil, want recursive error`)
		}
		if !strings.Contains(err.Error(), "inner") {
			t.Errorf("error = %q, want it to mention the outer field name 'inner'", err.Error())
		}
	})

	t.Run("Array of struct recurses into element fields", func(t *testing.T) {
		elem := types.DataType{Type: types.Struct, Fields: map[string]types.DataType{"k": intT}}
		arr := types.DataType{Type: types.Array, SubType: &elem}
		bad := []interface{}{map[string]interface{}{"k": "wrong"}}
		err := types.Validate(bad, arr, false)
		if err == nil {
			t.Fatal(`Validate([{k: "wrong"}], Array<Struct{k: Integer}>) = nil, want recursive error`)
		}
	})

	t.Run("Struct with extra fields stays permissive (Foundry round-trips unknown fields)", func(t *testing.T) {
		dt := types.DataType{Type: types.Struct, Fields: map[string]types.DataType{"a": strT}}
		value := map[string]interface{}{"a": "x", "extra": 42, "anotherExtra": true}
		if err := types.Validate(value, dt, false); err != nil {
			t.Errorf("Validate struct with extras = %v, want nil (extras tolerated)", err)
		}
	})

	t.Run("Struct missing declared fields stays permissive (partial MODIFY edits)", func(t *testing.T) {
		dt := types.DataType{Type: types.Struct, Fields: map[string]types.DataType{
			"a": strT,
			"b": intT,
			"c": strT,
		}}
		value := map[string]interface{}{"a": "only-this-field-is-present"}
		if err := types.Validate(value, dt, false); err != nil {
			t.Errorf("Validate partial struct = %v, want nil (absent declared fields tolerated for MODIFY edits)", err)
		}
	})

	t.Run("Outer-shape mismatch regression: bare string against Struct still fails", func(t *testing.T) {
		dt := types.DataType{Type: types.Struct, Fields: map[string]types.DataType{"a": strT}}
		if err := types.Validate("not-a-map", dt, false); err == nil {
			t.Error(`Validate("not-a-map", Struct) = nil, want shape-mismatch error`)
		}
	})

	t.Run("Outer-shape mismatch regression: scalar against Array still fails", func(t *testing.T) {
		dt := types.DataType{Type: types.Array, SubType: &intT}
		if err := types.Validate("not-an-array", dt, false); err == nil {
			t.Error(`Validate("not-an-array", Array<Integer>) = nil, want shape-mismatch error`)
		}
	})
}
