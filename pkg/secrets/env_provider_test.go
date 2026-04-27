package secrets

import (
	"context"
	"errors"
	"testing"
)

func TestEnvProvider_GetSetValue(t *testing.T) {
	t.Setenv("WEAVE_TEST_SECRET", "topsecret")
	p := NewEnvProvider()
	got, err := p.Get(context.Background(), "WEAVE_TEST_SECRET")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != "topsecret" {
		t.Errorf("got %q, want topsecret", got)
	}
}

func TestEnvProvider_NotFound(t *testing.T) {
	p := NewEnvProvider()
	_, err := p.Get(context.Background(), "WEAVE_DEFINITELY_UNSET_NAMA")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("want ErrSecretNotFound, got %v", err)
	}
}

func TestEnvProvider_EmptyValueIsNotFound(t *testing.T) {
	t.Setenv("WEAVE_TEST_SECRET_EMPTY", "")
	p := NewEnvProvider()
	_, err := p.Get(context.Background(), "WEAVE_TEST_SECRET_EMPTY")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("want ErrSecretNotFound for empty env, got %v", err)
	}
}
