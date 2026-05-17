package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"time"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
)

// AdminAuthKeysRotateDeps wires the JWT keyring rotation handler. Signer is
// the live *auth.JWTSigner used by the rest of the auth stack; the handler
// generates a fresh RSA-2048 keypair and appends it to the ring so the
// process atomically transitions to signing under the new key while older
// tokens keep verifying until their natural expiration.
//
// rsaBits is overridable for tests so they don't pay the cost of a real
// 2048-bit key generation on every assertion; production callers leave it
// zero to pick up the 2048 default.
type AdminAuthKeysRotateDeps struct {
	Signer  *auth.JWTSigner
	rsaBits int
}

// AdminAuthKeysRotateResponse is the wire shape of
// POST /api/admin/auth/keys/rotate.
type AdminAuthKeysRotateResponse struct {
	ActiveKeyId string   `json:"activeKeyId"`
	KeyIds      []string `json:"keyIds"`
	RotatedAt   string   `json:"rotatedAt"`
}

// NewAdminAuthKeysRotateHandler builds the HTTP handler that generates a
// fresh RSA keypair and appends it to the signer's keyring. The handler is
// expected to be wrapped in auth.RequirePermission(PermUserManage) by the
// router; it performs no authorization on its own.
//
// On success returns 200 with the new active kid + the full ring. Returns
// 503 if no signer is configured (degraded boot / dev-without-keys), or 500
// if key generation or rotation fails.
func NewAdminAuthKeysRotateHandler(deps AdminAuthKeysRotateDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deps.Signer == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"errorCode": "SERVICE_UNAVAILABLE",
				"errorName": "JWTSignerNotConfigured",
			})
			return
		}

		bits := deps.rsaBits
		if bits == 0 {
			bits = 2048
		}
		priv, err := rsa.GenerateKey(rand.Reader, bits)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("JWTKeyGenerationFailed", map[string]string{
				"reason": err.Error(),
			}))
			return
		}

		kid, err := deps.Signer.Rotate(priv, &priv.PublicKey)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("JWTKeyRotationFailed", map[string]string{
				"reason": err.Error(),
			}))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(AdminAuthKeysRotateResponse{
			ActiveKeyId: kid,
			KeyIds:      deps.Signer.KeyIDs(),
			RotatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		})
	})
}
