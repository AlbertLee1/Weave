package audit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientInfoRoundTrip(t *testing.T) {
	ctx := context.Background()
	info := ClientInfo{IP: "10.1.2.3", UserAgent: "weave-test/1.0"}

	ctx = WithClientInfo(ctx, info)
	got := ClientInfoFromContext(ctx)

	if got.IP != info.IP || got.UserAgent != info.UserAgent {
		t.Fatalf("ClientInfoFromContext = %#v, want %#v", got, info)
	}
}

func TestClientInfo_ZeroValueSkipsWrap(t *testing.T) {
	// An empty ClientInfo should not inject a value — callers reading from
	// plain ctx.Background() should see the same zero value.
	ctx := WithClientInfo(context.Background(), ClientInfo{})
	got := ClientInfoFromContext(ctx)
	if got.IP != "" || got.UserAgent != "" {
		t.Fatalf("zero-value ClientInfo should not be stamped, got %#v", got)
	}
}

func TestClientInfoFromContext_NilContext(t *testing.T) {
	// nil ctx path (defensive; real callers always have a ctx, but the
	// helper must not panic).
	got := ClientInfoFromContext(nil)
	if got.IP != "" || got.UserAgent != "" {
		t.Fatalf("expected zero-value, got %#v", got)
	}
}

func TestClientInfoMiddleware_XForwardedFor(t *testing.T) {
	var seen ClientInfo
	handler := ClientInfoMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = ClientInfoFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("X-Forwarded-For", "203.0.113.42, 10.0.0.1")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if seen.IP != "203.0.113.42" {
		t.Errorf("IP = %q, want 203.0.113.42 (XFF first entry)", seen.IP)
	}
	if seen.UserAgent != "Mozilla/5.0" {
		t.Errorf("UserAgent = %q, want Mozilla/5.0", seen.UserAgent)
	}
}

func TestClientInfoMiddleware_RemoteAddrFallback(t *testing.T) {
	var seen ClientInfo
	handler := ClientInfoMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = ClientInfoFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "198.51.100.9:443"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if seen.IP != "198.51.100.9" {
		t.Errorf("IP = %q, want 198.51.100.9 (host portion)", seen.IP)
	}
}
