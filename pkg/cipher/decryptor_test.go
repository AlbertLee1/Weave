package cipher_test

import (
	"context"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/cipher"
)

// US-040: CipherTextProperty decrypt endpoint.
//
// The cipher package provides the Decryptor interface backing the
// /objects/{type}/{pk}/ciphertexts/{property}/decrypt endpoint. The default
// AESGCMDecryptor implements envelope encryption: the ciphertext wire format
// is base64url of (nonce || aes-gcm(plaintext)) prefixed with a version tag,
// so callers can encrypt once and store the resulting blob on an object
// property; the decrypt endpoint round-trips it back to plaintext.

func TestAESGCMDecryptor_RoundTrip(t *testing.T) {
	dec, err := cipher.NewAESGCMDecryptor("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ct, err := dec.Encrypt(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if ct == "" {
		t.Fatal("ciphertext is empty")
	}
	if !strings.HasPrefix(ct, "v1:") {
		t.Errorf("ciphertext = %q, want v1: prefix", ct)
	}
	pt, err := dec.Decrypt(context.Background(), ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if pt != "hello world" {
		t.Errorf("plaintext = %q, want hello world", pt)
	}
}

func TestAESGCMDecryptor_DistinctCiphertexts(t *testing.T) {
	dec, err := cipher.NewAESGCMDecryptor("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	a, _ := dec.Encrypt(context.Background(), "same")
	b, _ := dec.Encrypt(context.Background(), "same")
	if a == b {
		t.Error("random nonce should make ciphertexts differ")
	}
}

func TestAESGCMDecryptor_RejectsBadVersion(t *testing.T) {
	dec, err := cipher.NewAESGCMDecryptor("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := dec.Decrypt(context.Background(), "v9:garbage"); err == nil {
		t.Error("expected error on unknown version tag")
	}
}

func TestAESGCMDecryptor_RejectsShortCiphertext(t *testing.T) {
	dec, err := cipher.NewAESGCMDecryptor("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := dec.Decrypt(context.Background(), "v1:aaaa"); err == nil {
		t.Error("expected error on short ciphertext")
	}
}

func TestAESGCMDecryptor_RejectsTampered(t *testing.T) {
	dec, err := cipher.NewAESGCMDecryptor("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ct, _ := dec.Encrypt(context.Background(), "secret")
	tampered := ct[:len(ct)-2] + "aa"
	if _, err := dec.Decrypt(context.Background(), tampered); err == nil {
		t.Error("expected error on tampered ciphertext")
	}
}

func TestNewAESGCMDecryptor_RejectsShortKey(t *testing.T) {
	if _, err := cipher.NewAESGCMDecryptor("tooShort"); err == nil {
		t.Error("expected error for key < 32 bytes")
	}
}
