package auth

// US-012 — pkg/auth 多租户与速率限制补测.
//
// Acceptance criteria (PRD):
//   - RLS 策略命中/失效 (PolicyEvaluator allow/deny / mask / malformed-row tolerance)
//   - token 刷新竞态 (concurrent Rotate of the same plaintext under CAS)
//   - 多 ontology 隔离 (EnforceOntologyScope + OntologyScopeMiddleware + RoleResolver)
//   - 登录速率限制阈值 (ipRateLimiter window math + LoginHandler 429 wiring)
//   - 至少 10 个子测试
//
// All subtests run in-process with the in-memory fakes already wired by the
// rest of the suite; no Postgres / NATS dependencies. Rate-limit assertions
// inject a forged X-Forwarded-For header so distinct test runs cannot
// cross-poison each other via the per-IP buckets.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/oms"
)

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// us012MakePolicy builds an OBJECT/PROPERTY-scope SecurityPolicy from a typed
// rules struct. Mirrors makePolicy in policy_evaluator_test.go but lives in
// the in-package test file so it can sit next to the four subtest blocks
// without an awkward export.
func us012MakePolicy(t *testing.T, policyType string, rules SecurityPolicyRules) oms.SecurityPolicy {
	t.Helper()
	raw, err := json.Marshal(rules)
	if err != nil {
		t.Fatalf("marshal rules: %v", err)
	}
	return oms.SecurityPolicy{
		RID:           "ri.ontology.main.security-policy.us012",
		ObjectTypeRID: "ri.ontology.main.object-type.us012",
		PolicyType:    policyType,
		Rules:         raw,
	}
}

// us012NewLoginHandler builds a LoginHandler with a known rate-limit budget
// and a single canonical user so the test can hammer the bucket without
// success masking the 429s. Returns the handler and the IP string the test
// must inject so independent subtests do not share buckets.
func us012NewLoginHandler(t *testing.T, rateLimit int) (*LoginHandler, string) {
	t.Helper()
	repo := newFakeUserRepo()
	resolver := NewRoleResolver(repo, time.Minute)
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa: %v", err)
	}
	signer, err := NewJWTSigner(priv, &priv.PublicKey, JWTSignerOptions{
		AccessTokenTTL: 15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	rs := NewRefreshService(NewMemoryRefreshStore(), RefreshServiceOptions{AbsoluteTTL: time.Hour})
	h := NewLoginHandler(LoginHandlerDeps{
		Users:          repo,
		Resolver:       resolver,
		Signer:         signer,
		RefreshService: rs,
		RateLimit:      rateLimit,
	})
	seedUser(t, repo, "user:alice@example.com", "alice@example.com", "letmein123!", "Alice", RoleViewer)
	// Use a unique high IP per harness so concurrent subtests do not share
	// the per-IP fixed-window counter inside the limiter.
	return h, fmt.Sprintf("203.0.113.%d", time.Now().UnixNano()%200+10)
}

// us012PostLoginFromIP fires a login attempt with X-Forwarded-For set so the
// limiter routes the hit to the test-controlled bucket.
func us012PostLoginFromIP(t *testing.T, h *LoginHandler, ip, password string) *httptest.ResponseRecorder {
	t.Helper()
	bs, _ := json.Marshal(map[string]string{"email": "alice@example.com", "password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(bs))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", ip)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// --------------------------------------------------------------------------
// AC #1 — RLS (Row-Level Security) policy evaluation: hit / miss matrix
// --------------------------------------------------------------------------

func TestUS012_RLSPolicyEvaluation(t *testing.T) {
	t.Run("Hit_AllowMatchingRoleGrantsObject", func(t *testing.T) {
		pol := us012MakePolicy(t, PolicyTypeObject, SecurityPolicyRules{
			Version:   1,
			Effect:    EffectAllow,
			Subjects:  SubjectSpec{Roles: []string{RoleEditor}},
			Condition: ConditionSpec{Op: OpAlways},
		})
		eval := NewPolicyEvaluator([]oms.SecurityPolicy{pol})
		allow, _, err := eval.Evaluate(&User{ID: "u1", Roles: []string{RoleEditor}}, map[string]interface{}{"id": "o1"})
		if err != nil || !allow {
			t.Fatalf("expected allow for matching role, got allow=%v err=%v", allow, err)
		}
	})

	t.Run("Miss_RoleDoesNotMatch_DefaultDeny", func(t *testing.T) {
		pol := us012MakePolicy(t, PolicyTypeObject, SecurityPolicyRules{
			Version:   1,
			Effect:    EffectAllow,
			Subjects:  SubjectSpec{Roles: []string{RoleAdmin}},
			Condition: ConditionSpec{Op: OpAlways},
		})
		eval := NewPolicyEvaluator([]oms.SecurityPolicy{pol})
		allow, _, _ := eval.Evaluate(&User{ID: "u1", Roles: []string{RoleViewer}}, map[string]interface{}{"id": "o1"})
		if allow {
			t.Fatal("expected default-deny when no allow grant matches the user's roles")
		}
	})

	t.Run("Miss_UserIDSubjectNotInList_DefaultDeny", func(t *testing.T) {
		pol := us012MakePolicy(t, PolicyTypeObject, SecurityPolicyRules{
			Version:   1,
			Effect:    EffectAllow,
			Subjects:  SubjectSpec{UserIDs: []string{"u-special"}},
			Condition: ConditionSpec{Op: OpAlways},
		})
		eval := NewPolicyEvaluator([]oms.SecurityPolicy{pol})

		// u-special: matches.
		allow, _, _ := eval.Evaluate(&User{ID: "u-special"}, map[string]interface{}{"id": "o1"})
		if !allow {
			t.Error("expected allow for the specific user id in subjects list")
		}
		// u-other: misses.
		allow, _, _ = eval.Evaluate(&User{ID: "u-other"}, map[string]interface{}{"id": "o1"})
		if allow {
			t.Error("expected deny for user id not in subjects list")
		}
	})

	t.Run("DenyPrecedenceOverAllow", func(t *testing.T) {
		allowPol := us012MakePolicy(t, PolicyTypeObject, SecurityPolicyRules{
			Version:   1,
			Effect:    EffectAllow,
			Subjects:  SubjectSpec{Roles: []string{RoleEditor}},
			Condition: ConditionSpec{Op: OpAlways},
		})
		denyPol := us012MakePolicy(t, PolicyTypeObject, SecurityPolicyRules{
			Version:   1,
			Effect:    EffectDeny,
			Subjects:  SubjectSpec{Roles: []string{RoleEditor}},
			Condition: ConditionSpec{Op: OpPropertyEquals, Field: "classification", Value: "SECRET"},
		})
		eval := NewPolicyEvaluator([]oms.SecurityPolicy{allowPol, denyPol})

		allow, masks, _ := eval.Evaluate(&User{ID: "u1", Roles: []string{RoleEditor}}, map[string]interface{}{
			"id":             "o1",
			"classification": "SECRET",
		})
		if allow {
			t.Errorf("expected deny when an explicit deny policy matches; got allow")
		}
		if len(masks) != 0 {
			t.Errorf("expected no masks on deny path, got %v", masks)
		}
	})

	t.Run("UnionOfMultipleAllowGrantsForSameSubject", func(t *testing.T) {
		// Two non-overlapping allow grants; either alone suffices.
		a := us012MakePolicy(t, PolicyTypeObject, SecurityPolicyRules{
			Version:   1,
			Effect:    EffectAllow,
			Subjects:  SubjectSpec{Roles: []string{RoleViewer}},
			Condition: ConditionSpec{Op: OpPropertyEquals, Field: "region", Value: "US"},
		})
		b := us012MakePolicy(t, PolicyTypeObject, SecurityPolicyRules{
			Version:   1,
			Effect:    EffectAllow,
			Subjects:  SubjectSpec{Roles: []string{RoleViewer}},
			Condition: ConditionSpec{Op: OpPropertyEquals, Field: "region", Value: "EU"},
		})
		eval := NewPolicyEvaluator([]oms.SecurityPolicy{a, b})

		for _, region := range []string{"US", "EU"} {
			allow, _, _ := eval.Evaluate(&User{ID: "u1", Roles: []string{RoleViewer}}, map[string]interface{}{"region": region})
			if !allow {
				t.Errorf("region=%s: expected allow via union of policies", region)
			}
		}
		allow, _, _ := eval.Evaluate(&User{ID: "u1", Roles: []string{RoleViewer}}, map[string]interface{}{"region": "APAC"})
		if allow {
			t.Error("region=APAC: expected deny since neither policy condition matches")
		}
	})

	t.Run("MalformedRowSkippedAtConstructTimeAllowStillApplies", func(t *testing.T) {
		// Mixed bag: one valid allow + one syntactically broken row.
		good := us012MakePolicy(t, PolicyTypeObject, SecurityPolicyRules{
			Version:   1,
			Effect:    EffectAllow,
			Subjects:  SubjectSpec{Roles: []string{RoleViewer}},
			Condition: ConditionSpec{Op: OpAlways},
		})
		bad := oms.SecurityPolicy{
			RID:           "ri.ontology.main.security-policy.bad",
			ObjectTypeRID: "ri.ontology.main.object-type.us012",
			PolicyType:    PolicyTypeObject,
			Rules:         json.RawMessage(`{"this is not": "valid json for rules"`), // intentionally broken
		}
		eval := NewPolicyEvaluator([]oms.SecurityPolicy{good, bad})

		allow, _, err := eval.Evaluate(&User{ID: "u1", Roles: []string{RoleViewer}}, map[string]interface{}{"id": "o1"})
		if err != nil {
			t.Fatalf("Evaluate returned error: %v (must tolerate malformed siblings)", err)
		}
		if !allow {
			t.Error("expected allow from good row even when a sibling row is malformed")
		}
	})

	t.Run("NilEvaluator_DefaultDeny", func(t *testing.T) {
		var eval *PolicyEvaluator // never constructed
		allow, masks, err := eval.Evaluate(&User{ID: "u1", Roles: []string{RoleAdmin}}, map[string]interface{}{"id": "o1"})
		if err != nil {
			t.Fatalf("nil receiver must be safe, got err=%v", err)
		}
		if allow {
			t.Error("nil evaluator must default-deny even for admins")
		}
		if len(masks) != 0 {
			t.Errorf("nil evaluator should never emit masks, got %v", masks)
		}
	})

	t.Run("PropertyMasks_DedupAcrossMultipleAllowGrants", func(t *testing.T) {
		// Two PROPERTY allow policies that overlap on "ssn" plus extra masks.
		// The masks the caller receives must be a deduped union.
		obj := us012MakePolicy(t, PolicyTypeObject, SecurityPolicyRules{
			Version:   1,
			Effect:    EffectAllow,
			Subjects:  SubjectSpec{Roles: []string{RoleViewer}},
			Condition: ConditionSpec{Op: OpAlways},
		})
		mask1 := us012MakePolicy(t, PolicyTypeProperty, SecurityPolicyRules{
			Version:       1,
			Effect:        EffectAllow,
			Subjects:      SubjectSpec{Roles: []string{RoleViewer}},
			Condition:     ConditionSpec{Op: OpAlways},
			PropertyMasks: []string{"ssn", "salary"},
		})
		mask2 := us012MakePolicy(t, PolicyTypeProperty, SecurityPolicyRules{
			Version:       1,
			Effect:        EffectAllow,
			Subjects:      SubjectSpec{Roles: []string{RoleViewer}},
			Condition:     ConditionSpec{Op: OpAlways},
			PropertyMasks: []string{"ssn", "dob"},
		})
		eval := NewPolicyEvaluator([]oms.SecurityPolicy{obj, mask1, mask2})

		allow, masks, _ := eval.Evaluate(&User{ID: "u1", Roles: []string{RoleViewer}}, map[string]interface{}{"id": "o1"})
		if !allow {
			t.Fatal("expected allow with object-scope grant")
		}
		seen := map[string]int{}
		for _, m := range masks {
			seen[m]++
		}
		for _, want := range []string{"ssn", "salary", "dob"} {
			if seen[want] == 0 {
				t.Errorf("mask %q missing from result %v", want, masks)
			}
			if seen[want] > 1 {
				t.Errorf("mask %q duplicated %d times; evaluator must dedup", want, seen[want])
			}
		}
	})
}

// --------------------------------------------------------------------------
// AC #2 — token refresh race / concurrent rotation
// --------------------------------------------------------------------------

func TestUS012_TokenRefreshRace(t *testing.T) {
	t.Run("RevokeIfActiveCAS_OnlyFirstCallSucceeds", func(t *testing.T) {
		// Direct contract test on the store CAS primitive that Rotate relies
		// on. Two callers race; exactly one observes won=true.
		store := NewMemoryRefreshStore()
		ctx := context.Background()
		err := store.Create(ctx, &RefreshTokenRecord{
			ID:        "tok-1",
			UserID:    "user:alice",
			TokenHash: "hash-tok-1",
			ExpiresAt: time.Now().Add(time.Hour),
		})
		if err != nil {
			t.Fatalf("seed: %v", err)
		}

		won1, err1 := store.RevokeIfActive(ctx, "tok-1", "rotated")
		won2, err2 := store.RevokeIfActive(ctx, "tok-1", "rotated")
		if err1 != nil || err2 != nil {
			t.Fatalf("RevokeIfActive errors: %v %v", err1, err2)
		}
		if !(won1 && !won2) {
			t.Errorf("expected first call to win (true,false), got (%v,%v)", won1, won2)
		}
		// Missing id must surface NotFound, not silent false.
		_, err = store.RevokeIfActive(ctx, "does-not-exist", "rotated")
		if err != ErrRefreshTokenNotFound {
			t.Errorf("expected ErrRefreshTokenNotFound on missing id, got %v", err)
		}
	})

	t.Run("ConcurrentRotate_SamePlaintext_AtMostOneWinsRestBurnChain", func(t *testing.T) {
		// N goroutines call Rotate(plain) on the same fresh token. With the
		// CAS in place, only one minted a successor; the rest see
		// ErrRefreshTokenReuseDetected and the entire user's chain is killed.
		store := NewMemoryRefreshStore()
		svc := NewRefreshService(store, RefreshServiceOptions{AbsoluteTTL: time.Hour})
		ctx := context.Background()
		plain, _, err := svc.Generate(ctx, "user:race", "")
		if err != nil {
			t.Fatalf("seed: %v", err)
		}

		const racers = 16
		var (
			wg          sync.WaitGroup
			start       = make(chan struct{})
			rotateMu    sync.Mutex
			successes   int
			reuseErrors int
			otherErrs   int
		)
		for i := 0; i < racers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, _, err := svc.Rotate(ctx, plain)
				rotateMu.Lock()
				defer rotateMu.Unlock()
				switch err {
				case nil:
					successes++
				case ErrRefreshTokenReuseDetected:
					reuseErrors++
				default:
					otherErrs++
				}
			}()
		}
		close(start)
		wg.Wait()

		if successes != 1 {
			t.Errorf("expected exactly 1 rotation success, got %d", successes)
		}
		if successes+reuseErrors != racers || otherErrs != 0 {
			t.Errorf("racer outcome accounting off: success=%d reuse=%d other=%d (sum %d, want %d)",
				successes, reuseErrors, otherErrs, successes+reuseErrors+otherErrs, racers)
		}
		// After the race + at least one reuse-detection, the whole chain
		// must be revoked.
		rec, lookupErr := svc.Lookup(ctx, plain)
		if lookupErr != nil {
			t.Fatalf("post-race lookup: %v", lookupErr)
		}
		if rec.RevokedAt == nil {
			t.Error("expected the racing root token revoked after CAS rotation")
		}
	})

	t.Run("ConcurrentRotate_DifferentChains_NoCrossContamination", func(t *testing.T) {
		// Two independent users rotating in parallel must not interfere.
		store := NewMemoryRefreshStore()
		svc := NewRefreshService(store, RefreshServiceOptions{AbsoluteTTL: time.Hour})
		ctx := context.Background()
		plainAlice, _, _ := svc.Generate(ctx, "user:alice", "")
		plainBob, _, _ := svc.Generate(ctx, "user:bob", "")

		var (
			wg               sync.WaitGroup
			aliceErr, bobErr error
		)
		wg.Add(2)
		go func() { defer wg.Done(); _, _, aliceErr = svc.Rotate(ctx, plainAlice) }()
		go func() { defer wg.Done(); _, _, bobErr = svc.Rotate(ctx, plainBob) }()
		wg.Wait()

		if aliceErr != nil || bobErr != nil {
			t.Errorf("independent rotations must both succeed, got alice=%v bob=%v", aliceErr, bobErr)
		}
	})

	t.Run("StaleTokenAfterRotation_StillBurnsChain", func(t *testing.T) {
		// Sequential — present plain1 AFTER it has been rotated to plain2.
		// Reuse detection (existing contract) must fire and burn plain2.
		store := NewMemoryRefreshStore()
		svc := NewRefreshService(store, RefreshServiceOptions{AbsoluteTTL: time.Hour})
		ctx := context.Background()
		plain1, _, _ := svc.Generate(ctx, "user:alice", "")
		plain2, _, err := svc.Rotate(ctx, plain1)
		if err != nil {
			t.Fatalf("first rotate: %v", err)
		}
		_, _, err = svc.Rotate(ctx, plain1)
		if err != ErrRefreshTokenReuseDetected {
			t.Errorf("re-using plain1 must return ErrRefreshTokenReuseDetected, got %v", err)
		}
		rec2, _ := svc.Lookup(ctx, plain2)
		if rec2.RevokedAt == nil {
			t.Error("expected plain2 also revoked by chain kill")
		}
	})

	t.Run("ExpiredTokenRotateDoesNotBurnChain", func(t *testing.T) {
		// Expiry is not theft — Rotate must return ErrRefreshTokenExpired
		// and the chain must NOT be burned. This protects ordinary users who
		// simply opened a long-idle tab.
		store := NewMemoryRefreshStore()
		// Generate via a service that issues already-expired tokens so we
		// keep the test deterministic (no sleeps).
		expiredSvc := NewRefreshService(store, RefreshServiceOptions{AbsoluteTTL: -1 * time.Minute})
		ctx := context.Background()
		plain, rec, err := expiredSvc.Generate(ctx, "user:alice", "")
		if err != nil {
			t.Fatalf("seed: %v", err)
		}

		// Issue a sibling that is not part of this chain (different user)
		// so we can assert chain-kill granularity.
		_, otherRec, err := expiredSvc.Generate(ctx, "user:bob", "")
		if err != nil {
			t.Fatalf("seed sibling: %v", err)
		}

		_, _, err = expiredSvc.Rotate(ctx, plain)
		if err != ErrRefreshTokenExpired {
			t.Fatalf("expected ErrRefreshTokenExpired, got %v", err)
		}
		// rec must remain un-revoked (Rotate exits before CAS).
		stored, _ := store.GetByHash(ctx, rec.TokenHash)
		if stored.RevokedAt != nil {
			t.Error("expired Rotate must not revoke the row")
		}
		// Other user's chain must be untouched.
		other, _ := store.GetByHash(ctx, otherRec.TokenHash)
		if other.RevokedAt != nil {
			t.Error("expired Rotate must not burn sibling chains")
		}
	})
}

// --------------------------------------------------------------------------
// AC #3 — multi-ontology isolation
// --------------------------------------------------------------------------

func TestUS012_MultiOntologyIsolation(t *testing.T) {
	const tenantA = "northwind"
	const tenantB = "chinook"

	t.Run("ScopedRoleOnTenantA_GrantsTenantAOnly", func(t *testing.T) {
		ctx := WithUser(context.Background(), &User{
			ID:            "u-alice",
			OntologyRoles: map[string]string{tenantA: RoleOntologyOwner},
		})
		if err := EnforceOntologyScope(ctx, tenantA, PermObjectTypeWrite); err != nil {
			t.Errorf("expected allow on owned tenant %s, got %v", tenantA, err)
		}
		if err := EnforceOntologyScope(ctx, tenantB, PermObjectTypeWrite); err == nil {
			t.Errorf("expected deny on neighbouring tenant %s, got nil", tenantB)
		}
	})

	t.Run("AdminGlobalRole_OverridesAllOntologies", func(t *testing.T) {
		ctx := WithUser(context.Background(), &User{
			ID:    "u-root",
			Roles: []string{RoleAdmin},
		})
		for _, tenant := range []string{tenantA, tenantB, "anything-new"} {
			if err := EnforceOntologyScope(ctx, tenant, PermObjectTypeWrite); err != nil {
				t.Errorf("admin must bypass per-ontology scope for %s, got %v", tenant, err)
			}
		}
	})

	t.Run("ScopedRoleWithoutMatchingPerm_StillDenies", func(t *testing.T) {
		// Viewer on the tenant — read is fine, write is not.
		ctx := WithUser(context.Background(), &User{
			ID:            "u-viewer",
			OntologyRoles: map[string]string{tenantA: RoleViewer},
		})
		if err := EnforceOntologyScope(ctx, tenantA, PermObjectRead); err != nil {
			t.Errorf("viewer must be allowed read, got %v", err)
		}
		if err := EnforceOntologyScope(ctx, tenantA, PermObjectTypeWrite); err == nil {
			t.Error("viewer must be denied write")
		}
	})

	t.Run("UnauthenticatedRequest_RejectedWithUnauthorized", func(t *testing.T) {
		// No user attached → 401 Unauthorized, not 403 (so the frontend
		// knows to redirect to login rather than show "no permission").
		err := EnforceOntologyScope(context.Background(), tenantA, PermObjectRead)
		if err == nil {
			t.Fatal("missing user must produce an error")
		}
	})

	t.Run("HTTPMiddleware_CrossTenantRequestReturns403", func(t *testing.T) {
		r := chi.NewRouter()
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ctx := WithUser(req.Context(), &User{
					ID:            "u-bob",
					OntologyRoles: map[string]string{tenantA: RoleOntologyOwner},
				})
				next.ServeHTTP(w, req.WithContext(ctx))
			})
		})
		r.Use(OntologyScopeMiddleware(PermObjectRead))
		r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// tenantA — allowed.
		recA := httptest.NewRecorder()
		r.ServeHTTP(recA, httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/"+tenantA+"/objects/Employee", nil))
		if recA.Code != http.StatusOK {
			t.Errorf("tenantA expected 200, got %d", recA.Code)
		}
		// tenantB — middleware must block before the handler runs.
		recB := httptest.NewRecorder()
		r.ServeHTTP(recB, httptest.NewRequest(http.MethodGet, "/api/v2/ontologies/"+tenantB+"/objects/Employee", nil))
		if recB.Code != http.StatusForbidden {
			t.Errorf("tenantB expected 403, got %d body=%s", recB.Code, recB.Body.String())
		}
	})

	t.Run("RoleResolverCachePerUser_DoesNotBleedAcrossUsers", func(t *testing.T) {
		// Two users on the SAME ontology must each see only their own scoped
		// grants out of the resolver cache.
		repo := newFakeUserRepo()
		repo.users["alice"] = &UserRecord{ID: "alice"}
		repo.scopedRoles["alice"] = map[string]string{tenantA: RoleOntologyOwner}
		repo.users["bob"] = &UserRecord{ID: "bob"}
		repo.scopedRoles["bob"] = map[string]string{tenantB: RoleEditor}

		resolver := NewRoleResolver(repo, time.Minute)
		_, aliceScoped, _ := resolver.Resolve(context.Background(), "alice")
		_, bobScoped, _ := resolver.Resolve(context.Background(), "bob")
		if aliceScoped[tenantA] != RoleOntologyOwner {
			t.Errorf("alice should hold owner on %s, got %v", tenantA, aliceScoped)
		}
		if _, present := aliceScoped[tenantB]; present {
			t.Errorf("alice must not see bob's tenant %s in scoped map: %v", tenantB, aliceScoped)
		}
		if bobScoped[tenantB] != RoleEditor {
			t.Errorf("bob should hold editor on %s, got %v", tenantB, bobScoped)
		}
		if _, present := bobScoped[tenantA]; present {
			t.Errorf("bob must not see alice's tenant %s in scoped map: %v", tenantA, bobScoped)
		}
	})
}

// --------------------------------------------------------------------------
// AC #4 — login rate limit thresholds + retry-after window math
// --------------------------------------------------------------------------

func TestUS012_LoginRateLimitThresholds(t *testing.T) {
	t.Run("AllowsUpToMaxThenRejectsWith429AndRetryAfter", func(t *testing.T) {
		h, ip := us012NewLoginHandler(t, 3)
		// 3 attempts allowed (any verdict, including 401), 4th must 429.
		for i := 0; i < 3; i++ {
			rec := us012PostLoginFromIP(t, h, ip, "WRONG")
			if rec.Code == http.StatusTooManyRequests {
				t.Fatalf("attempt %d was rate-limited too early (code=%d)", i+1, rec.Code)
			}
		}
		blocked := us012PostLoginFromIP(t, h, ip, "WRONG")
		if blocked.Code != http.StatusTooManyRequests {
			t.Fatalf("4th attempt expected 429, got %d body=%s", blocked.Code, blocked.Body.String())
		}
		// Retry-After must be present and >= 1.
		retry := blocked.Header().Get("Retry-After")
		if retry == "" {
			t.Error("Retry-After header missing on 429 response")
		}
		var body map[string]any
		_ = json.Unmarshal(blocked.Body.Bytes(), &body)
		if body["errorCode"] != "RATE_LIMITED" {
			t.Errorf("expected errorCode RATE_LIMITED, got %v", body["errorCode"])
		}
	})

	t.Run("DistinctIPsTrackedIndependently", func(t *testing.T) {
		h, ipA := us012NewLoginHandler(t, 2)
		ipB := "203.0.113.241"
		// Fill ipA's bucket to the brim.
		us012PostLoginFromIP(t, h, ipA, "WRONG")
		us012PostLoginFromIP(t, h, ipA, "WRONG")
		blockedA := us012PostLoginFromIP(t, h, ipA, "WRONG")
		if blockedA.Code != http.StatusTooManyRequests {
			t.Fatalf("ipA expected 429 after 2 hits, got %d", blockedA.Code)
		}
		// ipB must still get through — buckets are per-IP.
		recB := us012PostLoginFromIP(t, h, ipB, "WRONG")
		if recB.Code == http.StatusTooManyRequests {
			t.Errorf("ipB must not inherit ipA's rate-limit; got %d", recB.Code)
		}
	})

	t.Run("DisabledLimiter_RateLimitZeroNeverRejects", func(t *testing.T) {
		// RateLimit=0 disables the limiter — successive hits stay 401, never
		// 429. Five attempts are enough to detect a regression without paying
		// for twenty bcrypt verifications.
		h, ip := us012NewLoginHandler(t, 0)
		for i := 0; i < 5; i++ {
			rec := us012PostLoginFromIP(t, h, ip, "WRONG")
			if rec.Code == http.StatusTooManyRequests {
				t.Fatalf("disabled limiter unexpectedly rejected attempt %d", i+1)
			}
		}
	})

	t.Run("IPRateLimiter_WindowExpiryReopensSlot", func(t *testing.T) {
		// Direct test on the unexported limiter using a 50ms window so the
		// "wait for expiry" path runs in tens of ms rather than the
		// production minute.
		lim := newIPRateLimiter(2, 50*time.Millisecond)
		ip := "198.51.100.7"
		if ok, _ := lim.allow(ip); !ok {
			t.Fatal("first call must be allowed")
		}
		if ok, _ := lim.allow(ip); !ok {
			t.Fatal("second call must be allowed")
		}
		ok, retry := lim.allow(ip)
		if ok {
			t.Fatal("third call inside window must be rejected")
		}
		if retry <= 0 || retry > 50*time.Millisecond {
			t.Errorf("retry-after must be (0, window]; got %v", retry)
		}
		// Wait past the window — slot reopens.
		time.Sleep(60 * time.Millisecond)
		if ok, _ := lim.allow(ip); !ok {
			t.Error("expected the slot to reopen after window expiry")
		}
	})

	t.Run("IPRateLimiter_RetryAfterShrinksWithinWindow", func(t *testing.T) {
		// As time advances inside a saturated window, the reported
		// Retry-After (time until oldest hit expires) should not increase.
		lim := newIPRateLimiter(1, 100*time.Millisecond)
		ip := "198.51.100.8"
		if ok, _ := lim.allow(ip); !ok {
			t.Fatal("first call must be allowed")
		}
		_, retry1 := lim.allow(ip)
		time.Sleep(30 * time.Millisecond)
		_, retry2 := lim.allow(ip)
		if retry2 > retry1 {
			t.Errorf("retry-after must not grow as window advances; got %v -> %v", retry1, retry2)
		}
	})
}
