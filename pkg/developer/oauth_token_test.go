package developer

import (
	"crypto/subtle"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestGenerateAccessToken_ShapeAndUnique(t *testing.T) {
	raw, prefix, err := GenerateAccessToken()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.HasPrefix(raw, AccessTokenMarker) {
		t.Errorf("missing wvoa_ marker: %q", raw)
	}
	if len(prefix) != OAuthPrefixLen {
		t.Errorf("prefix length: got %d, want %d", len(prefix), OAuthPrefixLen)
	}
	p2, err := ParseOAuthToken(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p2 != prefix {
		t.Errorf("round-trip prefix mismatch: %q vs %q", p2, prefix)
	}

	// Uniqueness across generations.
	raw2, _, err := GenerateAccessToken()
	if err != nil {
		t.Fatalf("generate #2: %v", err)
	}
	if raw == raw2 {
		t.Errorf("expected distinct tokens")
	}
}

func TestGenerateRefreshToken_MarkerDiffersFromAccess(t *testing.T) {
	raw, _, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.HasPrefix(raw, RefreshTokenMarker) {
		t.Errorf("missing wvor_ marker: %q", raw)
	}
	if IsOAuthAccessToken(raw) {
		t.Errorf("refresh token classified as access")
	}
	if !IsOAuthRefreshToken(raw) {
		t.Errorf("IsOAuthRefreshToken returned false for refresh token")
	}
}

func TestParseOAuthToken_RejectsMalformed(t *testing.T) {
	cases := []string{
		"",
		"not-a-token",
		"wvoa_short",
		"wvoa_12345678",
		"wvoa__abcdefghijklmnopqrstuvwxyz012345",
		"Bearer wvoa_xxxxxxxx_yyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyy",
	}
	for _, c := range cases {
		if _, err := ParseOAuthToken(c); !errors.Is(err, ErrInvalidTokenFormat) {
			t.Errorf("ParseOAuthToken(%q) = %v, want ErrInvalidTokenFormat", c, err)
		}
	}
}

func TestHashOAuthToken_ConstantTimeCompareMatches(t *testing.T) {
	raw, _, _ := GenerateAccessToken()
	h1 := HashOAuthToken(raw)
	h2 := HashOAuthToken(raw)
	if subtle.ConstantTimeCompare(h1, h2) != 1 {
		t.Errorf("same input should produce identical hashes")
	}
	raw2, _, _ := GenerateAccessToken()
	h3 := HashOAuthToken(raw2)
	if subtle.ConstantTimeCompare(h1, h3) == 1 {
		t.Errorf("different tokens should not share a hash")
	}
}

func TestOAuthToken_IsUsable(t *testing.T) {
	now := time.Now()
	t.Run("fresh", func(t *testing.T) {
		tok := &OAuthToken{ExpiresAt: now.Add(time.Hour)}
		if err := tok.IsUsable(now); err != nil {
			t.Errorf("expected usable, got %v", err)
		}
	})
	t.Run("expired", func(t *testing.T) {
		tok := &OAuthToken{ExpiresAt: now.Add(-1 * time.Second)}
		if err := tok.IsUsable(now); !errors.Is(err, ErrTokenExpired) {
			t.Errorf("expected expired, got %v", err)
		}
	})
	t.Run("revoked", func(t *testing.T) {
		rev := now
		tok := &OAuthToken{ExpiresAt: now.Add(time.Hour), RevokedAt: &rev}
		if err := tok.IsUsable(now); !errors.Is(err, ErrTokenRevoked) {
			t.Errorf("expected revoked, got %v", err)
		}
	})
}

func TestScopeIntersects(t *testing.T) {
	cases := []struct {
		name     string
		granted  []string
		required []string
		want     bool
	}{
		{"noRequired", []string{"read"}, nil, true},
		{"match", []string{"read", "write"}, []string{"write"}, true},
		{"miss", []string{"read"}, []string{"write"}, false},
		{"anyOfMany", []string{"read"}, []string{"admin", "read"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ScopeIntersects(tc.granted, tc.required); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
