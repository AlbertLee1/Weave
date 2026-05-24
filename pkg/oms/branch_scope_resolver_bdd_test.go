package oms_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_ResolveBranchFromRequest covers PRD-V2 Gap-T4 round 39:
// branch pinning now accepts either `?branch=<name>` query
// parameter (the historical signal, supported since US-381 /
// US-384) OR `X-Weave-Branch: <name>` HTTP header (round 39
// addition). The query parameter wins when both are present so
// explicit URL pinning beats the implicit per-client default —
// matches Foundry's "request param is authoritative" rule.
//
// Acceptance criteria (Given → When → Then):
//
//   Given a request with NEITHER query param NOR header
//   When  ResolveBranchFromRequest runs
//   Then  it returns DefaultBranch ("main")
//
//   Given a request with ONLY ?branch=feature-x
//   When  ResolveBranchFromRequest runs
//   Then  it returns "feature-x"
//
//   Given a request with ONLY X-Weave-Branch: feature-y header
//   When  ResolveBranchFromRequest runs
//   Then  it returns "feature-y" (round 39 / Gap-T4 addition)
//
//   Given a request with BOTH ?branch=from-query AND
//         X-Weave-Branch: from-header
//   When  ResolveBranchFromRequest runs
//   Then  it returns "from-query" (query wins — explicit beats
//         implicit; matches Foundry's request-param-authoritative
//         rule)
//
//   Given a nil *http.Request
//   When  ResolveBranchFromRequest runs
//   Then  it returns DefaultBranch (defensive — never panics)
func TestBDD_ResolveBranchFromRequest(t *testing.T) {
	t.Run("neither query nor header → DefaultBranch", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
		got := oms.ResolveBranchFromRequest(req)
		if got != oms.DefaultBranch {
			t.Errorf("got %q, want %q (DefaultBranch)", got, oms.DefaultBranch)
		}
	})

	t.Run("query only → returns query value", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/whatever?branch=feature-x", nil)
		if got := oms.ResolveBranchFromRequest(req); got != "feature-x" {
			t.Errorf("got %q, want feature-x", got)
		}
	})

	t.Run("header only → returns header value (round 39 addition)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
		req.Header.Set(oms.BranchHeader, "feature-y")
		if got := oms.ResolveBranchFromRequest(req); got != "feature-y" {
			t.Errorf("got %q, want feature-y", got)
		}
	})

	t.Run("query + header → query wins (explicit beats implicit)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/whatever?branch=from-query", nil)
		req.Header.Set(oms.BranchHeader, "from-header")
		if got := oms.ResolveBranchFromRequest(req); got != "from-query" {
			t.Errorf("got %q, want from-query (query overrides header)", got)
		}
	})

	t.Run("nil request → DefaultBranch (defensive)", func(t *testing.T) {
		if got := oms.ResolveBranchFromRequest(nil); got != oms.DefaultBranch {
			t.Errorf("got %q, want DefaultBranch on nil request", got)
		}
	})

	t.Run("BranchHeader constant matches expected header name", func(t *testing.T) {
		// Documents the wire-contract header name for SDK clients
		// generating from this constant.
		if oms.BranchHeader != "X-Weave-Branch" {
			t.Errorf("BranchHeader = %q, want X-Weave-Branch", oms.BranchHeader)
		}
	})
}
