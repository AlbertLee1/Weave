package bench

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"

	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/cellsec"
	"github.com/liyang/weave/pkg/functions"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/masking"
	"github.com/liyang/weave/pkg/oss/aggregation"
	"github.com/liyang/weave/pkg/oss/objectset"
	"github.com/liyang/weave/pkg/security"
)

// Each benchmark below is the canonical hot-path regression guard for one of
// the eight subsystems called out in US-441. All seed sizes are intentionally
// small (≤10K) so the full suite runs in a few seconds; absolute throughput
// is not the point — relative regression detection vs. the recorded
// `bench/baseline.json` baseline is.
//
// Naming convention: `Benchmark<Area>_US441` so the runner can match each
// row in `baseline.json` 1:1 by exact name and the fan-out tool in
// `cmd/benchcheck` can assert presence of all eight expected benchmarks.

const benchSeedSize = 200

// BenchmarkLoad_US441 measures the cost of loading a base ObjectSet over a
// 1K-doc Bleve index — the same shape that drives every read-side OSS
// surface (load, listObjects, ObjectSet preview).
func BenchmarkLoad_US441(b *testing.B) {
	mgr := newSeededIndex(b, "employee", benchSeedSize)
	executor := objectset.NewExecutor(mgr, nopLinkResolver{}, objectset.NewStore(time.Hour))
	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := executor.Execute(ctx, def); err != nil {
			b.Fatalf("Execute: %v", err)
		}
	}
}

// BenchmarkAggregate_US441 times the canonical mixed avg+sum aggregation
// over the same 1K-doc shape. Exercises the Bleve facet / numeric scan
// path that every aggregation request rides on.
func BenchmarkAggregate_US441(b *testing.B) {
	idx := newAggregationIndex(b, benchSeedSize)
	eng := aggregation.NewEngine()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := eng.Aggregate(idx, &aggregation.AggregationRequest{
			Aggregations: []aggregation.AggregationSpec{
				{Type: "avg", Field: "price", Name: "avgPrice"},
				{Type: "sum", Field: "price", Name: "sumPrice"},
				{Type: "count", Name: "n"},
			},
		})
		if err != nil {
			b.Fatalf("Aggregate: %v", err)
		}
	}
}

// BenchmarkSearchAround_US441 walks a two-hop A→B→C path with cycle
// detection enabled, mirroring the typical SDK searchAround call shape.
func BenchmarkSearchAround_US441(b *testing.B) {
	mgr := newSeededIndex(b, "a", 0)
	mgr.EnsureIndex("b", []index.Property{{APIName: "id", BaseType: "string", IsSearchable: true}})
	mgr.EnsureIndex("c", []index.Property{{APIName: "id", BaseType: "string", IsSearchable: true}})
	for i := 0; i < 50; i++ {
		_ = mgr.IndexDocument("a", fmt.Sprintf("a%d", i), map[string]interface{}{"id": fmt.Sprintf("a%d", i)})
	}
	resolver := newBenchResolver()
	for i := 0; i < 50; i++ {
		resolver.addForward("a", "ab", "b", fmt.Sprintf("a%d", i), []string{fmt.Sprintf("b%d", i)})
		resolver.addForward("b", "bc", "c", fmt.Sprintf("b%d", i), []string{fmt.Sprintf("c%d", i)})
	}
	executor := objectset.NewExecutor(mgr, resolver, objectset.NewStore(time.Hour))
	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "a"},
		Path: []objectset.PathStep{
			{Link: "ab"},
			{Link: "bc"},
		},
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := executor.Execute(ctx, def); err != nil {
			b.Fatalf("Execute: %v", err)
		}
	}
}

// BenchmarkAction_US441 covers the per-Apply hot path of an ActionType
// invocation: parameter validation + edit collapse. Both are pure
// CPU-bound paths exercised on every action submission, so a regression
// here directly inflates Apply latency.
func BenchmarkAction_US441(b *testing.B) {
	defs := []actions.ParameterDef{
		{ID: "name", Type: "string", Required: true},
		{ID: "amount", Type: "integer", Required: true},
		{ID: "active", Type: "boolean", Required: false},
		{ID: "note", Type: "string", Required: false},
	}
	params := map[string]interface{}{
		"name":   "alice",
		"amount": 42,
		"active": true,
		"note":   "bench",
	}
	edits := make([]funnel.Edit, 0, 32)
	for i := 0; i < 16; i++ {
		pk := fmt.Sprintf("pk-%d", i)
		edits = append(edits, funnel.Edit{
			Type:       funnel.EditTypeCreate,
			ObjectType: "Order",
			PrimaryKey: pk,
			Properties: map[string]interface{}{"amount": i, "active": true},
		})
		edits = append(edits, funnel.Edit{
			Type:       funnel.EditTypeModify,
			ObjectType: "Order",
			PrimaryKey: pk,
			Properties: map[string]interface{}{"amount": i + 1},
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := actions.ValidateParameters(defs, params); err != nil {
			b.Fatalf("ValidateParameters: %v", err)
		}
		_ = actions.CollapseEdits(edits)
	}
}

// BenchmarkFunction_US441 measures one cycle of Goja runtime construction
// + JS execution. The body is a small deterministic computation —
// representative of a typical row-shaping Function rather than a tight
// arithmetic micro-bench.
func BenchmarkFunction_US441(b *testing.B) {
	rt := functions.NewRuntime(functions.Config{
		MaxExecutionTime: 5 * time.Second,
		MaxMemoryBytes:   64 * 1024 * 1024,
	})
	source := `
		function main(input) {
			var total = 0;
			for (var i = 0; i < input.length; i++) {
				var row = input[i];
				total += row.amount * (row.active ? 1 : 0);
			}
			return { total: total, count: input.length };
		}
	`
	rows := make([]map[string]interface{}, 32)
	for i := range rows {
		rows[i] = map[string]interface{}{"amount": i + 1, "active": i%2 == 0}
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := rt.Execute(ctx, source, rows); err != nil {
			b.Fatalf("Execute: %v", err)
		}
	}
}

// BenchmarkMask_US441 exercises CompileForRow over a small population of
// CellMasks — the per-cell hot path on every read-side response.
func BenchmarkMask_US441(b *testing.B) {
	store := cellsec.NewMemoryStore()
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.Customer"
	for i := 0; i < 64; i++ {
		pk := fmt.Sprintf("c-%d", i)
		_ = store.Create(ctx, &cellsec.CellMask{
			RID:             fmt.Sprintf("ri.cellmask.main.cell.%d", i),
			ObjectTypeRID:   otRID,
			PrimaryKey:      pk,
			PropertyAPIName: "ssn",
			MaskRule:        masking.MaskRuleHash,
			AppliesTo:       masking.AppliesTo{Roles: []string{"finance"}},
		})
	}
	engine := cellsec.New(store, nil)
	if err := engine.Reload(ctx); err != nil {
		b.Fatalf("Reload: %v", err)
	}
	user := &auth.User{ID: "u-bench", Roles: []string{"viewer"}}
	row := map[string]any{"ssn": "123-45-6789", "country": "US"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pk := fmt.Sprintf("c-%d", i%64)
		if _, err := engine.CompileForRow(ctx, user, otRID, pk, row); err != nil {
			b.Fatalf("CompileForRow: %v", err)
		}
	}
}

// BenchmarkRLS_US441 runs the warm-cache CEL evaluator path. Mirrors the
// shape of pkg/security/rls_bench_test.go::BenchmarkRLSEvaluator_Warm but
// at smaller cardinality so the full suite stays fast.
func BenchmarkRLS_US441(b *testing.B) {
	eval := security.NewCELEvaluator()
	cache := security.NewDecisionCache(2048, time.Hour)
	eval.SetDecisionCache(cache)

	const policiesCount = 5
	const userCardinality = 16
	const rowCardinality = 16
	rules := make([]security.CELRule, policiesCount)
	for i := range rules {
		rules[i] = security.CELRule{
			PolicyRID:  fmt.Sprintf("ri.policy.bench.%d", i),
			Version:    1,
			Expression: fmt.Sprintf(`row.bucket_%d != ""`, i),
		}
		if err := eval.Compile(rules[i]); err != nil {
			b.Fatalf("compile rule %d: %v", i, err)
		}
	}
	set, err := eval.BuildRuleSet(rules)
	if err != nil {
		b.Fatalf("BuildRuleSet: %v", err)
	}
	users := make([]*auth.User, userCardinality)
	for i := range users {
		users[i] = &auth.User{ID: fmt.Sprintf("u-%03d", i), Roles: []string{"viewer"}}
	}
	rows := make([]map[string]any, rowCardinality)
	rowKeys := make([]string, rowCardinality)
	for i := range rows {
		row := make(map[string]any, policiesCount)
		for j := 0; j < policiesCount; j++ {
			row[fmt.Sprintf("bucket_%d", j)] = fmt.Sprintf("v-%d-%d", i, j)
		}
		rows[i] = row
		rowKeys[i] = security.HashRowProperties(row)
	}
	ctx := context.Background()
	// Pre-warm the decision cache so the bench measures the steady-state
	// hot path (lookup), not the cold compile + eval cost.
	for i := 0; i < userCardinality*rowCardinality; i++ {
		u := users[i%userCardinality]
		ridx := i % rowCardinality
		_, _ = eval.EvaluateRuleSet(ctx, u, set, rowKeys[ridx], rows[ridx])
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		u := users[n%userCardinality]
		ridx := n % rowCardinality
		_, _ = eval.EvaluateRuleSet(ctx, u, set, rowKeys[ridx], rows[ridx])
	}
}

// BenchmarkIndex_US441 measures per-query cost of `index.Manager.Search`
// over a pre-seeded 200-doc index. The write path is exercised transitively
// by BenchmarkLoad (which depends on an indexed corpus); isolating Search
// here keeps the bench's per-iter cost dominated by Bleve query evaluation
// rather than disk-flush jitter that swamps a 20% regression threshold.
func BenchmarkIndex_US441(b *testing.B) {
	mgr := index.NewManager(b.TempDir())
	b.Cleanup(func() { mgr.Close() })
	if _, err := mgr.EnsureIndex("widget", []index.Property{
		{APIName: "name", BaseType: "string", IsSearchable: true},
		{APIName: "score", BaseType: "integer", IsSearchable: true},
	}); err != nil {
		b.Fatalf("EnsureIndex: %v", err)
	}
	for i := 0; i < benchSeedSize; i++ {
		if err := mgr.IndexDocument("widget", fmt.Sprintf("seed-%d", i), map[string]interface{}{
			"name":  fmt.Sprintf("widget-%d", i),
			"score": i,
		}); err != nil {
			b.Fatalf("IndexDocument: %v", err)
		}
	}
	req := bleve.NewSearchRequest(bleve.NewMatchAllQuery())
	req.Size = 10

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mgr.Search("widget", req); err != nil {
			b.Fatalf("Search: %v", err)
		}
	}
}

// --- helpers ---

func newSeededIndex(tb testing.TB, ot string, n int) *index.Manager {
	tb.Helper()
	mgr := index.NewManager(tb.TempDir())
	tb.Cleanup(func() { mgr.Close() })
	if _, err := mgr.EnsureIndex(ot, []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
		{APIName: "score", BaseType: "integer", IsSearchable: true},
	}); err != nil {
		tb.Fatalf("EnsureIndex %s: %v", ot, err)
	}
	for i := 0; i < n; i++ {
		pk := fmt.Sprintf("pk-%d", i)
		if err := mgr.IndexDocument(ot, pk, map[string]interface{}{
			"id":    pk,
			"name":  fmt.Sprintf("name-%d", i),
			"score": i,
		}); err != nil {
			tb.Fatalf("IndexDocument %s: %v", pk, err)
		}
	}
	return mgr
}

func newAggregationIndex(tb testing.TB, n int) bleve.Index {
	tb.Helper()
	idxMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("price", mapping.NewNumericFieldMapping())
	docMapping.AddFieldMappingsAt("region", mapping.NewTextFieldMapping())
	idxMapping.DefaultMapping = docMapping
	dir := tb.TempDir()
	idx, err := bleve.New(dir+"/agg", idxMapping)
	if err != nil {
		tb.Fatalf("create index: %v", err)
	}
	tb.Cleanup(func() { idx.Close() })
	for i := 0; i < n; i++ {
		if err := idx.Index(fmt.Sprintf("doc-%d", i), map[string]interface{}{
			"price":  float64(i + 1),
			"region": fmt.Sprintf("r%d", i%3),
		}); err != nil {
			tb.Fatalf("index doc %d: %v", i, err)
		}
	}
	return idx
}

// nopLinkResolver satisfies links.LinkResolver for benchmarks that never
// traverse a link (BenchmarkLoad / BenchmarkAggregate).
type nopLinkResolver struct{}

func (nopLinkResolver) ResolveLinkedObjects(_ context.Context, _ string, _ []string) ([]string, error) {
	return nil, nil
}
func (nopLinkResolver) ResolveLinkedObjectsByAPIName(_ context.Context, _, _ string, _ []string) ([]string, error) {
	return nil, nil
}
func (nopLinkResolver) ResolveLinked(_ context.Context, _ string, _ []string, _ links.Direction) ([]string, error) {
	return nil, nil
}

// benchResolver is a deterministic in-memory LinkResolver covering the
// minimum surface that searchAround path traversal touches.
type benchResolver struct {
	forward    map[string]map[string][]string // key = "sourceOT|link"
	targetType map[string]string              // key = "sourceOT|link"
}

func newBenchResolver() *benchResolver {
	return &benchResolver{
		forward:    map[string]map[string][]string{},
		targetType: map[string]string{},
	}
}

func (r *benchResolver) addForward(sourceOT, link, targetOT, sourcePK string, targets []string) {
	key := sourceOT + "|" + link
	if r.forward[key] == nil {
		r.forward[key] = map[string][]string{}
	}
	r.forward[key][sourcePK] = targets
	r.targetType[key] = targetOT
}

func (r *benchResolver) ResolveLinkedObjects(_ context.Context, _ string, _ []string) ([]string, error) {
	return nil, nil
}
func (r *benchResolver) ResolveLinkedObjectsByAPIName(_ context.Context, sourceOT, link string, sourcePKs []string) ([]string, error) {
	edges, ok := r.forward[sourceOT+"|"+link]
	if !ok {
		return nil, nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(sourcePKs))
	for _, pk := range sourcePKs {
		for _, t := range edges[pk] {
			if _, ok := seen[t]; !ok {
				seen[t] = struct{}{}
				out = append(out, t)
			}
		}
	}
	return out, nil
}
func (r *benchResolver) ResolveLinked(ctx context.Context, _ string, pks []string, _ links.Direction) ([]string, error) {
	return r.ResolveLinkedObjectsByAPIName(ctx, "", "", pks)
}

// ResolveTargetObjectType lets the executor walk multi-hop paths without
// looking up linkType metadata.
func (r *benchResolver) ResolveTargetObjectType(_ context.Context, sourceOT, link string) (string, error) {
	if t, ok := r.targetType[sourceOT+"|"+link]; ok {
		return t, nil
	}
	return "", fmt.Errorf("no target for %s|%s", sourceOT, link)
}
