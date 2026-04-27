package secrets

import (
	"context"
	"errors"
	"testing"

	vaultapi "github.com/hashicorp/vault/api"
)

// stubLogical satisfies vaultLogicalClient so tests can pin the
// response shape Vault returns for KV-v2 reads.
type stubLogical struct {
	resp *vaultapi.Secret
	err  error
}

func (s *stubLogical) ReadWithContext(_ context.Context, _ string) (*vaultapi.Secret, error) {
	return s.resp, s.err
}

func TestVaultProvider_DefaultField(t *testing.T) {
	stub := &stubLogical{resp: &vaultapi.Secret{
		Data: map[string]interface{}{
			"data": map[string]interface{}{
				"value": "from-vault",
			},
		},
	}}
	p := newVaultProviderWithLogical(stub, "secret")
	got, err := p.Get(context.Background(), "weave/jwt-key")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != "from-vault" {
		t.Errorf("got %q, want from-vault", got)
	}
}

func TestVaultProvider_NamedField(t *testing.T) {
	stub := &stubLogical{resp: &vaultapi.Secret{
		Data: map[string]interface{}{
			"data": map[string]interface{}{
				"username": "weave",
				"password": "shh",
			},
		},
	}}
	p := newVaultProviderWithLogical(stub, "secret")
	got, err := p.Get(context.Background(), "db/cred#password")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != "shh" {
		t.Errorf("got %q, want shh", got)
	}
}

func TestVaultProvider_NotFound(t *testing.T) {
	cases := map[string]*vaultapi.Secret{
		"nil response":    nil,
		"empty data":      {Data: map[string]interface{}{}},
		"no data subkey":  {Data: map[string]interface{}{"foo": "bar"}},
		"missing field":   {Data: map[string]interface{}{"data": map[string]interface{}{"name": "x"}}},
		"empty value str": {Data: map[string]interface{}{"data": map[string]interface{}{"value": ""}}},
	}
	for name, resp := range cases {
		t.Run(name, func(t *testing.T) {
			p := newVaultProviderWithLogical(&stubLogical{resp: resp}, "secret")
			_, err := p.Get(context.Background(), "any")
			if !errors.Is(err, ErrSecretNotFound) {
				t.Errorf("want ErrSecretNotFound, got %v", err)
			}
		})
	}
}

func TestVaultProvider_PropagatesError(t *testing.T) {
	netErr := errors.New("connection refused")
	p := newVaultProviderWithLogical(&stubLogical{err: netErr}, "secret")
	_, err := p.Get(context.Background(), "x")
	if err == nil || errors.Is(err, ErrSecretNotFound) {
		t.Errorf("want network err propagated, got %v", err)
	}
	if !errors.Is(err, netErr) {
		t.Errorf("expected wrap of original error, got %v", err)
	}
}

func TestVaultProvider_BadFieldType(t *testing.T) {
	stub := &stubLogical{resp: &vaultapi.Secret{
		Data: map[string]interface{}{
			"data": map[string]interface{}{"value": 42},
		},
	}}
	p := newVaultProviderWithLogical(stub, "secret")
	_, err := p.Get(context.Background(), "x")
	if err == nil {
		t.Error("non-string field should error")
	}
}

func TestSplitVaultKey(t *testing.T) {
	cases := []struct{ in, path, field string }{
		{"a/b", "a/b", "value"},
		{"a/b#pw", "a/b", "pw"},
		{"a/b#x#y", "a/b#x", "y"}, // last "#" wins
	}
	for _, tc := range cases {
		path, field := splitVaultKey(tc.in)
		if path != tc.path || field != tc.field {
			t.Errorf("split(%q): got (%q,%q), want (%q,%q)",
				tc.in, path, field, tc.path, tc.field)
		}
	}
}

func TestNewVaultProvider_Validates(t *testing.T) {
	if _, err := NewVaultProvider(nil, "secret"); err == nil {
		t.Error("nil client should fail")
	}
	c, _ := vaultapi.NewClient(vaultapi.DefaultConfig())
	if _, err := NewVaultProvider(c, ""); err == nil {
		t.Error("empty mount should fail")
	}
}
