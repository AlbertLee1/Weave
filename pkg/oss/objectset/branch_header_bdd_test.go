package objectset_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/liyang/weave/pkg/oss/objectset"
)

// TestBDD_LoadObjects_HonoursBranchHeader covers PRD-V2 Gap-T4 round
// 39 end-to-end via the LoadObjects endpoint: the X-Weave-Branch
// HTTP header pins reads to a non-main branch just like ?branch=,
// and the BranchScopeProvider receives the correct branch name on
// the live executor path.
//
// Acceptance criteria (Given → When → Then):
//
//	Given a LoadObjects request with X-Weave-Branch: feature-y
//	      and NO ?branch= query parameter
//	When  the handler runs
//	Then  the BranchScopeProvider is consulted with branch=feature-y
//
//	Given a LoadObjects request with BOTH ?branch=from-query and
//	      X-Weave-Branch: from-header
//	When  the handler runs
//	Then  the BranchScopeProvider is consulted with
//	      branch=from-query (query wins; matches round-39 contract)
//
//	Given a LoadObjects request with neither
//	When  the handler runs
//	Then  the BranchScopeProvider is NOT consulted (main-branch
//	      fast path, no overlay required)
func TestBDD_LoadObjects_HonoursBranchHeader(t *testing.T) {
	t.Run("X-Weave-Branch header pins to feature-y branch", func(t *testing.T) {
		handler, _, _ := setupHandlerTest(t)
		prov := newBranchHeaderRecordingProvider()
		handler.SetBranchScopeProvider(prov)

		body := `{"objectSet":{"type":"base","objectType":"employee"},"select":["id","name"]}`
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/myOntology/objectSets/loadObjects",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(objectset.BranchHeader, "feature-y")
		rec := httptest.NewRecorder()
		newAsOfRouter(t, handler).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		seen := prov.LastBranch()
		if seen != "feature-y" {
			t.Errorf("provider got branch = %q, want feature-y (X-Weave-Branch should be honored)", seen)
		}
	})

	t.Run("?branch=from-query wins over X-Weave-Branch:from-header", func(t *testing.T) {
		handler, _, _ := setupHandlerTest(t)
		prov := newBranchHeaderRecordingProvider()
		handler.SetBranchScopeProvider(prov)

		body := `{"objectSet":{"type":"base","objectType":"employee"},"select":["id","name"]}`
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/myOntology/objectSets/loadObjects?branch=from-query",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(objectset.BranchHeader, "from-header")
		rec := httptest.NewRecorder()
		newAsOfRouter(t, handler).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		seen := prov.LastBranch()
		if seen != "from-query" {
			t.Errorf("provider got branch = %q, want from-query (query overrides header)", seen)
		}
	})

	t.Run("neither query nor header → provider NOT consulted (main fast path)", func(t *testing.T) {
		handler, _, _ := setupHandlerTest(t)
		prov := newBranchHeaderRecordingProvider()
		handler.SetBranchScopeProvider(prov)

		body := `{"objectSet":{"type":"base","objectType":"employee"},"select":["id","name"]}`
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/myOntology/objectSets/loadObjects",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		newAsOfRouter(t, handler).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		if prov.CallCount() != 0 {
			t.Errorf("provider call count = %d, want 0 (main-branch fast path skips the overlay)", prov.CallCount())
		}
	})
}

// branchHeaderRecordingProvider is a minimal BranchScopeProvider
// double that records the most-recent branch name it was asked
// about. Identity-passes the live PK list so the LoadObjects body
// stays valid.
type branchHeaderRecordingProvider struct {
	mu         sync.Mutex
	lastBranch string
	calls      int
}

func newBranchHeaderRecordingProvider() *branchHeaderRecordingProvider {
	return &branchHeaderRecordingProvider{}
}

func (p *branchHeaderRecordingProvider) ScopeObjectSet(_ context.Context, branch, _, _ string, livePKs []string) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastBranch = branch
	p.calls++
	return livePKs, nil
}

func (p *branchHeaderRecordingProvider) LastBranch() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastBranch
}

func (p *branchHeaderRecordingProvider) CallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// Force imports honest in case other test files in the package
// don't already pull them in.
var _ = errors.New
