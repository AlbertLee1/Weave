package main

// US-249: the 5 service-account admin CRUD endpoints must be reachable
// through NewFullRouter when a ServiceAccountRepository is wired. In
// degraded mode (no repo) the routes stay unmounted.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/auth"
)

// us249FakeSARepo is a minimal ServiceAccountRepository stub for the wire
// test. It only needs to exist — the test asserts route registration, not
// persistence semantics (those live in pkg/auth/service_account_handlers_test.go).
type us249FakeSARepo struct{}

func (us249FakeSARepo) Create(context.Context, *auth.ServiceAccount) error   { return nil }
func (us249FakeSARepo) GetByID(_ context.Context, id string) (*auth.ServiceAccount, error) {
	return &auth.ServiceAccount{ID: id, Name: "stub", OwnerUserID: "user:admin@example.com"}, nil
}
func (us249FakeSARepo) GetByName(context.Context, string) (*auth.ServiceAccount, error) {
	return nil, auth.ErrServiceAccountNotFound
}
func (us249FakeSARepo) ListActive(context.Context) ([]*auth.ServiceAccount, error) {
	return nil, nil
}
func (us249FakeSARepo) Update(_ context.Context, id string, _ auth.ServiceAccountUpdate) (*auth.ServiceAccount, error) {
	return &auth.ServiceAccount{ID: id, Name: "stub", OwnerUserID: "user:admin@example.com", UpdatedAt: time.Now()}, nil
}
func (us249FakeSARepo) Disable(context.Context, string) error { return nil }

func TestUS249_ServiceAccountRoutesRegistered(t *testing.T) {
	deps := &ServerDeps{ServiceAccountRepo: us249FakeSARepo{}}
	router := NewFullRouter(deps)

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/admin/service-accounts"},
		{http.MethodGet, "/api/admin/service-accounts"},
		{http.MethodGet, "/api/admin/service-accounts/abc"},
		{http.MethodPatch, "/api/admin/service-accounts/abc"},
		{http.MethodDelete, "/api/admin/service-accounts/abc"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(`{"name":"ci-bot"}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			// Dev AUTH_MODE plus no RequirePermission bypass is handled
			// inside the middleware stack; the route being REGISTERED is
			// proven by any non-404 status (routes fall through to the
			// handler which then returns a typed JSON error).
			if rec.Code == http.StatusNotFound &&
				!strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
				t.Errorf("%s %s: route not mounted (chi 404)", tt.method, tt.path)
			}
		})
	}
}

// TestUS249_ServiceAccountRoutesUnmountedWithoutRepo confirms the degraded
// mode contract: no repo wired => routes not registered.
func TestUS249_ServiceAccountRoutesUnmountedWithoutRepo(t *testing.T) {
	router := NewFullRouter(&ServerDeps{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/service-accounts", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 with no repo wired, got %d", rec.Code)
	}
}
