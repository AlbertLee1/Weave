package secrets

import (
	"context"
	"os"
)

// EnvProvider satisfies Provider by reading values from the process
// environment. Suitable for dev / single-machine deployments where
// secrets are baked into the systemd unit or docker-compose file.
type EnvProvider struct{}

// NewEnvProvider returns the trivial env-backed Provider.
func NewEnvProvider() *EnvProvider { return &EnvProvider{} }

func (*EnvProvider) Name() string { return "env" }

func (*EnvProvider) Get(_ context.Context, key string) (string, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return "", ErrSecretNotFound
	}
	return v, nil
}
