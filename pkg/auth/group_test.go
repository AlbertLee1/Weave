package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateGroupName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"simple", "analysts", false},
		{"with-dot", "team.na", false},
		{"with-hyphen", "ingest-bots", false},
		{"with-underscore", "field_ops", false},
		{"alphanum", "abc123", false},
		{"leading-digit", "42-answer", false},

		{"empty", "", true},
		{"spaces", "two words", true},
		{"leading-dot", ".hidden", true},
		{"leading-hyphen", "-bad", true},
		{"slash", "a/b", true},
		{"colon", "a:b", true},
		{"too-long", strings.Repeat("a", MaxGroupNameLength+1), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateGroupName(tc.input)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidGroupName) {
					t.Fatalf("expected ErrInvalidGroupName, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("expected nil, got %v", err)
			}
		})
	}
}

func TestValidateGroupName_MaxLengthAccepted(t *testing.T) {
	// Exactly MaxGroupNameLength bytes is allowed (inclusive upper bound).
	name := strings.Repeat("a", MaxGroupNameLength)
	if err := ValidateGroupName(name); err != nil {
		t.Fatalf("expected name at exactly max length to pass, got %v", err)
	}
}
