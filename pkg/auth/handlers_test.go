package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestMeHandler_DevAdmin(t *testing.T) {
	h := MeHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/me", nil)
	ctx := WithUser(req.Context(), &User{
		ID:    "dev-user",
		Roles: []string{RoleAdmin},
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var got struct {
		ID            string            `json:"id"`
		Email         string            `json:"email"`
		Name          string            `json:"name"`
		Roles         []string          `json:"roles"`
		OntologyRoles map[string]string `json:"ontologyRoles"`
		Permissions   []string          `json:"permissions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if got.ID != "dev-user" {
		t.Errorf("expected id dev-user, got %q", got.ID)
	}
	if !slices.Contains(got.Roles, RoleAdmin) {
		t.Errorf("expected admin role, got %v", got.Roles)
	}
	if !slices.Contains(got.Permissions, PermSecurityPolicyManage) {
		t.Error("expected admin to have securityPolicy.manage permission")
	}
	if !slices.Contains(got.Permissions, PermOntologyRead) {
		t.Error("expected admin to have ontology.read permission")
	}
}

func TestMeHandler_Viewer(t *testing.T) {
	h := MeHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/me", nil)
	ctx := WithUser(req.Context(), &User{
		ID:    "alice",
		Roles: []string{RoleViewer},
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var got struct {
		Permissions []string `json:"permissions"`
	}
	json.NewDecoder(rec.Body).Decode(&got)
	if slices.Contains(got.Permissions, PermActionExecute) {
		t.Error("viewer must not have action.execute permission")
	}
	if !slices.Contains(got.Permissions, PermOntologyRead) {
		t.Error("viewer must have ontology.read permission")
	}
}

// TestMeEndpointMarkings verifies US-054: /api/v2/me must return the caller's
// markings array sourced from user.Attributes["markings"]. Downstream UI uses
// this list to render access chips and the security-policy editor uses it to
// preview evaluation.
func TestMeEndpointMarkings(t *testing.T) {
	h := MeHandler()

	t.Run("WithMarkings", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/me", nil)
		ctx := WithUser(req.Context(), &User{
			ID:    "user:alice",
			Roles: []string{RoleViewer},
			Attributes: map[string]any{
				MarkingsAttributeKey: []string{"PUBLIC", "INTERNAL", "PII"},
			},
		})
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var got struct {
			Markings []string `json:"markings"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		want := []string{"PUBLIC", "INTERNAL", "PII"}
		if len(got.Markings) != len(want) {
			t.Fatalf("len(markings): got %d want %d (%v)", len(got.Markings), len(want), got.Markings)
		}
		for i, m := range want {
			if got.Markings[i] != m {
				t.Errorf("markings[%d]: got %q want %q", i, got.Markings[i], m)
			}
		}
	})

	t.Run("NoMarkingsRendersEmptyArray", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/me", nil)
		ctx := WithUser(req.Context(), &User{
			ID:    "user:bob",
			Roles: []string{RoleViewer},
		})
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `"markings":[]`) {
			t.Errorf("expected empty markings array in body, got %s", body)
		}
	})

	t.Run("TolerateAnySliceShape", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/me", nil)
		ctx := WithUser(req.Context(), &User{
			ID: "user:carol",
			Attributes: map[string]any{
				MarkingsAttributeKey: []any{"ACME", "ACME2"},
			},
		})
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var got struct {
			Markings []string `json:"markings"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(got.Markings) != 2 || got.Markings[0] != "ACME" || got.Markings[1] != "ACME2" {
			t.Errorf("got %v", got.Markings)
		}
	})
}

// TestMarkingsFromContext verifies the pkg/auth.Markings(ctx) helper returns
// the caller's marking list regardless of the raw attribute shape (native
// []string from test fixtures vs []any from JSON-decoded JWT claims).
func TestMarkingsFromContext(t *testing.T) {
	t.Run("NoUser", func(t *testing.T) {
		if got := Markings(context.Background()); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
	t.Run("NoAttributes", func(t *testing.T) {
		ctx := WithUser(context.Background(), &User{ID: "x"})
		if got := Markings(ctx); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
	t.Run("StringSlice", func(t *testing.T) {
		ctx := WithUser(context.Background(), &User{
			Attributes: map[string]any{MarkingsAttributeKey: []string{"A", "B"}},
		})
		got := Markings(ctx)
		if len(got) != 2 || got[0] != "A" || got[1] != "B" {
			t.Errorf("got %v", got)
		}
	})
	t.Run("AnySlice", func(t *testing.T) {
		ctx := WithUser(context.Background(), &User{
			Attributes: map[string]any{MarkingsAttributeKey: []any{"A", "B"}},
		})
		got := Markings(ctx)
		if len(got) != 2 || got[0] != "A" || got[1] != "B" {
			t.Errorf("got %v", got)
		}
	})
}

func TestMeHandler_Unauthenticated(t *testing.T) {
	h := MeHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/me", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}
