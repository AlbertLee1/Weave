package ci_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const minGolangCILintForGo125 = "v2.4.0"

type workflowConfig struct {
	Jobs map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	Steps []workflowStep `yaml:"steps"`
}

type workflowStep struct {
	Name string         `yaml:"name"`
	Uses string         `yaml:"uses"`
	Run  string         `yaml:"run"`
	With map[string]any `yaml:"with"`
}

func TestBDD_GolangCILintWorkflowSupportsGo125(t *testing.T) {
	root := repoRoot(t)
	goMajor, goMinor := readGoVersion(t, filepath.Join(root, "go.mod"))
	lintVersion, onlyNewIssues := readGolangCILintActionConfig(t, filepath.Join(root, ".github", "workflows", "ci.yml"))

	if lintVersion == "latest" {
		t.Fatalf("golangci-lint CI version must be pinned to an explicit release, got %q", lintVersion)
	}
	if !onlyNewIssues {
		t.Fatal("golangci-lint CI must run with only-new-issues: true while the repository carries historical lint findings")
	}

	if goMajor > 1 || (goMajor == 1 && goMinor >= 25) {
		if compareVersions(lintVersion, minGolangCILintForGo125) < 0 {
			t.Fatalf(
				"go.mod targets Go %d.%d but CI pins golangci-lint %s; Go 1.25 targets require golangci-lint >= %s",
				goMajor,
				goMinor,
				lintVersion,
				minGolangCILintForGo125,
			)
		}
	}
}

func TestBDD_GovulncheckGateUsesFixedToolchainAndActionableScope(t *testing.T) {
	root := repoRoot(t)

	if got, want := readGoPatchVersion(t, filepath.Join(root, "go.mod")), [3]int{1, 26, 3}; compareVersionParts(got, want) < 0 {
		t.Errorf("go.mod must pin Go at or above %s for current standard-library govulncheck fixes, got %s", formatVersionParts(want), formatVersionParts(got))
	}

	if run := readGovulncheckWorkflowCommand(t, filepath.Join(root, ".github", "workflows", "ci.yml")); strings.TrimSpace(run) != "make vulncheck" {
		t.Errorf("CI govulncheck step must use local make vulncheck contract, got %q", run)
	}

	makefile := readFile(t, filepath.Join(root, "Makefile"))
	if !strings.Contains(makefile, "./scripts/ci/govulncheck.sh") {
		t.Error("Makefile vulncheck target must delegate to scripts/ci/govulncheck.sh so local and CI scans share one scope")
	}

	script := readFile(t, filepath.Join(root, "scripts", "ci", "govulncheck.sh"))
	for _, required := range []string{"./scripts/ci/go-packages.sh", "internal/testutil", "go env GOPATH", "\"$govulncheck_bin\" $packages"} {
		if !strings.Contains(script, required) {
			t.Errorf("scripts/ci/govulncheck.sh must contain %q", required)
		}
	}
}

func TestBDD_GoPackageListExcludesWebDependencyTrees(t *testing.T) {
	root := repoRoot(t)

	fixtureRoot := filepath.Join(root, "web", "node_modules", "ralph_go_package_fixture")
	fixturePkg := filepath.Join(fixtureRoot, "golang", "pkg", "foreign")
	if err := os.MkdirAll(fixturePkg, 0o755); err != nil {
		t.Fatalf("create node_modules Go fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(fixtureRoot); err != nil {
			t.Fatalf("remove node_modules Go fixture: %v", err)
		}
	})
	if err := os.WriteFile(filepath.Join(fixturePkg, "foreign.go"), []byte("package foreign\n"), 0o644); err != nil {
		t.Fatalf("write node_modules Go fixture: %v", err)
	}

	helper := filepath.Join(root, "scripts", "ci", "go-packages.sh")
	helperSource := readFile(t, helper)
	for _, required := range []string{"go list -e ./...", "node_modules"} {
		if !strings.Contains(helperSource, required) {
			t.Errorf("scripts/ci/go-packages.sh must contain %q", required)
		}
	}

	cmd := exec.Command(helper)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run Go package-list helper: %v\n%s", err, output)
	}
	packages := string(output)
	if strings.Contains(packages, "/web/node_modules/") || strings.Contains(packages, "ralph_go_package_fixture") {
		t.Fatalf("Go package-list helper must exclude web dependency trees, got:\n%s", packages)
	}
	if !strings.Contains(packages, "github.com/liyang/weave/cmd/server") {
		t.Fatalf("Go package-list helper must still include repository packages, got:\n%s", packages)
	}

	buildCmd := exec.Command(helper, "--build")
	buildCmd.Dir = root
	buildOutput, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run buildable Go package-list helper: %v\n%s", err, buildOutput)
	}
	buildPackages := string(buildOutput)
	if strings.Contains(buildPackages, "/web/node_modules/") || strings.Contains(buildPackages, "ralph_go_package_fixture") {
		t.Fatalf("buildable Go package-list helper must exclude web dependency trees, got:\n%s", buildPackages)
	}
	if strings.Contains(buildPackages, "github.com/liyang/weave/scripts/ci") {
		t.Fatalf("buildable Go package-list helper must exclude test-only packages, got:\n%s", buildPackages)
	}
	if !strings.Contains(buildPackages, "github.com/liyang/weave/cmd/server") {
		t.Fatalf("buildable Go package-list helper must still include buildable repository packages, got:\n%s", buildPackages)
	}

	makefile := readFile(t, filepath.Join(root, "Makefile"))
	if !strings.Contains(makefile, "./scripts/ci/go-packages.sh") {
		t.Error("Makefile Go gates must use scripts/ci/go-packages.sh so local package discovery excludes web dependencies")
	}

	workflow := readFile(t, filepath.Join(root, ".github", "workflows", "ci.yml"))
	if strings.Count(workflow, "./scripts/ci/go-packages.sh") < 3 {
		t.Error("CI build, vet, and test gates must use scripts/ci/go-packages.sh so fresh CI and local gates share package discovery")
	}
	if !strings.Contains(workflow, "./scripts/ci/go-packages.sh --build") {
		t.Error("CI build gate must use scripts/ci/go-packages.sh --build so explicit package arguments do not include test-only packages")
	}
}

func TestBDD_MCPDocsReflectLivePromptsResourcesBridge(t *testing.T) {
	root := repoRoot(t)
	docs := readFile(t, filepath.Join(root, "docs", "mcp.md"))

	staleClaims := []string{
		"`cmd/weave-mcp` binary (stub)",
		"The stdio binary is a stub",
		"prompts/list` | Returns an empty list",
		"`prompts/list` always returns an empty array",
		"Weave does not yet expose prompts",
	}
	for _, claim := range staleClaims {
		if strings.Contains(docs, claim) {
			t.Errorf("docs/mcp.md still contains stale MCP claim %q", claim)
		}
	}

	for _, required := range []string{
		"`cmd/weave-mcp` binary (stdio HTTP bridge)",
		"`WEAVE_MCP_URL`",
		"`WEAVE_MCP_TOKEN`",
		"`WEAVE_MCP_API_KEY`",
		"`prompts/list` | List prompts synthesized from OMS ActionType metadata",
		"`prompts/get` | Render one ActionType prompt with supplied arguments",
		"`weave://objecttype/<ontology>/<objectType>`",
		"JSON bundle of `objectType` + `properties` + `outgoingLinkTypes`",
		"prompts",
		"resources",
	} {
		if !strings.Contains(docs, required) {
			t.Errorf("docs/mcp.md must describe live MCP contract fragment %q", required)
		}
	}
}

func TestBDD_GeoTemporalDocsReflectPGStoreDefault(t *testing.T) {
	root := repoRoot(t)
	statusDoc := readFile(t, filepath.Join(root, "docs", "PRD-Weave-OSv2-深度复刻-V2.md"))
	storeDoc := readFile(t, filepath.Join(root, "pkg", "geotemporal", "store.go"))
	memoryStoreDoc := readFile(t, filepath.Join(root, "pkg", "geotemporal", "memory_store.go"))
	serverMain := readFile(t, filepath.Join(root, "cmd", "server", "main.go"))
	pgStore := readFile(t, filepath.Join(root, "pkg", "geotemporal", "pg_store.go"))
	migration205 := readFile(t, filepath.Join(root, "migrations", "000205_geotemporal_values.up.sql"))
	migration208 := readFile(t, filepath.Join(root, "migrations", "000208_geotemporal_spatial_indexes.up.sql"))

	staleClaims := []struct {
		name string
		text string
	}{
		{"statusDoc", statusDoc},
		{"storeDoc", storeDoc},
		{"memoryStoreDoc", memoryStoreDoc},
	}
	for _, source := range staleClaims {
		for _, claim := range []string{
			"Only an in-memory implementation ships today",
			"persistent backends (PostGIS,\n// JSONB) are deferred",
			"**仅内存 store**",
			"🔴 **内存**",
			"**GeoTemporal 仅 memory_store.go**",
			"`pkg/geotemporal` 目录 3 文件，全是内存",
			"**不持久化**，进程重启丢失",
			"GeoTemporal PG store + 可选 PostGIS",
			"| GeoTemporal | `pkg/geotemporal/memory_store.go` |",
		} {
			if strings.Contains(source.text, claim) {
				t.Errorf("%s still contains stale GeoTemporal claim %q", source.name, claim)
			}
		}
	}

	for _, required := range []string{
		"`pkg/geotemporal/pg_store.go`",
		"`geotemporal_values`",
		"`migrations/000205_geotemporal_values.up.sql`",
		"`migrations/000208_geotemporal_spatial_indexes.up.sql`",
		"`SpatialTemporalQuerier`",
		"`QueryBBoxRange`",
		"`cmd/server/main.go`",
		"PG-backed `PgStore`",
		"in-process MemoryStore as degraded mode",
	} {
		if !strings.Contains(statusDoc, required) {
			t.Errorf("GeoTemporal status doc must describe live PG contract fragment %q", required)
		}
	}

	for _, required := range []string{
		"PostgreSQL-backed PgStore",
		"geotemporal_values",
		"000205_geotemporal_values.up.sql",
		"000208_geotemporal_spatial_indexes.up.sql",
		"SpatialTemporalQuerier",
	} {
		if !strings.Contains(storeDoc, required) {
			t.Errorf("pkg/geotemporal package comment must describe %q", required)
		}
	}
	if !strings.Contains(memoryStoreDoc, "degraded-mode") {
		t.Error("MemoryStore comment must describe memory as degraded-mode fallback, not the durable default")
	}
	if !strings.Contains(serverMain, "geotemporal.NewPgStore") {
		t.Error("cmd/server must wire geotemporal.NewPgStore when PG is available")
	}
	for _, required := range []string{
		"func (s *PgStore) QueryBBoxRange",
		"var _ SpatialTemporalQuerier = (*PgStore)(nil)",
	} {
		if !strings.Contains(pgStore, required) {
			t.Errorf("PgStore must expose the spatial-temporal query capability fragment %q", required)
		}
	}
	for _, required := range []string{
		"CREATE TABLE IF NOT EXISTS geotemporal_values",
		"idx_geotemporal_values_series_ts",
	} {
		if !strings.Contains(migration205, required) {
			t.Errorf("migration 000205 must contain %q", required)
		}
	}
	for _, required := range []string{
		"idx_geotemporal_values_lng",
		"idx_geotemporal_values_lat",
		"postgis",
	} {
		if !strings.Contains(migration208, required) {
			t.Errorf("migration 000208 must contain %q", required)
		}
	}
}

func TestBDD_RealtimeSubscriptionStatusDocReflectsLiveSurfaces(t *testing.T) {
	root := repoRoot(t)
	statusDoc := readFile(t, filepath.Join(root, "docs", "PRD-Weave-OSv2-深度复刻-V2.md"))
	serverMain := readFile(t, filepath.Join(root, "cmd", "server", "main.go"))
	sseHandler := readFile(t, filepath.Join(root, "pkg", "oss", "subscribe_sse.go"))
	broadcastHub := readFile(t, filepath.Join(root, "pkg", "funnel", "broadcast.go"))
	browserPage := readFile(t, filepath.Join(root, "web", "src", "components", "browser", "BrowserPage.tsx"))
	objectSetLivePage := readFile(t, filepath.Join(root, "web", "src", "components", "objectsets", "ObjectSetLivePage.tsx"))
	subscriptionHook := readFile(t, filepath.Join(root, "web", "src", "hooks", "useObjectSetSubscription.ts"))

	for _, claim := range []string{
		"**无客户端订阅端点**",
		"**无 /stream / WebSocket / SSE 端点**",
		"无 subscribe UI；依赖后端 subscribe 端点缺失",
		"**广播不暴露给客户端**",
		"Funnel 内部有 broadcaster，没有 HTTP 暴露",
	} {
		if strings.Contains(statusDoc, claim) {
			t.Errorf("OSv2 status doc still contains stale realtime subscription claim %q", claim)
		}
	}

	for _, required := range []string{
		"`/api/v2/ontologies/{ontologyApiName}/subscriptions/ws`",
		"`/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe`",
		"`pkg/funnel/broadcast.go`",
		"`pkg/oss/subscribe_sse.go`",
		"`pkg/subscriptions`",
		"`web/src/hooks/useObjectSetSubscription.ts`",
		"`web/src/components/browser/BrowserPage.tsx`",
		"`web/src/components/objectsets/ObjectSetLivePage.tsx`",
		"`Last-Event-ID`",
		"`since` query parameter",
		"per-user connection guard",
	} {
		if !strings.Contains(statusDoc, required) {
			t.Errorf("OSv2 status doc must describe live realtime subscription contract fragment %q", required)
		}
	}

	for _, required := range []string{
		`/api/v2/ontologies/{ontologyApiName}/subscriptions/ws`,
		`/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe`,
		"subscriptions.NewHandler",
		"oss.NewSubscribeSSEHandler",
	} {
		if !strings.Contains(serverMain, required) {
			t.Errorf("cmd/server must wire live subscription fragment %q", required)
		}
	}
	for _, required := range []string{"Last-Event-ID", "since", "maxPerUser"} {
		if !strings.Contains(sseHandler, required) {
			t.Errorf("SSE subscribe handler must expose %q", required)
		}
	}
	for _, required := range []string{"SubscribeWithReplay", "Publish"} {
		if !strings.Contains(broadcastHub, required) {
			t.Errorf("funnel broadcast hub must expose %q", required)
		}
	}
	for _, source := range []struct {
		name string
		text string
	}{
		{"BrowserPage", browserPage},
		{"ObjectSetLivePage", objectSetLivePage},
		{"useObjectSetSubscription", subscriptionHook},
	} {
		if !strings.Contains(source.text, "useObjectSetSubscription") && source.name != "useObjectSetSubscription" {
			t.Errorf("%s must use the ObjectSet subscription hook", source.name)
		}
	}
	if !strings.Contains(subscriptionHook, "EventSource") {
		t.Error("useObjectSetSubscription must use EventSource for the SSE client path")
	}
}

func TestBDD_CLIStatusDocReflectsActionAggregateObjectSetCommands(t *testing.T) {
	root := repoRoot(t)
	statusDoc := readFile(t, filepath.Join(root, "docs", "PRD-Weave-OSv2-深度复刻-V2.md"))
	mainCLI := readFile(t, filepath.Join(root, "cmd", "weave-cli", "main.go"))
	actionCmd := readFile(t, filepath.Join(root, "cmd", "weave-cli", "cmd_action.go"))
	aggregateCmd := readFile(t, filepath.Join(root, "cmd", "weave-cli", "cmd_aggregate.go"))
	objectSetCmd := readFile(t, filepath.Join(root, "cmd", "weave-cli", "cmd_objectset.go"))
	cliTests := readFile(t, filepath.Join(root, "cmd", "weave-cli", "cli_us304_test.go"))

	for _, claim := range []string{
		"| CLI | action / aggregate / objectset | 🟡 | n/a | 🔴 | **30%** | 未暴露 |",
		"**Gap-D3 — CLI action / aggregate 子命令**\n- 现状：未暴露。",
		"`weave objectset run`",
		"CLI `action apply` / `aggregate` / `objectset run`",
	} {
		if strings.Contains(statusDoc, claim) {
			t.Errorf("OSv2 status doc still contains stale CLI claim %q", claim)
		}
	}

	for _, required := range []string{
		"`cmd/weave-cli/cmd_action.go`",
		"`cmd/weave-cli/cmd_aggregate.go`",
		"`cmd/weave-cli/cmd_objectset.go`",
		"`cmd/weave-cli/cli_us304_test.go`",
		"`weave action apply`",
		"`weave aggregate`",
		"`weave objectset load`",
		"`weave objectset create-temporary`",
		"remaining depth gaps",
	} {
		if !strings.Contains(statusDoc, required) {
			t.Errorf("OSv2 status doc must describe live CLI contract fragment %q", required)
		}
	}

	for _, required := range []string{`case "action":`, `case "aggregate":`, `case "objectset":`} {
		if !strings.Contains(mainCLI, required) {
			t.Errorf("cmd/weave-cli/main.go must dispatch %q", required)
		}
	}
	for _, required := range []string{"func runAction", `case "apply":`, "ApplyAction"} {
		if !strings.Contains(actionCmd, required) {
			t.Errorf("cmd_action.go must expose action apply fragment %q", required)
		}
	}
	for _, required := range []string{"func runAggregate", "AggregateObjects"} {
		if !strings.Contains(aggregateCmd, required) {
			t.Errorf("cmd_aggregate.go must expose aggregate fragment %q", required)
		}
	}
	for _, required := range []string{"func runObjectSet", `case "load":`, `"create-temporary"`, "CreateTemporaryObjectSetRaw"} {
		if !strings.Contains(objectSetCmd, required) {
			t.Errorf("cmd_objectset.go must expose objectset fragment %q", required)
		}
	}
	for _, required := range []string{
		"TestDispatch_KnownTopLevelCommands_US304",
		"TestRootUsage_ListsNewCommands_US304",
		"TestActionApply_GivenParamsKVAndReturnEdits_When_Apply_Then_RequestBodyMatchesAndOutputContainsValid_US304",
		"TestAggregate_GivenBodyFileAndTableOutput_When_Aggregate_Then_RequestForwardsBodyAndTableRendered_US304",
		"TestObjectSet_CreateTemporary_GivenBodyFile_When_Run_Then_ReturnsRid_US304",
		"TestObjectSet_Load_GivenBodyFile_When_Run_Then_ForwardsAndReturnsData_US304",
	} {
		if !strings.Contains(cliTests, required) {
			t.Errorf("cmd/weave-cli CLI contract tests must include %q", required)
		}
	}
}

func TestBDD_MCPStatusDocReflectsLivePromptsResourcesBridge(t *testing.T) {
	root := repoRoot(t)
	statusDoc := readFile(t, filepath.Join(root, "docs", "PRD-Weave-OSv2-深度复刻-V2.md"))
	mcpDocs := readFile(t, filepath.Join(root, "docs", "mcp.md"))
	mcpServer := readFile(t, filepath.Join(root, "pkg", "mcp", "server.go"))
	prompts := readFile(t, filepath.Join(root, "pkg", "mcp", "prompts.go"))
	resources := readFile(t, filepath.Join(root, "pkg", "mcp", "resources.go"))
	bridgeMain := readFile(t, filepath.Join(root, "cmd", "weave-mcp", "main.go"))
	httpBridge := readFile(t, filepath.Join(root, "cmd", "weave-mcp", "http_bridge.go"))

	for _, claim := range []string{
		"prompts/resources 为 stub",
		"`weave-mcp` 是存根",
		"**Gap-D4 — MCP prompts / resources / sampling**\n- 现状：stub。",
		"**Gap-D5 — weave-mcp stdio 真可用**\n- 现状：stub，不接 DB。",
	} {
		if strings.Contains(statusDoc, claim) {
			t.Errorf("OSv2 status doc still contains stale MCP claim %q", claim)
		}
	}

	for _, required := range []string{
		"`docs/mcp.md`",
		"`pkg/mcp/prompts.go`",
		"`pkg/mcp/resources.go`",
		"`cmd/weave-mcp/http_bridge.go`",
		"`WEAVE_MCP_URL`",
		"`prompts/list`",
		"`prompts/get`",
		"`resources/list`",
		"`resources/read`",
		"`weave://objecttype/<ontology>/<objectType>`",
		"stdio HTTP bridge",
		"remaining local-standalone gap",
	} {
		if !strings.Contains(statusDoc, required) {
			t.Errorf("OSv2 status doc must describe live MCP contract fragment %q", required)
		}
	}

	for _, required := range []string{
		"`cmd/weave-mcp` binary (stdio HTTP bridge)",
		"`WEAVE_MCP_URL`",
		"`prompts/list` | List prompts synthesized from OMS ActionType metadata",
		"`prompts/get` | Render one ActionType prompt with supplied arguments",
		"`resources/list` | List ontologies, ObjectTypes, and temporary ObjectSets as MCP resources",
		"`resources/read` | Return the schema for an ontology, ObjectType, or stored ObjectSet definition",
		"`weave://objecttype/<ontology>/<objectType>`",
	} {
		if !strings.Contains(mcpDocs, required) {
			t.Errorf("docs/mcp.md must describe live MCP contract fragment %q", required)
		}
	}

	for _, required := range []string{
		`case "prompts/list":`,
		`case "prompts/get":`,
		`case "resources/list":`,
		`case "resources/read":`,
		`"resources": map[string]any{"listChanged": false, "subscribe": false}`,
		`"prompts":   map[string]any{"listChanged": false}`,
	} {
		if !strings.Contains(mcpServer, required) {
			t.Errorf("pkg/mcp/server.go must dispatch or advertise MCP fragment %q", required)
		}
	}
	for _, required := range []string{"handlePromptsList", "handlePromptsGet", "ListActionTypes", "GetActionTypeByAPIName"} {
		if !strings.Contains(prompts, required) {
			t.Errorf("pkg/mcp/prompts.go must expose prompts fragment %q", required)
		}
	}
	for _, required := range []string{"handleResourcesList", "handleResourcesRead", "ListOntologies", "ListObjectTypes", "weave://objecttype"} {
		if !strings.Contains(resources, required) {
			t.Errorf("pkg/mcp/resources.go must expose resources fragment %q", required)
		}
	}
	for _, required := range []string{"WEAVE_MCP_URL", "RunHTTPBridge", "bridgeAuthOptionsFromEnv"} {
		if !strings.Contains(bridgeMain, required) {
			t.Errorf("cmd/weave-mcp/main.go must wire bridge fragment %q", required)
		}
	}
	for _, required := range []string{"RunHTTPBridge", `http.MethodPost`, "Authorization", "X-Weave-API-Key"} {
		if !strings.Contains(httpBridge, required) {
			t.Errorf("cmd/weave-mcp/http_bridge.go must expose bridge fragment %q", required)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func readGoVersion(t *testing.T, path string) (int, int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	m := regexp.MustCompile(`(?m)^go\s+([0-9]+)\.([0-9]+)(?:\.[0-9]+)?\s*$`).FindStringSubmatch(string(data))
	if m == nil {
		t.Fatal("go.mod does not declare a parseable Go version")
	}
	major, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("parse Go major version: %v", err)
	}
	minor, err := strconv.Atoi(m[2])
	if err != nil {
		t.Fatalf("parse Go minor version: %v", err)
	}
	return major, minor
}

func readGoPatchVersion(t *testing.T, path string) [3]int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	m := regexp.MustCompile(`(?m)^go\s+([0-9]+)\.([0-9]+)\.([0-9]+)\s*$`).FindStringSubmatch(string(data))
	if m == nil {
		t.Fatal("go.mod must declare a full major.minor.patch Go version")
	}
	var version [3]int
	for i := range version {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			t.Fatalf("parse Go version component %q: %v", m[i+1], err)
		}
		version[i] = n
	}
	return version
}

func readGolangCILintActionConfig(t *testing.T, path string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	var workflow workflowConfig
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("parse CI workflow YAML: %v", err)
	}
	for jobName, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if strings.HasPrefix(step.Uses, "golangci/golangci-lint-action@") {
				version, ok := step.With["version"].(string)
				if !ok || strings.TrimSpace(version) == "" {
					t.Fatalf("job %q golangci-lint action step %q must set with.version", jobName, step.Name)
				}
				return strings.TrimSpace(version), readBoolInput(step.With["only-new-issues"])
			}
		}
	}
	t.Fatal("CI workflow does not contain a golangci/golangci-lint-action step")
	return "", false
}

func readGovulncheckWorkflowCommand(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	var workflow workflowConfig
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("parse CI workflow YAML: %v", err)
	}
	for jobName, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if strings.EqualFold(strings.TrimSpace(step.Name), "govulncheck") {
				if strings.TrimSpace(step.Run) == "" {
					t.Fatalf("job %q govulncheck step must use a run command", jobName)
				}
				return strings.TrimSpace(step.Run)
			}
		}
	}
	t.Fatal("CI workflow does not contain a govulncheck step")
	return ""
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func readBoolInput(v any) bool {
	switch typed := v.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func compareVersions(left, right string) int {
	l := mustParseVersion(left)
	r := mustParseVersion(right)
	return compareVersionParts(l, r)
}

func compareVersionParts(l, r [3]int) int {
	for i := range l {
		if l[i] < r[i] {
			return -1
		}
		if l[i] > r[i] {
			return 1
		}
	}
	return 0
}

func formatVersionParts(v [3]int) string {
	return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
}

func mustParseVersion(v string) [3]int {
	trimmed := strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(trimmed, ".")
	if len(parts) != 3 {
		panic(fmt.Sprintf("version %q must have major.minor.patch format", v))
	}
	var parsed [3]int
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			panic(fmt.Sprintf("parse version %q: %v", v, err))
		}
		parsed[i] = n
	}
	return parsed
}
