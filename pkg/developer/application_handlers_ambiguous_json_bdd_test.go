package developer

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBDD_DeveloperApp_Create_RejectsAmbiguousJSONBody closes
// out the round-23 P2A-30x final sweep. CreateApplication is the
// last remaining ambiguous-JSON write surface in pkg/. A body
// like
// `{"name":"safe","redirectUris":["https://safe/cb"]}
//
//	{"name":"smuggled","redirectUris":["https://evil/cb"]}`
//
// would create an OAuth application with a safe redirect URI
// while audit pipelines re-parsing the raw bytes see the
// trailing evil redirect URI — a particularly bad GDPR/OAuth
// audit-trail desync since the trailing URI would otherwise look
// like a legitimately-authorized callback during incident
// response.
//
// Fix mirrors rounds 15-22: swap to httputil.ReadJSON which
// enforces dec.Decode(&extra) == io.EOF and returns 400 with the
// "single JSON value" reason.
func TestBDD_DeveloperApp_Create_RejectsAmbiguousJSONBody(t *testing.T) {
	t.Run("CreateApplication rejects concatenated JSON without persisting any app", func(t *testing.T) {
		h, repo := newHarness(t)

		body := `{"name":"safe","redirectUris":["https://safe/cb"],"scopes":["read:objects"]}{"name":"smuggled","redirectUris":["https://evil/cb"],"scopes":["read:objects","admin:write"]}`
		req := withUser(httptest.NewRequest(http.MethodPost, "/api/v2/developer/applications", bytes.NewReader([]byte(body))), "user:alice")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.Create(rec, req)

		assertDeveloperAppSingleJSONRejection(t, rec)

		// Non-mutation snapshot: no app should have been persisted.
		apps, _ := repo.ListByUser(context.Background(), "user:alice")
		if len(apps) != 0 {
			t.Errorf("ambiguous body must not persist any application; got %d (%+v)", len(apps), apps)
		}
	})

	t.Run("CreateApplication with well-formed body still succeeds (regression guard)", func(t *testing.T) {
		h, _ := newHarness(t)
		body, _ := json.Marshal(map[string]any{
			"name":         "HappyApp",
			"redirectUris": []string{"https://example.com/cb"},
			"scopes":       []string{"read:objects"},
		})
		req := withUser(httptest.NewRequest(http.MethodPost, "/api/v2/developer/applications", bytes.NewReader(body)), "user:alice")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("happy CreateApplication: status=%d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func assertDeveloperAppSingleJSONRejection(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&env)
	if env.ErrorName != "InvalidApplicationRequest" {
		t.Errorf("errorName: got %q, want InvalidApplicationRequest", env.ErrorName)
	}
	if !strings.Contains(strings.ToLower(env.Parameters["reason"]), "single json value") {
		t.Errorf("reason should mention 'single JSON value', got %q", env.Parameters["reason"])
	}
}
