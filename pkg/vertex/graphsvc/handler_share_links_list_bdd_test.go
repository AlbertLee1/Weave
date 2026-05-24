package graphsvc_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestBDD_ShareLinks_ListForGraph covers the round-69 Foundry-
// parity gap. The original ShareLinkStore interface comment in
// pkg/vertex/graphsvc/sharing.go explicitly defers list-by-graph
// to a "future manage-share-links surface that isn't part of
// VTX-013" — round 69 is that surface. Without it, a graph owner
// who creates a share link and forgets to save the token has no
// way to discover the link exists; the row stays in PG indefinitely
// (delete-graph cascade will eventually clean it on graph delete,
// but the live "manage shares" panel can't render).
//
// Wire shape:
//
//   GET /api/vertex/v1/graphs/{rid}/share-links
//     200 + {"shareLinks": [
//             {tokenSuffix, createdBy, createdAt, expiresAt,
//              revoked, revokedAt},
//             ...
//           ]}
//         sorted by createdAt DESC (newest first)
//     401 / 403 when caller isn't the graph owner — same
//         canManageShareLinks check as createShareLink so the
//         management surface mirrors the mutation surface
//     404 when graph doesn't exist
//
// Security note: the full Token is NOT included in the list
// response (only `tokenSuffix` = last 8 chars for identification).
// Returning the full token would let anyone with graph-manage
// access enumerate all live share URLs by listing — defeating
// the "save it at create time or lose it" lifecycle the create
// endpoint enforces. The BDD pins this invariant explicitly.
//
// Scenarios:
//   - Owner lists own graph's share-links: 200, response includes
//     all (active + revoked) sorted newest-first, tokenSuffix is
//     populated but full token field is absent.
//   - Empty graph (no share links) returns 200 + {shareLinks: []}.
//   - Non-owner gets 403 — same authorization gate as create.
//   - Anonymous caller gets 401.
//   - Graph doesn't exist → 404.
//   - Two links created in known order are returned newest-first
//     so the SPA Recent Shares panel ordering matches.
//   - Revoked link is INCLUDED in the list (owner needs to see
//     it to know why a recipient is hitting 410 Gone), but the
//     revoked field is true and revokedAt is populated.
func TestBDD_ShareLinks_ListForGraph(t *testing.T) {
	const owner = "u-owner"
	const other = "u-other"

	t.Run("Owner lists own graph's share-links newest-first", func(t *testing.T) {
		r, _, _ := newACLTestHandler(t)
		graphRID := createOwnedGraph(t, r, owner)

		// Create two share-links in known order so we can assert
		// newest-first ordering. Each create returns a full token
		// (one-time disclosure); we discard them deliberately —
		// the list path must reveal only the suffix.
		_ = createShareLinkAs(t, r, owner, graphRID)
		_ = createShareLinkAs(t, r, owner, graphRID)

		w := doAsUser(t, r, owner, "GET", "/api/vertex/v1/graphs/"+graphRID+"/share-links", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
		}
		var resp struct {
			ShareLinks []map[string]any `json:"shareLinks"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.ShareLinks) != 2 {
			t.Fatalf("len(shareLinks)=%d, want 2; body=%s", len(resp.ShareLinks), w.Body.String())
		}
		// Full token MUST NOT leak in the list. tokenSuffix is the
		// only token-shaped field that survives.
		for i, link := range resp.ShareLinks {
			if _, hasFullToken := link["token"]; hasFullToken {
				t.Errorf("shareLinks[%d] leaked full token field: %v", i, link)
			}
			suffix, ok := link["tokenSuffix"].(string)
			if !ok || suffix == "" {
				t.Errorf("shareLinks[%d] missing tokenSuffix; got %v", i, link["tokenSuffix"])
			}
			if len(suffix) > 8 {
				t.Errorf("shareLinks[%d] tokenSuffix length=%d, want <= 8 (last-8 identification only)",
					i, len(suffix))
			}
		}
		// Newest-first ordering: createdAt[0] >= createdAt[1].
		ca0, _ := resp.ShareLinks[0]["createdAt"].(string)
		ca1, _ := resp.ShareLinks[1]["createdAt"].(string)
		if ca0 < ca1 {
			t.Errorf("ordering broken: shareLinks[0].createdAt=%s < [1].createdAt=%s",
				ca0, ca1)
		}
	})

	t.Run("Empty graph returns {shareLinks: []}", func(t *testing.T) {
		r, _, _ := newACLTestHandler(t)
		graphRID := createOwnedGraph(t, r, owner)
		w := doAsUser(t, r, owner, "GET", "/api/vertex/v1/graphs/"+graphRID+"/share-links", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
		}
		var resp struct {
			ShareLinks []map[string]any `json:"shareLinks"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.ShareLinks == nil {
			t.Errorf("shareLinks is nil, want empty array")
		}
		if len(resp.ShareLinks) != 0 {
			t.Errorf("len(shareLinks)=%d, want 0", len(resp.ShareLinks))
		}
	})

	t.Run("Non-owner gets 403 (same ACL as createShareLink)", func(t *testing.T) {
		r, _, _ := newACLTestHandler(t)
		graphRID := createOwnedGraph(t, r, owner)
		_ = createShareLinkAs(t, r, owner, graphRID)
		w := doAsUser(t, r, other, "GET", "/api/vertex/v1/graphs/"+graphRID+"/share-links", nil)
		if w.Code != http.StatusForbidden {
			t.Errorf("status=%d, want 403", w.Code)
		}
	})

	t.Run("Anonymous caller gets 401", func(t *testing.T) {
		r, _, _ := newACLTestHandler(t)
		graphRID := createOwnedGraph(t, r, owner)
		w := doAsUser(t, r, "", "GET", "/api/vertex/v1/graphs/"+graphRID+"/share-links", nil)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status=%d, want 401", w.Code)
		}
	})

	t.Run("Unknown graph returns 404", func(t *testing.T) {
		r, _, _ := newACLTestHandler(t)
		w := doAsUser(t, r, owner, "GET",
			"/api/vertex/v1/graphs/ri.vertex.main.graph.NEVER-EXISTED/share-links", nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("status=%d, want 404", w.Code)
		}
	})

	t.Run("Revoked link is included with revoked=true", func(t *testing.T) {
		r, _, _ := newACLTestHandler(t)
		graphRID := createOwnedGraph(t, r, owner)
		token := createShareLinkAs(t, r, owner, graphRID)
		// Revoke it via the existing endpoint.
		w := doAsUser(t, r, owner, "DELETE",
			"/api/vertex/v1/share-links/"+token, nil)
		if w.Code != http.StatusNoContent && w.Code != http.StatusOK {
			t.Fatalf("revoke status=%d; body=%s", w.Code, w.Body.String())
		}
		// List should still include it with revoked=true.
		w = doAsUser(t, r, owner, "GET",
			"/api/vertex/v1/graphs/"+graphRID+"/share-links", nil)
		var resp struct {
			ShareLinks []map[string]any `json:"shareLinks"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if len(resp.ShareLinks) != 1 {
			t.Fatalf("len=%d, want 1 (revoked link must still appear so owner knows why it's 410ing)",
				len(resp.ShareLinks))
		}
		revoked, _ := resp.ShareLinks[0]["revoked"].(bool)
		if !revoked {
			t.Errorf("revoked=%v, want true", resp.ShareLinks[0]["revoked"])
		}
		// tokenSuffix matches the suffix of the originally-issued token.
		if suffix, ok := resp.ShareLinks[0]["tokenSuffix"].(string); !ok ||
			!strings.HasSuffix(token, suffix) {
			t.Errorf("tokenSuffix=%q does not match suffix of created token=%q",
				resp.ShareLinks[0]["tokenSuffix"], token)
		}
	})
}

// createShareLinkAs issues a POST against the create endpoint and
// returns the full token from the response so the test can assert
// suffix-matching later.
func createShareLinkAs(t *testing.T, r chi.Router, user, graphRID string) string {
	t.Helper()
	w := doAsUser(t, r, user, "POST",
		"/api/vertex/v1/graphs/"+graphRID+"/share-links", map[string]any{})
	if w.Code != http.StatusCreated {
		t.Fatalf("create share link status=%d; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if resp.Token == "" {
		t.Fatalf("created share link has empty token; body=%s", w.Body.String())
	}
	return resp.Token
}
