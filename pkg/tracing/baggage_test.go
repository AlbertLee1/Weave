package tracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	chimw "github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"
)

// withRequestID stamps a chi-style request ID onto the request so the
// downstream BaggageMiddleware sees a non-empty value. Avoids pulling
// in chi's full RequestID middleware in the test.
func withRequestID(reqID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), chimw.RequestIDKey, reqID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func TestBaggageMiddleware_PopulatesRequestIDAndUserID(t *testing.T) {
	rec := installRecordingProvider(t)

	mw := HTTPMiddleware()
	bag := BaggageMiddleware(func(_ context.Context) string { return "u-7" })

	var observedReq, observedUser string
	handler := withRequestID("req-abc")(mw(bag(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedReq = RequestIDFromBaggage(r.Context())
		observedUser = UserIDFromBaggage(r.Context())
		w.WriteHeader(http.StatusOK)
	}))))

	req := httptest.NewRequest(http.MethodGet, "/api/v2/things", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if observedReq != "req-abc" {
		t.Errorf("baggage request_id: got %q, want req-abc", observedReq)
	}
	if observedUser != "u-7" {
		t.Errorf("baggage user_id: got %q, want u-7", observedUser)
	}

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	attrs := map[string]string{}
	for _, kv := range spans[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value.Emit()
	}
	if attrs[BaggageRequestID] != "req-abc" {
		t.Errorf("span request_id attr: got %q, want req-abc", attrs[BaggageRequestID])
	}
	if attrs[BaggageUserID] != "u-7" {
		t.Errorf("span user_id attr: got %q, want u-7", attrs[BaggageUserID])
	}
}

func TestBaggageMiddleware_OmitsAbsentFields(t *testing.T) {
	rec := installRecordingProvider(t)

	mw := HTTPMiddleware()
	// Resolver returns "" simulating an unauthenticated request; no chi
	// RequestID middleware in the chain either.
	bag := BaggageMiddleware(func(_ context.Context) string { return "" })

	handler := mw(bag(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Baggage should be empty — assertions live below.
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodGet, "/api/v2/things", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	for _, kv := range spans[0].Attributes() {
		k := string(kv.Key)
		if k == BaggageRequestID || k == BaggageUserID {
			t.Errorf("unexpected baggage attribute %q on span", k)
		}
	}
}

func TestBaggageMiddleware_NilUserResolverIsSafe(t *testing.T) {
	rec := installRecordingProvider(t)

	mw := HTTPMiddleware()
	bag := BaggageMiddleware(nil)

	var observedUser string
	handler := withRequestID("req-xyz")(mw(bag(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedUser = UserIDFromBaggage(r.Context())
		w.WriteHeader(http.StatusOK)
	}))))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if observedUser != "" {
		t.Errorf("user_id with nil resolver: got %q, want empty", observedUser)
	}
	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	attrs := map[string]string{}
	for _, kv := range spans[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value.Emit()
	}
	if attrs[BaggageRequestID] != "req-xyz" {
		t.Errorf("request_id attr: got %q, want req-xyz", attrs[BaggageRequestID])
	}
}

func TestBaggageHelpersOnEmptyContext(t *testing.T) {
	if RequestIDFromBaggage(context.Background()) != "" {
		t.Errorf("RequestIDFromBaggage on empty context should be empty")
	}
	if UserIDFromBaggage(context.Background()) != "" {
		t.Errorf("UserIDFromBaggage on empty context should be empty")
	}
}

// TestBaggageMiddleware_ContextCarriesBaggage verifies the W3C Baggage
// member set is reachable via the standard otel/baggage API so a
// downstream package (audit, observability sink) can read it without
// pulling in pkg/tracing-specific helpers.
func TestBaggageMiddleware_ContextCarriesBaggage(t *testing.T) {
	mw := HTTPMiddleware()
	bag := BaggageMiddleware(func(_ context.Context) string { return "u-42" })

	var observed baggage.Baggage
	handler := withRequestID("req-7")(mw(bag(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = baggage.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if got := observed.Member(BaggageRequestID).Value(); got != "req-7" {
		t.Errorf("baggage request_id member: got %q, want req-7", got)
	}
	if got := observed.Member(BaggageUserID).Value(); got != "u-42" {
		t.Errorf("baggage user_id member: got %q, want u-42", got)
	}
}

// TestStartSpan_AttachesAttributesAndIsRecording rounds out coverage on
// the StartSpan helper used by handler-side opt-in business spans.
func TestStartSpan_AttachesAttributesAndIsRecording(t *testing.T) {
	rec := installRecordingProvider(t)

	ctx, span := StartSpan(context.Background(), "oms.GetOntology",
		attribute.String("ontology.api_name", "northwind"),
	)
	if !span.IsRecording() {
		t.Fatalf("expected span to be recording")
	}
	if !trace.SpanContextFromContext(ctx).IsValid() {
		t.Errorf("expected ctx to carry a valid span context")
	}
	span.End()

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Name() != "oms.GetOntology" {
		t.Errorf("span name: got %q, want oms.GetOntology", spans[0].Name())
	}
}
