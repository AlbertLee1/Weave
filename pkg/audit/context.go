package audit

import (
	"context"
	"net/http"
	"strings"
)

// ClientInfo captures the remote-caller identifiers that audit_events needs
// to persist alongside the actor/action tuple: the originating IP and the
// user-agent string. Values are extracted from the *http.Request at the
// chi middleware layer and stashed on the request context so service-layer
// code (pkg/oss read paths) can record audits without taking an http
// dependency.
type ClientInfo struct {
	IP        string
	UserAgent string
}

type clientInfoKey struct{}

// WithClientInfo stores ClientInfo on ctx. Returns ctx unchanged when info
// is zero-valued so middleware can unconditionally wrap every request
// without polluting the context tree.
func WithClientInfo(ctx context.Context, info ClientInfo) context.Context {
	if info.IP == "" && info.UserAgent == "" {
		return ctx
	}
	return context.WithValue(ctx, clientInfoKey{}, info)
}

// ClientInfoFromContext returns the ClientInfo stamped onto ctx by
// ClientInfoMiddleware. The zero value is returned when the middleware has
// not run (e.g. non-HTTP callers) — both IP and UserAgent empty.
func ClientInfoFromContext(ctx context.Context) ClientInfo {
	if ctx == nil {
		return ClientInfo{}
	}
	v, _ := ctx.Value(clientInfoKey{}).(ClientInfo)
	return v
}

// ClientInfoMiddleware extracts the caller's IP and User-Agent from each
// HTTP request and stamps them onto the request context. Downstream service
// code (pkg/oss data-access audit, future compliance hooks) reads the
// values via ClientInfoFromContext without reaching back to the *http.Request.
//
// Honours the standard proxy chain: X-Forwarded-For (first entry) wins when
// present, otherwise falls back to the RemoteAddr host. RequestURI/UA do
// not need normalisation.
func ClientInfoMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := ClientInfo{
			IP:        extractClientIP(r),
			UserAgent: r.UserAgent(),
		}
		ctx := WithClientInfo(r.Context(), info)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractClientIP returns the caller's IP, preferring X-Forwarded-For (first
// hop) over RemoteAddr. RemoteAddr is usually "host:port" from net/http so
// the port is stripped; malformed values fall through unchanged.
func extractClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		parts := strings.Split(xf, ",")
		return strings.TrimSpace(parts[0])
	}
	addr := r.RemoteAddr
	if i := strings.LastIndex(addr, ":"); i > 0 {
		return addr[:i]
	}
	return addr
}
