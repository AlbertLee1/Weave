package auth

import (
	"strings"
	"testing"
)

func TestHashPassword_RoundTrip(t *testing.T) {
	pwd := "correct horse battery staple"
	hash, err := HashPassword(pwd)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if hash == pwd {
		t.Fatal("hash must not equal plaintext")
	}
	if !strings.HasPrefix(hash, "$2") {
		t.Errorf("expected bcrypt prefix $2, got %q", hash[:4])
	}
	if err := VerifyPassword(hash, pwd); err != nil {
		t.Errorf("VerifyPassword on correct password: %v", err)
	}
}

func TestVerifyPassword_Wrong(t *testing.T) {
	hash, err := HashPassword("realpw12345")
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyPassword(hash, "wrongpw12345"); err == nil {
		t.Error("expected error for wrong password")
	}
}

func TestHashPassword_EmptyRejected(t *testing.T) {
	if _, err := HashPassword(""); err == nil {
		t.Error("expected error hashing empty password")
	}
}

func TestVerifyDummyPassword_AlwaysFails(t *testing.T) {
	// Used by login handler in the user-not-found path to keep timing constant.
	if err := VerifyDummyPassword("anything"); err == nil {
		t.Error("dummy compare must always return error")
	}
	if err := VerifyDummyPassword(""); err == nil {
		t.Error("dummy compare must always return error")
	}
}

func TestHashPassword_DifferentEachTime(t *testing.T) {
	// bcrypt uses random salt, so two hashes of the same password must differ.
	h1, err := HashPassword("samepassword")
	if err != nil {
		t.Fatal(err)
	}
	h2, err := HashPassword("samepassword")
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Error("expected different hashes due to random salt")
	}
}
