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
)

// TestBDD_APIKeyAdminWritesRejectAmbiguousJSONBody_P2A304 covers the two
// API-key admin write surfaces:
//
//   - POST /api/admin/api-keys                  (Create)
//   - POST /api/admin/api-keys/{id}/rotate      (Rotate)
//
// Both endpoints mint long-lived bearer credentials, so a body composed of
// two concatenated JSON objects must be rejected with HTTP 400 plus a
// "single JSON value" reason instead of silently dropping the trailing
// bytes. The test also asserts that the rejected request does not mutate
// repository state (no new key, predecessor not rotated) and that the
// existing happy paths (well-formed bodies, plus the rotate-with-no-body
// default-grace path) keep working after the hardening.
func TestBDD_APIKeyAdminWritesRejectAmbiguousJSONBody_P2A304(t *testing.T) {
	t.Run("Create rejects concatenated JSON without minting a key", func(t *testing.T) {
		h, repo := newAPIKeyHandlerHarness(t)
		seedPrefixes := snapshotAPIKeyPrefixes(repo)

		body := `{"name":"ci-bot","scopes":["read:objects"]}` +
			`{"name":"smuggled","scopes":["user:manage"]}`
		req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", strings.NewReader(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.Create(rec, req)

		assertSingleJSONValueRejection(t, rec, "InvalidAPIKeyRequest")

		afterPrefixes := snapshotAPIKeyPrefixes(repo)
		if !reflect.DeepEqual(afterPrefixes, seedPrefixes) {
			t.Fatalf("Create with concatenated body mutated api-key set: before=%v after=%v", seedPrefixes, afterPrefixes)
		}
		if rows, err := repo.ListByUser(context.Background(), "user:admin@example.com"); err != nil {
			t.Fatalf("ListByUser after rejected Create: %v", err)
		} else if len(rows) != 0 {
			t.Fatalf("Create with concatenated body persisted %d key(s) for admin: %+v", len(rows), rows)
		}
	})

	t.Run("Rotate rejects concatenated JSON without rotating the predecessor", func(t *testing.T) {
		h, repo := newAPIKeyHandlerHarness(t)

		// Seed a live key via Create so the predecessor matches the production
		// shape (hash + prefix populated, owned by admin).
		seedBody, _ := json.Marshal(map[string]any{"name": "ci-bot", "scopes": []string{"read:objects"}})
		seedReq := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", bytes.NewReader(seedBody)))
		seedReq.Header.Set("Content-Type", "application/json")
		seedRec := httptest.NewRecorder()
		h.Create(seedRec, seedReq)
		if seedRec.Code != http.StatusCreated {
			t.Fatalf("seed Create returned %d body=%s", seedRec.Code, seedRec.Body.String())
		}
		var seeded APIKeyCreateResponse
		if err := json.NewDecoder(seedRec.Body).Decode(&seeded); err != nil {
			t.Fatalf("decode seeded key: %v", err)
		}
		beforePrefixes := snapshotAPIKeyPrefixes(repo)

		// Concatenated rotate body: first object would set graceDays=1, smuggled
		// trailer would set graceDays=999. With ReadJSON both must be refused.
		body := `{"graceDays":1}{"graceDays":999}`
		rotReq := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys/"+seeded.ID+"/rotate", strings.NewReader(body)))
		rotReq.Header.Set("Content-Type", "application/json")
		rotRec := httptest.NewRecorder()

		h.RotateFor(rotRec, rotReq, seeded.ID)

		assertSingleJSONValueRejection(t, rotRec, "InvalidAPIKeyRequest")

		// Predecessor must not have been stamped with rotates_at/successor_id.
		pred, err := repo.GetByID(context.Background(), seeded.ID)
		if err != nil {
			t.Fatalf("re-read predecessor: %v", err)
		}
		if pred.RotatesAt != nil {
			t.Fatalf("ambiguous Rotate stamped predecessor.RotatesAt = %v (want nil)", pred.RotatesAt)
		}
		if pred.SuccessorID != nil {
			t.Fatalf("ambiguous Rotate stamped predecessor.SuccessorID = %v (want nil)", pred.SuccessorID)
		}

		// No successor row should have been written. The set of prefixes in the
		// repo must equal the pre-rotation snapshot.
		afterPrefixes := snapshotAPIKeyPrefixes(repo)
		if !reflect.DeepEqual(afterPrefixes, beforePrefixes) {
			t.Fatalf("ambiguous Rotate added or removed key rows: before=%v after=%v", beforePrefixes, afterPrefixes)
		}
	})

	t.Run("well-formed bodies still succeed across both surfaces", func(t *testing.T) {
		h, repo := newAPIKeyHandlerHarness(t)

		// Create happy path.
		createBody, _ := json.Marshal(map[string]any{
			"name":   "ci-bot",
			"scopes": []string{"read:objects"},
		})
		createReq := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", bytes.NewReader(createBody)))
		createReq.Header.Set("Content-Type", "application/json")
		createRec := httptest.NewRecorder()
		h.Create(createRec, createReq)
		if createRec.Code != http.StatusCreated {
			t.Fatalf("happy Create returned %d body=%s", createRec.Code, createRec.Body.String())
		}
		var created APIKeyCreateResponse
		if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
			t.Fatalf("decode created key: %v", err)
		}
		if created.RawKey == "" || created.ID == "" {
			t.Fatalf("happy Create returned empty rawKey / id: %+v", created)
		}

		// Rotate happy path with an explicit graceDays body.
		rotateBody, _ := json.Marshal(map[string]any{"graceDays": 2})
		rotReq := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys/"+created.ID+"/rotate", bytes.NewReader(rotateBody)))
		rotReq.Header.Set("Content-Type", "application/json")
		rotRec := httptest.NewRecorder()
		h.RotateFor(rotRec, rotReq, created.ID)
		if rotRec.Code != http.StatusCreated {
			t.Fatalf("happy Rotate returned %d body=%s", rotRec.Code, rotRec.Body.String())
		}
		var rotated APIKeyRotateResponse
		if err := json.NewDecoder(rotRec.Body).Decode(&rotated); err != nil {
			t.Fatalf("decode rotated key: %v", err)
		}
		if rotated.RawKey == "" || rotated.ID == "" || rotated.PredecessorID != created.ID {
			t.Fatalf("happy Rotate response shape unexpected: %+v", rotated)
		}
		pred, err := repo.GetByID(context.Background(), created.ID)
		if err != nil {
			t.Fatalf("re-read predecessor after happy rotate: %v", err)
		}
		if pred.RotatesAt == nil || pred.SuccessorID == nil || *pred.SuccessorID != rotated.ID {
			t.Fatalf("happy Rotate did not stamp predecessor: rotatesAt=%v successor=%v", pred.RotatesAt, pred.SuccessorID)
		}
	})

	t.Run("Rotate with no body still uses default grace (regression for empty body)", func(t *testing.T) {
		h, repo := newAPIKeyHandlerHarness(t)

		// Seed a live key.
		seedBody, _ := json.Marshal(map[string]any{"name": "ci-bot"})
		seedReq := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", bytes.NewReader(seedBody)))
		seedRec := httptest.NewRecorder()
		h.Create(seedRec, seedReq)
		var seeded APIKeyCreateResponse
		if err := json.NewDecoder(seedRec.Body).Decode(&seeded); err != nil {
			t.Fatalf("decode seeded key: %v", err)
		}

		// Rotate with nil body — the handler must treat this as "use default
		// grace" and succeed (httputil.ReadJSON would otherwise reject EOF).
		rotReq := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys/"+seeded.ID+"/rotate", nil))
		rotRec := httptest.NewRecorder()
		h.RotateFor(rotRec, rotReq, seeded.ID)
		if rotRec.Code != http.StatusCreated {
			t.Fatalf("no-body Rotate returned %d body=%s", rotRec.Code, rotRec.Body.String())
		}
		pred, err := repo.GetByID(context.Background(), seeded.ID)
		if err != nil {
			t.Fatalf("re-read predecessor after no-body rotate: %v", err)
		}
		if pred.RotatesAt == nil || pred.SuccessorID == nil {
			t.Fatalf("no-body Rotate did not stamp predecessor: rotatesAt=%v successor=%v", pred.RotatesAt, pred.SuccessorID)
		}
	})
}

func snapshotAPIKeyPrefixes(repo *fakeAPIKeyRepo) []string {
	out := make([]string, 0, len(repo.byID))
	for _, rec := range repo.byID {
		out = append(out, rec.KeyPrefix)
	}
	sort.Strings(out)
	return out
}
