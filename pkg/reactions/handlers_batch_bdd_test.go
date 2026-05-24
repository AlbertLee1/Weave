package reactions

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/auth"
)

// TestBDD_Reactions_BatchAggregate covers the round-67 Foundry-
// parity gap. Foundry's ObjectList view renders the reaction bar
// inline on every visible row; with 50 rows × 1 GET /api/v2/
// reactions per row that's 50 parallel HTTP roundtrips just to
// render a page. The bulk endpoint POST /api/v2/reactions/batch
// collapses those into one request and (on PG-backed deploys)
// one SQL round-trip via WHERE target_rid = ANY($1).
//
// Wire shape:
//
//   POST /api/v2/reactions/batch
//   {"targetRids": ["ri.a", "ri.b", "ri.c"]}
//     200 + {"summaries": [Summary{a}, Summary{b}, Summary{c}]}
//          summaries[i] always corresponds to targetRids[i] so
//          callers can index without re-keying.
//     400 InvalidRequestBody / InvalidReactionTarget on a malformed
//         body or a non-ri.* prefix in the array.
//
// Scenarios:
//   - Empty input array returns 200 + {summaries: []} (no error;
//     idempotent on degenerate input so a Foundry "render no rows"
//     state doesn't 400 the badge poll).
//   - Single target reproduces the single-Aggregate result
//     verbatim.
//   - Three-target batch returns each in input order with correct
//     counts.
//   - A target with NO reactions gets Summary{targetRid, emojis:[]}
//     (non-nil empty slice — SPA iterates without nil-checks).
//   - mine flag is set per-caller, not global: caller A's "mine"
//     for an emoji that only caller B reacted to is false.
//   - One bogus targetRid (no ri. prefix) rejects the WHOLE batch
//     with 400 — we don't want a partial-success contract where
//     the SPA has to inspect each Summary to see which targets
//     silently dropped.
//   - Authentication required: anonymous caller gets 401.
func TestBDD_Reactions_BatchAggregate(t *testing.T) {
	alice := &auth.User{ID: "u-alice"}
	bob := &auth.User{ID: "u-bob"}

	const (
		targetA = "ri.objects.main.Customer.1"
		targetB = "ri.objects.main.Customer.2"
		targetC = "ri.objects.main.Customer.3"
	)

	seedReactions := func(t *testing.T, store *MemoryStore) {
		t.Helper()
		seed := []struct {
			user      *auth.User
			targetRID string
			emoji     string
		}{
			{alice, targetA, "👍"},
			{bob, targetA, "👍"},
			{bob, targetA, "🔥"},
			{alice, targetB, "🎉"},
			// targetC: intentionally empty
		}
		for _, s := range seed {
			err := store.Create(nil, &Reaction{
				ID:        "r-" + s.user.ID + "-" + s.targetRID + "-" + s.emoji,
				UserID:    s.user.ID,
				TargetRID: s.targetRID,
				Emoji:     s.emoji,
			})
			if err != nil {
				t.Fatalf("seed: %v", err)
			}
		}
	}

	doBatch := func(t *testing.T, store Store, user *auth.User, body map[string]interface{}) *httptest.ResponseRecorder {
		t.Helper()
		r := newTestRouter(store, user)
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v2/reactions/batch", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("Empty input returns {summaries: []} not 400", func(t *testing.T) {
		store := NewMemoryStore()
		rec := doBatch(t, store, alice, map[string]interface{}{"targetRids": []string{}})
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp struct{ Summaries []Summary }
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Summaries == nil {
			t.Errorf("summaries is nil, want empty array")
		}
		if len(resp.Summaries) != 0 {
			t.Errorf("len(summaries)=%d, want 0", len(resp.Summaries))
		}
	})

	t.Run("Single-target batch matches Aggregate output", func(t *testing.T) {
		store := NewMemoryStore()
		seedReactions(t, store)
		rec := doBatch(t, store, alice, map[string]interface{}{"targetRids": []string{targetA}})
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d", rec.Code)
		}
		var resp struct{ Summaries []Summary }
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Summaries) != 1 {
			t.Fatalf("len(summaries)=%d, want 1", len(resp.Summaries))
		}
		got := resp.Summaries[0]
		if got.TargetRID != targetA {
			t.Errorf("targetRid=%q, want %q", got.TargetRID, targetA)
		}
		// Two emojis on targetA: 👍 has 2, 🔥 has 1.
		if len(got.Emojis) != 2 {
			t.Fatalf("len(emojis)=%d, want 2; got=%+v", len(got.Emojis), got.Emojis)
		}
	})

	t.Run("Three-target batch preserves input order", func(t *testing.T) {
		store := NewMemoryStore()
		seedReactions(t, store)
		rec := doBatch(t, store, alice, map[string]interface{}{
			"targetRids": []string{targetC, targetA, targetB}, // deliberately out of seed order
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d", rec.Code)
		}
		var resp struct{ Summaries []Summary }
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Summaries) != 3 {
			t.Fatalf("len(summaries)=%d, want 3", len(resp.Summaries))
		}
		// Input order preserved so callers can index by position.
		if resp.Summaries[0].TargetRID != targetC ||
			resp.Summaries[1].TargetRID != targetA ||
			resp.Summaries[2].TargetRID != targetB {
			t.Errorf("order broken: got [%s,%s,%s], want [%s,%s,%s]",
				resp.Summaries[0].TargetRID, resp.Summaries[1].TargetRID, resp.Summaries[2].TargetRID,
				targetC, targetA, targetB)
		}
	})

	t.Run("Target with no reactions returns non-nil empty Emojis", func(t *testing.T) {
		store := NewMemoryStore()
		seedReactions(t, store)
		rec := doBatch(t, store, alice, map[string]interface{}{"targetRids": []string{targetC}})
		var resp struct{ Summaries []Summary }
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Summaries[0].Emojis == nil {
			t.Errorf("emojis is nil, want empty array (SPA should iterate without nil check)")
		}
		if len(resp.Summaries[0].Emojis) != 0 {
			t.Errorf("len(emojis)=%d, want 0", len(resp.Summaries[0].Emojis))
		}
	})

	t.Run("Mine flag is per-caller", func(t *testing.T) {
		store := NewMemoryStore()
		seedReactions(t, store)
		// Alice didn't react with 🔥 on targetA — only Bob did.
		// So Alice's "mine" for 🔥 must be false.
		rec := doBatch(t, store, alice, map[string]interface{}{"targetRids": []string{targetA}})
		var resp struct{ Summaries []Summary }
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		fireMine := false
		thumbsMine := false
		for _, e := range resp.Summaries[0].Emojis {
			if e.Emoji == "🔥" {
				fireMine = e.Mine
			}
			if e.Emoji == "👍" {
				thumbsMine = e.Mine
			}
		}
		if fireMine {
			t.Errorf("alice's mine flag for 🔥 = true, want false (only bob reacted with 🔥)")
		}
		if !thumbsMine {
			t.Errorf("alice's mine flag for 👍 = false, want true (alice reacted with 👍)")
		}
	})

	t.Run("Bogus targetRid rejects whole batch with 400", func(t *testing.T) {
		store := NewMemoryStore()
		seedReactions(t, store)
		rec := doBatch(t, store, alice, map[string]interface{}{
			"targetRids": []string{targetA, "not-a-rid", targetB},
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "InvalidReactionTarget" {
			t.Errorf("errorName=%v, want InvalidReactionTarget", body["errorName"])
		}
	})

	t.Run("Anonymous caller gets 401", func(t *testing.T) {
		store := NewMemoryStore()
		rec := doBatch(t, store, nil, map[string]interface{}{"targetRids": []string{targetA}})
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status=%d, want 401", rec.Code)
		}
	})
}
