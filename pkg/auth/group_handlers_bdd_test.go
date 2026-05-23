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

// TestBDD_GroupAdminWritesRejectAmbiguousJSONBody_P2A302 covers the three
// group admin write surfaces:
//
//   - POST  /api/admin/groups                       (Create)
//   - PATCH /api/admin/groups/{id}                  (Update)
//   - POST  /api/admin/groups/{id}/members          (AddMember)
//
// For each surface it asserts that a body composed of two concatenated JSON
// objects is rejected with HTTP 400 plus a "single JSON value" reason, and
// that the underlying repository state is not mutated by the rejected
// request. A trailing well-formed regression sub-test confirms the existing
// happy paths still succeed after the hardening.
func TestBDD_GroupAdminWritesRejectAmbiguousJSONBody_P2A302(t *testing.T) {
	t.Run("Create rejects concatenated JSON without inserting a group", func(t *testing.T) {
		h, repo := newGroupHandlerHarness()
		seedNames := snapshotGroupNames(repo)

		body := `{"name":"analysts","description":"safe"}` +
			`{"name":"smuggled","description":"backdoor"}`
		req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/groups", strings.NewReader(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.Create(rec, req)

		assertSingleJSONValueRejection(t, rec, "InvalidGroupRequest")

		afterNames := snapshotGroupNames(repo)
		if !reflect.DeepEqual(afterNames, seedNames) {
			t.Fatalf("Create with concatenated body mutated group set: before=%v after=%v", seedNames, afterNames)
		}
		if _, err := repo.GetByName(context.Background(), "analysts"); err == nil {
			t.Fatalf("Create with concatenated body persisted first-decoded group 'analysts'")
		}
		if _, err := repo.GetByName(context.Background(), "smuggled"); err == nil {
			t.Fatalf("Create with concatenated body persisted smuggled group 'smuggled'")
		}
	})

	t.Run("Update rejects concatenated JSON without mutating the group", func(t *testing.T) {
		h, repo := newGroupHandlerHarness()
		g := &Group{Name: "analysts", Description: "original"}
		if err := repo.Create(context.Background(), g); err != nil {
			t.Fatalf("seed analysts group: %v", err)
		}

		firstDesc := "first-decoded"
		first, err := json.Marshal(GroupUpdateRequest{Description: &firstDesc})
		if err != nil {
			t.Fatalf("marshal first patch: %v", err)
		}
		body := string(first) + `{"description":"smuggled-second"}`
		req := withAdmin(httptest.NewRequest(http.MethodPatch, "/api/admin/groups/"+g.ID, strings.NewReader(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.updateFor(rec, req, g.ID)

		assertSingleJSONValueRejection(t, rec, "InvalidGroupUpdate")

		got, err := repo.GetByID(context.Background(), g.ID)
		if err != nil {
			t.Fatalf("re-read analysts: %v", err)
		}
		if got.Description != "original" {
			t.Fatalf("ambiguous Update mutated description to %q (want %q)", got.Description, "original")
		}
		if got.Name != "analysts" {
			t.Fatalf("ambiguous Update mutated name to %q (want %q)", got.Name, "analysts")
		}
	})

	t.Run("AddMember rejects concatenated JSON without adding a member", func(t *testing.T) {
		h, repo := newGroupHandlerHarness()
		g := &Group{Name: "analysts"}
		if err := repo.Create(context.Background(), g); err != nil {
			t.Fatalf("seed analysts group: %v", err)
		}

		body := `{"userId":"user:alice"}{"userId":"user:eve"}`
		req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/groups/"+g.ID+"/members", strings.NewReader(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.addMemberFor(rec, req, g.ID)

		assertSingleJSONValueRejection(t, rec, "InvalidMemberRequest")

		members, err := repo.ListMembers(context.Background(), g.ID)
		if err != nil {
			t.Fatalf("re-read analysts members: %v", err)
		}
		if len(members) != 0 {
			t.Fatalf("ambiguous AddMember mutated membership: got %v want []", members)
		}
	})

	t.Run("well-formed bodies still succeed across all three surfaces", func(t *testing.T) {
		h, repo := newGroupHandlerHarness()

		// Create happy path.
		createBody, _ := json.Marshal(map[string]any{
			"name":        "analysts",
			"description": "ML team",
		})
		createReq := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/groups", bytes.NewReader(createBody)))
		createReq.Header.Set("Content-Type", "application/json")
		createRec := httptest.NewRecorder()
		h.Create(createRec, createReq)
		if createRec.Code != http.StatusCreated {
			t.Fatalf("happy Create returned %d body=%s", createRec.Code, createRec.Body.String())
		}
		var created GroupResponse
		if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
			t.Fatalf("decode created group: %v", err)
		}
		if created.ID == "" {
			t.Fatalf("happy Create returned empty group ID")
		}

		// Update happy path.
		newDesc := "renamed"
		updateBody, _ := json.Marshal(GroupUpdateRequest{Description: &newDesc})
		updateReq := withAdmin(httptest.NewRequest(http.MethodPatch, "/api/admin/groups/"+created.ID, bytes.NewReader(updateBody)))
		updateReq.Header.Set("Content-Type", "application/json")
		updateRec := httptest.NewRecorder()
		h.updateFor(updateRec, updateReq, created.ID)
		if updateRec.Code != http.StatusOK {
			t.Fatalf("happy Update returned %d body=%s", updateRec.Code, updateRec.Body.String())
		}
		afterUpd, err := repo.GetByID(context.Background(), created.ID)
		if err != nil {
			t.Fatalf("re-read analysts after happy update: %v", err)
		}
		if afterUpd.Description != newDesc {
			t.Fatalf("happy Update did not persist new description: %q", afterUpd.Description)
		}

		// AddMember happy path.
		memberBody, _ := json.Marshal(map[string]any{"userId": "user:alice"})
		memberReq := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/groups/"+created.ID+"/members", bytes.NewReader(memberBody)))
		memberReq.Header.Set("Content-Type", "application/json")
		memberRec := httptest.NewRecorder()
		h.addMemberFor(memberRec, memberReq, created.ID)
		if memberRec.Code != http.StatusNoContent {
			t.Fatalf("happy AddMember returned %d body=%s", memberRec.Code, memberRec.Body.String())
		}
		members, err := repo.ListMembers(context.Background(), created.ID)
		if err != nil {
			t.Fatalf("re-read members after happy add: %v", err)
		}
		if len(members) != 1 || members[0] != "user:alice" {
			t.Fatalf("happy AddMember: got %v want [user:alice]", members)
		}
	})
}

func snapshotGroupNames(repo *fakeGroupRepo) []string {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	out := make([]string, 0, len(repo.byID))
	for _, g := range repo.byID {
		out = append(out, g.Name)
	}
	sort.Strings(out)
	return out
}
