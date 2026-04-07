package auth

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestGenerateKey_Format(t *testing.T) {
	raw, prefix, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if !strings.HasPrefix(raw, "wvk_") {
		t.Errorf("expected raw key to start with wvk_, got %q", raw)
	}
	if len(prefix) != APIKeyPrefixLen {
		t.Errorf("expected prefix length %d, got %d (%q)", APIKeyPrefixLen, len(prefix), prefix)
	}
	// Format: wvk_<prefix>_<random>
	parts := strings.Split(raw, "_")
	if len(parts) != 3 {
		t.Fatalf("expected 3 underscore-separated parts, got %d in %q", len(parts), raw)
	}
	if parts[0] != "wvk" {
		t.Errorf("expected first segment 'wvk', got %q", parts[0])
	}
	if parts[1] != prefix {
		t.Errorf("prefix segment %q does not match returned prefix %q", parts[1], prefix)
	}
	// Random part must be at least 32 base32 chars (256 bits ~ 52 chars b32, but
	// allow padding-stripped variants).
	if len(parts[2]) < 32 {
		t.Errorf("expected random segment >=32 chars, got %d (%q)", len(parts[2]), parts[2])
	}
}

func TestGenerateKey_UniquePerCall(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		raw, _, err := GenerateAPIKey()
		if err != nil {
			t.Fatalf("GenerateAPIKey: %v", err)
		}
		if seen[raw] {
			t.Errorf("duplicate key generated: %q", raw)
		}
		seen[raw] = true
	}
}

func TestHashAPIKey_Deterministic(t *testing.T) {
	raw, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	h1 := HashAPIKey(raw)
	h2 := HashAPIKey(raw)
	if len(h1) != 32 {
		t.Errorf("expected SHA-256 hash length 32, got %d", len(h1))
	}
	if hex.EncodeToString(h1) != hex.EncodeToString(h2) {
		t.Errorf("HashAPIKey not deterministic: %x vs %x", h1, h2)
	}
}

func TestHashAPIKey_DifferentForDifferentKeys(t *testing.T) {
	raw1, _, _ := GenerateAPIKey()
	raw2, _, _ := GenerateAPIKey()
	if hex.EncodeToString(HashAPIKey(raw1)) == hex.EncodeToString(HashAPIKey(raw2)) {
		t.Error("expected distinct hashes for distinct keys")
	}
}

func TestParseAPIKey_ValidFormat(t *testing.T) {
	raw, expectedPrefix, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	prefix, err := ParseAPIKey(raw)
	if err != nil {
		t.Fatalf("ParseAPIKey: %v", err)
	}
	if prefix != expectedPrefix {
		t.Errorf("ParseAPIKey returned prefix %q, expected %q", prefix, expectedPrefix)
	}
}

func TestParseAPIKey_InvalidFormat(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"missing wvk prefix", "abc_12345678_random"},
		{"only wvk_", "wvk_"},
		{"missing random", "wvk_12345678"},
		{"jwt-like", "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJhbGljZSJ9.signed"},
		{"prefix wrong length", "wvk_short_randomranddomrandomrandom"},
		{"random too short", "wvk_12345678_x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseAPIKey(tc.raw); err == nil {
				t.Errorf("expected error for %q, got nil", tc.raw)
			}
		})
	}
}

func TestIsAPIKey_True(t *testing.T) {
	raw, _, _ := GenerateAPIKey()
	if !IsAPIKey(raw) {
		t.Errorf("expected IsAPIKey(%q)=true", raw)
	}
}

func TestIsAPIKey_False(t *testing.T) {
	cases := []string{
		"",
		"abc",
		"eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJhbGljZSJ9.signed",
		"Bearer something",
	}
	for _, c := range cases {
		if IsAPIKey(c) {
			t.Errorf("expected IsAPIKey(%q)=false", c)
		}
	}
}
