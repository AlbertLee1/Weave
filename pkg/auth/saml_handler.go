// Package auth — SAML 2.0 handler (US-248).
//
// The SAML handler exposes three HTTP endpoints implementing the SP side of
// SAML 2.0 Web Browser SSO against an external Identity Provider (Okta,
// ADFS, Keycloak, OneLogin, ...):
//
//	GET  /api/auth/saml/metadata  — SP descriptor XML (IdP imports this)
//	GET  /api/auth/saml/login     — generate AuthnRequest, redirect to IdP
//	POST /api/auth/saml/acs       — Assertion Consumer Service: verify + mint session
//
// The handler delegates SAML protocol details (signature verification,
// NotOnOrAfter checks, audience restriction) to russellhaering/gosaml2 via a
// narrow SAMLAssertionVerifier interface so handler unit tests can stub the
// SP without standing up a full IdP. On a successful ACS the handler upserts
// a UserRecord keyed on email and issues a Weave access + refresh token pair
// — the same shape LoginHandler / OIDCHandler return — so downstream API
// calls keep going through the existing JWT middleware unchanged.
package auth

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	saml2 "github.com/russellhaering/gosaml2"
	"github.com/russellhaering/gosaml2/types"
	dsig "github.com/russellhaering/goxmldsig"

	"github.com/liyang/weave/pkg/apierror"
)

// SAMLConfig is the minimum configuration the SAML handler needs. The IdP
// section identifies the upstream Identity Provider (issuer, SSO endpoint,
// signing certificate); the SP section identifies this server (entity ID,
// ACS URL). SuccessRedirectURL mirrors OIDCConfig — when set the ACS handler
// emits a 302 with tokens in the query string, otherwise it returns the
// LoginResponse JSON body.
type SAMLConfig struct {
	Enabled            bool
	IdPSSOURL          string
	IdPIssuer          string
	IdPCertificatePEM  string
	SPEntityID         string
	SPACSURL           string
	SuccessRedirectURL string
	// AttributeEmail is the SAML attribute name that carries the user's
	// email address. Defaults to "email" then "mail" then NameID. Identity
	// providers vary: Azure AD ships email under "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress".
	AttributeEmail string
	// AttributeName is the SAML attribute name carrying the display name.
	// Defaults to "displayName" then "name".
	AttributeName string
}

// SAMLAssertionInfo is the narrow set of assertion fields the handler needs.
// Decoupling from gosaml2's *AssertionInfo lets the handler stub the verifier
// in tests AND lets the caller-side mapping logic stay independent of any
// specific SAML library.
type SAMLAssertionInfo struct {
	NameID      string
	Email       string
	DisplayName string
	Attributes  map[string][]string
}

// SAMLAssertionVerifier is the subset of *saml2.SAMLServiceProvider the
// handler exercises. The production builder NewSAMLDepsFromConfig wraps a
// real ServiceProvider into this interface; tests substitute a stub that
// returns canned AssertionInfo / metadata bytes.
type SAMLAssertionVerifier interface {
	BuildAuthURL(relayState string) (string, error)
	RetrieveAssertionInfo(samlResponseB64 string) (*SAMLAssertionInfo, error)
	Metadata() ([]byte, error)
}

// SAMLHandlerDeps groups the collaborators a SAMLHandler needs. The shape
// mirrors OIDCHandlerDeps so the two handlers stay structurally aligned and
// the wiring code in cmd/server stays a near-copy of the OIDC block.
type SAMLHandlerDeps struct {
	Config         SAMLConfig
	SP             SAMLAssertionVerifier
	Users          UserRepository
	Resolver       *RoleResolver
	Signer         *JWTSigner
	RefreshService *RefreshService
	MarkingRepo    MarkingRepository
	Now            func() time.Time
}

// SAMLHandler wires the /api/auth/saml/{metadata,login,acs} endpoints.
type SAMLHandler struct {
	deps SAMLHandlerDeps
}

// samlRelayCookieName carries the opaque RelayState between the
// SP-initiated /login redirect and the ACS callback to defend against CSRF
// on the assertion response. IdP-initiated SSO has no SP-side login leg
// (and therefore no cookie + no RelayState in the form) — the ACS handler
// permits that flow as long as both sides are absent together.
const samlRelayCookieName = "weave_saml_relay"

// samlRelayCookieMaxAge mirrors the OIDC state-cookie TTL. The auth flow
// completes in seconds; ten minutes is a generous upper bound that still
// invalidates abandoned redirects.
const samlRelayCookieMaxAge = 10 * 60

// NewSAMLHandler constructs a handler. The caller owns the verifier — in
// production it comes from NewSAMLDepsFromConfig, in tests it's a stub.
func NewSAMLHandler(deps SAMLHandlerDeps) *SAMLHandler {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &SAMLHandler{deps: deps}
}

// RegisterRoutes attaches the three SAML endpoints to the supplied mux.
func (h *SAMLHandler) RegisterRoutes(mux interface {
	Method(method, pattern string, handler http.Handler)
}) {
	mux.Method(http.MethodGet, "/api/auth/saml/metadata", http.HandlerFunc(h.Metadata))
	mux.Method(http.MethodGet, "/api/auth/saml/login", http.HandlerFunc(h.Login))
	mux.Method(http.MethodPost, "/api/auth/saml/acs", http.HandlerFunc(h.ACS))
}

// Metadata returns the SP EntityDescriptor as XML so the IdP administrator
// can import this server's SAML configuration without copy-pasting fields.
func (h *SAMLHandler) Metadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MethodNotAllowed", map[string]string{
			"reason": "GET required",
		}))
		return
	}
	xmlBytes, err := h.deps.SP.Metadata()
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("SAMLMetadataFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	w.Header().Set("Content-Type", "application/samlmetadata+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(xmlBytes)
}

// Login generates a fresh RelayState, stores it in a short-lived HTTP-only
// cookie, and redirects the caller to the IdP's SSO endpoint with an encoded
// AuthnRequest in the SAMLRequest query parameter.
func (h *SAMLHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MethodNotAllowed", map[string]string{
			"reason": "GET required",
		}))
		return
	}
	relay, err := newRandomState()
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("SAMLRelayFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	authURL, err := h.deps.SP.BuildAuthURL(relay)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("SAMLAuthRequestFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     samlRelayCookieName,
		Value:    relay,
		Path:     "/",
		MaxAge:   samlRelayCookieMaxAge,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, authURL, http.StatusFound)
}

// ACS (Assertion Consumer Service) handles the IdP's POST-bound SAML
// Response. It verifies relay-state pairing, hands the assertion to the
// configured verifier (signature + audience + expiry checks), upserts a
// UserRecord from the resulting claims, and mints a Weave session.
func (h *SAMLHandler) ACS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MethodNotAllowed", map[string]string{
			"reason": "POST required",
		}))
		return
	}
	if err := r.ParseForm(); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("SAMLFormParseFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	samlResp := r.Form.Get("SAMLResponse")
	if samlResp == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("SAMLMissingResponse", map[string]string{
			"reason": "SAMLResponse form field is required",
		}))
		return
	}

	relay := r.Form.Get("RelayState")
	cookie, cerr := r.Cookie(samlRelayCookieName)
	switch {
	case relay == "" && cerr != nil:
		// IdP-initiated SSO: neither side has a relay value. Permitted —
		// the assertion's signature + audience + replay protection are
		// what gate access in this flow.
	case relay != "" && cerr == nil && cookie.Value == relay:
		// SP-initiated SSO: matching pair. Clear the cookie immediately so
		// a replay can't re-consume the same assertion.
		http.SetCookie(w, &http.Cookie{Name: samlRelayCookieName, Path: "/", MaxAge: -1})
	default:
		apierror.WriteJSON(w, apierror.NewUnauthorized("SAMLRelayMismatch", map[string]string{
			"reason": "RelayState form value does not match session cookie",
		}))
		return
	}

	info, err := h.deps.SP.RetrieveAssertionInfo(samlResp)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("SAMLAssertionInvalid", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if info == nil || strings.TrimSpace(info.Email) == "" {
		apierror.WriteJSON(w, apierror.NewUnauthorized("SAMLClaimsIncomplete", map[string]string{
			"reason": "assertion missing email attribute",
		}))
		return
	}

	ctx := r.Context()
	user, err := h.upsertUser(ctx, info)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("SAMLUserUpsertFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	resp, err := h.issueSession(ctx, user)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("SAMLSessionFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	if h.deps.Config.SuccessRedirectURL != "" {
		http.Redirect(w, r, buildSuccessRedirect(h.deps.Config.SuccessRedirectURL, resp), http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// upsertUser resolves the UserRecord for the assertion, creating one on
// first sight of the email. The ID is derived from the email (same
// "user:<email>" convention BootstrapAdmin / OIDCHandler use) so downstream
// RBAC lookups on user.ID keep working. Existing PasswordHash is preserved
// so a hybrid password+SSO org can still sign the same user in via either path.
func (h *SAMLHandler) upsertUser(ctx context.Context, info *SAMLAssertionInfo) (*UserRecord, error) {
	email := strings.ToLower(strings.TrimSpace(info.Email))
	if email == "" {
		return nil, errors.New("email is empty")
	}
	if existing, err := h.deps.Users.GetUserByEmail(ctx, email); err == nil {
		if info.DisplayName != "" && existing.Name != info.DisplayName {
			existing.Name = info.DisplayName
		}
		return existing, nil
	} else if !errors.Is(err, ErrUserNotFound) {
		return nil, err
	}
	rec := &UserRecord{
		ID:    "user:" + email,
		Email: email,
		Name:  info.DisplayName,
	}
	if err := h.deps.Users.CreateUser(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// issueSession mirrors the tail of LoginHandler.ServeHTTP / OIDC.issueSession.
func (h *SAMLHandler) issueSession(ctx context.Context, user *UserRecord) (*LoginResponse, error) {
	global, scoped, err := h.deps.Resolver.Resolve(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("role resolve: %w", err)
	}
	var markings []string
	if h.deps.MarkingRepo != nil {
		markings, err = h.deps.MarkingRepo.GetUserMarkings(ctx, user.ID)
		if err != nil {
			return nil, fmt.Errorf("marking resolve: %w", err)
		}
	}
	access, err := h.deps.Signer.Sign(SignInput{
		UserID:        user.ID,
		Email:         user.Email,
		Name:          user.Name,
		Roles:         global,
		OntologyRoles: scoped,
		Markings:      markings,
	})
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}
	refreshPlain, _, err := h.deps.RefreshService.Generate(ctx, user.ID, "")
	if err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
	}
	ttl := 15 * time.Minute
	if h.deps.Signer != nil && h.deps.Signer.ttl > 0 {
		ttl = h.deps.Signer.ttl
	}
	return &LoginResponse{
		AccessToken:  access,
		RefreshToken: refreshPlain,
		TokenType:    "Bearer",
		ExpiresIn:    int(ttl.Seconds()),
		User: LoginUser{
			ID:            user.ID,
			Email:         user.Email,
			Name:          user.Name,
			Roles:         emptyIfNilStrings(global),
			OntologyRoles: emptyIfNilMap(scoped),
		},
	}, nil
}

// gosamlVerifier wraps a *saml2.SAMLServiceProvider so it satisfies the
// narrow SAMLAssertionVerifier interface. Lives in the same file as the
// handler so the handler doesn't need to import gosaml2 transitively from
// any test that wants to stub the verifier.
type gosamlVerifier struct {
	sp     *saml2.SAMLServiceProvider
	cfg    SAMLConfig
	emails []string // attribute names to try in order when extracting email
	names  []string // attribute names to try in order when extracting display name
}

func (g *gosamlVerifier) BuildAuthURL(relayState string) (string, error) {
	return g.sp.BuildAuthURL(relayState)
}

func (g *gosamlVerifier) RetrieveAssertionInfo(samlResponseB64 string) (*SAMLAssertionInfo, error) {
	raw, err := g.sp.RetrieveAssertionInfo(samlResponseB64)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, errors.New("nil AssertionInfo from gosaml2")
	}
	if raw.WarningInfo != nil {
		if raw.WarningInfo.InvalidTime {
			return nil, errors.New("assertion outside valid time window")
		}
		if raw.WarningInfo.NotInAudience {
			return nil, errors.New("assertion audience does not match SP entity ID")
		}
	}
	attrs := flattenAttributes(raw.Values)
	return &SAMLAssertionInfo{
		NameID:      raw.NameID,
		Email:       firstAttribute(attrs, g.emails, raw.NameID),
		DisplayName: firstAttribute(attrs, g.names, ""),
		Attributes:  attrs,
	}, nil
}

// Metadata builds the SP descriptor XML directly rather than calling through
// gosaml2's *SAMLServiceProvider.Metadata() helper because that helper insists
// on an SP encryption certificate — a feature this handler doesn't use (we
// neither sign AuthnRequests nor consume encrypted assertions). Constructing
// the EntityDescriptor here keeps the metadata endpoint working in the common
// "plain SP, no SP keypair" deployment.
func (g *gosamlVerifier) Metadata() ([]byte, error) {
	desc := &types.EntityDescriptor{
		ValidUntil: time.Now().UTC().Add(7 * 24 * time.Hour),
		EntityID:   g.cfg.SPEntityID,
		SPSSODescriptor: &types.SPSSODescriptor{
			AuthnRequestsSigned:        false,
			WantAssertionsSigned:       true,
			ProtocolSupportEnumeration: saml2.SAMLProtocolNamespace,
			AssertionConsumerServices: []types.IndexedEndpoint{{
				Binding:  saml2.BindingHttpPost,
				Location: g.cfg.SPACSURL,
				Index:    1,
			}},
		},
	}
	return marshalSAMLMetadata(desc)
}

// NewSAMLDepsFromConfig builds a production verifier from a SAMLConfig.
// Errors out loudly on missing IdP cert / unparseable PEM so cmd/server can
// log a clear "SAML not mounted" message at boot rather than serving a
// permanently broken endpoint.
func NewSAMLDepsFromConfig(cfg SAMLConfig) (SAMLAssertionVerifier, error) {
	if strings.TrimSpace(cfg.IdPCertificatePEM) == "" {
		return nil, errors.New("SAMLConfig.IdPCertificatePEM is required")
	}
	certs, err := parseIdPCertificates(cfg.IdPCertificatePEM)
	if err != nil {
		return nil, fmt.Errorf("parse IdP certificate: %w", err)
	}
	if len(certs) == 0 {
		return nil, errors.New("no certificates parsed from IdPCertificatePEM")
	}

	sp := &saml2.SAMLServiceProvider{
		IdentityProviderSSOURL:      cfg.IdPSSOURL,
		IdentityProviderIssuer:      cfg.IdPIssuer,
		AssertionConsumerServiceURL: cfg.SPACSURL,
		ServiceProviderIssuer:       cfg.SPEntityID,
		AudienceURI:                 cfg.SPEntityID,
		IDPCertificateStore: &dsig.MemoryX509CertificateStore{
			Roots: certs,
		},
		// We don't sign AuthnRequests yet — most IdPs don't require it for
		// the simple HTTP-Redirect binding this handler uses.
		SignAuthnRequests: false,
		// The library has a small allowlist of NameID formats; leave empty
		// to let the IdP pick its default.
		NameIdFormat: "",
	}

	emails := dedupeStringsKeepOrder([]string{
		cfg.AttributeEmail,
		"email",
		"mail",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
	})
	names := dedupeStringsKeepOrder([]string{
		cfg.AttributeName,
		"displayName",
		"name",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name",
	})

	return &gosamlVerifier{sp: sp, cfg: cfg, emails: emails, names: names}, nil
}

// parseIdPCertificates walks the PEM blob and returns every CERTIFICATE
// block found. Identity providers occasionally rotate keys, so the on-disk
// PEM may legitimately carry multiple certs concatenated; we accept all of
// them as valid signing roots.
func parseIdPCertificates(pemBlob string) ([]*x509.Certificate, error) {
	rest := []byte(pemBlob)
	var out []*x509.Certificate
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("x509.ParseCertificate: %w", err)
		}
		out = append(out, cert)
	}
	return out, nil
}

// flattenAttributes converts gosaml2's attribute map into a simple
// name → []string projection so downstream callers don't need to import the
// library's types package.
func flattenAttributes(vals saml2.Values) map[string][]string {
	out := map[string][]string{}
	for k, v := range vals {
		var ss []string
		for _, av := range v.Values {
			ss = append(ss, av.Value)
		}
		out[k] = ss
		if v.FriendlyName != "" && v.FriendlyName != k {
			out[v.FriendlyName] = ss
		}
	}
	return out
}

// firstAttribute walks candidate attribute names in declared order and
// returns the first non-empty value, falling back to the supplied default.
// Empty / whitespace-only candidate names are skipped.
func firstAttribute(attrs map[string][]string, candidates []string, fallback string) string {
	for _, name := range candidates {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if vs, ok := attrs[name]; ok && len(vs) > 0 && strings.TrimSpace(vs[0]) != "" {
			return vs[0]
		}
	}
	return fallback
}

// marshalSAMLMetadata encodes a gosaml2 EntityDescriptor as XML bytes ready
// to ship to an IdP administrator. We re-marshal via encoding/xml because
// the EntityDescriptor type carries the right struct tags; gosaml2 doesn't
// expose its own marshal helper.
func marshalSAMLMetadata(desc *types.EntityDescriptor) ([]byte, error) {
	xmlBytes, err := xml.MarshalIndent(desc, "", "  ")
	if err != nil {
		return nil, err
	}
	// Prepend the XML declaration so the response is a complete document.
	return append([]byte(`<?xml version="1.0" encoding="UTF-8"?>`+"\n"), xmlBytes...), nil
}

func dedupeStringsKeepOrder(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
