package tenants

import (
	"context"
	"net/http"
	"strconv"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
)

// Middleware returns a chi-compatible HTTP middleware that gates every
// request on the caller's per-tenant QPS quota. Anonymous callers (no
// auth.User on ctx, or no tenant claim) bypass the check — quotas only
// apply once a request is attributable to a tenant. The middleware
// also stamps the Manager on the request context so downstream handlers
// can call CheckObjectQuota / CheckStorageQuota at write time.
//
// On exceedance the middleware writes a 429 Too Many Requests response
// with the standardised error envelope (matches cmd/server/rate_limit.go
// shape) so SDK clients see the same retry semantics.
func Middleware(mgr *Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := WithManager(r.Context(), mgr)
			if mgr == nil {
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			tenant := TenantFromUser(auth.UserFromContext(ctx))
			if tenant == "" {
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			if !mgr.CheckQPS(ctx, tenant) {
				writeQuotaExceeded(w, mgr, ctx, tenant)
				return
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeQuotaExceeded(w http.ResponseWriter, mgr *Manager, ctx context.Context, tenant string) {
	// Provide a Retry-After hint based on the configured QPS so well-
	// behaved clients back off proportionally to their cap. 1/QPS rounded
	// up to whole seconds, minimum 1.
	retry := 1
	if mgr != nil && mgr.store != nil {
		if q, err := mgr.store.GetQuota(ctx, tenant); err == nil && q != nil && q.MaxQPS > 0 {
			s := 1.0 / q.MaxQPS
			if s > 1 {
				retry = int(s + 0.5)
			}
		}
	}
	w.Header().Set("Retry-After", strconv.Itoa(retry))
	apierror.WriteJSON(w, apierror.NewTooManyRequests("TenantQPSQuotaExceeded", map[string]string{
		"tenant":     tenant,
		"retryAfter": strconv.Itoa(retry),
	}))
}
