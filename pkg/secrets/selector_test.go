package secrets

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSelect_DefaultsToEnvWithCache(t *testing.T) {
	t.Setenv("SECRET_PROVIDER", "")
	t.Setenv("WEAVE_SECRETS_TTL", "")
	p, err := Select(SelectorConfig{})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	cp, ok := p.(*CachingProvider)
	if !ok {
		t.Fatalf("want CachingProvider, got %T", p)
	}
	if _, ok := cp.inner.(*EnvProvider); !ok {
		t.Errorf("inner should be EnvProvider, got %T", cp.inner)
	}
	if cp.ttl != 5*time.Minute {
		t.Errorf("default ttl: got %v, want 5m", cp.ttl)
	}
}

func TestSelect_File(t *testing.T) {
	dir := t.TempDir()
	p, err := Select(SelectorConfig{Provider: "file", FileDir: dir, CacheTTL: -1})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if p.Name() != "file" {
		t.Errorf("name: got %q, want file (cache disabled by ttl=-1)", p.Name())
	}
}

func TestSelect_FileMissingDir(t *testing.T) {
	t.Setenv("WEAVE_SECRETS_DIR", "")
	_, err := Select(SelectorConfig{Provider: "file", FileDir: "/nope/does-not-exist"})
	if err == nil {
		t.Error("expected error for missing dir")
	}
}

func TestSelect_VaultBuildsClient(t *testing.T) {
	t.Setenv("VAULT_ADDR", "http://127.0.0.1:8200")
	t.Setenv("VAULT_TOKEN", "test-token")
	p, err := Select(SelectorConfig{Provider: "vault", VaultMount: "kv", CacheTTL: -1})
	if err != nil {
		t.Fatalf("select vault: %v", err)
	}
	if p.Name() != "vault" {
		t.Errorf("name: got %q, want vault", p.Name())
	}
}

func TestSelect_RejectsInvalidProvider(t *testing.T) {
	if _, err := Select(SelectorConfig{Provider: "k8s"}); err == nil {
		t.Error("invalid provider should error")
	}
}

func TestSelect_BadTTLEnv(t *testing.T) {
	t.Setenv("WEAVE_SECRETS_TTL", "not-a-duration")
	_, err := Select(SelectorConfig{Provider: "env"})
	if err == nil {
		t.Error("invalid ttl should error")
	}
}

func TestSelect_CachingDisabledByNegativeTTL(t *testing.T) {
	p, err := Select(SelectorConfig{Provider: "env", CacheTTL: -1})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if p.Name() != "env" {
		t.Errorf("name: got %q, want bare env", p.Name())
	}
}

func TestSelect_SelectFromEnv_Smoke(t *testing.T) {
	t.Setenv("SECRET_PROVIDER", "env")
	t.Setenv("WEAVE_SECRETS_TTL", "1m")
	p, err := SelectFromEnv()
	if err != nil {
		t.Fatalf("select-from-env: %v", err)
	}
	t.Setenv("WEAVE_SMOKE", "ok")
	v, err := p.Get(context.Background(), "WEAVE_SMOKE")
	if err != nil || v != "ok" {
		t.Errorf("smoke get: %q %v", v, err)
	}
	if _, err := p.Get(context.Background(), "WEAVE_SMOKE_MISSING"); !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("missing should be ErrSecretNotFound, got %v", err)
	}
}
