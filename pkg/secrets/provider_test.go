package secrets

import (
	"errors"
	"testing"
)

func TestParseProviderKind(t *testing.T) {
	cases := []struct {
		in      string
		want    ProviderKind
		wantErr bool
	}{
		{"", ProviderEnv, false},
		{"env", ProviderEnv, false},
		{"file", ProviderFile, false},
		{"vault", ProviderVault, false},
		{"VAULT", "", true},
		{"k8s", "", true},
	}
	for _, tc := range cases {
		got, err := ParseProviderKind(tc.in)
		if tc.wantErr && err == nil {
			t.Errorf("input %q: expected err", tc.in)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("input %q: unexpected err: %v", tc.in, err)
		}
		if !tc.wantErr && got != tc.want {
			t.Errorf("input %q: want %q, got %q", tc.in, tc.want, got)
		}
	}
}

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(ErrSecretNotFound) {
		t.Error("IsNotFound should match the canonical sentinel")
	}
	wrapped := errors.New("wrap: " + ErrSecretNotFound.Error())
	if IsNotFound(wrapped) {
		t.Error("IsNotFound should NOT match a string-wrapped variant")
	}
	if IsNotFound(nil) {
		t.Error("IsNotFound(nil) should be false")
	}
}
