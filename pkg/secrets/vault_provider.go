package secrets

import (
	"context"
	"errors"
	"fmt"
	"strings"

	vaultapi "github.com/hashicorp/vault/api"
)

// VaultProvider satisfies Provider by reading values from a HashiCorp
// Vault KV-v2 mount. Keys carry the path inside the mount, e.g.
// "weave/jwt-signing-key" — the provider looks up
// $mount/data/$key and returns the named field from the response. By
// default the field is "value"; pass an explicit field via key syntax
// "<path>#<field>" to read an arbitrary key inside the secret blob.
//
// Production wiring imports the vault api lazily via vault.Config from
// the standard env vars (VAULT_ADDR / VAULT_TOKEN / VAULT_NAMESPACE) and
// hands the resulting *vaultapi.Client into NewVaultProvider so tests
// can stub the client without standing up real Vault.
type VaultProvider struct {
	logical vaultLogicalClient
	mount   string
}

// vaultLogicalClient is the narrow read shape we depend on; satisfied
// by *vaultapi.Logical at runtime and by a hand-rolled stub in tests.
type vaultLogicalClient interface {
	ReadWithContext(ctx context.Context, path string) (*vaultapi.Secret, error)
}

// NewVaultProvider builds a VaultProvider against the given client and
// KV-v2 mount path (e.g. "secret"). The mount must already be enabled
// on the Vault server.
func NewVaultProvider(client *vaultapi.Client, mount string) (*VaultProvider, error) {
	if client == nil {
		return nil, errors.New("NewVaultProvider: nil client")
	}
	if mount == "" {
		return nil, errors.New("NewVaultProvider: empty mount path")
	}
	return &VaultProvider{logical: client.Logical(), mount: strings.Trim(mount, "/")}, nil
}

// newVaultProviderWithLogical is the test-only ctor that lets fixtures
// inject a hand-rolled vaultLogicalClient stub.
func newVaultProviderWithLogical(logical vaultLogicalClient, mount string) *VaultProvider {
	return &VaultProvider{logical: logical, mount: strings.Trim(mount, "/")}
}

func (p *VaultProvider) Name() string { return "vault" }

// Get reads $mount/data/$key from Vault and returns the requested
// field. key syntax: "<path>" → field="value"; "<path>#<field>" → that
// named field.
func (p *VaultProvider) Get(ctx context.Context, key string) (string, error) {
	path, field := splitVaultKey(key)
	if path == "" {
		return "", fmt.Errorf("VaultProvider: empty key")
	}
	full := p.mount + "/data/" + strings.TrimPrefix(path, "/")
	sec, err := p.logical.ReadWithContext(ctx, full)
	if err != nil {
		return "", fmt.Errorf("VaultProvider read %q: %w", full, err)
	}
	if sec == nil || sec.Data == nil {
		return "", ErrSecretNotFound
	}
	// KV-v2 wraps the actual secret blob inside Data["data"].
	dataNode, ok := sec.Data["data"].(map[string]interface{})
	if !ok || dataNode == nil {
		return "", ErrSecretNotFound
	}
	raw, ok := dataNode[field]
	if !ok {
		return "", ErrSecretNotFound
	}
	str, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("VaultProvider: field %q is %T not string", field, raw)
	}
	if str == "" {
		return "", ErrSecretNotFound
	}
	return str, nil
}

func splitVaultKey(key string) (path, field string) {
	if idx := strings.LastIndexByte(key, '#'); idx >= 0 {
		return key[:idx], key[idx+1:]
	}
	return key, "value"
}
