package main_test

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestDockerComposeWeaveService verifies that docker-compose.yml has a properly
// configured weave service with health check, dependencies, and correct env.
func TestDockerComposeWeaveService(t *testing.T) {
	data, err := os.ReadFile("../../docker-compose.yml")
	if err != nil {
		t.Fatalf("failed to read docker-compose.yml: %v", err)
	}

	var compose struct {
		Services map[string]struct {
			Build      interface{} `yaml:"build"`
			Image      string      `yaml:"image"`
			Ports      []string    `yaml:"ports"`
			DependsOn  interface{} `yaml:"depends_on"`
			Healthcheck struct {
				Test     interface{} `yaml:"test"`
				Interval string      `yaml:"interval"`
				Timeout  string      `yaml:"timeout"`
				Retries  int         `yaml:"retries"`
			} `yaml:"healthcheck"`
			Environment interface{} `yaml:"environment"`
		} `yaml:"services"`
	}

	if err := yaml.Unmarshal(data, &compose); err != nil {
		t.Fatalf("failed to parse docker-compose.yml: %v", err)
	}

	// Weave service must exist
	weave, ok := compose.Services["weave"]
	if !ok {
		t.Fatal("docker-compose.yml must have a 'weave' service")
	}

	// Must have a build context (not just an image)
	if weave.Build == nil {
		t.Error("weave service must have a 'build' configuration")
	}

	// Must expose port 9117
	found9117 := false
	for _, p := range weave.Ports {
		if strings.Contains(p, "9117") {
			found9117 = true
			break
		}
	}
	if !found9117 {
		t.Error("weave service must expose port 9117")
	}

	// Must depend on postgres and nats
	if weave.DependsOn == nil {
		t.Fatal("weave service must depend on postgres and nats")
	}

	// Must have a health check
	if weave.Healthcheck.Test == nil {
		t.Error("weave service must have a healthcheck")
	}

	// Postgres and nats services must still exist
	if _, ok := compose.Services["postgres"]; !ok {
		t.Error("docker-compose.yml must retain 'postgres' service")
	}
	if _, ok := compose.Services["nats"]; !ok {
		t.Error("docker-compose.yml must retain 'nats' service")
	}
}

// TestDockerfileExists verifies that a multi-stage Dockerfile exists with
// the correct builder and runtime stages.
func TestDockerfileExists(t *testing.T) {
	data, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatalf("Dockerfile must exist: %v", err)
	}

	content := string(data)

	// Must be multi-stage with golang builder
	if !strings.Contains(content, "FROM golang:") {
		t.Error("Dockerfile must have a golang builder stage")
	}
	if !strings.Contains(content, "AS builder") {
		t.Error("Dockerfile must name the builder stage 'AS builder'")
	}

	// Must have alpine runtime stage
	if !strings.Contains(content, "FROM alpine") {
		t.Error("Dockerfile must use alpine as the runtime base")
	}

	// Must copy the binary
	if !strings.Contains(content, "COPY --from=builder") {
		t.Error("Dockerfile must copy the binary from the builder stage")
	}

	// Must copy migrations
	if !strings.Contains(content, "migrations") {
		t.Error("Dockerfile must include migrations directory")
	}

	// Must expose port 9117
	if !strings.Contains(content, "EXPOSE 9117") {
		t.Error("Dockerfile must expose port 9117")
	}
}
