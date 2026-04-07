package main

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/liyang/weave/internal/config"
	"github.com/liyang/weave/pkg/auth"
)

// InitLogger builds the application's structured slog.Logger from cfg and
// the WEAVE_LOG_LEVEL / WEAVE_LOG_FORMAT environment variables. Output is
// written to w (typically os.Stderr) so tests can capture it. The level
// resolution order is: env override > cfg.LogLevel > info. The format
// resolution order is: env override > json (default).
func InitLogger(cfg *config.Config, w io.Writer) *slog.Logger {
	if w == nil {
		w = os.Stderr
	}

	level := resolveLogLevel(cfg)
	format := resolveLogFormat()

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if format == "text" {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}

	logger := slog.New(handler)
	return logger
}

func resolveLogLevel(cfg *config.Config) slog.Level {
	level := ""
	if env := os.Getenv("WEAVE_LOG_LEVEL"); env != "" {
		level = env
	} else if cfg != nil && cfg.LogLevel != "" {
		level = cfg.LogLevel
	}
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func resolveLogFormat() string {
	if env := os.Getenv("WEAVE_LOG_FORMAT"); env != "" {
		return strings.ToLower(env)
	}
	return "json"
}

// RequestLoggerMiddleware emits one structured slog.Info record per HTTP
// request, capturing method, path, status, duration_ms, request_id,
// remote_ip, and user_id (if auth.UserFromContext yields a user). It must
// be installed AFTER chi's middleware.RequestID so the request_id field
// has a value.
func RequestLoggerMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			attrs := []any{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", ww.Status()),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				slog.String("request_id", middleware.GetReqID(r.Context())),
				slog.String("remote_ip", r.RemoteAddr),
			}
			if u := auth.UserFromContext(r.Context()); u != nil {
				attrs = append(attrs, slog.String("user_id", u.ID))
			}
			logger.Info("http_request", attrs...)
		})
	}
}
