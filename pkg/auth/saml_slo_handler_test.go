package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// stubSAMLLogoutVerifier substitutes for the real gosaml2-backed SP in
// SLO unit tests. The production-builder smoke test exercises the real
// gosaml2 path; here we drive the handler logic without standing up an
// IdP-signed LogoutRequest fixture.
type stubSAMLLogoutVerifier struct {
	info             *SAMLLogoutRequestInfo
	validateErr      error
	responseXML      []byte
	responseErr      error
	lastSAMLRequest  string
	lastResponseReq  string
	lastResponseStat string
}

func (s *stubSAMLLogoutVerifier) ValidateLogoutRequest(samlRequestB64 string) (*SAMLLogoutRequestInfo, error) {
	s.lastSAMLRequest = samlRequestB64
	if s.validateErr != nil {
		return nil, s.validateErr
	}
	return s.info, nil
}

func (s *stubSAMLLogoutVerifier) BuildLogoutResponse(status, requestID string) ([]byte, error) {
	s.lastResponseReq = requestID
	s.lastResponseStat = status
	if s.responseErr != nil {
		return nil, s.responseErr
	}
	if len(s.responseXML) == 0 {
		return []byte(`<samlp:LogoutResponse InResponseTo="` + requestID + `"/>`), nil
	}
	return s.responseXML, nil
}

type samlSLOHarness struct {
	handler      *SAMLSLOHandler
	verifier     *stubSAMLLogoutVerifier
	users        *fakeUserRepo
	sessions     *MemorySessionStore
	refreshStore *MemoryRefreshStore
}

func newSAMLSLOHarness(t *testing.T) *samlSLOHarness {
	t.Helper()
	users := newFakeUserRepo()
	sessions := NewMemorySessionStore()
	refreshStore := NewMemoryRefreshStore()
	rs := NewRefreshService(refreshStore, RefreshServiceOptions{AbsoluteTTL: 7 * 24 * time.Hour})

	v := &stubSAMLLogoutVerifier{
		info: &SAMLLogoutRequestInfo{
			RequestID: "logout-req-id-1",
			NameID:    "alice@example.com",
			Issuer:    "https://idp.example.com",
		},
	}
	h := NewSAMLSLOHandler(SAMLSLOHandlerDeps{
		LogoutVerifier: v,
		Users:          users,
		SessionStore:   sessions,
		RefreshService: rs,
	})
	return &samlSLOHarness{
		handler:      h,
		verifier:     v,
		users:        users,
		sessions:     sessions,
		refreshStore: refreshStore,
	}
}

// seedSAMLActiveSession is the SAML-side equivalent of
// seedActiveSession from the OIDC tests — same semantics, distinct ids
// so the two test files don't accidentally cross-pollute.
func seedSAMLActiveSession(t *testing.T, h *samlSLOHarness, email string) string {
	t.Helper()
	user := &UserRecord{ID: "user:" + email, Email: email, Name: "Alice"}
	if err := h.users.CreateUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	refresh := &RefreshTokenRecord{
		ID:        "rt-saml-" + email,
		UserID:    user.ID,
		TokenHash: "hash-saml-" + email,
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}
	if err := h.refreshStore.Create(context.Background(), refresh); err != nil {
		t.Fatal(err)
	}
	sess := &SessionRecord{
		ID:             "sess-saml-" + email,
		UserID:         user.ID,
		RefreshTokenID: refresh.ID,
	}
	if err := h.sessions.Create(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	return user.ID
}

func postSLO(t *testing.T, h *SAMLSLOHandler, samlRequest string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	form.Set("SAMLRequest", samlRequest)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/saml/slo", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSAMLSLO_HappyPath_ClearsSessionAndRefresh(t *testing.T) {
	h := newSAMLSLOHarness(t)
	userID := seedSAMLActiveSession(t, h, "alice@example.com")

	rec := postSLO(t, h.handler, "<base64-encoded-logout-request>")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200. body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control=%q, want no-store", rec.Header().Get("Cache-Control"))
	}
	// Body MUST be base64 — gosaml2's response-validation path expects
	// base64-encoded XML, so the SP-side response body should round-trip.
	if _, err := base64.StdEncoding.DecodeString(rec.Body.String()); err != nil {
		t.Fatalf("response body not base64: %v (got %q)", err, rec.Body.String())
	}
	if h.verifier.lastSAMLRequest != "<base64-encoded-logout-request>" {
		t.Fatalf("verifier saw %q, want raw form value", h.verifier.lastSAMLRequest)
	}
	if h.verifier.lastResponseReq != "logout-req-id-1" {
		t.Fatalf("LogoutResponse InResponseTo=%q, want logout-req-id-1", h.verifier.lastResponseReq)
	}
	if !strings.HasSuffix(h.verifier.lastResponseStat, "status:Success") {
		t.Fatalf("LogoutResponse status=%q, want urn:oasis:names:tc:SAML:2.0:status:Success", h.verifier.lastResponseStat)
	}

	sessions, _ := h.sessions.ListByUser(context.Background(), userID)
	if len(sessions) != 0 {
		t.Fatalf("sessions not cleared after SLO: %d remaining", len(sessions))
	}

	rec2, err := h.refreshStore.GetByHash(context.Background(), "hash-saml-alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !rec2.IsRevoked() {
		t.Fatal("refresh token not revoked")
	}
	if rec2.RevocationReason != "saml_slo" {
		t.Fatalf("revocation reason=%q, want saml_slo", rec2.RevocationReason)
	}
}

func TestSAMLSLO_RejectsNonPOST(t *testing.T) {
	h := newSAMLSLOHarness(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/saml/slo", nil)
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("GET should not return 200")
	}
}

func TestSAMLSLO_RejectsMissingRequest(t *testing.T) {
	h := newSAMLSLOHarness(t)
	form := url.Values{}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/saml/slo", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400. body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "SAMLSLOMissingRequest") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestSAMLSLO_RejectsInvalidLogoutRequest(t *testing.T) {
	h := newSAMLSLOHarness(t)
	h.verifier.validateErr = errors.New("signature verification failed")

	rec := postSLO(t, h.handler, "<bad>")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "SAMLSLORequestInvalid") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestSAMLSLO_RejectsMissingNameID(t *testing.T) {
	h := newSAMLSLOHarness(t)
	h.verifier.info = &SAMLLogoutRequestInfo{RequestID: "x", Issuer: "https://idp"}

	rec := postSLO(t, h.handler, "<x>")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
}

func TestSAMLSLO_UnknownNameID_ReturnsSuccessIdempotently(t *testing.T) {
	// No user seeded — the handler still emits StatusSuccess so the IdP
	// doesn't loop on retries. The SP intentionally doesn't disclose
	// which NameIDs map to local users.
	h := newSAMLSLOHarness(t)
	h.verifier.info = &SAMLLogoutRequestInfo{RequestID: "x", NameID: "stranger@example.com"}

	rec := postSLO(t, h.handler, "<x>")
	if rec.Code != http.StatusOK {
		t.Fatalf("unknown NameID should return 200, got %d", rec.Code)
	}
	if !strings.HasSuffix(h.verifier.lastResponseStat, "status:Success") {
		t.Fatalf("status=%q, want Success even for unknown NameID", h.verifier.lastResponseStat)
	}
}

func TestSAMLSLO_OnlyAffectsNamedUser(t *testing.T) {
	// Bulk revocation must be NameID-scoped, not global. Two seeded
	// users; an SLO request for one must leave the other untouched.
	h := newSAMLSLOHarness(t)
	aliceID := seedSAMLActiveSession(t, h, "alice@example.com")
	bobID := seedSAMLActiveSession(t, h, "bob@example.com")

	rec := postSLO(t, h.handler, "<x>")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}

	aliceSessions, _ := h.sessions.ListByUser(context.Background(), aliceID)
	if len(aliceSessions) != 0 {
		t.Fatalf("alice session not cleared: %d", len(aliceSessions))
	}
	bobSessions, _ := h.sessions.ListByUser(context.Background(), bobID)
	if len(bobSessions) != 1 {
		t.Fatalf("bob session wrongly cleared: %d, want 1", len(bobSessions))
	}
}

func TestSAMLSLO_ResponseBuildFailure_Surfaces500(t *testing.T) {
	h := newSAMLSLOHarness(t)
	h.verifier.responseErr = errors.New("missing SP signing key")
	seedSAMLActiveSession(t, h, "alice@example.com")

	rec := postSLO(t, h.handler, "<x>")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500. body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "SAMLSLOResponseBuildFailed") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestSAMLSLO_RegisterRoutes_MountsPath(t *testing.T) {
	h := newSAMLSLOHarness(t)
	got := ""
	mux := &routeCollector{collect: func(method, pattern string, _ http.Handler) {
		if method == http.MethodPost {
			got = pattern
		}
	}}
	h.handler.RegisterRoutes(mux)
	if got != "/api/auth/saml/slo" {
		t.Fatalf("route=%q, want /api/auth/saml/slo", got)
	}
}

// TestNewSAMLDepsFromConfig_ImplementsLogoutVerifier is the
// production-builder smoke test for the SAML SLO surface: the
// gosamlVerifier returned by NewSAMLDepsFromConfig must also satisfy
// SAMLLogoutVerifier so a single instance can be wired into both the
// login (ACS) and logout (SLO) handlers.
func TestNewSAMLDepsFromConfig_ImplementsLogoutVerifier(t *testing.T) {
	idpCertPEM := generateSelfSignedIdPCert(t)
	v, err := NewSAMLDepsFromConfig(SAMLConfig{
		SPEntityID:        "https://weave.example.com",
		SPACSURL:          "https://weave.example.com/api/auth/saml/acs",
		IdPSSOURL:         "https://idp.example.com/sso",
		IdPIssuer:         "https://idp.example.com",
		IdPCertificatePEM: idpCertPEM,
	})
	if err != nil {
		t.Fatal(err)
	}
	lv, ok := v.(SAMLLogoutVerifier)
	if !ok {
		t.Fatal("gosaml2 verifier does not implement SAMLLogoutVerifier")
	}
	// BuildLogoutResponse should produce non-empty bytes for a valid
	// (status, requestID) pair even without an SP keypair — we use
	// BuildLogoutResponseDocumentNoSig so signing isn't required.
	out, err := lv.BuildLogoutResponse("urn:oasis:names:tc:SAML:2.0:status:Success", "req-x")
	if err != nil {
		t.Fatalf("BuildLogoutResponse: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("BuildLogoutResponse returned empty bytes")
	}
	if !strings.Contains(string(out), "LogoutResponse") {
		t.Fatalf("response missing LogoutResponse element: %s", string(out))
	}
	if !strings.Contains(string(out), "InResponseTo=\"req-x\"") {
		t.Fatalf("response missing InResponseTo=req-x: %s", string(out))
	}
}

// Confirm the candidate-walking helper behaves as documented (mostly so
// future enhancements that change the precedence don't silently
// regress the email-first-then-userid contract).
func TestSAMLNameIDCandidates(t *testing.T) {
	cases := []struct {
		name        string
		nameID      string
		wantEmails  []string
		wantUserIDs []string
	}{
		{"empty", "", nil, nil},
		{"plain email", "alice@example.com", []string{"alice@example.com"}, []string{"user:alice@example.com", "alice@example.com"}},
		{"opaque", "opaque-id-123", nil, []string{"opaque-id-123"}},
		{"upper", "Alice@Example.com", []string{"alice@example.com"}, []string{"user:alice@example.com", "Alice@Example.com"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := samlNameIDCandidates(tc.nameID)
			if !sameStrings(got.emails, tc.wantEmails) {
				t.Fatalf("emails=%v, want %v", got.emails, tc.wantEmails)
			}
			if !sameStrings(got.userIDs, tc.wantUserIDs) {
				t.Fatalf("userIDs=%v, want %v", got.userIDs, tc.wantUserIDs)
			}
		})
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
