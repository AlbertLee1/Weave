package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/liyang/weave/internal/config"
	"github.com/liyang/weave/pkg/auth"
)

// captureHandler is a slog.Handler that captures Records into a slice for
// assertions. It is safe for concurrent use because the request logging
// middleware fires from per-request goroutines in tests.
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (c *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (c *captureHandler) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, r.Clone())
	return nil
}

func (c *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *captureHandler) WithGroup(string) slog.Handler      { return c }

func (c *captureHandler) snapshot() []slog.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]slog.Record, len(c.records))
	copy(out, c.records)
	return out
}

// findAttr returns the value of the first attribute with the given key
// in the given record, or "", false if not present. Bool returned because
// some fields (e.g. duration_ms) may be zero in fast tests.
func findAttr(r slog.Record, key string) (slog.Value, bool) {
	var (
		val   slog.Value
		found bool
	)
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			val = a.Value
			found = true
			return false
		}
		return true
	})
	return val, found
}

func newTestRouter(logger *slog.Logger) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(RequestLoggerMiddleware(logger))
	r.Get("/echo", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("ok"))
	})
	return r
}

func TestRequestLoggingMiddleware_LogsMethod(t *testing.T) {
	cap := &captureHandler{}
	logger := slog.New(cap)
	r := newTestRouter(logger)

	req := httptest.NewRequest(http.MethodGet, "/echo", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	records := cap.snapshot()
	if len(records) == 0 {
		t.Fatal("expected at least one log record from request middleware")
	}
	rec := records[0]
	v, ok := findAttr(rec, "method")
	if !ok || v.String() != "GET" {
		t.Errorf("expected method=GET, got %v (present=%v)", v, ok)
	}
	pv, _ := findAttr(rec, "path")
	if pv.String() != "/echo" {
		t.Errorf("expected path=/echo, got %v", pv)
	}
	sv, _ := findAttr(rec, "status")
	if sv.Int64() != http.StatusTeapot {
		t.Errorf("expected status=%d, got %v", http.StatusTeapot, sv)
	}
}

func TestRequestLoggingMiddleware_LogsRequestID(t *testing.T) {
	cap := &captureHandler{}
	logger := slog.New(cap)
	r := newTestRouter(logger)

	req := httptest.NewRequest(http.MethodGet, "/echo", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	records := cap.snapshot()
	if len(records) == 0 {
		t.Fatal("expected at least one log record")
	}
	v, ok := findAttr(records[0], "request_id")
	if !ok {
		t.Fatal("expected request_id attribute on log record")
	}
	if v.String() == "" {
		t.Errorf("expected non-empty request_id, got empty string")
	}
}

func TestRequestLoggingMiddleware_LogsDurationMs(t *testing.T) {
	cap := &captureHandler{}
	logger := slog.New(cap)
	r := newTestRouter(logger)

	req := httptest.NewRequest(http.MethodGet, "/echo", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	records := cap.snapshot()
	if len(records) == 0 {
		t.Fatal("expected at least one log record")
	}
	if _, ok := findAttr(records[0], "duration_ms"); !ok {
		t.Error("expected duration_ms attribute on log record")
	}
}

func TestRequestLoggingMiddleware_LogsRemoteIP(t *testing.T) {
	cap := &captureHandler{}
	logger := slog.New(cap)
	r := newTestRouter(logger)

	req := httptest.NewRequest(http.MethodGet, "/echo", nil)
	req.RemoteAddr = "203.0.113.7:54321"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	records := cap.snapshot()
	if len(records) == 0 {
		t.Fatal("expected at least one log record")
	}
	v, ok := findAttr(records[0], "remote_ip")
	if !ok {
		t.Fatal("expected remote_ip attribute on log record")
	}
	if v.String() == "" {
		t.Errorf("expected non-empty remote_ip")
	}
}

func TestRequestLoggingMiddleware_LogsUserIDIfPresent(t *testing.T) {
	cap := &captureHandler{}
	logger := slog.New(cap)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	// Inject a user before the logging middleware so we exercise the
	// auth.UserFromContext branch.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.WithUser(req.Context(), &auth.User{ID: "user:alice@example.com"})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Use(RequestLoggerMiddleware(logger))
	r.Get("/x", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	records := cap.snapshot()
	if len(records) == 0 {
		t.Fatal("expected at least one log record")
	}
	v, ok := findAttr(records[0], "user_id")
	if !ok {
		t.Fatal("expected user_id attribute when auth.User is in context")
	}
	if v.String() != "user:alice@example.com" {
		t.Errorf("expected user_id=user:alice@example.com, got %v", v)
	}
}

func TestInitLogger_DefaultsToJSONInfo(t *testing.T) {
	t.Setenv("WEAVE_LOG_LEVEL", "")
	t.Setenv("WEAVE_LOG_FORMAT", "")
	cfg := &config.Config{LogLevel: "info"}

	var buf bytes.Buffer
	logger := InitLogger(cfg, &buf)
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}

	logger.Info("hello", "k", "v")
	out := buf.String()
	if out == "" {
		t.Fatal("expected log output, got nothing")
	}
	// Default format is JSON; verify the line is valid JSON.
	var line map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &line); err != nil {
		t.Errorf("expected JSON log line, got %q: %v", out, err)
	}
	if line["msg"] != "hello" {
		t.Errorf("expected msg=hello, got %v", line["msg"])
	}
}

func TestInitLogger_TextFormat(t *testing.T) {
	t.Setenv("WEAVE_LOG_FORMAT", "text")
	cfg := &config.Config{LogLevel: "info"}

	var buf bytes.Buffer
	logger := InitLogger(cfg, &buf)
	logger.Info("hello", "k", "v")

	out := buf.String()
	if out == "" {
		t.Fatal("expected log output")
	}
	// Text format is NOT JSON.
	var line map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &line); err == nil {
		t.Errorf("expected text format (not JSON), got valid JSON: %s", out)
	}
}

func TestInitLogger_DebugLevelEnv(t *testing.T) {
	t.Setenv("WEAVE_LOG_LEVEL", "debug")
	cfg := &config.Config{LogLevel: "info"} // env should override

	var buf bytes.Buffer
	logger := InitLogger(cfg, &buf)
	logger.Debug("debug-message")

	if !strings.Contains(buf.String(), "debug-message") {
		t.Errorf("expected debug log to be emitted when WEAVE_LOG_LEVEL=debug, got %q", buf.String())
	}
}

func TestInitLogger_WarnLevelSuppressesInfo(t *testing.T) {
	t.Setenv("WEAVE_LOG_LEVEL", "warn")
	cfg := &config.Config{LogLevel: "info"}

	var buf bytes.Buffer
	logger := InitLogger(cfg, &buf)
	logger.Info("info-should-be-dropped")

	if strings.Contains(buf.String(), "info-should-be-dropped") {
		t.Errorf("expected info log to be filtered when level=warn, got %q", buf.String())
	}
}
