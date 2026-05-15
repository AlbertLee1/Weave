package main_test

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// composeFile is the project-root docker-compose.yml relative to cmd/server.
const composeFile = "../../docker-compose.yml"

// composeShape is the shared shape we unmarshal docker-compose.yml into. Kept
// in sync with the keys the test suite inspects — extend additively when new
// services / fields get checked so the helpers below stay reusable.
type composeShape struct {
	Services map[string]composeService `yaml:"services"`
	Volumes  map[string]interface{}    `yaml:"volumes"`
}

type composeService struct {
	Build       interface{}       `yaml:"build"`
	Image       string            `yaml:"image"`
	Ports       []string          `yaml:"ports"`
	Command     interface{}       `yaml:"command"`
	DependsOn   interface{}       `yaml:"depends_on"`
	Profiles    []string          `yaml:"profiles"`
	Environment interface{}       `yaml:"environment"`
	Volumes     []string          `yaml:"volumes"`
	Healthcheck composeHealthcheck `yaml:"healthcheck"`
}

type composeHealthcheck struct {
	Test     interface{} `yaml:"test"`
	Interval string      `yaml:"interval"`
	Timeout  string      `yaml:"timeout"`
	Retries  int         `yaml:"retries"`
}

func loadCompose(t *testing.T) composeShape {
	t.Helper()
	data, err := os.ReadFile(composeFile)
	if err != nil {
		t.Fatalf("failed to read docker-compose.yml: %v", err)
	}
	var c composeShape
	if err := yaml.Unmarshal(data, &c); err != nil {
		t.Fatalf("failed to parse docker-compose.yml: %v", err)
	}
	return c
}

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

// TestDockerCompose_VTX124_TimescaleDBService verifies that docker-compose.yml
// ships a `timescaledb` service ready for the VTX-028+ time series path. The
// image must match internal/testutil.StartTimescaleDBContainer so local dev
// and `go test -tags integration` see the same hypertable surface, and the
// service must expose a healthcheck so `docker compose up --wait` blocks
// until the database is actually serving connections.
func TestDockerCompose_VTX124_TimescaleDBService(t *testing.T) {
	c := loadCompose(t)

	ts, ok := c.Services["timescaledb"]
	if !ok {
		t.Fatal("docker-compose.yml must declare a 'timescaledb' service for VTX-028+ time series tests")
	}

	if !strings.Contains(ts.Image, "timescale/timescaledb") {
		t.Errorf("timescaledb.image must be timescale/timescaledb:* (got %q)", ts.Image)
	}
	if !strings.Contains(ts.Image, "pg16") {
		t.Errorf("timescaledb.image must pin to a -pg16 tag to match StartTimescaleDBContainer (got %q)", ts.Image)
	}

	// Postgres-compatible service — must not collide with the main postgres
	// service on host port 5432. We map host 5433 → container 5432 so both
	// can run side-by-side.
	foundHostPort := false
	for _, p := range ts.Ports {
		if strings.HasPrefix(p, "5433:") {
			foundHostPort = true
			break
		}
	}
	if !foundHostPort {
		t.Errorf("timescaledb must publish container :5432 on host :5433 to avoid colliding with postgres (got ports=%v)", ts.Ports)
	}

	if ts.Healthcheck.Test == nil {
		t.Error("timescaledb must declare a healthcheck so `docker compose up --wait` can block on readiness")
	}
}

// TestDockerCompose_VTX124_FunctionRuntimeService verifies that
// docker-compose.yml ships a `function-runtime` service so the Tier 3.2
// function-backed action dispatcher (US-215 / pkg/actions/http_dispatcher)
// has a target without operators having to run a separate binary. The
// service listens on container :9000 and is published on host :9000 to match
// the FunctionsConfig.BaseURL default ("http://localhost:9000/functions").
func TestDockerCompose_VTX124_FunctionRuntimeService(t *testing.T) {
	c := loadCompose(t)

	fr, ok := c.Services["function-runtime"]
	if !ok {
		t.Fatal("docker-compose.yml must declare a 'function-runtime' service for the Tier 3.2 dispatcher")
	}

	// Must build from the in-repo Dockerfile.function-runtime (or use a
	// `build:` target) — there is no upstream image we trust for this.
	if fr.Build == nil {
		t.Error("function-runtime must use a `build:` block, not a third-party image")
	}

	foundHostPort := false
	for _, p := range fr.Ports {
		if strings.HasPrefix(p, "9000:") {
			foundHostPort = true
			break
		}
	}
	if !foundHostPort {
		t.Errorf("function-runtime must publish container :9000 on host :9000 to match WEAVE_FUNCTIONS_BASE_URL default (got ports=%v)", fr.Ports)
	}

	if fr.Healthcheck.Test == nil {
		t.Error("function-runtime must declare a healthcheck so the weave service can depends_on it")
	}
}

// TestDockerCompose_VTX124_WeaveStackDependencies verifies the weave service
// boots with the full Vertex stack wired in: it depends on function-runtime
// (so action execution works out of the box) and exports WEAVE_FUNCTIONS_*
// envs pointing at the in-stack container. timescaledb stays opt-in (the
// vertex timeseries surface is opt-in via WEAVE_TS_BACKEND), but the service
// must exist so `docker compose up timescaledb` works.
func TestDockerCompose_VTX124_WeaveStackDependencies(t *testing.T) {
	c := loadCompose(t)
	weave, ok := c.Services["weave"]
	if !ok {
		t.Fatal("weave service must exist")
	}

	deps, ok := weave.DependsOn.(map[string]interface{})
	if !ok {
		t.Fatalf("weave.depends_on must be a map (got %T)", weave.DependsOn)
	}
	if _, ok := deps["function-runtime"]; !ok {
		t.Error("weave service must depends_on function-runtime so action dispatch is wired on a single `docker compose up`")
	}

	envMap, ok := weave.Environment.(map[string]interface{})
	if !ok {
		t.Fatalf("weave.environment must be a map (got %T)", weave.Environment)
	}
	baseURL, _ := envMap["WEAVE_FUNCTIONS_BASE_URL"].(string)
	if baseURL == "" {
		t.Error("weave service must set WEAVE_FUNCTIONS_BASE_URL so the Tier 3.2 dispatcher routes to the in-stack runtime")
	} else if !strings.Contains(baseURL, "function-runtime") {
		t.Errorf("WEAVE_FUNCTIONS_BASE_URL must point at the in-stack function-runtime service (got %q)", baseURL)
	}
}

// TestDevScript_VTX124_MentionsFullStack verifies scripts/dev.sh advertises
// the full single-machine stack in its banner so `make dev` users see PG,
// NATS, TimescaleDB, and the function runtime at a glance. The banner is the
// canonical handoff surface for new contributors — keep it in lockstep with
// the docker-compose services. The Vertex frontend is served by the same
// Vite dev server (the dev script always boots Vite), so the banner must
// also call out the Vertex UI surface explicitly.
func TestDevScript_VTX124_MentionsFullStack(t *testing.T) {
	data, err := os.ReadFile("../../scripts/dev.sh")
	if err != nil {
		t.Fatalf("scripts/dev.sh must exist: %v", err)
	}
	body := string(data)

	for _, want := range []string{"TimescaleDB", "Function"} {
		if !strings.Contains(body, want) {
			t.Errorf("scripts/dev.sh must mention %q in the banner (full-stack handoff)", want)
		}
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
