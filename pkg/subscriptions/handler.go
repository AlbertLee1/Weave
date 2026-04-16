package subscriptions

import (
	"errors"
	"net/http"

	"github.com/liyang/weave/pkg/apierror"
)

// ErrInvalidToken is returned by a TokenValidator when the token is missing,
// malformed, expired, or otherwise invalid.
var ErrInvalidToken = errors.New("invalid or missing token")

// TokenValidator verifies a bearer token from the ?token= query parameter
// and returns the authenticated user ID. Return ErrInvalidToken (or any
// error) to reject the connection with 401.
//
// A nil TokenValidator means dev mode: all connections are accepted
// regardless of whether a token is supplied.
type TokenValidator func(token string) (userID string, err error)

// Handler is an http.Handler that validates the ?token= query parameter,
// then upgrades to WebSocket and registers the connection with the Hub.
type Handler struct {
	hub      *Hub
	validate TokenValidator
}

// NewHandler creates a Handler. Pass nil for validate to accept all
// connections without authentication (dev mode).
func NewHandler(hub *Hub, validate TokenValidator) *Handler {
	return &Handler{
		hub:      hub,
		validate: validate,
	}
}

// ServeHTTP implements http.Handler. It checks the ?token= query parameter
// against the validator (if configured) before upgrading to WebSocket.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.validate != nil {
		token := r.URL.Query().Get("token")
		_, err := h.validate(token)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewUnauthorized("InvalidToken", map[string]string{
				"reason": "Bearer token in ?token= query parameter is required",
			}))
			return
		}
	}

	h.hub.HandleWS(w, r)
}
