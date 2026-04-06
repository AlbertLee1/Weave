package auth

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
)

// minRSABits is the minimum acceptable RSA modulus size for JWT signing
// keys. 2048 is the floor recommended by NIST and the JWT BCP.
const minRSABits = 2048

// LoadRSAKeysFromFiles parses RSA keys from PEM files at the given paths.
// It accepts PKCS#8 / PKCS#1 private keys and PKIX / PKCS#1 public keys.
// Returns an error if any file is missing, malformed, or the key is shorter
// than minRSABits.
func LoadRSAKeysFromFiles(privPath, pubPath string) (*rsa.PrivateKey, *rsa.PublicKey, error) {
	privPEM, err := os.ReadFile(privPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read private key %q: %w", privPath, err)
	}
	pubPEM, err := os.ReadFile(pubPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read public key %q: %w", pubPath, err)
	}
	return LoadRSAKeysFromPEM(string(privPEM), string(pubPEM))
}

// LoadRSAKeysFromPEM parses RSA keys from inline PEM strings.
func LoadRSAKeysFromPEM(privPEM, pubPEM string) (*rsa.PrivateKey, *rsa.PublicKey, error) {
	priv, err := parseRSAPrivateKey([]byte(privPEM))
	if err != nil {
		return nil, nil, fmt.Errorf("parse private key: %w", err)
	}
	pub, err := parseRSAPublicKey([]byte(pubPEM))
	if err != nil {
		return nil, nil, fmt.Errorf("parse public key: %w", err)
	}
	if priv.N.BitLen() < minRSABits {
		return nil, nil, fmt.Errorf("RSA private key is %d bits; minimum is %d", priv.N.BitLen(), minRSABits)
	}
	if pub.N.BitLen() < minRSABits {
		return nil, nil, fmt.Errorf("RSA public key is %d bits; minimum is %d", pub.N.BitLen(), minRSABits)
	}
	return priv, pub, nil
}

func parseRSAPrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no PEM block found in private key input")
	}
	// Try PKCS#8 first.
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rk, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("PKCS#8 key is not RSA")
		}
		return rk, nil
	}
	// Fall back to PKCS#1.
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, errors.New("private key PEM did not parse as PKCS#8 or PKCS#1 RSA")
}

func parseRSAPublicKey(pemBytes []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no PEM block found in public key input")
	}
	// Try PKIX first.
	if key, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		rk, ok := key.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("PKIX key is not RSA")
		}
		return rk, nil
	}
	// Fall back to PKCS#1 public key.
	if key, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, errors.New("public key PEM did not parse as PKIX or PKCS#1 RSA")
}
