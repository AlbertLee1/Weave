package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image/png"
	"net/http"
	"strings"
	"time"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/pquerna/otp"
)

// MFAHandlerDeps groups collaborators for the MFA endpoints.
type MFAHandlerDeps struct {
	Users          UserRepository
	MFAStore       MFASecretStore
	Resolver       *RoleResolver
	Signer         *JWTSigner
	RefreshService *RefreshService
	MFAChallenges  *MFAChallengeStore
	MarkingRepo    MarkingRepository
	AuditStore     audit.Store
	// Sessions is the optional session inventory (US-254). When wired,
	// successful /verify inserts a row alongside the access-token issuance.
	Sessions SessionStore
	// Issuer is the label embedded in the otpauth:// URL that authenticator
	// apps surface as the account scope. Defaults to DefaultMFAIssuer.
	Issuer string
	// Now is an injectable clock used by the TOTP validator. nil falls
	// back to time.Now.
	Now func() time.Time
}

// MFAHandler implements the four MFA endpoints:
//
//	POST /api/auth/mfa/setup    — start enrollment, return secret + QR
//	POST /api/auth/mfa/enable   — finish enrollment, flip mfa_enabled=true
//	POST /api/auth/mfa/disable  — wipe secret + flag (requires valid code)
//	POST /api/auth/mfa/verify   — second-factor step at login time
//
// The first three require an authenticated request (UserFromContext != nil)
// because they only ever modify the caller's own MFA state. /verify is
// public — it consumes the challenge token minted by the login handler.
type MFAHandler struct {
	deps MFAHandlerDeps
	now  func() time.Time
}

// NewMFAHandler builds a handler. Panics if Users / MFAStore are nil since
// every endpoint depends on both.
func NewMFAHandler(deps MFAHandlerDeps) *MFAHandler {
	if deps.Users == nil {
		panic("MFAHandler requires Users")
	}
	if deps.MFAStore == nil {
		panic("MFAHandler requires MFAStore")
	}
	if deps.Issuer == "" {
		deps.Issuer = DefaultMFAIssuer
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	return &MFAHandler{deps: deps, now: now}
}

// RegisterRoutes mounts all MFA endpoints on the supplied router. The
// minimal router interface keeps this method usable from chi.Router as
// well as the contract-test stub router.
func (h *MFAHandler) RegisterRoutes(mux interface {
	Method(method, pattern string, handler http.Handler)
}) {
	mux.Method(http.MethodPost, "/api/auth/mfa/setup", http.HandlerFunc(h.Setup))
	mux.Method(http.MethodPost, "/api/auth/mfa/enable", http.HandlerFunc(h.Enable))
	mux.Method(http.MethodPost, "/api/auth/mfa/disable", http.HandlerFunc(h.Disable))
	mux.Method(http.MethodPost, "/api/auth/mfa/verify", http.HandlerFunc(h.Verify))
}

// MFASetupResponse is the body of POST /api/auth/mfa/setup. Authenticator
// apps consume either the otpauth URL (preferred — pre-encodes issuer +
// account) or scan the QR PNG. Secret is provided as a fallback for apps
// that want manual entry.
type MFASetupResponse struct {
	Secret    string `json:"secret"`
	OTPAuth   string `json:"otpauth_url"`
	QRPNG     string `json:"qr_png_base64"`
	Issuer    string `json:"issuer"`
	Account   string `json:"account"`
	Activated bool   `json:"activated"`
}

// Setup generates a fresh TOTP secret for the calling user and persists it
// (un-activated). Returns the secret + QR code so the user can register the
// secret in their authenticator app. Calling /setup again rotates the
// secret — any previously-issued QR codes stop working immediately.
func (h *MFAHandler) Setup(w http.ResponseWriter, r *http.Request) {
	caller := UserFromContext(r.Context())
	if caller == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("Unauthorized", map[string]string{"reason": "authentication required"}))
		return
	}
	user, err := h.deps.Users.GetUserByID(r.Context(), caller.ID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("UserNotFound", map[string]string{"reason": err.Error()}))
		return
	}
	account := user.Email
	if account == "" {
		account = user.ID
	}
	key, err := GenerateTOTPSecret(h.deps.Issuer, account)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("MFASetupFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if err := h.deps.MFAStore.SetMFASecret(r.Context(), user.ID, key.Secret()); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("MFAPersistFailed", map[string]string{"reason": err.Error()}))
		return
	}
	// Disable enforcement until the user proves possession of the secret
	// via /enable. Calling /setup again on an already-enabled account
	// resets enrollment back to "pending".
	if user.MFAEnabled {
		if err := h.deps.MFAStore.SetMFAEnabled(r.Context(), user.ID, false); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("MFAPersistFailed", map[string]string{"reason": err.Error()}))
			return
		}
	}

	qrBase64, err := renderQRPNG(key.URL())
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("MFAQRRenderFailed", map[string]string{"reason": err.Error()}))
		return
	}
	h.audit(r.Context(), user.ID, "mfa_setup", r)
	writeJSON(w, http.StatusOK, MFASetupResponse{
		Secret:    key.Secret(),
		OTPAuth:   key.URL(),
		QRPNG:     qrBase64,
		Issuer:    h.deps.Issuer,
		Account:   account,
		Activated: false,
	})
}

// MFACodeRequest is the JSON body for /enable, /disable, and /verify.
type MFACodeRequest struct {
	Code           string `json:"code"`
	ChallengeToken string `json:"challenge_token,omitempty"`
}

// Enable verifies the supplied code against the previously-persisted secret
// and flips mfa_enabled=true. Idempotent: calling enable on an already-
// enabled account succeeds as long as the code is valid.
func (h *MFAHandler) Enable(w http.ResponseWriter, r *http.Request) {
	caller := UserFromContext(r.Context())
	if caller == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("Unauthorized", map[string]string{"reason": "authentication required"}))
		return
	}
	var req MFACodeRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidMFARequest", map[string]string{"reason": err.Error()}))
		return
	}
	req.Code = strings.TrimSpace(req.Code)
	if req.Code == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingMFACode", map[string]string{"reason": "code is required"}))
		return
	}
	user, err := h.deps.Users.GetUserByID(r.Context(), caller.ID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("UserNotFound", map[string]string{"reason": err.Error()}))
		return
	}
	if user.MFASecret == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MFANotEnrolled", map[string]string{"reason": "call /api/auth/mfa/setup first"}))
		return
	}
	if err := ValidateTOTPCode(user.MFASecret, req.Code, h.now()); err != nil {
		h.audit(r.Context(), user.ID, "mfa_enable_failed", r)
		apierror.WriteJSON(w, apierror.NewUnauthorized("InvalidMFACode", map[string]string{"reason": err.Error()}))
		return
	}
	if err := h.deps.MFAStore.SetMFAEnabled(r.Context(), user.ID, true); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("MFAPersistFailed", map[string]string{"reason": err.Error()}))
		return
	}
	h.audit(r.Context(), user.ID, "mfa_enabled", r)
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true})
}

// Disable verifies the supplied code (preventing accidental disable by an
// attacker who only stole the session) and clears both the secret and the
// flag. Idempotent.
func (h *MFAHandler) Disable(w http.ResponseWriter, r *http.Request) {
	caller := UserFromContext(r.Context())
	if caller == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("Unauthorized", map[string]string{"reason": "authentication required"}))
		return
	}
	var req MFACodeRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidMFARequest", map[string]string{"reason": err.Error()}))
		return
	}
	req.Code = strings.TrimSpace(req.Code)
	user, err := h.deps.Users.GetUserByID(r.Context(), caller.ID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("UserNotFound", map[string]string{"reason": err.Error()}))
		return
	}
	if user.MFASecret == "" {
		// Already disabled — make this a no-op success rather than 400.
		h.audit(r.Context(), user.ID, "mfa_disabled", r)
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	if req.Code == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingMFACode", map[string]string{"reason": "code is required"}))
		return
	}
	if err := ValidateTOTPCode(user.MFASecret, req.Code, h.now()); err != nil {
		h.audit(r.Context(), user.ID, "mfa_disable_failed", r)
		apierror.WriteJSON(w, apierror.NewUnauthorized("InvalidMFACode", map[string]string{"reason": err.Error()}))
		return
	}
	if err := h.deps.MFAStore.ClearMFA(r.Context(), user.ID); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("MFAPersistFailed", map[string]string{"reason": err.Error()}))
		return
	}
	h.audit(r.Context(), user.ID, "mfa_disabled", r)
	writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
}

// Verify is the second-factor step at login time. Consumes the challenge
// token issued by the login handler and the user's 6-digit TOTP code, then
// returns the same LoginResponse the login handler would have returned for
// a non-MFA user.
func (h *MFAHandler) Verify(w http.ResponseWriter, r *http.Request) {
	if h.deps.MFAChallenges == nil {
		apierror.WriteJSON(w, apierror.NewInternal("MFAUnavailable", map[string]string{"reason": "challenge store not configured"}))
		return
	}
	if h.deps.Signer == nil || h.deps.RefreshService == nil || h.deps.Resolver == nil {
		apierror.WriteJSON(w, apierror.NewInternal("MFAUnavailable", map[string]string{"reason": "auth dependencies missing"}))
		return
	}
	var req MFACodeRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidMFARequest", map[string]string{"reason": err.Error()}))
		return
	}
	req.Code = strings.TrimSpace(req.Code)
	req.ChallengeToken = strings.TrimSpace(req.ChallengeToken)
	if req.Code == "" || req.ChallengeToken == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingMFAFields", map[string]string{"reason": "code and challenge_token are required"}))
		return
	}
	userID, err := h.deps.MFAChallenges.Consume(req.ChallengeToken)
	if err != nil {
		switch {
		case errors.Is(err, ErrMFAChallengeNotFound), errors.Is(err, ErrMFAChallengeExpired):
			apierror.WriteJSON(w, apierror.NewUnauthorized("InvalidMFAChallenge", map[string]string{"reason": err.Error()}))
		default:
			apierror.WriteJSON(w, apierror.NewInternal("MFAChallengeFailed", map[string]string{"reason": err.Error()}))
		}
		return
	}
	user, err := h.deps.Users.GetUserByID(r.Context(), userID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("UserNotFound", map[string]string{"reason": err.Error()}))
		return
	}
	if !user.MFAEnabled || user.MFASecret == "" {
		// Defensive: between login and verify the admin disabled MFA on
		// the account. Do NOT mint a token — force the SPA back through
		// /api/auth/login (which will skip the challenge altogether).
		apierror.WriteJSON(w, apierror.NewUnauthorized("MFANotEnrolled", map[string]string{"reason": "user no longer has mfa enabled"}))
		return
	}
	if err := ValidateTOTPCode(user.MFASecret, req.Code, h.now()); err != nil {
		h.audit(r.Context(), user.ID, "mfa_verify_failed", r)
		apierror.WriteJSON(w, apierror.NewUnauthorized("InvalidMFACode", map[string]string{"reason": err.Error()}))
		return
	}
	global, scoped, err := h.deps.Resolver.Resolve(r.Context(), user.ID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("MFARoleResolveFailed", map[string]string{"reason": err.Error()}))
		return
	}
	var markings []string
	if h.deps.MarkingRepo != nil {
		markings, err = h.deps.MarkingRepo.GetUserMarkings(r.Context(), user.ID)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("MFAMarkingResolveFailed", map[string]string{"reason": err.Error()}))
			return
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
		apierror.WriteJSON(w, apierror.NewInternal("MFASignFailed", map[string]string{"reason": err.Error()}))
		return
	}
	refreshPlain, refreshRec, err := h.deps.RefreshService.Generate(r.Context(), user.ID, "")
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("MFARefreshFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if h.deps.Sessions != nil && refreshRec != nil {
		_ = h.deps.Sessions.Create(r.Context(), &SessionRecord{
			UserID:         user.ID,
			RefreshTokenID: refreshRec.ID,
			IP:             clientIP(r),
			UserAgent:      r.UserAgent(),
		})
	}
	ttl := 15 * 60
	if h.deps.Signer.ttl > 0 {
		ttl = int(h.deps.Signer.ttl.Seconds())
	}
	h.audit(r.Context(), user.ID, "mfa_verify_success", r)
	writeJSON(w, http.StatusOK, LoginResponse{
		AccessToken:  access,
		RefreshToken: refreshPlain,
		TokenType:    "Bearer",
		ExpiresIn:    ttl,
		User: LoginUser{
			ID:            user.ID,
			Email:         user.Email,
			Name:          user.Name,
			Roles:         emptyIfNilStrings(global),
			OntologyRoles: emptyIfNilMap(scoped),
		},
	})
}

func (h *MFAHandler) audit(ctx context.Context, actorID, action string, r *http.Request) {
	if h.deps.AuditStore == nil {
		return
	}
	_ = audit.Record(ctx, h.deps.AuditStore, audit.AuditEvent{
		ActorID:      actorID,
		Action:       action,
		ResourceType: "User",
		ResourceRID:  actorID,
		IP:           clientIP(r),
		UserAgent:    r.UserAgent(),
	})
}

// renderQRPNG encodes the supplied otpauth:// URL as a 256x256 QR PNG and
// returns the base64 (no data: prefix) so the SPA can drop it into an
// <img src="data:image/png;base64,...">.
func renderQRPNG(otpURL string) (string, error) {
	key, err := otp.NewKeyFromURL(otpURL)
	if err != nil {
		return "", err
	}
	img, err := key.Image(256, 256)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
