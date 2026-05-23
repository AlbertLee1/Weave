package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestBDD_ServiceAccountAdminWritesRejectAmbiguousJSONBody_P2A303 covers the
// two service-account admin write surfaces:
//
//   - POST  /api/admin/service-accounts          (Create)
//   - PATCH /api/admin/service-accounts/{id}     (Update)
//
// For each surface it asserts that a body composed of two concatenated JSON
// objects is rejected with HTTP 400 plus a "single JSON value" reason, and
// that the underlying repository state is not mutated by the rejected
// request. A trailing well-formed regression sub-test confirms the existing
// happy paths still succeed after the hardening.
func TestBDD_ServiceAccountAdminWritesRejectAmbiguousJSONBody_P2A303(t *testing.T) {
	t.Run("Create rejects concatenated JSON without inserting a service account", func(t *testing.T) {
		h, repo := newServiceAccountHandlerHarness(t)
		seedNames := snapshotServiceAccountNames(repo)

		body := `{"name":"ci-bot","description":"safe","scopes":["read:objects"]}` +
			`{"name":"smuggled","scopes":["user:manage"]}`
		req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/service-accounts", strings.NewReader(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.Create(rec, req)

		assertSingleJSONValueRejection(t, rec, "InvalidServiceAccountRequest")

		afterNames := snapshotServiceAccountNames(repo)
		if !reflect.DeepEqual(afterNames, seedNames) {
			t.Fatalf("Create with concatenated body mutated service-account set: before=%v after=%v", seedNames, afterNames)
		}
		if _, err := repo.GetByName(context.Background(), "ci-bot"); err == nil {
			t.Fatalf("Create with concatenated body persisted first-decoded service account 'ci-bot'")
		}
		if _, err := repo.GetByName(context.Background(), "smuggled"); err == nil {
			t.Fatalf("Create with concatenated body persisted smuggled service account 'smuggled'")
		}
	})

	t.Run("Update rejects concatenated JSON without mutating the service account", func(t *testing.T) {
		h, repo := newServiceAccountHandlerHarness(t)
		sa := &ServiceAccount{
			Name:        "ci-bot",
			Description: "original",
			OwnerUserID: "user:admin@example.com",
			Scopes:      []string{"read:objects"},
		}
		if err := repo.Create(context.Background(), sa); err != nil {
			t.Fatalf("seed ci-bot service account: %v", err)
		}

		firstDesc := "first-decoded"
		first, err := json.Marshal(ServiceAccountUpdateRequest{Description: &firstDesc})
		if err != nil {
			t.Fatalf("marshal first patch: %v", err)
		}
		body := string(first) + `{"description":"smuggled-second"}`
		req := withAdmin(httptest.NewRequest(http.MethodPatch, "/api/admin/service-accounts/"+sa.ID, strings.NewReader(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.updateFor(rec, req, sa.ID)

		assertSingleJSONValueRejection(t, rec, "InvalidServiceAccountUpdate")

		got, err := repo.GetByID(context.Background(), sa.ID)
		if err != nil {
			t.Fatalf("re-read ci-bot: %v", err)
		}
		if got.Description != "original" {
			t.Fatalf("ambiguous Update mutated description to %q (want %q)", got.Description, "original")
		}
		gotScopes := append([]string(nil), got.Scopes...)
		sort.Strings(gotScopes)
		wantScopes := []string{"read:objects"}
		if !reflect.DeepEqual(gotScopes, wantScopes) {
			t.Fatalf("ambiguous Update mutated scopes: got %v want %v", gotScopes, wantScopes)
		}
	})

	t.Run("well-formed bodies still succeed across both surfaces", func(t *testing.T) {
		h, repo := newServiceAccountHandlerHarness(t)

		// Create happy path.
		exp := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
		createBody, _ := json.Marshal(map[string]any{
			"name":        "ci-bot",
			"description": "GitHub Actions",
			"scopes":      []string{"read:objects"},
			"expiresAt":   exp.Format(time.RFC3339),
		})
		createReq := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/service-accounts", bytes.NewReader(createBody)))
		createReq.Header.Set("Content-Type", "application/json")
		createRec := httptest.NewRecorder()
		h.Create(createRec, createReq)
		if createRec.Code != http.StatusCreated {
			t.Fatalf("happy Create returned %d body=%s", createRec.Code, createRec.Body.String())
		}
		var created ServiceAccountResponse
		if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
			t.Fatalf("decode created service account: %v", err)
		}
		if created.ID == "" {
			t.Fatalf("happy Create returned empty service-account ID")
		}

		// Update happy path.
		newDesc := "renamed"
		updateBody, _ := json.Marshal(ServiceAccountUpdateRequest{Description: &newDesc})
		updateReq := withAdmin(httptest.NewRequest(http.MethodPatch, "/api/admin/service-accounts/"+created.ID, bytes.NewReader(updateBody)))
		updateReq.Header.Set("Content-Type", "application/json")
		updateRec := httptest.NewRecorder()
		h.updateFor(updateRec, updateReq, created.ID)
		if updateRec.Code != http.StatusOK {
			t.Fatalf("happy Update returned %d body=%s", updateRec.Code, updateRec.Body.String())
		}
		afterUpd, err := repo.GetByID(context.Background(), created.ID)
		if err != nil {
			t.Fatalf("re-read ci-bot after happy update: %v", err)
		}
		if afterUpd.Description != newDesc {
			t.Fatalf("happy Update did not persist new description: %q", afterUpd.Description)
		}
		if afterUpd.ExpiresAt == nil || !afterUpd.ExpiresAt.Equal(exp) {
			t.Fatalf("happy Update clobbered ExpiresAt: got %v want %v", afterUpd.ExpiresAt, exp)
		}
	})
}

func snapshotServiceAccountNames(repo *fakeServiceAccountRepo) []string {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	out := make([]string, 0, len(repo.byID))
	for _, sa := range repo.byID {
		out = append(out, sa.Name)
	}
	sort.Strings(out)
	return out
}
