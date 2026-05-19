package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/attachment"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/cipher"
	"github.com/liyang/weave/pkg/developer"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/geotemporal"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/aggregation"
	"github.com/liyang/weave/pkg/oss/objectset"
	"github.com/liyang/weave/pkg/subscriptions"
	"github.com/liyang/weave/pkg/timeseries"
	"github.com/liyang/weave/pkg/transactions"
	"gopkg.in/yaml.v3"
)

// canonicalSpecPath returns the on-disk path to the canonical OpenAPI spec
// file used by humans, the build, and the contract tests. The path is
// computed relative to this test file so the test runs correctly regardless
// of the caller's working directory.
func canonicalSpecPath(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// cmd/server -> repo root -> api/openapi.yaml
	return filepath.Join(wd, "..", "..", "api", "openapi.yaml")
}

// loadCanonicalSpec parses the on-disk YAML spec into a generic map. We use
// a generic map (rather than kin-openapi or similar) so the test has zero
// runtime dependencies beyond gopkg.in/yaml.v3.
func loadCanonicalSpec(t *testing.T) map[string]any {
	t.Helper()
	path := canonicalSpecPath(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return doc
}

// specOperationKey is "METHOD path" for direct comparison with chi routes.
type specOperationKey struct {
	Method string
	Path   string
}

// extractSpecOperations walks the parsed YAML and returns one specOperationKey
// per (method, path) operation declared under `paths`.
func extractSpecOperations(t *testing.T, doc map[string]any) map[specOperationKey]bool {
	t.Helper()
	out := map[specOperationKey]bool{}
	pathsRaw, ok := doc["paths"]
	if !ok {
		return out
	}
	paths, ok := pathsRaw.(map[string]any)
	if !ok {
		t.Fatalf("paths: expected map, got %T", pathsRaw)
	}
	verbs := map[string]string{
		"get":     "GET",
		"post":    "POST",
		"put":     "PUT",
		"delete":  "DELETE",
		"patch":   "PATCH",
		"head":    "HEAD",
		"options": "OPTIONS",
	}
	for path, item := range paths {
		op, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for verb, methodName := range verbs {
			if _, hasVerb := op[verb]; hasVerb {
				out[specOperationKey{Method: methodName, Path: path}] = true
			}
		}
	}
	return out
}

// extractChiRoutes walks the chi router tree and returns the set of
// (method, path) pairs registered on it. chi templates use the same
// {param} syntax as OpenAPI, so paths can be compared directly.
func extractChiRoutes(t *testing.T, r *chi.Mux) map[specOperationKey]bool {
	t.Helper()
	out := map[specOperationKey]bool{}
	walkErr := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		// Drop the trailing "/*" chi appends to wildcard routes; we never
		// document those because they are catch-alls for the SPA fallback.
		clean := strings.TrimSuffix(route, "/*")
		out[specOperationKey{Method: method, Path: clean}] = true
		return nil
	})
	if walkErr != nil {
		t.Fatalf("chi.Walk: %v", walkErr)
	}
	return out
}

// undocumentedRouteAllowList is the set of (method, path) pairs registered
// on the router that we intentionally do NOT document in the OpenAPI spec
// because they are infrastructure (the spec, the Swagger UI, redirect
// shims) or static-asset catch-alls.
var undocumentedRouteAllowList = map[specOperationKey]bool{
	{Method: "GET", Path: "/api/openapi.yaml"}: true,
	{Method: "GET", Path: "/swagger/"}:         true,
	{Method: "GET", Path: "/swagger"}:          true,
	// VTX-009: Vertex SystemGraph REST surface. The OpenAPI schemas for graph
	// payloads (layers, edges, savedSelections, timeSettings, positions) land
	// alongside the JSON Schema work in VTX-011; until then the chi routes are
	// the canonical contract.
	{Method: "POST", Path: "/api/vertex/v1/graphs"}:                         true,
	{Method: "GET", Path: "/api/vertex/v1/graphs/{rid}"}:                    true,
	{Method: "PUT", Path: "/api/vertex/v1/graphs/{rid}"}:                    true,
	{Method: "PATCH", Path: "/api/vertex/v1/graphs/{rid}/layout"}:           true,
	{Method: "POST", Path: "/api/vertex/v1/graphs/{rid}/duplicate"}:         true,
	{Method: "POST", Path: "/api/vertex/v1/graphs/{rid}/save-as-template"}:  true,
	{Method: "GET", Path: "/api/vertex/v1/graphs/{rid}/history"}:            true,
	{Method: "GET", Path: "/api/vertex/v1/graphs/{rid}/versions/{version}"}: true,
	// US-480: RFC 6902 JSON Patch diff between two graph versions.
	{Method: "GET", Path: "/api/vertex/v1/graphs/{rid}/diff"}:            true,
	{Method: "POST", Path: "/api/vertex/v1/templates/{rid}/instantiate"}: true,
	// VTX-013: Vertex graph share-link surface — owner mints / revokes
	// opaque tokens; recipients exchange them for masked graph payloads.
	// OpenAPI entries follow once the wire-format ResponseBody schemas
	// stabilise alongside the rest of the VTX-009 schemas.
	{Method: "POST", Path: "/api/vertex/v1/graphs/{rid}/share-links"}: true,
	{Method: "DELETE", Path: "/api/vertex/v1/share-links/{token}"}:    true,
	{Method: "GET", Path: "/api/vertex/v1/share-links/{token}/graph"}: true,
	// VTX-014: Workshop-embedded vertex_graph widget surface. GET returns a
	// compact payload (no savedSelections / history); POST persists with an
	// optional overrideGraphRid target. Same OpenAPI cycle as the rest of
	// the VTX-009 routes.
	{Method: "GET", Path: "/api/vertex/v1/graphs/{rid}/widget"}:       true,
	{Method: "POST", Path: "/api/vertex/v1/graphs/{rid}/widget/save"}: true,
}

// orphanSpecPathAllowList is the set of (method, path) pairs declared in the
// OpenAPI spec that have no chi route in this server. This list is empty by
// design: every documented path SHOULD map to a registered route.
var orphanSpecPathAllowList = map[specOperationKey]bool{}

func TestBDD_VertexControlPanelOpenAPIContract(t *testing.T) {
	doc := loadCanonicalSpec(t)
	specOps := extractSpecOperations(t, doc)
	controlPanelOps := []specOperationKey{
		{Method: "GET", Path: "/api/vertex/v1/admin/control-panel"},
		{Method: "PUT", Path: "/api/vertex/v1/admin/control-panel"},
	}
	for _, op := range controlPanelOps {
		if undocumentedRouteAllowList[op] {
			t.Errorf("%s %s must be documented in OpenAPI, not allow-listed as undocumented", op.Method, op.Path)
		}
		if !specOps[op] {
			t.Errorf("api/openapi.yaml must document %s %s", op.Method, op.Path)
		}
	}

	schemas := openAPISchemas(t, doc)
	config := openAPIProperties(t, schemas, "VertexControlPanelConfig")
	update := openAPIProperties(t, schemas, "VertexControlPanelUpdateRequest")
	expectedDefaults := map[string]string{
		"defaultWindowDays":       "30",
		"pollingIntervalSec":      "5",
		"searchAroundMaxNodes":    "200",
		"searchAroundMaxDepth":    "3",
		"missingDataWarningHours": "24",
	}
	for field, wantDefault := range expectedDefaults {
		prop, ok := config[field].(map[string]any)
		if !ok {
			t.Errorf("VertexControlPanelConfig must expose %s", field)
			continue
		}
		if got := fmt.Sprint(prop["default"]); got != wantDefault {
			t.Errorf("VertexControlPanelConfig.%s default = %s, want %s", field, got, wantDefault)
		}
		if _, ok := update[field]; !ok {
			t.Errorf("VertexControlPanelUpdateRequest must expose sparse update field %s", field)
		}
	}
}

func openAPISchemas(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	components, ok := doc["components"].(map[string]any)
	if !ok {
		t.Fatalf("components: expected map, got %T", doc["components"])
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatalf("components.schemas: expected map, got %T", components["schemas"])
	}
	return schemas
}

func openAPIProperties(t *testing.T, schemas map[string]any, name string) map[string]any {
	t.Helper()
	schema, ok := schemas[name].(map[string]any)
	if !ok {
		t.Fatalf("components.schemas.%s: expected map, got %T", name, schemas[name])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("components.schemas.%s.properties: expected map, got %T", name, schema["properties"])
	}
	return props
}

func newContractTestRouter(t *testing.T) *chi.Mux {
	t.Helper()
	repo := newFakeUserRepo()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := auth.NewJWTSigner(priv, &priv.PublicKey, auth.JWTSignerOptions{
		Issuer:         "weave-test",
		Audience:       "weave-api",
		AccessTokenTTL: 15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	rs := auth.NewRefreshService(auth.NewMemoryRefreshStore(),
		auth.RefreshServiceOptions{AbsoluteTTL: 7 * 24 * time.Hour})

	// Wire OSS / Action / Aggregation / ObjectSet deps with stubs so the
	// full router registers every route. The handlers are never invoked by
	// chi.Walk, so panics on nil-call would never fire from contract tests.
	omsRepo := contractOmsRepo{}
	indexMgr := index.NewManager(t.TempDir())
	t.Cleanup(func() { _ = indexMgr.Close() })
	linkResolver := links.NewResolver(omsRepo, indexMgr)
	ossSvc := oss.NewService(omsRepo, indexMgr, linkResolver)
	aggEngine := aggregation.NewEngine()
	objSetStore := objectset.NewStore(time.Hour)
	objSetExecutor := objectset.NewExecutor(indexMgr, linkResolver, objSetStore)
	actionExecutor := actions.NewExecutor(omsRepo, nil)

	deps := &ServerDeps{
		OmsRepo:          omsRepo,
		UserRepo:         repo,
		RoleResolver:     auth.NewRoleResolver(repo, time.Minute),
		JWTSigner:        signer,
		RefreshService:   rs,
		IndexMgr:         indexMgr,
		LinkResolver:     linkResolver,
		OssSvc:           ossSvc,
		AggEngine:        aggEngine,
		ActionExecutor:   actionExecutor,
		ObjSetStore:      objSetStore,
		ObjSetExecutor:   objSetExecutor,
		AttachmentStore:  attachment.NewLocalStore(t.TempDir()),
		TimeSeriesStore:  timeseries.NewMemoryStore(),
		GeotemporalStore: geotemporal.NewMemoryStore(),
		CipherDecryptor:  mustContractCipherDecryptor(t),
		TransactionStore: transactions.NewMemoryStore(),
		FunnelBroadcast:  funnel.NewBroadcast(),
		FunnelPublisher:  stubIngestPublisher{},
		WebSocketHub:     subscriptions.NewHub(),
		ApplicationRepo:  stubApplicationRepo{},
		AuthCodeRepo:     stubAuthCodeRepo{},
		OAuthTokenRepo:   stubOAuthTokenRepo{},
	}
	// US-446: contract / pact tests assume /health(z)/ready returns 200 in the
	// degraded contract harness; MarkReady flips the lifecycle gate so the
	// readiness handler runs the dependency probes (which all skip in
	// degraded mode) instead of short-circuiting on starting.
	deps.ServerState.MarkReady()
	return NewFullRouter(deps)
}

func mustContractCipherDecryptor(t *testing.T) cipher.Decryptor {
	t.Helper()
	dec, err := cipher.NewAESGCMDecryptor("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("cipher.NewAESGCMDecryptor: %v", err)
	}
	return dec
}

// TestContract_AllRoutesDocumented verifies that every chi route registered
// on NewFullRouter is documented in api/openapi.yaml. This is the contract
// test that prevents spec drift: a developer who adds a new route MUST also
// add the corresponding entry to the spec, or this test fails.
func TestContract_AllRoutesDocumented(t *testing.T) {
	router := newContractTestRouter(t)
	chiRoutes := extractChiRoutes(t, router)
	specOps := extractSpecOperations(t, loadCanonicalSpec(t))

	var missing []specOperationKey
	for key := range chiRoutes {
		if undocumentedRouteAllowList[key] {
			continue
		}
		if !specOps[key] {
			missing = append(missing, key)
		}
	}
	if len(missing) == 0 {
		return
	}
	sort.Slice(missing, func(i, j int) bool {
		if missing[i].Path != missing[j].Path {
			return missing[i].Path < missing[j].Path
		}
		return missing[i].Method < missing[j].Method
	})
	var b strings.Builder
	b.WriteString("openapi.yaml is missing entries for the following chi routes:\n")
	for _, k := range missing {
		fmt.Fprintf(&b, "  %s %s\n", k.Method, k.Path)
	}
	b.WriteString("\nAdd a corresponding `paths` entry in api/openapi.yaml or extend ")
	b.WriteString("undocumentedRouteAllowList in cmd/server/contract_test.go.")
	t.Fatal(b.String())
}

// TestContract_NoOrphanedSpecPaths is the reverse check: every operation in
// api/openapi.yaml MUST map to a chi route currently registered on the
// server. This catches deleted routes whose spec entries were left behind.
func TestContract_NoOrphanedSpecPaths(t *testing.T) {
	router := newContractTestRouter(t)
	chiRoutes := extractChiRoutes(t, router)
	specOps := extractSpecOperations(t, loadCanonicalSpec(t))

	var orphans []specOperationKey
	for key := range specOps {
		if orphanSpecPathAllowList[key] {
			continue
		}
		if !chiRoutes[key] {
			orphans = append(orphans, key)
		}
	}
	if len(orphans) == 0 {
		return
	}
	sort.Slice(orphans, func(i, j int) bool {
		if orphans[i].Path != orphans[j].Path {
			return orphans[i].Path < orphans[j].Path
		}
		return orphans[i].Method < orphans[j].Method
	})
	var b strings.Builder
	b.WriteString("openapi.yaml has paths with no matching chi route:\n")
	for _, k := range orphans {
		fmt.Fprintf(&b, "  %s %s\n", k.Method, k.Path)
	}
	b.WriteString("\nRemove the unused entry from api/openapi.yaml or register the route on NewFullRouter.")
	t.Fatal(b.String())
}

// TestContract_EmbeddedSpecMatchesCanonical guards against the embedded
// /api/openapi.yaml drifting from the on-disk source. The embedded copy
// (cmd/server/openapi_spec.yaml) is what the running server returns, so it
// MUST be byte-identical to the canonical api/openapi.yaml that the spec
// review and contract tests work against.
func TestContract_EmbeddedSpecMatchesCanonical(t *testing.T) {
	canonical, err := os.ReadFile(canonicalSpecPath(t))
	if err != nil {
		t.Fatalf("read canonical: %v", err)
	}
	if string(canonical) != string(openapiSpecYAML) {
		t.Fatalf("embedded openapi spec drifted from api/openapi.yaml; " +
			"copy api/openapi.yaml to cmd/server/openapi_spec.yaml and rerun")
	}
}

// contractOmsRepo is a minimal stub OMS repo so the full router wires every
// route. It satisfies oms.Repository through interface embedding (every
// method panics with nil-deref if invoked, but chi.Walk only inspects route
// metadata, never executes the handlers).
type contractOmsRepo struct{ oms.Repository }

// stubIngestPublisher satisfies oss.IngestPublisher for contract tests.
type stubIngestPublisher struct{}

func (stubIngestPublisher) Publish(batch *funnel.EditBatch) (uint64, error) { return 0, nil }

// stubApplicationRepo satisfies developer.ApplicationRepository for contract
// tests. chi.Walk only inspects route metadata, so every method is allowed
// to return a trivial error/empty value — the handlers never run here.
type stubApplicationRepo struct{}

func (stubApplicationRepo) Create(context.Context, *developer.Application) error { return nil }
func (stubApplicationRepo) GetByID(context.Context, string) (*developer.Application, error) {
	return nil, developer.ErrApplicationNotFound
}
func (stubApplicationRepo) GetByClientID(context.Context, string) (*developer.Application, error) {
	return nil, developer.ErrApplicationNotFound
}
func (stubApplicationRepo) ListByUser(context.Context, string) ([]*developer.Application, error) {
	return nil, nil
}
func (stubApplicationRepo) Delete(context.Context, string) error { return nil }

// stubAuthCodeRepo satisfies developer.AuthorizationCodeRepository for
// contract tests. The handlers never actually execute during chi.Walk so
// every method is allowed to return a trivial error/value.
type stubAuthCodeRepo struct{}

func (stubAuthCodeRepo) Create(context.Context, *developer.AuthorizationCode) error { return nil }
func (stubAuthCodeRepo) GetByCode(context.Context, string) (*developer.AuthorizationCode, error) {
	return nil, developer.ErrAuthorizationCodeNotFound
}
func (stubAuthCodeRepo) MarkConsumed(context.Context, string, time.Time) error { return nil }

// stubOAuthTokenRepo satisfies developer.OAuthTokenRepository for contract
// tests.
type stubOAuthTokenRepo struct{}

func (stubOAuthTokenRepo) Create(context.Context, *developer.OAuthToken) error { return nil }
func (stubOAuthTokenRepo) GetByPrefix(context.Context, string, string) ([]*developer.OAuthToken, error) {
	return nil, nil
}
func (stubOAuthTokenRepo) Revoke(context.Context, string, time.Time) error { return nil }
