package security

import (
	"context"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2/search/query"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
)

// TestPolicyCacheHitAndInvalidate — Red test for US-045: compiled policy
// results must be cached per (userID, objectTypeRID, policyVersion) so a
// second Evaluate call for the same triple is a hit. Updating policies
// via SetPolicies must bump the version and invalidate stale entries so the
// next Evaluate recompiles (miss), not serves a stale result.
func TestPolicyCacheHitAndInvalidate(t *testing.T) {
	ot := oms.ObjectType{
		RID:     "ri.ontology.main.object-type.employee",
		APIName: "Employee",
	}

	engine := NewEngine()
	cache := NewPolicyCache(16, 5*time.Minute)
	engine.SetCache(cache)

	engine.SetPolicies(ot.RID, []Policy{{
		RID:           "p1",
		ObjectTypeRID: ot.RID,
		PolicyType:    PolicyTypeObject,
		Rules: []Rule{{
			Type:           RuleTypeEq,
			UserAttr:       "dept",
			ObjectProperty: "owner_dept",
		}},
	}})

	v1 := engine.PolicyVersion(ot.RID)
	if v1 <= 0 {
		t.Fatalf("SetPolicies must bump version, got %d", v1)
	}

	user := &auth.User{
		ID:         "u-alice",
		Attributes: map[string]any{"dept": "ENG"},
	}
	ctx := context.Background()

	q1, err := engine.Evaluate(ctx, user, ot)
	if err != nil {
		t.Fatalf("first Evaluate: %v", err)
	}
	if _, ok := q1.(*query.TermQuery); !ok {
		t.Fatalf("first Evaluate returned %T, want *query.TermQuery", q1)
	}
	st := cache.Stats()
	if st.Hits != 0 || st.Misses != 1 {
		t.Fatalf("after first Evaluate: hits=%d misses=%d, want 0/1", st.Hits, st.Misses)
	}

	q2, err := engine.Evaluate(ctx, user, ot)
	if err != nil {
		t.Fatalf("second Evaluate: %v", err)
	}
	if q2 != q1 {
		t.Fatalf("second Evaluate should return the cached Query instance")
	}
	st = cache.Stats()
	if st.Hits != 1 || st.Misses != 1 {
		t.Fatalf("after second Evaluate: hits=%d misses=%d, want 1/1", st.Hits, st.Misses)
	}

	engine.SetPolicies(ot.RID, []Policy{{
		RID:           "p1",
		ObjectTypeRID: ot.RID,
		PolicyType:    PolicyTypeObject,
		Rules: []Rule{{
			Type:           RuleTypeEq,
			UserAttr:       "region",
			ObjectProperty: "region",
		}},
	}})

	v2 := engine.PolicyVersion(ot.RID)
	if v2 == v1 {
		t.Fatalf("SetPolicies did not bump version: still %d", v2)
	}

	user2 := &auth.User{
		ID: "u-alice",
		Attributes: map[string]any{
			"dept":   "ENG",
			"region": "APAC",
		},
	}
	q3, err := engine.Evaluate(ctx, user2, ot)
	if err != nil {
		t.Fatalf("third Evaluate: %v", err)
	}
	tq, ok := q3.(*query.TermQuery)
	if !ok {
		t.Fatalf("third Evaluate returned %T, want *query.TermQuery", q3)
	}
	if tq.FieldVal != "region" || tq.Term != "APAC" {
		t.Fatalf("third Evaluate term=%q field=%q, want APAC/region", tq.Term, tq.FieldVal)
	}
	st = cache.Stats()
	if st.Hits != 1 || st.Misses != 2 {
		t.Fatalf("after policy update: hits=%d misses=%d, want 1/2", st.Hits, st.Misses)
	}

	if stale, ok := cache.Get("u-alice", ot.RID, v1); ok {
		t.Fatalf("stale version %d still in cache: %T", v1, stale)
	}
}

// TestPolicyCacheTTLExpiry — entries past their TTL behave like a miss, even
// while the policy version is unchanged. Uses an injected clock to avoid any
// wall-clock sleeps.
func TestPolicyCacheTTLExpiry(t *testing.T) {
	now := time.Date(2026, 4, 12, 10, 0, 0, 0, time.UTC)
	cache := NewPolicyCache(4, 1*time.Minute)
	cache.now = func() time.Time { return now }

	q := newTestTermQuery("dept", "ENG")
	cache.Put("u-alice", "ri.ot.employee", 1, q)

	if got, ok := cache.Get("u-alice", "ri.ot.employee", 1); !ok || got != q {
		t.Fatalf("immediate Get: ok=%v got=%v", ok, got)
	}

	now = now.Add(2 * time.Minute)
	if _, ok := cache.Get("u-alice", "ri.ot.employee", 1); ok {
		t.Fatalf("expected TTL expiry after 2m, got hit")
	}
	st := cache.Stats()
	if st.Hits != 1 || st.Misses != 1 {
		t.Fatalf("stats after ttl expiry: %+v", st)
	}
}

// TestPolicyCacheLRUEviction — when capacity is exceeded the least-recently
// used entry is evicted.
func TestPolicyCacheLRUEviction(t *testing.T) {
	cache := NewPolicyCache(2, 5*time.Minute)

	q1 := newTestTermQuery("f", "a")
	q2 := newTestTermQuery("f", "b")
	q3 := newTestTermQuery("f", "c")

	cache.Put("u1", "ri.ot.x", 1, q1)
	cache.Put("u2", "ri.ot.x", 1, q2)
	// Access q1 so q2 becomes the LRU entry.
	if _, ok := cache.Get("u1", "ri.ot.x", 1); !ok {
		t.Fatalf("expected q1 in cache")
	}
	cache.Put("u3", "ri.ot.x", 1, q3)

	if _, ok := cache.Get("u2", "ri.ot.x", 1); ok {
		t.Fatalf("u2 should have been evicted as LRU")
	}
	if _, ok := cache.Get("u1", "ri.ot.x", 1); !ok {
		t.Fatalf("u1 should still be cached")
	}
	if _, ok := cache.Get("u3", "ri.ot.x", 1); !ok {
		t.Fatalf("u3 should still be cached")
	}
}

// TestPolicyCacheHitRate — HitRate exposes the hits / (hits+misses) ratio so
// an external metric exporter can publish it.
func TestPolicyCacheHitRate(t *testing.T) {
	cache := NewPolicyCache(8, 5*time.Minute)

	if got := cache.HitRate(); got != 0 {
		t.Fatalf("empty cache HitRate = %v, want 0", got)
	}

	q := newTestTermQuery("f", "a")
	cache.Put("u1", "ri.ot.x", 1, q)

	// 1 miss (before Put is irrelevant; Put does not count), then 2 hits.
	if _, ok := cache.Get("u1", "ri.ot.x", 1); !ok {
		t.Fatalf("expected hit after Put")
	}
	if _, ok := cache.Get("u1", "ri.ot.x", 1); !ok {
		t.Fatalf("expected hit after Put")
	}
	if _, ok := cache.Get("u-missing", "ri.ot.x", 1); ok {
		t.Fatalf("expected miss")
	}

	rate := cache.HitRate()
	want := 2.0 / 3.0
	if rate < want-1e-9 || rate > want+1e-9 {
		t.Fatalf("HitRate = %v, want %v", rate, want)
	}
}

// TestPolicyCacheInvalidateObjectType — invalidating one ObjectType must not
// disturb entries for other ObjectTypes.
func TestPolicyCacheInvalidateObjectType(t *testing.T) {
	cache := NewPolicyCache(16, 5*time.Minute)
	q := newTestTermQuery("f", "a")

	cache.Put("u1", "ri.ot.x", 1, q)
	cache.Put("u1", "ri.ot.y", 1, q)
	cache.Put("u2", "ri.ot.x", 1, q)

	cache.InvalidateObjectType("ri.ot.x")

	if _, ok := cache.Get("u1", "ri.ot.x", 1); ok {
		t.Fatalf("u1/x should be invalidated")
	}
	if _, ok := cache.Get("u2", "ri.ot.x", 1); ok {
		t.Fatalf("u2/x should be invalidated")
	}
	if _, ok := cache.Get("u1", "ri.ot.y", 1); !ok {
		t.Fatalf("u1/y must survive x-scoped invalidation")
	}
}

func newTestTermQuery(field, term string) query.Query {
	tq := query.NewTermQuery(term)
	tq.SetField(field)
	return tq
}
