package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// statusCapturingResponseWriter wraps an http.ResponseWriter so the
// middleware can read the status code chi handlers wrote. The default
// http.ResponseWriter has no public way to read it back.
type statusCapturingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusCapturingResponseWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Write makes sure the implicit 200 path still records something. The
// stdlib http.ResponseWriter writes the 200 header lazily on first Write,
// so the wrapper needs to populate `status` even if WriteHeader was never
// called explicitly.
func (s *statusCapturingResponseWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// HTTPMiddleware returns a chi-compatible middleware that records request
// counts and durations against the package-level Prometheus metrics. The
// `path` label is the chi route template (e.g. /api/v2/things/{id}) so the
// label cardinality stays bounded. When chi has no matching route the
// fallback is the request URL path so 404s still produce a metric.
func HTTPMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			capture := &statusCapturingResponseWriter{ResponseWriter: w, status: 0}

			next.ServeHTTP(capture, r)

			// Resolve the labels AFTER the handler runs so chi has had a
			// chance to populate the route context.
			path := r.URL.Path
			if rctx := chi.RouteContext(r.Context()); rctx != nil {
				if pat := rctx.RoutePattern(); pat != "" {
					path = pat
				}
			}
			if capture.status == 0 {
				// Handler returned without calling WriteHeader and without
				// calling Write — treat as 200 (this is what http.Server
				// reports to clients).
				capture.status = http.StatusOK
			}

			method := r.Method
			status := strconv.Itoa(capture.status)
			httpRequestsTotal.WithLabelValues(method, path, status).Inc()
			observeDuration(httpRequestDuration.WithLabelValues(method, path), time.Since(start))
		})
	}
}
