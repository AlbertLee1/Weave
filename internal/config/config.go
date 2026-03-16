package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port     int
	LogLevel string
	DataDir  string
	PGDSN    string
	NATSURL  string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:     8080,
		LogLevel: "info",
		DataDir:  "./data",
	}

	if v := os.Getenv("WEAVE_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid WEAVE_PORT %q: %w", v, err)
		}
		cfg.Port = p
	}

	if v := os.Getenv("WEAVE_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}

	if v := os.Getenv("WEAVE_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}

	if v := os.Getenv("PG_DSN"); v != "" {
		cfg.PGDSN = v
	}

	if v := os.Getenv("NATS_URL"); v != "" {
		cfg.NATSURL = v
	}

	return cfg, nil
}
