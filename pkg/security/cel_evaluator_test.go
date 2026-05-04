package security

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/auth"
)

func newAllowEvaluator(t *testing.T) *CELEvaluator {
	t.Helper()
	e := NewCELEvaluator()
	e.SetDecisionCache(NewDecisionCache(1024, time.Minute))
	return e
}

func TestCELEvaluator_EmptyRules_AllowsRow(t *testing.T) {
	eval := newAllowEvaluator(t)
	allow, err := eval.Evaluate(context.Background(), &auth.User{ID: "u1"}, nil, "row-1", map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !allow {
		t.Fatalf("expected allow=true with no rules")
	}
}

func TestCELEvaluator_SingleRule_AllowAndDeny(t *testing.T) {
	eval := newAllowEvaluator(t)
	rules := []CELRule{{
		PolicyRID:  "ri.policy.eq",
		Version:    1,
		Expression: `row.country == "US"`,
	}}

	user := &auth.User{ID: "u1"}
	allow, err := eval.Evaluate(context.Background(), user, rules, "row-us", map[string]any{"country": "US"})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !allow {
		t.Fatalf("expected allow=true for matching row")
	}

	allow, err = eval.Evaluate(context.Background(), user, rules, "row-de", map[string]any{"country": "DE"})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if allow {
		t.Fatalf("expected allow=false for non-matching row")
	}
}

func TestCELEvaluator_AndCombinesMultipleRules(t *testing.T) {
	eval := newAllowEvaluator(t)
	rules := []CELRule{
		{PolicyRID: "p1", Version: 1, Expression: `row.country == "US"`},
		{PolicyRID: "p2", Version: 1, Expression: `"viewer" in user.roles`},
	}

	user := &auth.User{ID: "u1", Roles: []string{"viewer"}}
	row := map[string]any{"country": "US"}
	allow, err := eval.Evaluate(context.Background(), user, rules, "rk", row)
	if err != nil || !allow {
		t.Fatalf("both rules pass → allow=true; got allow=%v err=%v", allow, err)
	}

	user2 := &auth.User{ID: "u2", Roles: []string{"editor"}}
	allow, err = eval.Evaluate(context.Background(), user2, rules, "rk2", row)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if allow {
		t.Fatalf("rule p2 fails → allow=false; got allow=true")
	}
}

func TestCELEvaluator_CompileCache_HitOnSecondRule(t *testing.T) {
	eval := newAllowEvaluator(t)
	rule := CELRule{PolicyRID: "p1", Version: 1, Expression: `row.x > 0`}

	if err := eval.Compile(rule); err != nil {
		t.Fatalf("compile: %v", err)
	}
	hits, misses := eval.CompileStats()
	if misses != 1 || hits != 0 {
		t.Fatalf("after first Compile: hits=%d misses=%d, want 0/1", hits, misses)
	}
	if err := eval.Compile(rule); err != nil {
		t.Fatalf("second compile: %v", err)
	}
	hits, misses = eval.CompileStats()
	if misses != 1 || hits != 1 {
		t.Fatalf("after second Compile: hits=%d misses=%d, want 1/1", hits, misses)
	}
}

func TestCELEvaluator_VersionBump_Recompiles(t *testing.T) {
	eval := newAllowEvaluator(t)
	v1 := CELRule{PolicyRID: "p1", Version: 1, Expression: `row.x > 0`}
	v2 := CELRule{PolicyRID: "p1", Version: 2, Expression: `row.x > 0`}

	_ = eval.Compile(v1)
	_ = eval.Compile(v2)
	if got := eval.CompileSize(); got != 2 {
		t.Fatalf("CompileSize = %d, want 2 (versions are independent cache entries)", got)
	}
}

func TestCELEvaluator_DecisionCache_HitSecondCall(t *testing.T) {
	eval := newAllowEvaluator(t)
	rules := []CELRule{{
		PolicyRID:  "p1",
		Version:    1,
		Expression: `row.country == "US"`,
	}}
	user := &auth.User{ID: "u1"}
	row := map[string]any{"country": "US"}

	if _, err := eval.Evaluate(context.Background(), user, rules, "rk", row); err != nil {
		t.Fatalf("first eval: %v", err)
	}
	st := eval.DecisionCache().Stats()
	if st.Hits != 0 || st.Misses != 1 || st.Size != 1 {
		t.Fatalf("after first eval: %+v, want 0/1/1", st)
	}

	if _, err := eval.Evaluate(context.Background(), user, rules, "rk", row); err != nil {
		t.Fatalf("second eval: %v", err)
	}
	st = eval.DecisionCache().Stats()
	if st.Hits != 1 || st.Misses != 1 {
		t.Fatalf("after second eval: %+v, want hits=1 misses=1", st)
	}
}

func TestCELEvaluator_DecisionCache_VersionBumpInvalidates(t *testing.T) {
	eval := newAllowEvaluator(t)
	user := &auth.User{ID: "u1"}
	row := map[string]any{"country": "US"}

	rulesV1 := []CELRule{{PolicyRID: "p1", Version: 1, Expression: `row.country == "US"`}}
	if _, err := eval.Evaluate(context.Background(), user, rulesV1, "rk", row); err != nil {
		t.Fatalf("v1 eval: %v", err)
	}

	// Bump version → ruleSetSignature changes → decision cache key changes
	rulesV2 := []CELRule{{PolicyRID: "p1", Version: 2, Expression: `row.country == "US"`}}
	if _, err := eval.Evaluate(context.Background(), user, rulesV2, "rk", row); err != nil {
		t.Fatalf("v2 eval: %v", err)
	}

	st := eval.DecisionCache().Stats()
	if st.Misses != 2 {
		t.Fatalf("expected 2 misses after version bump, got %+v", st)
	}
}

func TestCELEvaluator_DecisionCache_SkippedWithoutRowKey(t *testing.T) {
	eval := newAllowEvaluator(t)
	rules := []CELRule{{PolicyRID: "p1", Version: 1, Expression: `row.x > 0`}}
	user := &auth.User{ID: "u1"}
	row := map[string]any{"x": 1}

	if _, err := eval.Evaluate(context.Background(), user, rules, "", row); err != nil {
		t.Fatalf("eval: %v", err)
	}
	if _, err := eval.Evaluate(context.Background(), user, rules, "", row); err != nil {
		t.Fatalf("eval: %v", err)
	}
	st := eval.DecisionCache().Stats()
	if st.Hits != 0 || st.Misses != 0 || st.Size != 0 {
		t.Fatalf("decision cache should be untouched when rowKey is empty, got %+v", st)
	}
}

func TestCELEvaluator_DecisionCache_RuleSetOrderIndependent(t *testing.T) {
	eval := newAllowEvaluator(t)
	user := &auth.User{ID: "u1", Roles: []string{"viewer"}}
	row := map[string]any{"country": "US"}

	rulesA := []CELRule{
		{PolicyRID: "p1", Version: 1, Expression: `row.country == "US"`},
		{PolicyRID: "p2", Version: 1, Expression: `"viewer" in user.roles`},
	}
	rulesB := []CELRule{
		{PolicyRID: "p2", Version: 1, Expression: `"viewer" in user.roles`},
		{PolicyRID: "p1", Version: 1, Expression: `row.country == "US"`},
	}

	if _, err := eval.Evaluate(context.Background(), user, rulesA, "rk", row); err != nil {
		t.Fatalf("rulesA: %v", err)
	}
	if _, err := eval.Evaluate(context.Background(), user, rulesB, "rk", row); err != nil {
		t.Fatalf("rulesB: %v", err)
	}
	st := eval.DecisionCache().Stats()
	if st.Hits != 1 || st.Misses != 1 {
		t.Fatalf("expected order-independent decision cache: %+v", st)
	}
}

func TestCELEvaluator_FailClosed_OnInvalidExpression(t *testing.T) {
	eval := newAllowEvaluator(t)
	rules := []CELRule{{PolicyRID: "p1", Version: 1, Expression: `row.country ===`}}
	allow, err := eval.Evaluate(context.Background(), &auth.User{ID: "u1"}, rules, "rk", map[string]any{"country": "US"})
	if err == nil {
		t.Fatalf("expected compile error")
	}
	if allow {
		t.Fatalf("expected allow=false on compile error (fail-closed)")
	}
}

func TestCELEvaluator_FailClosed_OnEvalError(t *testing.T) {
	eval := newAllowEvaluator(t)
	// references a row field that's missing → cel-go returns a runtime error
	rules := []CELRule{{PolicyRID: "p1", Version: 1, Expression: `row.missing == "x"`}}
	allow, err := eval.Evaluate(context.Background(), &auth.User{ID: "u1"}, rules, "rk", map[string]any{})
	if err == nil {
		t.Fatalf("expected eval error for missing row field")
	}
	if allow {
		t.Fatalf("expected allow=false on eval error (fail-closed)")
	}

	// Subsequent call hits the cached false verdict
	allow, _ = eval.Evaluate(context.Background(), &auth.User{ID: "u1"}, rules, "rk", map[string]any{})
	if allow {
		t.Fatalf("expected cached deny on second call")
	}
	st := eval.DecisionCache().Stats()
	if st.Hits != 1 {
		t.Fatalf("expected the deny verdict to be cached, hits=%d", st.Hits)
	}
}

func TestCELEvaluator_RejectsNonBoolExpression(t *testing.T) {
	eval := newAllowEvaluator(t)
	if err := eval.Compile(CELRule{PolicyRID: "p1", Version: 1, Expression: `row.x`}); err == nil {
		t.Fatalf("expected error for non-bool expression")
	}
}

func TestCELEvaluator_NilUser_BindsEmpty(t *testing.T) {
	eval := newAllowEvaluator(t)
	rules := []CELRule{{PolicyRID: "p1", Version: 1, Expression: `size(user.roles) == 0`}}
	allow, err := eval.Evaluate(context.Background(), nil, rules, "rk", map[string]any{})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !allow {
		t.Fatalf("expected allow=true: nil user binds to empty roles")
	}
}

func TestCELEvaluator_UserMarkings_NormalisedFromAttributes(t *testing.T) {
	eval := newAllowEvaluator(t)
	rules := []CELRule{{PolicyRID: "p1", Version: 1, Expression: `"PII" in user.markings`}}
	user := &auth.User{
		ID:         "u1",
		Attributes: map[string]any{"markings": []any{"PII", "PUBLIC"}},
	}
	allow, err := eval.Evaluate(context.Background(), user, rules, "rk", map[string]any{})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !allow {
		t.Fatalf("expected allow=true: user.markings should expose 'PII'")
	}
}

func TestCELEvaluator_InvalidatePolicy_DropsAllVersions(t *testing.T) {
	eval := newAllowEvaluator(t)
	_ = eval.Compile(CELRule{PolicyRID: "p1", Version: 1, Expression: `true`})
	_ = eval.Compile(CELRule{PolicyRID: "p1", Version: 2, Expression: `true`})
	_ = eval.Compile(CELRule{PolicyRID: "p2", Version: 1, Expression: `true`})

	if got := eval.CompileSize(); got != 3 {
		t.Fatalf("compile size = %d, want 3", got)
	}
	eval.InvalidatePolicy("p1")
	if got := eval.CompileSize(); got != 1 {
		t.Fatalf("compile size after invalidate = %d, want 1", got)
	}
}

func TestCELEvaluator_HashRowProperties_DeterministicAndOrderIndependent(t *testing.T) {
	a := HashRowProperties(map[string]any{"x": 1, "y": "ab"})
	b := HashRowProperties(map[string]any{"y": "ab", "x": 1})
	if a != b {
		t.Fatalf("HashRowProperties should be order-independent: %s vs %s", a, b)
	}
	c := HashRowProperties(map[string]any{"x": 1, "y": "ac"})
	if a == c {
		t.Fatalf("HashRowProperties should differ when content differs: both %s", a)
	}
	if HashRowProperties(nil) != HashRowProperties(map[string]any{}) {
		t.Fatalf("nil and empty map should hash identically")
	}
}

func TestCELEvaluator_CompileError_WrapsExpression(t *testing.T) {
	eval := newAllowEvaluator(t)
	err := eval.Compile(CELRule{PolicyRID: "p1", Version: 1, Expression: `"unterminated`})
	if err == nil {
		t.Fatalf("expected error for malformed expression")
	}
	if !strings.Contains(err.Error(), `compile`) {
		t.Fatalf("expected wrapped compile error, got %v", err)
	}
}
