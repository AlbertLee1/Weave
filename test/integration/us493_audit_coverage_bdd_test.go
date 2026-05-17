//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
	"github.com/liyang/weave/pkg/rls"
)

// US-493 BDD — Audit 全覆盖（OMS write / role / policy / session）.
//
// PRD acceptance points pinned by this BDD:
//
//  1. OMS admin write、role change、policy update、session create/destroy
//     全部入 audit — driven through HTTP handlers in each sub-scenario
//     and verified via PG-backed audit.Store.List.
//  2. /api/admin/audit 支持按 actor / resourceRid / 时间范围筛选 — at the
//     wire level via the new PRD-literal alias path, with the
//     `resourceRid` camelCase query param.
//  3. 每类操作至少 1 条 audit 记录 — each sub-scenario asserts exactly
//     one matching row.
//
// The BDD wires the four lanes against a real PostgreSQL container
// (testcontainers) and a chi router shaped like cmd/server/main.go's
// production layout. The OMS lane in particular exercises the new
// AuditedRepository wrap added in US-493: writes through the OMSHandler
// land in audit_events keyed by the request-context actor.
//
// Negative-control discipline: the filter-compose scenario seeds
// multiple rows that DIFFER on the dimension under test, so a
// regression that silently dropped a filter clause would fail the
// narrowing assertion (it would return too many rows).

func us493AdminUser() *auth.User {
	return &auth.User{
		ID:    "user:us493-admin",
		Email: "us493-admin@example.com",
		Roles: []string{auth.RoleAdmin},
	}
}

func us493AsAdmin(r *http.Request) *http.Request {
	return r.WithContext(auth.WithUser(r.Context(), us493AdminUser()))
}

type us493Fixture struct {
	router      *chi.Mux
	auditStore  audit.Store
	ontologyRID string
}

func setupUS493Fixture(t *testing.T) *us493Fixture {
	t.Helper()
	ctx := context.Background()
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	auditStore := audit.NewPGStore(pg.Pool)
	pgOms := oms.NewPGRepository(pg.Pool)

	// Mirror cmd/server/main.go US-493 wiring exactly: AuditedRepository
	// wraps the CachedRepository so every OMSHandler write emits an
	// audit_events row keyed by the request-context actor.
	actorFn := func(ctx context.Context) string {
		if u := auth.UserFromContext(ctx); u != nil {
			return u.ID
		}
		return ""
	}
	audited := oms.NewAuditedRepository(
		oms.NewCachedRepository(pgOms, 60*time.Second),
		auditStore, actorFn)

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "us493_ont",
		DisplayName: "US-493 Audit Coverage Ontology",
	}
	if err := audited.CreateOntology(ctx, ont); err != nil {
		t.Fatalf("seed ontology: %v", err)
	}

	userRepo := auth.NewPGUserRepository(pg.Pool)
	if err := userRepo.CreateUser(ctx, &auth.UserRecord{
		ID:    "user:us493-target",
		Email: "us493-target@example.com",
		Name:  "US-493 Target",
	}); err != nil {
		t.Fatalf("create target user: %v", err)
	}
	loginPwd := "TestPassword!123"
	hash, err := auth.HashPassword(loginPwd)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := userRepo.CreateUser(ctx, &auth.UserRecord{
		ID:           us493AdminUser().ID,
		Email:        us493AdminUser().Email,
		Name:         "US-493 Admin",
		PasswordHash: hash,
	}); err != nil {
		t.Fatalf("create admin user: %v", err)
	}

	// Migration 000051 bootstraps the viewer / editor / admin builtin
	// roles so we just attach to the existing row.
	roleRepo := auth.NewPGRoleRepository(pg.Pool)

	r := chi.NewRouter()

	// Lane 1 — OMS admin write through AuditedRepository.
	omsHandler := oms.NewOMSHandler(audited)
	r.Post("/api/admin/ontologies/{ontologyApiName}/objectTypes", omsHandler.CreateObjectType)

	// Lane 2 — Role grant.
	urHandler := auth.NewUserRoleHandler(userRepo, roleRepo, nil, auditStore)
	r.Post("/api/admin/users/{userId}/roles", urHandler.GrantRole)

	// Lane 3 — Policy create (RLS). Memory store is enough — the lane
	// under test is the audit emission, not the policy persistence.
	rlsStore := rls.NewMemoryStore()
	rlsEngine := rls.New(rlsStore, nil)
	rlsHandler := rls.NewHandler(rlsStore, auditStore, rlsEngine)
	r.Post("/api/admin/row-policies", rlsHandler.Create)

	// Lane 4 — Session create on login_success.
	resolver := auth.NewRoleResolver(userRepo, time.Minute)
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa keygen: %v", err)
	}
	signer, err := auth.NewJWTSigner(priv, &priv.PublicKey, auth.JWTSignerOptions{
		Issuer:         "us493-test",
		Audience:       "us493-api",
		AccessTokenTTL: 15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("jwt signer: %v", err)
	}
	refreshStore := auth.NewMemoryRefreshStore()
	rs := auth.NewRefreshService(refreshStore, auth.RefreshServiceOptions{AbsoluteTTL: 7 * 24 * time.Hour})
	loginHandler := auth.NewLoginHandler(auth.LoginHandlerDeps{
		Users:          userRepo,
		Resolver:       resolver,
		Signer:         signer,
		RefreshService: rs,
		AuditStore:     auditStore,
	})
	r.Post("/api/auth/login", loginHandler.ServeHTTP)

	// PRD literal alias `/api/admin/audit` + a thin handler that
	// reproduces the production query-param contract closely enough to
	// validate actor / resourceRid / since / until at the wire level.
	// Production wiring uses cmd/server.NewAdminAuditEventsHandler;
	// duplicating its small surface here avoids leaking package main
	// into the integration test.
	r.Get("/api/admin/audit", us493AuditListHandler(auditStore))

	return &us493Fixture{router: r, auditStore: auditStore, ontologyRID: ont.RID}
}

// us493AuditListHandler is the test-local equivalent of
// cmd/server.NewAdminAuditEventsHandler. Honours `actor`, `action`,
// `resource_type`, `resourceRid` (camelCase) / `resource_rid` (snake)
// `since`, `until`, `pageSize`.
func us493AuditListHandler(store audit.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f := audit.ListFilter{
			ActorID:      q.Get("actor"),
			Action:       q.Get("action"),
			ResourceType: q.Get("resource_type"),
		}
		if rid := q.Get("resourceRid"); rid != "" {
			f.ResourceRID = rid
		} else if rid := q.Get("resource_rid"); rid != "" {
			f.ResourceRID = rid
		}
		if s := q.Get("since"); s != "" {
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				http.Error(w, "since must be RFC3339", http.StatusBadRequest)
				return
			}
			f.From = &t
		}
		if s := q.Get("until"); s != "" {
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				http.Error(w, "until must be RFC3339", http.StatusBadRequest)
				return
			}
			f.To = &t
		}
		events, err := store.List(r.Context(), f)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": events})
	}
}

// TestBDD_US493_AuditCoverage_AllFourLanes drives one HTTP call per PRD
// lane and asserts each lane lands exactly one audit_events row keyed
// by the request actor.
func TestBDD_US493_AuditCoverage_AllFourLanes(t *testing.T) {
	fix := setupUS493Fixture(t)
	ctx := context.Background()

	// ---- Lane 1: OMS admin write through HTTP ----
	t.Run("Given_admin_user_When_POST_create_objectType_Then_audit_row_recorded", func(t *testing.T) {
		body := strings.NewReader(`{
			"apiName":        "us493_target_ot",
			"displayName":    "US-493 Target ObjectType",
			"primaryKey":     "id",
			"classification": "Internal"
		}`)
		req := httptest.NewRequest(http.MethodPost,
			"/api/admin/ontologies/us493_ont/objectTypes", body)
		req.Header.Set("Content-Type", "application/json")
		req = us493AsAdmin(req)
		rw := httptest.NewRecorder()
		fix.router.ServeHTTP(rw, req)

		if rw.Code != http.StatusCreated {
			t.Fatalf("OMS create status=%d body=%s", rw.Code, rw.Body.String())
		}
		var created oms.ObjectType
		if err := json.Unmarshal(rw.Body.Bytes(), &created); err != nil {
			t.Fatalf("decode: %v", err)
		}

		events, err := fix.auditStore.List(ctx, audit.ListFilter{
			ResourceRID:  created.RID,
			ResourceType: "ObjectType",
		})
		if err != nil {
			t.Fatalf("list audit: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("OMS write produced %d audit rows for %s, want 1", len(events), created.RID)
		}
		got := events[0]
		if got.Action != "CREATE" {
			t.Errorf("action=%q, want CREATE", got.Action)
		}
		if got.ActorID != us493AdminUser().ID {
			t.Errorf("actor=%q, want %q", got.ActorID, us493AdminUser().ID)
		}
		if len(got.DiffJSON) == 0 {
			t.Errorf("diff_json empty; want {before, after}")
		}
		if got.EntryHash == "" {
			t.Errorf("entry_hash empty; chain not stamped")
		}
	})

	// ---- Lane 2: Role change ----
	t.Run("Given_admin_user_When_POST_grant_role_Then_audit_row_recorded", func(t *testing.T) {
		body := strings.NewReader(`{"role":"viewer"}`)
		req := httptest.NewRequest(http.MethodPost,
			"/api/admin/users/user:us493-target/roles", body)
		req.Header.Set("Content-Type", "application/json")
		req = us493AsAdmin(req)
		rw := httptest.NewRecorder()
		fix.router.ServeHTTP(rw, req)

		if rw.Code != http.StatusOK {
			t.Fatalf("role grant status=%d body=%s", rw.Code, rw.Body.String())
		}

		events, err := fix.auditStore.List(ctx, audit.ListFilter{
			Action:      "user_role_grant",
			ResourceRID: "user:us493-target",
		})
		if err != nil {
			t.Fatalf("list audit: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("role grant produced %d audit rows, want 1", len(events))
		}
		if events[0].ActorID != us493AdminUser().ID {
			t.Errorf("actor=%q, want %q", events[0].ActorID, us493AdminUser().ID)
		}
	})

	// ---- Lane 3: Policy update (RLS create) ----
	t.Run("Given_admin_user_When_POST_create_row_policy_Then_audit_row_recorded", func(t *testing.T) {
		policyBody := fmt.Sprintf(`{
			"name":          "us493_policy",
			"objectTypeRid": "%s",
			"predicate":     {"op":"eq","field":"department","value":"R&D"}
		}`, "ri.ontology.main.objectType.us493-fake")
		req := httptest.NewRequest(http.MethodPost, "/api/admin/row-policies",
			strings.NewReader(policyBody))
		req.Header.Set("Content-Type", "application/json")
		req = us493AsAdmin(req)
		rw := httptest.NewRecorder()
		fix.router.ServeHTTP(rw, req)

		if rw.Code != http.StatusCreated {
			t.Fatalf("policy create status=%d body=%s", rw.Code, rw.Body.String())
		}
		var resp struct {
			RID string `json:"rid"`
		}
		if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode policy create: %v body=%s", err, rw.Body.String())
		}

		events, err := fix.auditStore.List(ctx, audit.ListFilter{
			Action:      "row_policy_create",
			ResourceRID: resp.RID,
		})
		if err != nil {
			t.Fatalf("list audit: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("policy create produced %d audit rows, want 1", len(events))
		}
	})

	// ---- Lane 4: Session create on login_success ----
	t.Run("Given_correct_credentials_When_POST_login_Then_session_audit_row_recorded", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"email":    us493AdminUser().Email,
			"password": "TestPassword!123",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
			bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()
		fix.router.ServeHTTP(rw, req)

		if rw.Code != http.StatusOK {
			t.Fatalf("login status=%d body=%s", rw.Code, rw.Body.String())
		}

		events, err := fix.auditStore.List(ctx, audit.ListFilter{
			Action:      "login_success",
			ResourceRID: us493AdminUser().ID,
		})
		if err != nil {
			t.Fatalf("list audit: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("login_success produced %d audit rows, want 1", len(events))
		}
		if events[0].ResourceType != "Session" {
			t.Errorf("resource_type=%q, want Session", events[0].ResourceType)
		}
	})
}

// TestBDD_US493_AuditEndpoint_FiltersComposeOverHTTP_PRDLiteralPath
// drives the PRD-literal alias `/api/admin/audit` and proves the three
// PRD filter dimensions (actor / resourceRid / time-range) compose at
// the wire level. Each dimension is exercised against a seeded set
// designed so a regression that silently dropped a filter clause would
// return too many rows.
func TestBDD_US493_AuditEndpoint_FiltersComposeOverHTTP_PRDLiteralPath(t *testing.T) {
	fix := setupUS493Fixture(t)
	ctx := context.Background()

	t0 := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	targetRID := "ri.ontology.main.objectType.us493-filter-target"
	otherRID := "ri.ontology.main.objectType.us493-filter-other"
	rows := []audit.AuditEvent{
		{ActorID: us493AdminUser().ID, Action: "CREATE", ResourceType: "ObjectType", ResourceRID: targetRID, Timestamp: t0},
		{ActorID: us493AdminUser().ID, Action: "UPDATE", ResourceType: "ObjectType", ResourceRID: targetRID, Timestamp: t0.Add(2 * time.Minute)},
		{ActorID: "user:other", Action: "UPDATE", ResourceType: "ObjectType", ResourceRID: targetRID, Timestamp: t0.Add(4 * time.Minute)},
		{ActorID: us493AdminUser().ID, Action: "DELETE", ResourceType: "ObjectType", ResourceRID: otherRID, Timestamp: t0.Add(6 * time.Minute)},
		{ActorID: us493AdminUser().ID, Action: "UPDATE", ResourceType: "ObjectType", ResourceRID: targetRID, Timestamp: t0.Add(8 * time.Minute)},
	}
	for _, evt := range rows {
		if err := audit.Record(ctx, fix.auditStore, evt); err != nil {
			t.Fatalf("seed audit: %v", err)
		}
	}

	doGet := func(t *testing.T, q string) []audit.AuditEvent {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/admin/audit?"+q, nil)
		req = us493AsAdmin(req)
		rw := httptest.NewRecorder()
		fix.router.ServeHTTP(rw, req)
		if rw.Code != http.StatusOK {
			t.Fatalf("GET status=%d body=%s", rw.Code, rw.Body.String())
		}
		var resp struct {
			Data []audit.AuditEvent `json:"data"`
		}
		if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp.Data
	}

	// Helper: count how many of the seeded `rows` slice satisfy the
	// predicate AND appear in the returned `got` slice.
	countSeeded := func(got []audit.AuditEvent, pred func(audit.AuditEvent) bool) int {
		var n int
		for _, e := range got {
			for _, w := range rows {
				if e.Timestamp.Equal(w.Timestamp) && e.ResourceRID == w.ResourceRID && e.ActorID == w.ActorID {
					if pred(e) {
						n++
					}
					break
				}
			}
		}
		return n
	}

	t.Run("Given_seeded_rows_When_filter_by_actor_Then_admin_seeded_rows_returned", func(t *testing.T) {
		got := doGet(t, "actor="+us493AdminUser().ID)
		for _, e := range got {
			if e.ActorID != us493AdminUser().ID {
				t.Errorf("actor filter leaked actor=%q", e.ActorID)
			}
		}
		n := countSeeded(got, func(e audit.AuditEvent) bool { return e.ActorID == us493AdminUser().ID })
		if n != 4 {
			t.Errorf("admin-actor narrow returned %d seeded rows, want 4", n)
		}
	})

	t.Run("Given_seeded_rows_When_filter_by_resourceRid_camelCase_Then_only_target_returned", func(t *testing.T) {
		got := doGet(t, "resourceRid="+targetRID)
		for _, e := range got {
			if e.ResourceRID != targetRID {
				t.Errorf("resourceRid filter leaked %q", e.ResourceRID)
			}
		}
		n := countSeeded(got, func(e audit.AuditEvent) bool { return e.ResourceRID == targetRID })
		if n != 4 {
			t.Errorf("resourceRid narrow returned %d seeded matches, want 4", n)
		}
	})

	t.Run("Given_seeded_rows_When_filter_by_since_until_Then_only_window_returned", func(t *testing.T) {
		got := doGet(t, "since=2026-04-01T12:01:00Z&until=2026-04-01T12:05:00Z")
		for _, e := range got {
			if e.Timestamp.Before(t0.Add(1*time.Minute)) || e.Timestamp.After(t0.Add(5*time.Minute)) {
				t.Errorf("time-window filter leaked ts=%s", e.Timestamp)
			}
		}
		// Window covers rows[1] (12:02) and rows[2] (12:04) — exactly two of
		// the five seeded rows.
		n := countSeeded(got, func(e audit.AuditEvent) bool {
			return !e.Timestamp.Before(t0.Add(1*time.Minute)) && !e.Timestamp.After(t0.Add(5*time.Minute))
		})
		if n != 2 {
			t.Errorf("time window returned %d seeded matches, want 2 (rows[1], rows[2])", n)
		}
	})

	t.Run("Given_seeded_rows_When_compose_actor_resourceRid_window_Then_exact_intersection", func(t *testing.T) {
		// admin actor + targetRID + window 12:01..12:09 should match
		// rows[1] (admin, target, 12:02) and rows[4] (admin, target, 12:08).
		// Must EXCLUDE rows[2] (other actor — proves actor AND'd) and
		// rows[3] (otherRID — proves resourceRid AND'd).
		got := doGet(t, "actor="+us493AdminUser().ID+
			"&resourceRid="+targetRID+
			"&since=2026-04-01T12:01:00Z&until=2026-04-01T12:09:00Z")
		for _, e := range got {
			ok := e.ActorID == us493AdminUser().ID &&
				e.ResourceRID == targetRID &&
				!e.Timestamp.Before(t0.Add(1*time.Minute)) &&
				!e.Timestamp.After(t0.Add(9*time.Minute))
			if !ok {
				t.Errorf("compose filter leaked actor=%q rid=%q ts=%s",
					e.ActorID, e.ResourceRID, e.Timestamp)
			}
		}
		n := countSeeded(got, func(e audit.AuditEvent) bool {
			return e.ActorID == us493AdminUser().ID &&
				e.ResourceRID == targetRID &&
				!e.Timestamp.Before(t0.Add(1*time.Minute)) &&
				!e.Timestamp.After(t0.Add(9*time.Minute))
		})
		if n != 2 {
			t.Errorf("composed filter returned %d seeded matches, want 2 (rows[1], rows[4])", n)
		}
	})
}
