package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func writeTestKeyFiles(t *testing.T) (privPath, pubPath string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})
	privPath = filepath.Join(dir, "jwt-private.pem")
	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	pubBytes, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})
	pubPath = filepath.Join(dir, "jwt-public.pem")
	if err := os.WriteFile(pubPath, pubPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return
}

func TestLoadRSAKeysFromFiles(t *testing.T) {
	priv, pub := writeTestKeyFiles(t)
	privKey, pubKey, err := LoadRSAKeysFromFiles(priv, pub)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if privKey == nil || pubKey == nil {
		t.Fatal("expected non-nil keys")
	}
	if privKey.N.Cmp(pubKey.N) != 0 {
		t.Error("public key does not match private key")
	}
}

func TestLoadRSAKeysFromFiles_MissingFile(t *testing.T) {
	_, _, err := LoadRSAKeysFromFiles("/no/such/file", "/no/such/file2")
	if err == nil {
		t.Error("expected error for missing files")
	}
}

func TestLoadRSAKeysFromFiles_NotPEM(t *testing.T) {
	dir := t.TempDir()
	priv := filepath.Join(dir, "p")
	pub := filepath.Join(dir, "u")
	os.WriteFile(priv, []byte("not a pem"), 0o600)
	os.WriteFile(pub, []byte("not a pem"), 0o600)
	_, _, err := LoadRSAKeysFromFiles(priv, pub)
	if err == nil {
		t.Error("expected error for non-PEM input")
	}
}

func TestLoadRSAKeysFromPEM(t *testing.T) {
	privPath, pubPath := writeTestKeyFiles(t)
	privPEM, _ := os.ReadFile(privPath)
	pubPEM, _ := os.ReadFile(pubPath)

	priv, pub, err := LoadRSAKeysFromPEM(string(privPEM), string(pubPEM))
	if err != nil {
		t.Fatalf("LoadFromPEM: %v", err)
	}
	if priv == nil || pub == nil {
		t.Fatal("expected non-nil keys")
	}
}

func TestLoadRSAKeysFromFiles_RejectsSmallKeys(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pBytes, _ := x509.MarshalPKCS8PrivateKey(priv)
	pPath := filepath.Join(dir, "p.pem")
	os.WriteFile(pPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pBytes}), 0o600)
	uBytes, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	uPath := filepath.Join(dir, "u.pem")
	os.WriteFile(uPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: uBytes}), 0o600)

	if _, _, err := LoadRSAKeysFromFiles(pPath, uPath); err == nil {
		t.Error("expected error for RSA <2048 bits")
	}
}
