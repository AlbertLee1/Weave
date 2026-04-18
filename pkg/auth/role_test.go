package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateRoleName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"viewer", "viewer", false},
		{"editor", "editor", false},
		{"ontology-owner", "ontology-owner", false},
		{"admin", "admin", false},
		{"ingest-writer", "ingest-writer", false},
		{"custom-with-dot", "data.scientist", false},
		{"custom-with-underscore", "field_ops", false},

		{"empty", "", true},
		{"with-space", "admin user", true},
		{"leading-hyphen", "-admin", true},
		{"slash", "admin/super", true},
		{"too-long", strings.Repeat("r", MaxRoleNameLength+1), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRoleName(tc.input)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidRoleName) {
					t.Fatalf("expected ErrInvalidRoleName, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("expected nil, got %v", err)
			}
		})
	}
}
