// Package secrets provides a pluggable Provider interface for fetching
// runtime secrets (DB passwords, API keys, JWT signing material, ...)
// from one of three sources (US-278):
//
//	env     pkg/secrets.EnvProvider     – read os.Getenv (default for dev)
//	file    pkg/secrets.FileProvider    – read $WEAVE_SECRETS_DIR/<key>
//	vault   pkg/secrets.VaultProvider   – read from a HashiCorp Vault KV-v2 mount
//
// Production wiring picks one provider at boot from $SECRET_PROVIDER and
// hands the resulting Provider to every subsystem that needs to load a
// secret. Wrapping the chosen provider in CachingProvider gives short-
// lived caching + TTL-driven rotation awareness so secrets refreshed in
// the underlying source pick up automatically without restarts.
package secrets

import (
	"context"
	"errors"
	"fmt"
)

// Provider is the narrow read interface every secret backend satisfies.
// Get returns the value of key. Implementations must be safe for
// concurrent use.
type Provider interface {
	Get(ctx context.Context, key string) (string, error)
	// Name returns a human-readable identifier (env / file / vault) for
	// log lines and health probes.
	Name() string
}

// ErrSecretNotFound is the canonical sentinel returned when a key has no
// value in the underlying source. Callers that have a fallback should
// errors.Is-check this; callers that hard-require the secret should
// surface the error verbatim.
var ErrSecretNotFound = errors.New("secret not found")

// IsNotFound is a tiny convenience wrapper around errors.Is that callers
// in cmd/server use to decide whether to fall back to a default.
func IsNotFound(err error) bool { return errors.Is(err, ErrSecretNotFound) }

// ProviderKind enumerates the three supported provider shapes. Used by
// the Selector below to map $SECRET_PROVIDER to a concrete Provider.
type ProviderKind string

const (
	ProviderEnv   ProviderKind = "env"
	ProviderFile  ProviderKind = "file"
	ProviderVault ProviderKind = "vault"
)

// ParseProviderKind validates and normalises raw to a ProviderKind.
// Empty / "env" / unrecognised → ProviderEnv with no error so the dev
// default stays permissive; explicit "file" / "vault" are passed
// through; anything else is reported.
func ParseProviderKind(raw string) (ProviderKind, error) {
	switch raw {
	case "", string(ProviderEnv):
		return ProviderEnv, nil
	case string(ProviderFile):
		return ProviderFile, nil
	case string(ProviderVault):
		return ProviderVault, nil
	default:
		return "", fmt.Errorf("invalid SECRET_PROVIDER %q (want env/file/vault)", raw)
	}
}
