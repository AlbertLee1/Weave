package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/audit"
)

// RefreshCookieName is the name of the httpOnly refresh cookie. Kept here so
// the handler and any future cookie helpers stay in sync.
const RefreshCookieName = "weave_refresh"

// RefreshRequest is the optional JSON body for /api/auth/refresh. If absent,
// the handler reads the refresh token from the RefreshCookieName cookie.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// RefreshHandlerDeps groups collaborators for the refresh handler.
type RefreshHandlerDeps struct {
	Users          UserRepository
	Resolver       *RoleResolver
	Signer         *JWTSigner
	RefreshService *RefreshService
	AuditStore     audit.Store
}

// RefreshHandler implements POST /api/auth/refresh. It rotates the refresh
// token and issues a fresh access token with newly-resolved roles.
type RefreshHandler struct {
	deps RefreshHandlerDeps
}

// NewRefreshHandler builds a refresh handler.
func NewRefreshHandler(deps RefreshHandlerDeps) *RefreshHandler {
	return &RefreshHandler{deps: deps}
}

// ServeHTTP implements http.Handler.
func (h *RefreshHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MethodNotAllowed", map[string]string{
			"reason": "POST required",
		}))
		return
	}

	var req RefreshRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	plain := req.RefreshToken
	if plain == "" {
		if c, err := r.Cookie(RefreshCookieName); err == nil {
			plain = c.Value
		}
	}
	if plain == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingRefreshToken", map[string]string{
			"reason": "refresh_token is required (in body or cookie)",
		}))
		return
	}

	ctx := r.Context()
	newPlain, newRec, err := h.deps.RefreshService.Rotate(ctx, plain)
	if err != nil {
		switch {
		case errors.Is(err, ErrRefreshTokenNotFound),
			errors.Is(err, ErrRefreshTokenExpired),
			errors.Is(err, ErrRefreshTokenReuseDetected),
			errors.Is(err, ErrRefreshTokenRevoked):
			apierror.WriteJSON(w, apierror.NewUnauthorized("InvalidRefreshToken", map[string]string{
				"reason": err.Error(),
			}))
		default:
			apierror.WriteJSON(w, apierror.NewInternal("RefreshFailed", map[string]string{"reason": err.Error()}))
		}
		return
	}

	user, err := h.deps.Users.GetUserByID(ctx, newRec.UserID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("UserNotFound", map[string]string{"reason": err.Error()}))
		return
	}

	global, scoped, err := h.deps.Resolver.Resolve(ctx, user.ID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("RefreshRoleResolveFailed", map[string]string{"reason": err.Error()}))
		return
	}

	access, err := h.deps.Signer.Sign(SignInput{
		UserID:        user.ID,
		Email:         user.Email,
		Name:          user.Name,
		Roles:         global,
		OntologyRoles: scoped,
	})
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("RefreshSignFailed", map[string]string{"reason": err.Error()}))
		return
	}

	if h.deps.AuditStore != nil {
		_ = audit.Record(ctx, h.deps.AuditStore, audit.AuditEvent{
			ActorID:      user.ID,
			Action:       "token_refresh",
			ResourceType: "Session",
			ResourceRID:  user.ID,
		})
	}

	ttl := 15 * 60
	if h.deps.Signer != nil && h.deps.Signer.ttl > 0 {
		ttl = int(h.deps.Signer.ttl.Seconds())
	}
	resp := LoginResponse{
		AccessToken:  access,
		RefreshToken: newPlain,
		TokenType:    "Bearer",
		ExpiresIn:    ttl,
		User: LoginUser{
			ID:            user.ID,
			Email:         user.Email,
			Name:          user.Name,
			Roles:         emptyIfNilStrings(global),
			OntologyRoles: emptyIfNilMap(scoped),
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

var _ http.Handler = (*RefreshHandler)(nil)
