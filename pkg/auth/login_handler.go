package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/audit"
)

// LoginRequest is the JSON request body for POST /api/auth/login.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginUser is the user payload returned on successful login.
type LoginUser struct {
	ID            string            `json:"id"`
	Email         string            `json:"email"`
	Name          string            `json:"name"`
	Roles         []string          `json:"roles"`
	OntologyRoles map[string]string `json:"ontologyRoles"`
}

// LoginResponse is the JSON response for POST /api/auth/login and refresh.
type LoginResponse struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	User         LoginUser `json:"user"`
}

// LoginHandlerDeps groups all collaborators for the login handler.
type LoginHandlerDeps struct {
	Users          UserRepository
	Resolver       *RoleResolver
	Signer         *JWTSigner
	RefreshService *RefreshService
	// RateLimit is the max attempts per IP per minute. <=0 disables.
	RateLimit   int
	AuditStore  audit.Store
	MarkingRepo MarkingRepository // US-082: optional; when set, user markings are included in the JWT
}

// LoginHandler implements POST /api/auth/login. It returns access + refresh
// tokens on success, and a generic 401 on any failure (wrong password,
// missing user, password not set, etc.) to avoid user enumeration.
type LoginHandler struct {
	deps            LoginHandlerDeps
	limiter         *ipRateLimiter
	signerAccessTTL time.Duration
}

// NewLoginHandler builds a handler. Pass RateLimit=0 to disable.
func NewLoginHandler(deps LoginHandlerDeps) *LoginHandler {
	var limiter *ipRateLimiter
	if deps.RateLimit > 0 {
		limiter = newIPRateLimiter(deps.RateLimit, time.Minute)
	}
	ttl := 15 * time.Minute
	if deps.Signer != nil && deps.Signer.ttl > 0 {
		ttl = deps.Signer.ttl
	}
	return &LoginHandler{deps: deps, limiter: limiter, signerAccessTTL: ttl}
}

// ServeHTTP makes LoginHandler an http.Handler.
func (h *LoginHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MethodNotAllowed", map[string]string{
			"reason": "POST required",
		}))
		return
	}

	if h.limiter != nil {
		ip := clientIP(r)
		if ok, retryAfter := h.limiter.allow(ip); !ok {
			retryS := int(retryAfter.Seconds())
			if retryS < 1 {
				retryS = 1
			}
			w.Header().Set("Retry-After", fmt.Sprintf("%d", retryS))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errorCode":         "RATE_LIMITED",
				"errorName":         "TooManyLoginAttempts",
				"retryAfterSeconds": retryS,
			})
			return
		}
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidLoginRequest", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || req.Password == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingCredentials", map[string]string{
			"reason": "email and password are required",
		}))
		return
	}

	ctx := r.Context()
	user, err := h.deps.Users.GetUserByEmail(ctx, req.Email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			// Constant-time dummy compare to keep timing flat.
			_ = VerifyDummyPassword(req.Password)
			h.auditLogin(ctx, req.Email, "login_failed", r)
			writeInvalidCredentials(w)
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("LoginLookupFailed", map[string]string{"reason": err.Error()}))
		return
	}

	if user.PasswordHash == "" {
		_ = VerifyDummyPassword(req.Password)
		h.auditLogin(ctx, req.Email, "login_failed", r)
		writeInvalidCredentials(w)
		return
	}
	if err := VerifyPassword(user.PasswordHash, req.Password); err != nil {
		h.auditLogin(ctx, user.ID, "login_failed", r)
		writeInvalidCredentials(w)
		return
	}

	// Resolve fresh role grants for the access-token claims.
	global, scoped, err := h.deps.Resolver.Resolve(ctx, user.ID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("LoginRoleResolveFailed", map[string]string{"reason": err.Error()}))
		return
	}

	// US-082: resolve user marking grants so the JWT carries the caller's
	// held markings for downstream marking-based row filter enforcement.
	var markings []string
	if h.deps.MarkingRepo != nil {
		markings, err = h.deps.MarkingRepo.GetUserMarkings(ctx, user.ID)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("LoginMarkingResolveFailed", map[string]string{"reason": err.Error()}))
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
		apierror.WriteJSON(w, apierror.NewInternal("LoginSignFailed", map[string]string{"reason": err.Error()}))
		return
	}

	refreshPlain, _, err := h.deps.RefreshService.Generate(ctx, user.ID, "")
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("LoginRefreshFailed", map[string]string{"reason": err.Error()}))
		return
	}

	h.auditLogin(ctx, user.ID, "login_success", r)

	resp := LoginResponse{
		AccessToken:  access,
		RefreshToken: refreshPlain,
		TokenType:    "Bearer",
		ExpiresIn:    int(h.signerAccessTTL.Seconds()),
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

func (h *LoginHandler) auditLogin(ctx context.Context, actorID, action string, r *http.Request) {
	if h.deps.AuditStore == nil {
		return
	}
	_ = audit.Record(ctx, h.deps.AuditStore, audit.AuditEvent{
		ActorID:      actorID,
		Action:       action,
		ResourceType: "Session",
		ResourceRID:  actorID,
		IP:           clientIP(r),
		UserAgent:    r.UserAgent(),
	})
}

func writeInvalidCredentials(w http.ResponseWriter) {
	apierror.WriteJSON(w, apierror.NewUnauthorized("InvalidCredentials", map[string]string{
		"reason": "invalid email or password",
	}))
}

func clientIP(r *http.Request) string {
	// chi's middleware.RealIP populates RemoteAddr; honour any X-Forwarded-For too.
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		parts := strings.Split(xf, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func emptyIfNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func emptyIfNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// ipRateLimiter is a tiny per-IP fixed-window counter.
type ipRateLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func newIPRateLimiter(max int, window time.Duration) *ipRateLimiter {
	return &ipRateLimiter{
		max:    max,
		window: window,
		hits:   map[string][]time.Time{},
	}
}

func (l *ipRateLimiter) allow(ip string) (bool, time.Duration) {
	now := time.Now()
	cutoff := now.Add(-l.window)
	l.mu.Lock()
	defer l.mu.Unlock()
	hits := l.hits[ip]
	// drop expired
	pruned := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	if len(pruned) >= l.max {
		l.hits[ip] = pruned
		// Return time until the oldest hit expires.
		retryAfter := pruned[0].Add(l.window).Sub(now)
		return false, retryAfter
	}
	pruned = append(pruned, now)
	l.hits[ip] = pruned
	return true, 0
}

// Compile-time interface check.
var _ http.Handler = (*LoginHandler)(nil)
