package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// stubSAMLVerifier substitutes for the real gosaml2-backed SP in handler unit
// tests. The handler logic (relay-state cookie, user mapping, session minting,
// success-redirect behaviour) is what we want to exercise here; the SAML
// signature / NotOnOrAfter / audience checks live inside gosaml2 and are
// covered separately by the production-builder smoke tests.
type stubSAMLVerifier struct {
	authURL          string
	metadataXML      []byte
	metadataErr      error
	authURLErr       error
	assertion        *SAMLAssertionInfo
	assertionErr     error
	lastRelayState   string
	lastSAMLResponse string
}

func (s *stubSAMLVerifier) BuildAuthURL(relayState string) (string, error) {
	s.lastRelayState = relayState
	if s.authURLErr != nil {
		return "", s.authURLErr
	}
	base := s.authURL
	if base == "" {
		base = "https://idp.example.com/sso"
	}
	u, _ := url.Parse(base)
	q := u.Query()
	q.Set("SAMLRequest", "stub-encoded-authn-request")
	q.Set("RelayState", relayState)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (s *stubSAMLVerifier) RetrieveAssertionInfo(samlResponseB64 string) (*SAMLAssertionInfo, error) {
	s.lastSAMLResponse = samlResponseB64
	if s.assertionErr != nil {
		return nil, s.assertionErr
	}
	if s.assertion == nil {
		return nil, errors.New("stub: no assertion configured")
	}
	return s.assertion, nil
}

func (s *stubSAMLVerifier) Metadata() ([]byte, error) {
	if s.metadataErr != nil {
		return nil, s.metadataErr
	}
	if len(s.metadataXML) == 0 {
		return []byte(`<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="weave"/>`), nil
	}
	return s.metadataXML, nil
}

// goodAssertion is the canonical "alice authenticated successfully" SAML
// assertion that maps cleanly to the same UserRecord shape the OIDC tests use,
// so the user-mapping invariant ("create a UserRecord keyed on email,
// preserving any pre-existing PasswordHash") can be asserted identically.
func goodAssertion() *SAMLAssertionInfo {
	return &SAMLAssertionInfo{
		NameID:      "alice-saml-nameid",
		Email:       "alice@example.com",
		DisplayName: "Alice Example",
		Attributes:  map[string][]string{"email": {"alice@example.com"}, "displayName": {"Alice Example"}},
	}
}

func newSAMLHarness(t *testing.T) (*SAMLHandler, *stubSAMLVerifier, *fakeUserRepo) {
	t.Helper()
	repo := newFakeUserRepo()
	resolver := NewRoleResolver(repo, time.Minute)

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewJWTSigner(priv, &priv.PublicKey, JWTSignerOptions{
		Issuer:         "weave-test",
		Audience:       "weave-api",
		AccessTokenTTL: 15 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	rs := NewRefreshService(NewMemoryRefreshStore(), RefreshServiceOptions{AbsoluteTTL: 7 * 24 * time.Hour})

	stub := &stubSAMLVerifier{assertion: goodAssertion()}
	h := NewSAMLHandler(SAMLHandlerDeps{
		Config: SAMLConfig{
			SPEntityID: "https://weave.example.com",
			SPACSURL:   "https://weave.example.com/api/auth/saml/acs",
			IdPSSOURL:  "https://idp.example.com/sso",
			IdPIssuer:  "https://idp.example.com",
		},
		SP:             stub,
		Users:          repo,
		Resolver:       resolver,
		Signer:         signer,
		RefreshService: rs,
	})
	return h, stub, repo
}

func TestSAMLHandler_Login_RedirectsAndSetsRelayCookie(t *testing.T) {
	h, stub, _ := newSAMLHarness(t)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/saml/login", nil)
	rec := httptest.NewRecorder()
	h.Login(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("got %d, want 302. body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatal("no Location header on redirect")
	}
	if !strings.HasPrefix(loc, "https://idp.example.com/sso?") {
		t.Fatalf("unexpected redirect: %s", loc)
	}
	parsed, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse loc: %v", err)
	}
	if parsed.Query().Get("RelayState") != stub.lastRelayState {
		t.Fatalf("RelayState in URL %q != BuildAuthURL relay %q",
			parsed.Query().Get("RelayState"), stub.lastRelayState)
	}

	var found *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == samlRelayCookieName {
			found = c
		}
	}
	if found == nil {
		t.Fatal("relay-state cookie not set")
	}
	if !found.HttpOnly {
		t.Fatal("relay cookie must be HttpOnly")
	}
	if found.SameSite != http.SameSiteLaxMode {
		t.Fatalf("relay cookie SameSite=%v, want Lax (Strict breaks IdP-initiated POST)", found.SameSite)
	}
	if found.Value != stub.lastRelayState {
		t.Fatalf("cookie value %q != relay state %q", found.Value, stub.lastRelayState)
	}
}

func TestSAMLHandler_Login_RejectsNonGET(t *testing.T) {
	h, _, _ := newSAMLHarness(t)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/saml/login", nil)
	rec := httptest.NewRecorder()
	h.Login(rec, req)
	if rec.Code == http.StatusFound {
		t.Fatal("POST should not redirect")
	}
}

func TestSAMLHandler_Metadata_ReturnsXML(t *testing.T) {
	h, _, _ := newSAMLHarness(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/saml/metadata", nil)
	rec := httptest.NewRecorder()
	h.Metadata(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200. body=%s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "xml") {
		t.Fatalf("Content-Type=%q, want xml", ct)
	}
	if !strings.Contains(rec.Body.String(), "EntityDescriptor") {
		t.Fatalf("body missing EntityDescriptor: %s", rec.Body.String())
	}
}

func TestSAMLHandler_Metadata_RejectsNonGET(t *testing.T) {
	h, _, _ := newSAMLHarness(t)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/saml/metadata", nil)
	rec := httptest.NewRecorder()
	h.Metadata(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("POST returned 200; expected 4xx")
	}
}

func TestSAMLHandler_ACS_HappyPath(t *testing.T) {
	h, stub, repo := newSAMLHarness(t)

	relay := "test-relay-xyz"
	form := url.Values{}
	form.Set("SAMLResponse", "<base64-payload>")
	form.Set("RelayState", relay)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/saml/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: samlRelayCookieName, Value: relay})
	rec := httptest.NewRecorder()
	h.ACS(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200. body=%s", rec.Code, rec.Body.String())
	}
	if stub.lastSAMLResponse != "<base64-payload>" {
		t.Fatalf("verifier received %q, want raw form value", stub.lastSAMLResponse)
	}
	var resp LoginResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Fatal("empty tokens in response")
	}
	if resp.TokenType != "Bearer" {
		t.Fatalf("token_type=%q, want Bearer", resp.TokenType)
	}
	if resp.User.Email != "alice@example.com" {
		t.Fatalf("user.email=%q", resp.User.Email)
	}
	if resp.User.ID != "user:alice@example.com" {
		t.Fatalf("user.id=%q", resp.User.ID)
	}
	if resp.User.Name != "Alice Example" {
		t.Fatalf("user.name=%q", resp.User.Name)
	}
	if _, err := repo.GetUserByEmail(context.Background(), "alice@example.com"); err != nil {
		t.Fatalf("user not persisted: %v", err)
	}
}

func TestSAMLHandler_ACS_RejectsNonPOST(t *testing.T) {
	h, _, _ := newSAMLHarness(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/saml/acs", nil)
	rec := httptest.NewRecorder()
	h.ACS(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("GET should not be accepted on ACS endpoint")
	}
}

func TestSAMLHandler_ACS_RejectsMissingResponse(t *testing.T) {
	h, _, _ := newSAMLHarness(t)
	form := url.Values{}
	form.Set("RelayState", "r")
	req := httptest.NewRequest(http.MethodPost, "/api/auth/saml/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: samlRelayCookieName, Value: "r"})
	rec := httptest.NewRecorder()
	h.ACS(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400. body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "SAMLMissingResponse") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestSAMLHandler_ACS_RejectsRelayMismatch(t *testing.T) {
	h, _, _ := newSAMLHarness(t)
	form := url.Values{}
	form.Set("SAMLResponse", "x")
	form.Set("RelayState", "from-form")
	req := httptest.NewRequest(http.MethodPost, "/api/auth/saml/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: samlRelayCookieName, Value: "from-cookie"})
	rec := httptest.NewRecorder()
	h.ACS(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "SAMLRelayMismatch") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestSAMLHandler_ACS_AllowsIdPInitiatedWithoutRelay(t *testing.T) {
	// IdP-initiated SSO doesn't carry a RelayState because there's no
	// SP-side login flow that planted one. As long as the form omits
	// RelayState AND no cookie is present, we accept the assertion.
	h, _, _ := newSAMLHarness(t)
	form := url.Values{}
	form.Set("SAMLResponse", "x")
	req := httptest.NewRequest(http.MethodPost, "/api/auth/saml/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ACS(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("IdP-initiated SSO should succeed without relay state, got %d. body=%s",
			rec.Code, rec.Body.String())
	}
}

func TestSAMLHandler_ACS_RejectsAssertionInvalid(t *testing.T) {
	h, stub, _ := newSAMLHarness(t)
	stub.assertionErr = errors.New("signature verification failed")

	form := url.Values{}
	form.Set("SAMLResponse", "x")
	form.Set("RelayState", "r")
	req := httptest.NewRequest(http.MethodPost, "/api/auth/saml/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: samlRelayCookieName, Value: "r"})
	rec := httptest.NewRecorder()
	h.ACS(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "SAMLAssertionInvalid") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestSAMLHandler_ACS_RejectsMissingEmail(t *testing.T) {
	h, stub, _ := newSAMLHarness(t)
	stub.assertion = &SAMLAssertionInfo{NameID: "alice-saml", DisplayName: "Alice"}

	form := url.Values{}
	form.Set("SAMLResponse", "x")
	form.Set("RelayState", "r")
	req := httptest.NewRequest(http.MethodPost, "/api/auth/saml/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: samlRelayCookieName, Value: "r"})
	rec := httptest.NewRecorder()
	h.ACS(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "SAMLClaimsIncomplete") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestSAMLHandler_ACS_ExistingUserKeepsPassword(t *testing.T) {
	h, _, repo := newSAMLHarness(t)
	seedUser(t, repo, "user:alice@example.com", "alice@example.com", "letmein123!", "Old Name", "editor")

	form := url.Values{}
	form.Set("SAMLResponse", "x")
	form.Set("RelayState", "r")
	req := httptest.NewRequest(http.MethodPost, "/api/auth/saml/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: samlRelayCookieName, Value: "r"})
	rec := httptest.NewRecorder()
	h.ACS(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200. body=%s", rec.Code, rec.Body.String())
	}
	u, err := repo.GetUserByEmail(context.Background(), "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if u.PasswordHash == "" {
		t.Fatal("password hash wiped by SAML login")
	}
	if u.ID != "user:alice@example.com" {
		t.Fatalf("user.id mutated to %q", u.ID)
	}
}

func TestSAMLHandler_ACS_SuccessRedirectURL(t *testing.T) {
	h, _, _ := newSAMLHarness(t)
	h.deps.Config.SuccessRedirectURL = "https://weave.example.com/sso-done"

	form := url.Values{}
	form.Set("SAMLResponse", "x")
	form.Set("RelayState", "r")
	req := httptest.NewRequest(http.MethodPost, "/api/auth/saml/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: samlRelayCookieName, Value: "r"})
	rec := httptest.NewRecorder()
	h.ACS(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("got %d, want 302. body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://weave.example.com/sso-done?access_token=") {
		t.Fatalf("unexpected redirect %q", loc)
	}
	if !strings.Contains(loc, "refresh_token=") {
		t.Fatalf("refresh_token missing from redirect %q", loc)
	}
}

// TestNewSAMLDepsFromConfig_BuildsAuthURLAndMetadata is the production-builder
// smoke test: a real *saml2.SAMLServiceProvider (constructed from a self-signed
// IdP cert) must produce a non-empty AuthnRequest URL AND non-empty SP
// metadata XML. Exercises the gosaml2 wiring end-to-end without standing up
// a full IdP.
func TestNewSAMLDepsFromConfig_BuildsAuthURLAndMetadata(t *testing.T) {
	idpCertPEM := generateSelfSignedIdPCert(t)

	verifier, err := NewSAMLDepsFromConfig(SAMLConfig{
		SPEntityID:        "https://weave.example.com",
		SPACSURL:          "https://weave.example.com/api/auth/saml/acs",
		IdPSSOURL:         "https://idp.example.com/sso",
		IdPIssuer:         "https://idp.example.com",
		IdPCertificatePEM: idpCertPEM,
	})
	if err != nil {
		t.Fatalf("NewSAMLDepsFromConfig: %v", err)
	}

	authURL, err := verifier.BuildAuthURL("relay-x")
	if err != nil {
		t.Fatalf("BuildAuthURL: %v", err)
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse authURL: %v", err)
	}
	if !strings.HasPrefix(authURL, "https://idp.example.com/sso?") {
		t.Fatalf("authURL doesn't target IdP SSO endpoint: %s", authURL)
	}
	if parsed.Query().Get("SAMLRequest") == "" {
		t.Fatalf("SAMLRequest param missing from authURL: %s", authURL)
	}
	if parsed.Query().Get("RelayState") != "relay-x" {
		t.Fatalf("RelayState=%q, want relay-x", parsed.Query().Get("RelayState"))
	}

	md, err := verifier.Metadata()
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	mdStr := string(md)
	if !strings.Contains(mdStr, "EntityDescriptor") {
		t.Fatalf("metadata missing EntityDescriptor: %s", mdStr)
	}
	if !strings.Contains(mdStr, "https://weave.example.com/api/auth/saml/acs") {
		t.Fatalf("metadata missing ACS URL: %s", mdStr)
	}
	if !strings.Contains(mdStr, "https://weave.example.com") {
		t.Fatalf("metadata missing entity ID: %s", mdStr)
	}
}

func TestNewSAMLDepsFromConfig_RejectsBlankIdPCert(t *testing.T) {
	if _, err := NewSAMLDepsFromConfig(SAMLConfig{
		SPEntityID: "https://weave.example.com",
		SPACSURL:   "https://weave.example.com/api/auth/saml/acs",
		IdPSSOURL:  "https://idp.example.com/sso",
		IdPIssuer:  "https://idp.example.com",
	}); err == nil {
		t.Fatal("expected error when IdP certificate PEM is empty")
	}
}

// generateSelfSignedIdPCert produces a throw-away IdP signing certificate in
// PEM form. Used by the production-builder smoke test to hydrate the
// X509CertificateStore that gosaml2 expects.
func generateSelfSignedIdPCert(t *testing.T) string {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Test IdP"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// Avoid unused-import errors when iterating on the test file: base64 is used
// indirectly by the gosaml2 verifier exercising RetrieveAssertionInfo, and we
// keep the import here so future test additions that need to encode a real
// SAML response can do so without touching imports.
var _ = base64.StdEncoding
