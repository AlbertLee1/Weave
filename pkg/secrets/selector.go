package secrets

import (
	"errors"
	"fmt"
	"os"
	"time"

	vaultapi "github.com/hashicorp/vault/api"
)

// SelectorConfig controls Selector / SelectFromEnv. Every field has a
// sensible default so callers can leave it zeroed for the common case.
type SelectorConfig struct {
	// Provider chooses the backend. "" / "env" / "file" / "vault".
	Provider string
	// FileDir is the on-disk root for the file provider. Defaults to
	// $WEAVE_SECRETS_DIR or /var/run/secrets/weave.
	FileDir string
	// VaultMount is the KV-v2 mount path. Defaults to $VAULT_KV_MOUNT
	// or "secret".
	VaultMount string
	// CacheTTL is the rotation interval for refresh-on-expiry caching.
	// Defaults to $WEAVE_SECRETS_TTL parsed as a Go duration, or 5m
	// when both are unset. Pass time.Duration(-1) to disable caching.
	CacheTTL time.Duration
}

// Select builds the configured Provider, wrapped in CachingProvider.
// Returns an explicit error rather than a fallback so misconfigured
// boots fail loudly.
func Select(cfg SelectorConfig) (Provider, error) {
	kind, err := ParseProviderKind(cfg.Provider)
	if err != nil {
		return nil, err
	}
	var inner Provider
	switch kind {
	case ProviderEnv:
		inner = NewEnvProvider()
	case ProviderFile:
		dir := cfg.FileDir
		if dir == "" {
			dir = os.Getenv("WEAVE_SECRETS_DIR")
		}
		if dir == "" {
			dir = "/var/run/secrets/weave"
		}
		fp, err := NewFileProvider(dir)
		if err != nil {
			return nil, err
		}
		inner = fp
	case ProviderVault:
		mount := cfg.VaultMount
		if mount == "" {
			mount = os.Getenv("VAULT_KV_MOUNT")
		}
		if mount == "" {
			mount = "secret"
		}
		client, err := buildVaultClient()
		if err != nil {
			return nil, err
		}
		vp, err := NewVaultProvider(client, mount)
		if err != nil {
			return nil, err
		}
		inner = vp
	default:
		return nil, fmt.Errorf("unsupported provider kind %q", kind)
	}

	ttl := cfg.CacheTTL
	if ttl == 0 {
		if raw := os.Getenv("WEAVE_SECRETS_TTL"); raw != "" {
			d, err := time.ParseDuration(raw)
			if err != nil {
				return nil, fmt.Errorf("WEAVE_SECRETS_TTL: %w", err)
			}
			ttl = d
		} else {
			ttl = 5 * time.Minute
		}
	}
	if ttl < 0 {
		// Caller explicitly disabled caching.
		return inner, nil
	}
	return NewCachingProvider(inner, ttl), nil
}

// SelectFromEnv is the cmd/server-side convenience wrapper around
// Select that pulls every field from environment variables.
func SelectFromEnv() (Provider, error) {
	return Select(SelectorConfig{
		Provider:   os.Getenv("SECRET_PROVIDER"),
		FileDir:    os.Getenv("WEAVE_SECRETS_DIR"),
		VaultMount: os.Getenv("VAULT_KV_MOUNT"),
	})
}

// buildVaultClient constructs a *vaultapi.Client from the standard
// VAULT_ADDR / VAULT_TOKEN / VAULT_NAMESPACE env vars. Kept private so
// tests can inject a fake Logical client via newVaultProviderWithLogical
// instead of standing up real Vault.
func buildVaultClient() (*vaultapi.Client, error) {
	cfg := vaultapi.DefaultConfig()
	if cfg == nil {
		return nil, errors.New("vault: failed to build default config")
	}
	if addr := os.Getenv("VAULT_ADDR"); addr != "" {
		cfg.Address = addr
	}
	client, err := vaultapi.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("vault: build client: %w", err)
	}
	if tok := os.Getenv("VAULT_TOKEN"); tok != "" {
		client.SetToken(tok)
	}
	if ns := os.Getenv("VAULT_NAMESPACE"); ns != "" {
		client.SetNamespace(ns)
	}
	return client, nil
}
