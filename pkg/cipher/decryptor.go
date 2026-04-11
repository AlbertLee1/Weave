// Package cipher implements the Decryptor interface backing Foundry OSv2
// CipherTextProperty decrypt endpoint.
//
// The default AESGCMDecryptor is an envelope-encryption implementation
// suitable for single-machine deployments: plaintext is encrypted with
// AES-256-GCM using a fixed master key, and the wire format is a versioned,
// base64url-encoded blob "v1:<base64url(nonce||ciphertext||tag)>". Swapping
// in a KMS-backed Decryptor (AWS KMS, GCP KMS, Vault) is a matter of
// providing another implementation of the interface; the rest of the server
// never sees the key material directly.
package cipher

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Decryptor turns a stored ciphertext blob back into plaintext. Encrypt is
// exposed on the same interface so tests and seed tooling can produce
// ciphertexts without reaching into the internals.
type Decryptor interface {
	Encrypt(ctx context.Context, plaintext string) (string, error)
	Decrypt(ctx context.Context, ciphertext string) (string, error)
}

// ErrInvalidCiphertext is returned when the wire blob cannot be decoded or
// is rejected by the AEAD check (tampered / wrong key / wrong version).
// Handlers map this to a 400 InvalidCiphertext response.
var ErrInvalidCiphertext = errors.New("cipher: invalid ciphertext")

// AESGCMDecryptor is the default Decryptor. It is safe for concurrent use.
type AESGCMDecryptor struct {
	aead cipher.AEAD
}

// NewAESGCMDecryptor builds an AES-256-GCM Decryptor from a 32-byte master
// key. The key is accepted as a string for easy env-var wiring; anything
// shorter than 32 bytes is rejected.
func NewAESGCMDecryptor(key string) (*AESGCMDecryptor, error) {
	if len(key) < 32 {
		return nil, fmt.Errorf("cipher: master key must be at least 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher([]byte(key)[:32])
	if err != nil {
		return nil, fmt.Errorf("cipher: aes.NewCipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher: cipher.NewGCM: %w", err)
	}
	return &AESGCMDecryptor{aead: aead}, nil
}

// Encrypt produces a ciphertext of the form "v1:<base64url(nonce||sealed)>".
// A fresh random nonce is drawn for every call, so the same plaintext
// produces distinct ciphertexts.
func (d *AESGCMDecryptor) Encrypt(_ context.Context, plaintext string) (string, error) {
	nonce := make([]byte, d.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("cipher: rand: %w", err)
	}
	sealed := d.aead.Seal(nil, nonce, []byte(plaintext), nil)
	blob := append(nonce, sealed...)
	return "v1:" + base64.RawURLEncoding.EncodeToString(blob), nil
}

// Decrypt parses the wire blob and returns the plaintext, or
// ErrInvalidCiphertext if the blob is malformed or the AEAD check fails.
func (d *AESGCMDecryptor) Decrypt(_ context.Context, ciphertext string) (string, error) {
	const prefix = "v1:"
	if !strings.HasPrefix(ciphertext, prefix) {
		return "", fmt.Errorf("%w: unknown version", ErrInvalidCiphertext)
	}
	blob, err := base64.RawURLEncoding.DecodeString(ciphertext[len(prefix):])
	if err != nil {
		return "", fmt.Errorf("%w: base64: %v", ErrInvalidCiphertext, err)
	}
	nonceSize := d.aead.NonceSize()
	if len(blob) < nonceSize+d.aead.Overhead() {
		return "", fmt.Errorf("%w: blob too short", ErrInvalidCiphertext)
	}
	nonce, sealed := blob[:nonceSize], blob[nonceSize:]
	plaintext, err := d.aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidCiphertext, err)
	}
	return string(plaintext), nil
}
