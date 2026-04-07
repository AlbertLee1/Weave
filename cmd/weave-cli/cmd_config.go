package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Config holds the persisted CLI configuration. It is a flat map for
// simplicity — no profiles yet.
type Config struct {
	BaseURL     string
	AccessToken string
	APIKey      string
}

// configKeys is the set of keys recognised by `weave config get|set`.
var configKeys = []string{"base_url", "access_token", "api_key"}

func configDir() string {
	if v := os.Getenv("WEAVE_CONFIG_DIR"); v != "" {
		return v
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "weave")
	}
	return ".weave"
}

func configPath() string {
	return filepath.Join(configDir(), "config.toml")
}

// LoadConfig reads the on-disk config file. A missing file is not an error;
// it returns the zero Config.
func LoadConfig() (*Config, error) {
	cfg := &Config{}
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"`)
		switch key {
		case "base_url":
			cfg.BaseURL = val
		case "access_token":
			cfg.AccessToken = val
		case "api_key":
			cfg.APIKey = val
		}
	}
	return cfg, nil
}

// SaveConfig writes the config file, creating parent dirs as needed.
func SaveConfig(cfg *Config) error {
	dir := configDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("[default]\n")
	b.WriteString(fmt.Sprintf("base_url = %q\n", cfg.BaseURL))
	b.WriteString(fmt.Sprintf("access_token = %q\n", cfg.AccessToken))
	b.WriteString(fmt.Sprintf("api_key = %q\n", cfg.APIKey))
	return os.WriteFile(configPath(), []byte(b.String()), 0o600)
}

// Token returns the access token, falling back to the API key.
func (c *Config) Token() string {
	if c.AccessToken != "" {
		return c.AccessToken
	}
	return c.APIKey
}

func runConfig(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: weave config <get|set> [key] [value]")
		return 2
	}
	switch args[0] {
	case "get":
		return configGet(args[1:], stdout, stderr)
	case "set":
		return configSet(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "weave config: unknown subcommand %q\n", args[0])
		return 2
	}
}

func configGet(args []string, stdout, stderr io.Writer) int {
	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return 1
	}
	if len(args) == 0 {
		// Dump everything in stable order.
		all := map[string]string{
			"base_url":     cfg.BaseURL,
			"access_token": cfg.AccessToken,
			"api_key":      cfg.APIKey,
		}
		keys := make([]string, 0, len(all))
		for k := range all {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(stdout, "%s = %q\n", k, all[k])
		}
		return 0
	}
	key := args[0]
	switch key {
	case "base_url":
		fmt.Fprintln(stdout, cfg.BaseURL)
	case "access_token":
		fmt.Fprintln(stdout, cfg.AccessToken)
	case "api_key":
		fmt.Fprintln(stdout, cfg.APIKey)
	default:
		fmt.Fprintf(stderr, "weave config get: unknown key %q (valid: %s)\n", key, strings.Join(configKeys, ", "))
		return 2
	}
	return 0
}

func configSet(args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		fmt.Fprintln(stderr, "usage: weave config set <key> <value>")
		return 2
	}
	key, val := args[0], args[1]
	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return 1
	}
	switch key {
	case "base_url":
		cfg.BaseURL = val
	case "access_token":
		cfg.AccessToken = val
	case "api_key":
		cfg.APIKey = val
	default:
		fmt.Fprintf(stderr, "weave config set: unknown key %q (valid: %s)\n", key, strings.Join(configKeys, ", "))
		return 2
	}
	if err := SaveConfig(cfg); err != nil {
		fmt.Fprintf(stderr, "save config: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "set %s\n", key)
	return 0
}
