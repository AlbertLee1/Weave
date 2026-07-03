package oss_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/security"
)

// TestPropertyFilter is the US-048 RED test. It wires a PROPERTY-scope
// SecurityPolicy through security.Engine.AllowedProperties, applies the
// result to a WireObject via FilterProperties, and verifies that the JSON
// serialization drops unlisted properties (omitted — NOT nulled) while
// preserving the reserved __rid / __primaryKey / __apiName keys.
//
// Matrix: one employee ObjectType with three properties (name / role /
// salary) and two callers (manager / peer). Baseline rule grants name+role
// to everyone; a conditional rule grants salary only when the caller's
// role attribute is "manager".
func TestPropertyFilter(t *testing.T) {
	ot := oms.ObjectType{
		RID:     "ri.ontology.main.object-type.employee",
		APIName: "employee",
	}
	policy := security.Policy{
		RID:           "ri.ontology.main.security-policy.employee-props",
		ObjectTypeRID: ot.RID,
		PolicyType:    security.PolicyTypeProperty,
		Rules: []security.Rule{
			// Baseline: every caller sees name + role.
			{Properties: []string{"name", "role"}},
			// Managers also see salary.
			{
				UserAttr:   "role",
				Values:     []string{"manager"},
				Properties: []string{"salary"},
			},
		},
	}

	engine := security.NewEngine()
	engine.SetPolicies(ot.RID, []security.Policy{policy})

	manager := &auth.User{ID: "u-m", Attributes: map[string]any{"role": "manager"}}
	peer := &auth.User{ID: "u-p", Attributes: map[string]any{"role": "engineer"}}

	rawProps := map[string]interface{}{
		"name":   "alice",
		"role":   "engineer",
		"salary": float64(120000),
	}

	// --- Manager sees all three fields + reserved keys.
	mAllowed := engine.AllowedProperties(context.Background(), manager, ot)
	if mAllowed == nil {
		t.Fatal("manager AllowedProperties returned nil (would disable filtering)")
	}
	mObj := oss.FormatObject("employee", "e1", copyProps(rawProps)).FilterProperties(mAllowed)
	mDecoded := roundTripJSON(t, mObj)
	for _, k := range []string{"name", "role", "salary", "__rid", "__primaryKey", "__apiName"} {
		if _, ok := mDecoded[k]; !ok {
			t.Errorf("manager view missing %q (decoded=%v)", k, mDecoded)
		}
	}

	// --- Peer sees name/role but salary is OMITTED (not nulled).
	pAllowed := engine.AllowedProperties(context.Background(), peer, ot)
	if pAllowed == nil {
		t.Fatal("peer AllowedProperties returned nil (would disable filtering)")
	}
	pObj := oss.FormatObject("employee", "e1", copyProps(rawProps)).FilterProperties(pAllowed)
	pDecoded := roundTripJSON(t, pObj)
	for _, k := range []string{"name", "role", "__rid", "__primaryKey", "__apiName"} {
		if _, ok := pDecoded[k]; !ok {
			t.Errorf("peer view missing %q (decoded=%v)", k, pDecoded)
		}
	}
	if _, ok := pDecoded["salary"]; ok {
		t.Errorf("peer view MUST NOT contain salary, got %v", pDecoded["salary"])
	}
	// Confirm salary is absent from the raw JSON bytes too (guards against
	// a null-encoded regression that a map decoder would hide).
	pBytes, err := json.Marshal(pObj)
	if err != nil {
		t.Fatalf("marshal peer view: %v", err)
	}
	if bytesContain(pBytes, `"salary"`) {
		t.Errorf("peer view JSON must not mention salary, got %s", string(pBytes))
	}
}

// TestPropertyFilter_NoPolicy_AllowsAll verifies the back-compat fast path:
// when no PROPERTY-scope policy is registered the engine returns nil and
// WireObject.FilterProperties is a no-op, so existing un-policied callers
// keep their full property payload.
func TestPropertyFilter_NoPolicy_AllowsAll(t *testing.T) {
	ot := oms.ObjectType{RID: "ri.x", APIName: "widget"}
	engine := security.NewEngine()

	allowed := engine.AllowedProperties(context.Background(), nil, ot)
	if allowed != nil {
		t.Fatalf("un-policied AllowedProperties should return nil, got %v", allowed)
	}

	obj := oss.FormatObject("widget", "w1", map[string]interface{}{"a": 1, "b": 2})
	filtered := obj.FilterProperties(allowed)
	if filtered != obj {
		t.Errorf("nil allowed list should return the same WireObject pointer")
	}
}

// TestPropertyFilter_EmptyAllowStripsAll verifies the explicit
// "restricted-to-nothing" path: a non-nil but zero-length allow list
// omits every property field while retaining the reserved keys.
func TestPropertyFilter_EmptyAllowStripsAll(t *testing.T) {
	obj := oss.FormatObject("widget", "w1", map[string]interface{}{"a": 1, "b": 2})
	filtered := obj.FilterProperties([]string{})
	decoded := roundTripJSON(t, filtered)
	for _, k := range []string{"a", "b"} {
		if _, ok := decoded[k]; ok {
			t.Errorf("empty allow list must drop %q, got %v", k, decoded[k])
		}
	}
	for _, k := range []string{"__rid", "__primaryKey", "__apiName"} {
		if _, ok := decoded[k]; !ok {
			t.Errorf("empty allow list must keep reserved key %q", k)
		}
	}
}

// TestProjectProperties_Subset verifies the Foundry `select` projection: the
// returned WireObject keeps only the selected apiNames plus any keepAlways
// fields (primary key here), drops every unselected property, and still emits
// the reserved __rid / __primaryKey / __apiName keys.
func TestProjectProperties_Subset(t *testing.T) {
	obj := oss.FormatObject("widget", "w1", map[string]interface{}{
		"widgetId": "w1",
		"name":     "alpha",
		"color":    "red",
		"weight":   float64(10),
	})
	projected := obj.ProjectProperties([]string{"name"}, "widgetId")
	decoded := roundTripJSON(t, projected)

	for _, k := range []string{"name", "widgetId", "__rid", "__primaryKey", "__apiName"} {
		if _, ok := decoded[k]; !ok {
			t.Errorf("projection missing %q; decoded=%v", k, decoded)
		}
	}
	for _, k := range []string{"color", "weight"} {
		if _, ok := decoded[k]; ok {
			t.Errorf("projection must drop unselected %q, got %v", k, decoded[k])
		}
	}
	// Original object is never mutated.
	if _, ok := obj.Properties["color"]; !ok {
		t.Errorf("ProjectProperties must not mutate the source object")
	}
}

// TestProjectProperties_EmptySelect verifies the "return everything" default:
// an empty select is a no-op that returns the same pointer unchanged.
func TestProjectProperties_EmptySelect(t *testing.T) {
	obj := oss.FormatObject("widget", "w1", map[string]interface{}{"a": 1, "b": 2})
	if got := obj.ProjectProperties(nil, "a"); got != obj {
		t.Errorf("empty select must return the same WireObject pointer")
	}
	if got := obj.ProjectProperties([]string{}, "a"); got != obj {
		t.Errorf("empty select slice must return the same WireObject pointer")
	}
}

// TestProjectProperties_UnknownName verifies that selecting an apiName the
// object does not carry is silently ignored — no null key, no panic.
func TestProjectProperties_UnknownName(t *testing.T) {
	obj := oss.FormatObject("widget", "w1", map[string]interface{}{
		"widgetId": "w1",
		"name":     "alpha",
	})
	decoded := roundTripJSON(t, obj.ProjectProperties([]string{"name", "ghost"}, "widgetId"))
	if _, ok := decoded["ghost"]; ok {
		t.Errorf("unknown select name must not appear, got %v", decoded["ghost"])
	}
	if decoded["name"] != "alpha" {
		t.Errorf("name = %v, want alpha", decoded["name"])
	}
}

func copyProps(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func roundTripJSON(t *testing.T, obj *oss.WireObject) map[string]interface{} {
	t.Helper()
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

func bytesContain(b []byte, s string) bool {
	return len(b) >= len(s) && indexOf(b, s) >= 0
}

func indexOf(b []byte, s string) int {
	for i := 0; i+len(s) <= len(b); i++ {
		if string(b[i:i+len(s)]) == s {
			return i
		}
	}
	return -1
}
