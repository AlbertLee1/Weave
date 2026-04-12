//go:build integration

package phase7_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// TestPhase7_AuditCoverage verifies that OMS changes, auth events, and policy
// changes all produce persisted audit_events rows in PostgreSQL.
func TestPhase7_AuditCoverage(t *testing.T) {
	ctx := context.Background()

	// ---- infrastructure ----
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	auditStore := audit.NewPGStore(pg.Pool)
	omsRepo := oms.NewPGRepository(pg.Pool)

	actorFn := func(ctx context.Context) string {
		if u := auth.UserFromContext(ctx); u != nil {
			return u.ID
		}
		return ""
	}
	auditedRepo := oms.NewAuditedRepository(omsRepo, auditStore, actorFn)

	// ---- shared ontology for OMS + policy tests ----
	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "audit_coverage_ont",
		DisplayName: "Audit Coverage Ontology",
	}
	if err := auditedRepo.CreateOntology(ctx, ont); err != nil {
		t.Fatalf("create ontology: %v", err)
	}

	// ================================================================
	// Sub-test 1: Create ObjectType -> audit row exists
	// ================================================================
	t.Run("CreateObjectType_produces_audit_row", func(t *testing.T) {
		ot := &oms.ObjectType{
			RID:         rid.NewObjectTypeRID(),
			OntologyRID: ont.RID,
			APIName:     "auditTestType",
			DisplayName: "Audit Test Type",
			PrimaryKey:  "id",
			Status:      "ACTIVE",
			Visibility:  "NORMAL",
		}
		if err := auditedRepo.CreateObjectType(ctx, ot); err != nil {
			t.Fatalf("create object type: %v", err)
		}

		events, err := auditStore.List(ctx, audit.ListFilter{
			Action:       "CREATE",
			ResourceType: "ObjectType",
		})
		if err != nil {
			t.Fatalf("list audit events: %v", err)
		}

		found := false
		for _, evt := range events {
			if evt.ResourceRID == ot.RID {
				found = true
				if evt.Action != "CREATE" {
					t.Errorf("expected Action=CREATE, got %q", evt.Action)
				}
				if evt.DiffJSON == nil {
					t.Error("expected non-nil DiffJSON")
				}
				break
			}
		}
		if !found {
			t.Errorf("no audit event found for ObjectType RID %s", ot.RID)
		}
	})

	// ================================================================
	// Sub-test 2: Login failure -> audit row
	// ================================================================
	t.Run("LoginFailure_produces_audit_row", func(t *testing.T) {
		userRepo := auth.NewPGUserRepository(pg.Pool)

		// Seed a user with a known password.
		hash, err := auth.HashPassword("correctPassword1!")
		if err != nil {
			t.Fatal(err)
		}
		if err := userRepo.CreateUser(ctx, &auth.UserRecord{
			ID:           "user:audit-login-test",
			Email:        "audit-login@example.com",
			Name:         "Audit Login",
			PasswordHash: hash,
		}); err != nil {
			t.Fatalf("create user: %v", err)
		}

		resolver := auth.NewRoleResolver(userRepo, time.Minute)
		priv, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		signer, err := auth.NewJWTSigner(priv, &priv.PublicKey, auth.JWTSignerOptions{
			Issuer:         "weave-test",
			Audience:       "weave-api",
			AccessTokenTTL: 15 * time.Minute,
		})
		if err != nil {
			t.Fatal(err)
		}

		refreshStore := auth.NewMemoryRefreshStore()
		rs := auth.NewRefreshService(refreshStore, auth.RefreshServiceOptions{AbsoluteTTL: 7 * 24 * time.Hour})

		loginHandler := auth.NewLoginHandler(auth.LoginHandlerDeps{
			Users:          userRepo,
			Resolver:       resolver,
			Signer:         signer,
			RefreshService: rs,
			RateLimit:      0,
			AuditStore:     auditStore,
		})

		// Fire a login with wrong password.
		body, _ := json.Marshal(map[string]string{
			"email":    "audit-login@example.com",
			"password": "WRONG_PASSWORD",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		loginHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
		}

		events, err := auditStore.List(ctx, audit.ListFilter{
			Action:       "login_failed",
			ResourceType: "Session",
		})
		if err != nil {
			t.Fatalf("list audit events: %v", err)
		}

		found := false
		for _, evt := range events {
			if evt.ActorID == "user:audit-login-test" {
				found = true
				if evt.Action != "login_failed" {
					t.Errorf("expected Action=login_failed, got %q", evt.Action)
				}
				break
			}
		}
		if !found {
			t.Error("no audit event found for login_failed")
		}
	})

	// ================================================================
	// Sub-test 3: API key create + revoke -> audit rows
	// ================================================================
	t.Run("APIKey_create_and_revoke_produce_audit_rows", func(t *testing.T) {
		// The api_keys table has a FK on user_id -> users, so seed a real user first.
		userRepo := auth.NewPGUserRepository(pg.Pool)
		if err := userRepo.CreateUser(ctx, &auth.UserRecord{
			ID:    "user:apikey-audit-test",
			Email: "apikey-audit@example.com",
			Name:  "API Key Audit",
		}); err != nil {
			t.Fatalf("create user for api key test: %v", err)
		}

		apiKeyRepo := auth.NewPGAPIKeyRepository(pg.Pool)
		apiKeyHandler := auth.NewAPIKeyHandler(apiKeyRepo, auditStore)

		// Inject authenticated user context.
		apiUser := &auth.User{
			ID:    "user:apikey-audit-test",
			Email: "apikey-audit@example.com",
			Roles: []string{auth.RoleAdmin},
		}

		// ---- Create API key ----
		createBody, _ := json.Marshal(map[string]string{"name": "audit-test-key"})
		createReq := httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", bytes.NewReader(createBody))
		createReq.Header.Set("Content-Type", "application/json")
		createReq = createReq.WithContext(auth.WithUser(createReq.Context(), apiUser))
		createRec := httptest.NewRecorder()
		apiKeyHandler.Create(createRec, createReq)

		if createRec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d body=%s", createRec.Code, createRec.Body.String())
		}

		var createResp struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(createRec.Body).Decode(&createResp); err != nil {
			t.Fatalf("decode create response: %v", err)
		}

		// Verify create audit event.
		createEvents, err := auditStore.List(ctx, audit.ListFilter{
			Action:       "api_key_create",
			ResourceType: "APIKey",
		})
		if err != nil {
			t.Fatalf("list create events: %v", err)
		}
		foundCreate := false
		for _, evt := range createEvents {
			if evt.ResourceRID == createResp.ID && evt.ActorID == apiUser.ID {
				foundCreate = true
				break
			}
		}
		if !foundCreate {
			t.Error("no audit event found for api_key_create")
		}

		// ---- Revoke API key ----
		revokeReq := httptest.NewRequest(http.MethodDelete, "/api/admin/api-keys/"+createResp.ID, nil)
		revokeReq = revokeReq.WithContext(auth.WithUser(revokeReq.Context(), apiUser))
		revokeRec := httptest.NewRecorder()
		apiKeyHandler.DeleteFor(revokeRec, revokeReq, createResp.ID)

		if revokeRec.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d body=%s", revokeRec.Code, revokeRec.Body.String())
		}

		// Verify revoke audit event.
		revokeEvents, err := auditStore.List(ctx, audit.ListFilter{
			Action:       "api_key_revoke",
			ResourceType: "APIKey",
		})
		if err != nil {
			t.Fatalf("list revoke events: %v", err)
		}
		foundRevoke := false
		for _, evt := range revokeEvents {
			if evt.ResourceRID == createResp.ID && evt.ActorID == apiUser.ID {
				foundRevoke = true
				break
			}
		}
		if !foundRevoke {
			t.Error("no audit event found for api_key_revoke")
		}
	})

	// ================================================================
	// Sub-test 4: Policy create -> audit row
	// ================================================================
	t.Run("PolicyCreate_produces_audit_row", func(t *testing.T) {
		// Need an ObjectType for the policy to reference.
		ot := &oms.ObjectType{
			RID:         rid.NewObjectTypeRID(),
			OntologyRID: ont.RID,
			APIName:     "policyAuditType",
			DisplayName: "Policy Audit Type",
			PrimaryKey:  "id",
			Status:      "ACTIVE",
			Visibility:  "NORMAL",
		}
		if err := auditedRepo.CreateObjectType(ctx, ot); err != nil {
			t.Fatalf("create object type: %v", err)
		}

		rules, _ := json.Marshal([]map[string]string{
			{"type": "eq", "userAttr": "department", "objectProperty": "department"},
		})
		sp := &oms.SecurityPolicy{
			RID:           rid.New("ontology", "main", "security-policy"),
			ObjectTypeRID: ot.RID,
			PolicyType:    "OBJECT",
			Rules:         rules,
		}
		if err := auditedRepo.CreateSecurityPolicy(ctx, sp); err != nil {
			t.Fatalf("create security policy: %v", err)
		}

		events, err := auditStore.List(ctx, audit.ListFilter{
			Action:       "CREATE",
			ResourceType: "SecurityPolicy",
		})
		if err != nil {
			t.Fatalf("list audit events: %v", err)
		}

		found := false
		for _, evt := range events {
			if evt.ResourceRID == sp.RID {
				found = true
				if evt.Action != "CREATE" {
					t.Errorf("expected Action=CREATE, got %q", evt.Action)
				}
				if evt.DiffJSON == nil {
					t.Error("expected non-nil DiffJSON")
				}
				break
			}
		}
		if !found {
			t.Errorf("no audit event found for SecurityPolicy RID %s", sp.RID)
		}
	})
}
