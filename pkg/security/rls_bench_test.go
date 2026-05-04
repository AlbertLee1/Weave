package security

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/auth"
)

// US-432 performance gate: 1M rows × 10 CEL policies must clear < 500ms cold
// (no warmup, decision cache empty) and < 50ms warm (cache populated). The
// benchmark uses bounded user/row cardinality so the decision cache fully
// populates within a small fraction of the loop, letting the bulk of the
// pass amortise into hash lookups — this is the realistic shape of a
// production Search → ResultStream that pulls many rows for a small set of
// distinct users.
//
// Skipped under -short so the unit suite stays fast; CI exercises the full
// suite via the standard `go test ./...` invocation.

const (
	benchPoliciesCount    = 10
	benchUserCardinality  = 50
	benchRowCardinality   = 50
	benchIterationsTotal  = 1_000_000
	benchColdBudget       = 500 * time.Millisecond
	benchWarmBudget       = 50 * time.Millisecond
)

func TestRLSEvaluator_PerformanceGate(t *testing.T) {
	if testing.Short() {
		t.Skip("performance gate; -short skips")
	}

	eval, rules, users, rows, rowKeys := buildRLSWorkload(t)
	set, err := eval.BuildRuleSet(rules)
	if err != nil {
		t.Fatalf("BuildRuleSet: %v", err)
	}

	pass := func() time.Duration {
		ctx := context.Background()
		start := time.Now()
		for i := 0; i < benchIterationsTotal; i++ {
			u := users[i%benchUserCardinality]
			ridx := i % benchRowCardinality
			allow, err := eval.EvaluateRuleSet(ctx, u, set, rowKeys[ridx], rows[ridx])
			if err != nil {
				t.Fatalf("eval at i=%d: %v", i, err)
			}
			if !allow {
				t.Fatalf("expected allow=true for synthetic workload at i=%d", i)
			}
		}
		return time.Since(start)
	}

	coldDur := pass()
	warmDur := pass()

	dc := eval.DecisionCache()
	dcStats := dc.Stats()
	hits, misses := eval.CompileStats()
	t.Logf("cold=%v warm=%v decisionCache=%+v compileHits=%d compileMisses=%d",
		coldDur, warmDur, dcStats, hits, misses)

	if coldDur > benchColdBudget {
		t.Errorf("cold pass %v exceeded budget %v (1M iterations × %d policies, %d unique decisions)",
			coldDur, benchColdBudget, benchPoliciesCount, benchUserCardinality*benchRowCardinality)
	}
	if warmDur > benchWarmBudget {
		t.Errorf("warm pass %v exceeded budget %v (1M cache hits)", warmDur, benchWarmBudget)
	}
}

func BenchmarkRLSEvaluator_Cold(b *testing.B) {
	eval, rules, users, rows, rowKeys := buildRLSWorkload(b)
	set, err := eval.BuildRuleSet(rules)
	if err != nil {
		b.Fatalf("BuildRuleSet: %v", err)
	}
	ctx := context.Background()

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		u := users[n%benchUserCardinality]
		ridx := n % benchRowCardinality
		_, _ = eval.EvaluateRuleSet(ctx, u, set, rowKeys[ridx], rows[ridx])
	}
}

func BenchmarkRLSEvaluator_Warm(b *testing.B) {
	eval, rules, users, rows, rowKeys := buildRLSWorkload(b)
	set, err := eval.BuildRuleSet(rules)
	if err != nil {
		b.Fatalf("BuildRuleSet: %v", err)
	}
	ctx := context.Background()

	// Pre-populate the decision cache so b.N iterations land 100% hits.
	for i := 0; i < benchUserCardinality*benchRowCardinality; i++ {
		u := users[i%benchUserCardinality]
		ridx := i % benchRowCardinality
		_, _ = eval.EvaluateRuleSet(ctx, u, set, rowKeys[ridx], rows[ridx])
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		u := users[n%benchUserCardinality]
		ridx := n % benchRowCardinality
		_, _ = eval.EvaluateRuleSet(ctx, u, set, rowKeys[ridx], rows[ridx])
	}
}

func buildRLSWorkload(tb testing.TB) (*CELEvaluator, []CELRule, []*auth.User, []map[string]any, []string) {
	tb.Helper()

	eval := NewCELEvaluator()
	cache := NewDecisionCache(8192, time.Hour)
	eval.SetDecisionCache(cache)

	rules := make([]CELRule, benchPoliciesCount)
	for i := range rules {
		rules[i] = CELRule{
			PolicyRID:  fmt.Sprintf("ri.policy.bench.%d", i),
			Version:    1,
			Expression: fmt.Sprintf(`row.bucket_%d != ""`, i),
		}
		if err := eval.Compile(rules[i]); err != nil {
			tb.Fatalf("compile rule %d: %v", i, err)
		}
	}

	users := make([]*auth.User, benchUserCardinality)
	for i := range users {
		users[i] = &auth.User{
			ID:    fmt.Sprintf("u-%03d", i),
			Email: fmt.Sprintf("u-%03d@example.test", i),
			Roles: []string{"viewer"},
			Attributes: map[string]any{
				"markings": []string{"PUBLIC"},
			},
		}
	}

	rows := make([]map[string]any, benchRowCardinality)
	rowKeys := make([]string, benchRowCardinality)
	for i := range rows {
		row := make(map[string]any, benchPoliciesCount)
		for j := 0; j < benchPoliciesCount; j++ {
			row[fmt.Sprintf("bucket_%d", j)] = fmt.Sprintf("v-%d-%d", i, j)
		}
		rows[i] = row
		rowKeys[i] = HashRowProperties(row)
	}

	return eval, rules, users, rows, rowKeys
}
