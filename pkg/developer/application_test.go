package developer

import (
	"crypto/subtle"
	"encoding/hex"
	"strings"
	"testing"
)

func TestGenerateClientID_Format(t *testing.T) {
	id, err := GenerateClientID()
	if err != nil {
		t.Fatalf("GenerateClientID: %v", err)
	}
	if !strings.HasPrefix(id, ClientIDPrefix) {
		t.Errorf("expected prefix %q, got %q", ClientIDPrefix, id)
	}
	rest := strings.TrimPrefix(id, ClientIDPrefix)
	if len(rest) != clientIDRandomLen {
		t.Errorf("random segment length: got %d, want %d (%q)", len(rest), clientIDRandomLen, id)
	}
}

func TestGenerateClientID_UniquePerCall(t *testing.T) {
	seen := make(map[string]bool, 32)
	for i := 0; i < 32; i++ {
		id, err := GenerateClientID()
		if err != nil {
			t.Fatalf("GenerateClientID: %v", err)
		}
		if seen[id] {
			t.Errorf("duplicate client_id generated: %q", id)
		}
		seen[id] = true
	}
}

func TestGenerateClientSecret_Format(t *testing.T) {
	sec, err := GenerateClientSecret()
	if err != nil {
		t.Fatalf("GenerateClientSecret: %v", err)
	}
	if !strings.HasPrefix(sec, ClientSecretPrefix) {
		t.Errorf("expected prefix %q, got %q", ClientSecretPrefix, sec)
	}
	rest := strings.TrimPrefix(sec, ClientSecretPrefix)
	if len(rest) != clientSecretRandomLen {
		t.Errorf("random segment length: got %d, want %d", len(rest), clientSecretRandomLen)
	}
}

func TestHashClientSecret_DeterministicAndDistinct(t *testing.T) {
	s1, _ := GenerateClientSecret()
	s2, _ := GenerateClientSecret()

	h1a := HashClientSecret(s1)
	h1b := HashClientSecret(s1)
	if len(h1a) != 32 {
		t.Errorf("expected 32-byte SHA-256 digest, got %d", len(h1a))
	}
	if subtle.ConstantTimeCompare(h1a, h1b) != 1 {
		t.Errorf("HashClientSecret not deterministic: %s vs %s", hex.EncodeToString(h1a), hex.EncodeToString(h1b))
	}

	h2 := HashClientSecret(s2)
	if subtle.ConstantTimeCompare(h1a, h2) == 1 {
		t.Errorf("expected distinct hashes for distinct secrets")
	}
}

func TestValidateClientSecretShape(t *testing.T) {
	good, _ := GenerateClientSecret()
	if err := ValidateClientSecretShape(good); err != nil {
		t.Errorf("unexpected error on generated secret: %v", err)
	}

	bad := []string{
		"",
		"secret",
		"wvk_12345678_abc",
		ClientSecretPrefix,
		ClientSecretPrefix + "short",
	}
	for _, b := range bad {
		if err := ValidateClientSecretShape(b); err == nil {
			t.Errorf("expected error on %q, got nil", b)
		}
	}
}
