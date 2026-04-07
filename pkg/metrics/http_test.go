package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// httpTestRouter wires a chi router with the HTTPMiddleware so the tests
// exercise the same code path the production server uses.
func httpTestRouter(t *testing.T) *chi.Mux {
	t.Helper()
	r := chi.NewRouter()
	r.Use(HTTPMiddleware())
	r.Get("/api/v2/things/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Post("/api/v2/things", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	r.Get("/boom", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	return r
}

func TestHTTPMiddleware_IncrementsRequestCounter(t *testing.T) {
	// Reset the package-level counter so we can assert exact values.
	httpRequestsTotal.Reset()
	httpRequestDuration.Reset()

	r := httpTestRouter(t)

	// Hit the route twice; counter should increment by 2.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/things/42", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
	}

	got := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/api/v2/things/{id}", "200"))
	if got != 2 {
		t.Fatalf("expected 2 GET 200 requests, got %v", got)
	}
}

func TestHTTPMiddleware_RecordsDuration(t *testing.T) {
	httpRequestsTotal.Reset()
	httpRequestDuration.Reset()

	r := httpTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/things/42", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	count := testutil.CollectAndCount(httpRequestDuration, "weave_http_request_duration_seconds")
	if count == 0 {
		t.Fatalf("expected at least one observation in weave_http_request_duration_seconds")
	}
}

func TestHTTPMiddleware_CapturesStatusCode(t *testing.T) {
	httpRequestsTotal.Reset()
	httpRequestDuration.Reset()

	r := httpTestRouter(t)

	tests := []struct {
		method  string
		url     string
		path    string // chi route template
		status  string
	}{
		{http.MethodGet, "/api/v2/things/abc", "/api/v2/things/{id}", "200"},
		{http.MethodPost, "/api/v2/things", "/api/v2/things", "201"},
		{http.MethodGet, "/boom", "/boom", "500"},
	}
	for _, tc := range tests {
		req := httptest.NewRequest(tc.method, tc.url, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		got := testutil.ToFloat64(httpRequestsTotal.WithLabelValues(tc.method, tc.path, tc.status))
		if got != 1 {
			t.Errorf("status %s for %s %s: expected counter=1, got %v", tc.status, tc.method, tc.url, got)
		}
	}
}

func TestHTTPMiddleware_UnmatchedRouteUsesPath(t *testing.T) {
	// When chi has no template for the URL (404), the middleware should
	// fall back to the request URL path so we still record SOMETHING and
	// don't drop the metric. We deliberately use the literal path, not
	// the route pattern, so a 404 doesn't produce an empty label.
	httpRequestsTotal.Reset()

	r := chi.NewRouter()
	r.Use(HTTPMiddleware())
	// register a stub route so chi materializes the middleware tree
	r.Get("/__stub", func(w http.ResponseWriter, _ *http.Request) {})

	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	expected := `
# HELP weave_http_requests_total Total HTTP requests handled by the Weave server, partitioned by method, route template, and status code.
# TYPE weave_http_requests_total counter
weave_http_requests_total{method="GET",path="/nope",status="404"} 1
`
	if err := testutil.CollectAndCompare(httpRequestsTotal, strings.NewReader(expected), "weave_http_requests_total"); err != nil {
		t.Fatalf("compare: %v", err)
	}
}
