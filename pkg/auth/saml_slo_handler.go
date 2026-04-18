// Package auth — SAML 2.0 Single Logout (SLO) handler (US-255).
//
// SLO is the SAML counterpart to OIDC back-channel logout: the IdP signals
// "this user has logged out upstream" by POSTing a signed LogoutRequest to
// a registered SP endpoint, and the SP responds with a LogoutResponse and
// terminates every local session belonging to the named NameID.
//
// Endpoint:
//
//	POST /api/auth/saml/slo
//	Content-Type: application/x-www-form-urlencoded
//	Body:         SAMLRequest=<base64-encoded LogoutRequest XML> [&RelayState=...]
//
// The handler is wired in the SAML metadata SingleLogoutService entry so
// the IdP knows where to send LogoutRequest. Signature validation is
// delegated to gosaml2 via the narrow SAMLLogoutVerifier interface
// (mirrors the SAMLAssertionVerifier split — same reasoning, same
// production builder, same stubbing pattern in tests).
//
// On success the handler:
//  1. revokes every outstanding refresh token for the named user
//     (RefreshService.RevokeAllForUser → reason="saml_slo")
//  2. deletes every active session row for the user
//     (SessionStore.DeleteAllForUser)
//  3. responds with a base64-encoded LogoutResponse XML body so the IdP
//     can confirm the SLO completed successfully.
//
// The handler is idempotent: unknown NameIDs still emit StatusSuccess
// rather than leak which subjects the SP knows about.
package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	saml2 "github.com/russellhaering/gosaml2"

	"github.com/liyang/weave/pkg/apierror"
)

// SAMLLogoutRequestInfo is the narrow set of LogoutRequest fields the
// handler exercises. Decoupling from gosaml2's *LogoutRequest lets the
// handler stub the verifier in tests AND keeps the caller-side mapping
// independent of any specific SAML library.
type SAMLLogoutRequestInfo struct {
	// RequestID is the LogoutRequest's ID attribute. Must be echoed back
	// in the LogoutResponse's InResponseTo so the IdP can correlate.
	RequestID string
	// NameID is the IdP-side identifier the LogoutRequest names. Most
	// IdPs ship NameID == email; the handler attempts both an email
	// lookup and a "user:<email>" id lookup as fallbacks.
	NameID string
	// Issuer carries the LogoutRequest's <saml:Issuer> for audit / log
	// trails — the verifier already validated it equals the configured
	// IdentityProviderIssuer.
	Issuer string
}

// SAMLLogoutVerifier is the SLO-shaped subset of gosaml2's SP. Kept
// separate from SAMLAssertionVerifier so handler tests can stub login
// without growing a logout method, and so the production builder can
// register both surfaces from one underlying gosaml2 SP. Same shape as
// the OIDC story (login Verifier vs back-channel logout Verifier reuse
// the same JWKS).
type SAMLLogoutVerifier interface {
	// ValidateLogoutRequest parses + signature-verifies the supplied
	// base64 LogoutRequest. Returns the extracted IdP-side identifiers
	// or an error if the signature, version, destination, or issuer
	// fail validation.
	ValidateLogoutRequest(samlRequestB64 string) (*SAMLLogoutRequestInfo, error)
	// BuildLogoutResponse renders a LogoutResponse XML element naming
	// the supplied request id + status code, returning the bytes to ship
	// back to the IdP. The implementation may sign the response when an
	// SP keypair is available; otherwise it returns an unsigned shape.
	BuildLogoutResponse(status, requestID string) ([]byte, error)
}

// SAMLSLOHandlerDeps groups the collaborators an SLOHandler needs.
type SAMLSLOHandlerDeps struct {
	// LogoutVerifier validates incoming LogoutRequest XML + builds the
	// LogoutResponse XML. Production uses the gosaml2-backed
	// gosamlVerifier; tests substitute a stub.
	LogoutVerifier SAMLLogoutVerifier
	// Users resolves the LogoutRequest's NameID to a Weave UserRecord.
	// nil disables user lookup (handler still emits a successful
	// LogoutResponse so the IdP doesn't loop on retries).
	Users UserRepository
	// SessionStore is where the handler calls DeleteAllForUser. Nil =
	// no session teardown.
	SessionStore SessionStore
	// RefreshService bulk-revokes the user's refresh tokens. Nil = no
	// refresh revocation.
	RefreshService *RefreshService
}

// SAMLSLOHandler wires POST /api/auth/saml/slo.
type SAMLSLOHandler struct {
	deps SAMLSLOHandlerDeps
}

// NewSAMLSLOHandler constructs an SLO handler. The caller owns the
// LogoutVerifier — production wiring reuses the same gosaml2 SP that
// powers the ACS, tests substitute a stub.
func NewSAMLSLOHandler(deps SAMLSLOHandlerDeps) *SAMLSLOHandler {
	return &SAMLSLOHandler{deps: deps}
}

// RegisterRoutes attaches POST /api/auth/saml/slo. Mounted alongside the
// other SAML endpoints — see SAMLHandler.RegisterRoutes — but kept on a
// separate handler so SLO support can be wired independently when an
// operator opts into it via SAMLConfig.SPSLOURL.
func (h *SAMLSLOHandler) RegisterRoutes(mux interface {
	Method(method, pattern string, handler http.Handler)
}) {
	mux.Method(http.MethodPost, "/api/auth/saml/slo", http.HandlerFunc(h.ServeHTTP))
}

// ServeHTTP implements http.Handler.
func (h *SAMLSLOHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MethodNotAllowed", map[string]string{
			"reason": "POST required",
		}))
		return
	}
	if h.deps.LogoutVerifier == nil {
		apierror.WriteJSON(w, apierror.NewInternal("SAMLSLOUnavailable", map[string]string{
			"reason": "logout verifier not wired",
		}))
		return
	}
	if err := r.ParseForm(); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("SAMLSLOFormParseFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	samlReq := strings.TrimSpace(r.Form.Get("SAMLRequest"))
	if samlReq == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("SAMLSLOMissingRequest", map[string]string{
			"reason": "SAMLRequest form field is required",
		}))
		return
	}

	info, err := h.deps.LogoutVerifier.ValidateLogoutRequest(samlReq)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("SAMLSLORequestInvalid", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if info == nil || strings.TrimSpace(info.NameID) == "" {
		apierror.WriteJSON(w, apierror.NewUnauthorized("SAMLSLORequestInvalid", map[string]string{
			"reason": "logout request missing NameID",
		}))
		return
	}

	// Map NameID → Weave user. Most IdPs configure NameID = email,
	// matching the convention used by the SAML ACS handler. Fall back
	// to "user:<email>" id lookup if the email lookup misses (covers
	// IdPs that send an opaque NameID matching the user.ID directly).
	user := h.resolveUser(r.Context(), info.NameID)
	if user != nil {
		if h.deps.RefreshService != nil {
			_ = h.deps.RefreshService.RevokeAllForUser(r.Context(), user.ID, "saml_slo")
		}
		if h.deps.SessionStore != nil {
			_ = h.deps.SessionStore.DeleteAllForUser(r.Context(), user.ID)
		}
	}

	// Always respond with StatusSuccess so the IdP doesn't loop on
	// retries. Unknown subjects are intentionally indistinguishable
	// from successful teardowns.
	respXML, err := h.deps.LogoutVerifier.BuildLogoutResponse(saml2.StatusCodeSuccess, info.RequestID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("SAMLSLOResponseBuildFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	w.Header().Set("Content-Type", "application/samlmetadata+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// Some IdPs accept the LogoutResponse as a raw XML body; others
	// expect the body to be base64-encoded so it round-trips through a
	// browser POST binding. The gosaml2 ValidateEncodedLogoutResponsePOST
	// path expects base64, so we ship base64 and let a future enhancement
	// add a redirect-binding variant if needed.
	encoded := base64.StdEncoding.EncodeToString(respXML)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(encoded))
}

// resolveUser walks the email → user-id fallback chain.
func (h *SAMLSLOHandler) resolveUser(ctx context.Context, nameID string) *UserRecord {
	if h.deps.Users == nil {
		return nil
	}
	candidates := samlNameIDCandidates(nameID)
	for _, email := range candidates.emails {
		if u, err := h.deps.Users.GetUserByEmail(ctx, email); err == nil && u != nil {
			return u
		}
	}
	for _, id := range candidates.userIDs {
		if u, err := h.deps.Users.GetUserByID(ctx, id); err == nil && u != nil {
			return u
		}
	}
	return nil
}

func samlNameIDCandidates(nameID string) subjectCandidates {
	out := subjectCandidates{}
	trimmed := strings.TrimSpace(nameID)
	if trimmed == "" {
		return out
	}
	if looksLikeEmail(trimmed) {
		out.emails = appendUnique(out.emails, strings.ToLower(trimmed))
		out.userIDs = appendUnique(out.userIDs, "user:"+strings.ToLower(trimmed))
	}
	out.userIDs = appendUnique(out.userIDs, trimmed)
	return out
}

// gosamlVerifier already implements SAMLAssertionVerifier — extend it so
// the same instance also implements SAMLLogoutVerifier. Production wiring
// can hand the same value to both the SAMLHandler (login) and the
// SAMLSLOHandler (logout).
func (g *gosamlVerifier) ValidateLogoutRequest(samlRequestB64 string) (*SAMLLogoutRequestInfo, error) {
	if g.sp == nil {
		return nil, errors.New("nil SAML SP")
	}
	req, err := g.sp.ValidateEncodedLogoutRequestPOST(samlRequestB64)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, errors.New("nil LogoutRequest from gosaml2")
	}
	info := &SAMLLogoutRequestInfo{
		RequestID: req.ID,
	}
	if req.NameID != nil {
		info.NameID = req.NameID.Value
	}
	if req.Issuer != nil {
		info.Issuer = req.Issuer.Value
	}
	return info, nil
}

func (g *gosamlVerifier) BuildLogoutResponse(status, requestID string) ([]byte, error) {
	if g.sp == nil {
		return nil, errors.New("nil SAML SP")
	}
	// SignLogoutResponse needs an SP keypair we don't ship by default.
	// Return an unsigned LogoutResponse — most IdPs accept it when the
	// SP-side LogoutRequest verification chain already authenticated the
	// other direction. Operators that need signed responses can wire an
	// SP keypair into the gosaml2 SP and switch to BuildLogoutResponseDocument.
	doc, err := g.sp.BuildLogoutResponseDocumentNoSig(status, requestID)
	if err != nil {
		return nil, err
	}
	return doc.WriteToBytes()
}

// Compile-time proof both interfaces are satisfied.
var (
	_ http.Handler       = (*SAMLSLOHandler)(nil)
	_ SAMLLogoutVerifier = (*gosamlVerifier)(nil)
)
