package links_test

import (
	"context"
	"testing"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/oms"
)

// --- Direction parsing ---

func TestParseDirection(t *testing.T) {
	cases := []struct {
		in      string
		want    links.Direction
		wantErr bool
	}{
		{"", links.DirectionForward, false},
		{"forward", links.DirectionForward, false},
		{"reverse", links.DirectionReverse, false},
		{"FORWARD", links.DirectionForward, true}, // case-sensitive on purpose
		{"invalid", links.DirectionForward, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := links.ParseDirection(tc.in)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q", tc.in)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("ParseDirection(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestDirection_String(t *testing.T) {
	if links.DirectionForward.String() != "forward" {
		t.Errorf("DirectionForward.String() = %q, want %q", links.DirectionForward.String(), "forward")
	}
	if links.DirectionReverse.String() != "reverse" {
		t.Errorf("DirectionReverse.String() = %q, want %q", links.DirectionReverse.String(), "reverse")
	}
}

// --- FK reverse traversal ---

// TestResolveLinked_FKReverse exercises the new reverse-direction FK path:
// given department PKs, return the employees whose deptid matches.
func TestResolveLinked_FKReverse(t *testing.T) {
	resolver := setupEmployeeDept(t)
	ctx := context.Background()

	// Reverse on "ri.lt.emp-dept" (emp -> dept): given dept d1, should
	// return emp1 and emp2 (the employees whose deptid=d1).
	pks, err := resolver.ResolveLinked(ctx, "ri.lt.emp-dept", []string{"d1"}, links.DirectionReverse)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pks) != 2 {
		t.Fatalf("expected 2 source PKs for reverse traversal, got %d: %v", len(pks), pks)
	}
	found := map[string]bool{}
	for _, pk := range pks {
		found[pk] = true
	}
	if !found["emp1"] || !found["emp2"] {
		t.Errorf("expected {emp1, emp2} for reverse dept->emp, got %v", pks)
	}
}

func TestResolveLinked_FKForwardBackwardCompat(t *testing.T) {
	// DirectionForward must produce the same result as the legacy
	// ResolveLinkedObjects call — protects backwards compatibility.
	resolver := setupEmployeeDept(t)
	ctx := context.Background()

	legacy, err := resolver.ResolveLinkedObjects(ctx, "ri.lt.emp-dept", []string{"emp1"})
	if err != nil {
		t.Fatalf("legacy resolve: %v", err)
	}
	modern, err := resolver.ResolveLinked(ctx, "ri.lt.emp-dept", []string{"emp1"}, links.DirectionForward)
	if err != nil {
		t.Fatalf("ResolveLinked forward: %v", err)
	}
	if len(legacy) != len(modern) {
		t.Fatalf("legacy vs ResolveLinked forward length mismatch: %v vs %v", legacy, modern)
	}
	for i := range legacy {
		if legacy[i] != modern[i] {
			t.Errorf("legacy[%d]=%q, modern[%d]=%q", i, legacy[i], i, modern[i])
		}
	}
}

func TestResolveLinked_FKReverseEmpty(t *testing.T) {
	resolver := setupEmployeeDept(t)
	ctx := context.Background()

	pks, err := resolver.ResolveLinked(ctx, "ri.lt.emp-dept", []string{}, links.DirectionReverse)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pks != nil {
		t.Errorf("expected nil for empty reverse PKs, got %v", pks)
	}
}

// --- M2M via join table ---

// stubEdgeRepo is an in-memory EdgeRepository for testing the M2M resolver
// path without a real PostgreSQL.
type stubEdgeRepo struct {
	// edges[linkTypeRID] = [(source, target), ...]
	edges map[string][][2]string
}

func (s *stubEdgeRepo) ListEdgeTargets(_ context.Context, linkTypeRID string, sourcePKs []string) ([]string, error) {
	src := map[string]bool{}
	for _, p := range sourcePKs {
		src[p] = true
	}
	seen := map[string]bool{}
	var result []string
	for _, e := range s.edges[linkTypeRID] {
		if src[e[0]] && !seen[e[1]] {
			result = append(result, e[1])
			seen[e[1]] = true
		}
	}
	return result, nil
}

func (s *stubEdgeRepo) ListEdgeSources(_ context.Context, linkTypeRID string, targetPKs []string) ([]string, error) {
	tgt := map[string]bool{}
	for _, p := range targetPKs {
		tgt[p] = true
	}
	seen := map[string]bool{}
	var result []string
	for _, e := range s.edges[linkTypeRID] {
		if tgt[e[1]] && !seen[e[0]] {
			result = append(result, e[0])
			seen[e[0]] = true
		}
	}
	return result, nil
}

// setupOrderProductsM2M constructs a resolver with an M2M link modelled on
// Northwind's order_details table.
func setupOrderProductsM2M(t *testing.T) (*links.Resolver, *stubEdgeRepo) {
	t.Helper()

	mgr := index.NewManager(t.TempDir())
	t.Cleanup(func() { mgr.Close() })

	// Indexes are required by the resolver infrastructure but our stub
	// EdgeRepository doesn't consult them for M2M — they are only here so
	// NewResolverWithEdges has a valid IndexManager.
	_, _ = mgr.EnsureIndex("orders", []index.Property{{APIName: "orderid", BaseType: "string", IsSearchable: true}})
	_, _ = mgr.EnsureIndex("products", []index.Property{{APIName: "productid", BaseType: "string", IsSearchable: true}})

	repo := &mockRepo{
		objectTypes: map[string]*oms.ObjectType{
			"ri.ot.orders":   {RID: "ri.ot.orders", APIName: "orders", PrimaryKey: "orderid"},
			"ri.ot.products": {RID: "ri.ot.products", APIName: "products", PrimaryKey: "productid"},
		},
		linkTypes: map[string]*oms.LinkType{
			"ri.lt.order-products": {
				RID:              "ri.lt.order-products",
				APIName:          "orderProducts",
				SourceObjectType: "ri.ot.orders",
				TargetObjectType: "ri.ot.products",
				Cardinality:      "MANY_TO_MANY",
			},
		},
		outgoing: map[string][]oms.LinkType{},
	}

	// Edges modelled on Northwind order_details:
	//   order 10248 -> products 11, 42, 72
	//   order 10249 -> products 14, 51
	//   order 10250 -> products 41, 51, 65
	// Reverse: product 51 -> orders 10249, 10250
	edges := &stubEdgeRepo{
		edges: map[string][][2]string{
			"ri.lt.order-products": {
				{"10248", "11"},
				{"10248", "42"},
				{"10248", "72"},
				{"10249", "14"},
				{"10249", "51"},
				{"10250", "41"},
				{"10250", "51"},
				{"10250", "65"},
			},
		},
	}

	return links.NewResolverWithEdges(repo, mgr, edges), edges
}

func TestResolveLinked_M2MForward(t *testing.T) {
	resolver, _ := setupOrderProductsM2M(t)
	ctx := context.Background()

	// Given order 10248, expect 3 products.
	pks, err := resolver.ResolveLinked(ctx, "ri.lt.order-products", []string{"10248"}, links.DirectionForward)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pks) != 3 {
		t.Fatalf("expected 3 products for order 10248, got %d: %v", len(pks), pks)
	}

	// Given orders 10249 + 10250, expect a deduplicated union of products.
	pks, err = resolver.ResolveLinked(ctx, "ri.lt.order-products", []string{"10249", "10250"}, links.DirectionForward)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Union is {14, 51, 41, 65} — 51 appears twice in source edges but is deduped.
	if len(pks) != 4 {
		t.Errorf("expected 4 deduped products, got %d: %v", len(pks), pks)
	}
}

func TestResolveLinked_M2MReverse(t *testing.T) {
	resolver, _ := setupOrderProductsM2M(t)
	ctx := context.Background()

	// Given product 51, reverse should give orders 10249 and 10250.
	pks, err := resolver.ResolveLinked(ctx, "ri.lt.order-products", []string{"51"}, links.DirectionReverse)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pks) != 2 {
		t.Fatalf("expected 2 orders for product 51, got %d: %v", len(pks), pks)
	}
	found := map[string]bool{}
	for _, pk := range pks {
		found[pk] = true
	}
	if !found["10249"] || !found["10250"] {
		t.Errorf("expected {10249, 10250}, got %v", pks)
	}
}

func TestResolveLinked_M2MWithoutEdgeRepo(t *testing.T) {
	// An M2M link without a configured EdgeRepository must return a clear error.
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo := &mockRepo{
		objectTypes: make(map[string]*oms.ObjectType),
		linkTypes: map[string]*oms.LinkType{
			"ri.lt.m2m": {
				RID:              "ri.lt.m2m",
				APIName:          "manytomany",
				SourceObjectType: "ri.ot.a",
				TargetObjectType: "ri.ot.b",
				Cardinality:      "MANY_TO_MANY",
			},
		},
		outgoing: make(map[string][]oms.LinkType),
	}

	resolver := links.NewResolver(repo, mgr) // no edge repo
	_, err := resolver.ResolveLinked(context.Background(), "ri.lt.m2m", []string{"a1"}, links.DirectionForward)
	if err == nil {
		t.Fatal("expected error for M2M without edge repository")
	}
}

// --- US-209 bidirectional links ---

// TestBidirectionalLinks_SymmetricTraversal verifies that an A↔B LinkType pair
// (with inverse_link_rid cross-references) yields mirrored results regardless
// of which side is walked: forward on A should equal reverse on B, and
// forward on B should equal reverse on A.
func TestBidirectionalLinks_SymmetricTraversal(t *testing.T) {
	resolver := setupEmployeeDept(t)
	ctx := context.Background()

	// 1. Metadata: both LinkTypes cross-reference each other via InverseLinkRID.
	a, err := resolver.ResolveLinked(ctx, "ri.lt.emp-dept", []string{"emp1"}, links.DirectionForward)
	if err != nil {
		t.Fatalf("forward A: %v", err)
	}
	b, err := resolver.ResolveLinked(ctx, "ri.lt.dept-emp", []string{"emp1"}, links.DirectionReverse)
	if err != nil {
		t.Fatalf("reverse B: %v", err)
	}
	if !sameSet(a, b) {
		t.Errorf("forward(A, emp1) vs reverse(B, emp1) mismatch: %v vs %v", a, b)
	}

	// 2. Forward B from d1 == Reverse A from d1 — both should give {emp1, emp2}.
	fwdB, err := resolver.ResolveLinked(ctx, "ri.lt.dept-emp", []string{"d1"}, links.DirectionForward)
	if err != nil {
		t.Fatalf("forward B: %v", err)
	}
	revA, err := resolver.ResolveLinked(ctx, "ri.lt.emp-dept", []string{"d1"}, links.DirectionReverse)
	if err != nil {
		t.Fatalf("reverse A: %v", err)
	}
	if !sameSet(fwdB, revA) {
		t.Errorf("forward(B, d1) vs reverse(A, d1) mismatch: %v vs %v", fwdB, revA)
	}
	if !sameSet(fwdB, []string{"emp1", "emp2"}) {
		t.Errorf("forward(B, d1) = %v, want {emp1, emp2}", fwdB)
	}
}

// TestBidirectionalLinks_RoundTripReturnsSource verifies that forward-then-
// inverse-forward traversal through a paired LinkType comes back to include
// the original source set. This is the canonical "bidirectional" property.
func TestBidirectionalLinks_RoundTripReturnsSource(t *testing.T) {
	resolver := setupEmployeeDept(t)
	ctx := context.Background()

	// emp1 -> forward(A) -> d1 -> forward(B) -> {emp1, emp2}
	hop1, err := resolver.ResolveLinked(ctx, "ri.lt.emp-dept", []string{"emp1"}, links.DirectionForward)
	if err != nil {
		t.Fatalf("hop1: %v", err)
	}
	hop2, err := resolver.ResolveLinked(ctx, "ri.lt.dept-emp", hop1, links.DirectionForward)
	if err != nil {
		t.Fatalf("hop2: %v", err)
	}
	found := map[string]bool{}
	for _, pk := range hop2 {
		found[pk] = true
	}
	if !found["emp1"] {
		t.Errorf("round-trip should include originating emp1, got %v", hop2)
	}
}

// sameSet returns true when a and b contain the same elements, order-agnostic.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, x := range a {
		seen[x]++
	}
	for _, x := range b {
		seen[x]--
	}
	for _, v := range seen {
		if v != 0 {
			return false
		}
	}
	return true
}

func TestResolveViaJoinTable_WrongCardinality(t *testing.T) {
	// The exported helper must reject non-M2M link types.
	lt := &oms.LinkType{
		RID:         "ri.lt.fk",
		APIName:     "fk",
		Cardinality: "ONE_TO_MANY",
	}
	edgeRepo := &stubEdgeRepo{}
	_, err := links.ResolveViaJoinTable(context.Background(), edgeRepo, lt, []string{"a"}, links.DirectionForward)
	if err == nil {
		t.Fatal("expected error for ONE_TO_MANY passed to join-table resolver")
	}
}
