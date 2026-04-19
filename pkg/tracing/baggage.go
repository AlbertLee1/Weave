package tracing

import (
	"context"
	"net/http"

	chimw "github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"
)

// Baggage member keys. Kept as exported constants so SDKs / downstream
// consumers can fish them out of inbound spans without re-typing the
// string literal in three places.
const (
	BaggageRequestID = "request_id"
	BaggageUserID    = "user_id"
)

// UserIDFunc resolves the caller's user ID from a request context. The
// hook is supplied by cmd/server (auth.UserFromContext) so pkg/tracing
// stays free of any pkg/auth import.
type UserIDFunc func(ctx context.Context) string

// BaggageMiddleware enriches every request with W3C baggage members
// carrying the chi-issued request_id and (when authenticated) the
// caller's user ID. The same values are stamped onto the active span
// as attributes so an operator can pivot from a span to its baggage
// without joining tables.
//
// Install AFTER chi's middleware.RequestID so request_id is populated,
// and AFTER any auth middleware so UserFromContext returns a non-nil
// user. userIDFn may be nil — callers without auth wired (tests,
// degraded-mode routers) get request_id only.
func BaggageMiddleware(userIDFn UserIDFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			members := make([]baggage.Member, 0, 2)
			if reqID := chimw.GetReqID(ctx); reqID != "" {
				if m, err := baggage.NewMember(BaggageRequestID, reqID); err == nil {
					members = append(members, m)
				}
				if span := trace.SpanFromContext(ctx); span.IsRecording() {
					span.SetAttributes(attribute.String(BaggageRequestID, reqID))
				}
			}
			if userIDFn != nil {
				if uid := userIDFn(ctx); uid != "" {
					if m, err := baggage.NewMember(BaggageUserID, uid); err == nil {
						members = append(members, m)
					}
					if span := trace.SpanFromContext(ctx); span.IsRecording() {
						span.SetAttributes(attribute.String(BaggageUserID, uid))
					}
				}
			}

			if len(members) > 0 {
				if bag, err := baggage.New(members...); err == nil {
					ctx = baggage.ContextWithBaggage(ctx, bag)
				}
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequestIDFromBaggage reads the request_id baggage member from ctx.
// Returns "" when the member is absent. Provided for symmetry with
// UserIDFromBaggage so downstream code never has to reach into the
// baggage API directly.
func RequestIDFromBaggage(ctx context.Context) string {
	return baggage.FromContext(ctx).Member(BaggageRequestID).Value()
}

// UserIDFromBaggage reads the user_id baggage member from ctx. Returns
// "" when the member is absent (unauthenticated requests, background
// jobs, tests, etc.).
func UserIDFromBaggage(ctx context.Context) string {
	return baggage.FromContext(ctx).Member(BaggageUserID).Value()
}
