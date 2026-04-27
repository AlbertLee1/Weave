package tenants

import (
	"errors"
	"testing"
)

func TestValidateTenant(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty", "", true},
		{"basic", "acme", false},
		{"with-dot", "acme.corp", false},
		{"with-dash", "acme-corp", false},
		{"with-underscore", "acme_corp", false},
		{"with-digit", "tenant123", false},
		{"space", "acme corp", true},
		{"slash", "acme/corp", true},
		{"too-long", string(make([]byte, 129)), true},
		{"unicode", "公司", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTenant(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("expected error for %q", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error for %q: %v", tc.input, err)
			}
			if tc.wantErr && err != nil && !errors.Is(err, ErrTenantInvalid) {
				t.Errorf("expected ErrTenantInvalid, got %v", err)
			}
		})
	}
}
