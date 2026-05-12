//go:build bdd

package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/cucumber/godog"

	"github.com/liyang/weave/pkg/oms"
)

// registerPackageInstallSteps wires the US-020 package_install_lifecycle
// feature's step regex onto the scenario context.
//
// The harness drives the public package endpoints exactly as the production
// chi router does — /api/v2/pkg/install, /api/v2/pkg/builtin/{slug}/install,
// and DELETE /api/v2/pkg/{name} — and asserts three layers per scenario:
//
//   - HTTP status (201 install / 204 uninstall);
//   - PackageInstallResponse fields (name, version, ontology);
//   - the installed_packages PG row reachable through
//     pkg/oms/installedpkgpg.Store, AND the imported ontology entities via
//     *oms.PGRepository.GetObjectTypeByAPIName so the contract that "install
//     materialises ontology entities" stays a documented invariant.
//
// A small in-process BuiltinPackageProvider stub (bddBuiltinPackageProvider)
// stands in for cmd/server/builtin_packages.go so the BDD does not need to
// drag the embedded examples/packages tree or pkg/weavepkg through the test
// binary — feature steps construct minimal synthetic packages on the fly.
func registerPackageInstallSteps(t testing.TB, sc *godog.ScenarioContext, state *suiteState) {

	// --- Given: register a synthetic built-in package -------------------

	sc.Given(
		`^a built-in package "([^"]+)" version "([^"]+)" targeting ontology "([^"]+)" with object type "([^"]+)"$`,
		func(slug, version, ontologyAPIName, objectTypeAPIName string) error {
			// Background "Given a fresh weave database with migrations applied"
			// already calls ensureContainer. Calling it again here would re-
			// truncate mid-scenario (see Codebase Patterns line 64 / US-015)
			// and wipe the widget seeded by an earlier install in the
			// Update-Version flow.
			if state.repo == nil {
				return fmt.Errorf("BDD harness not initialised — add a Background that runs " +
					"\"Given a fresh weave database with migrations applied\"")
			}
			body, err := buildSyntheticOntologyExport(ontologyAPIName, objectTypeAPIName)
			if err != nil {
				return err
			}
			req := &oms.PackageInstallRequest{
				Manifest: oms.PackageManifest{
					Name:        slug,
					Version:     version,
					Description: "synthetic package for BDD US-020",
				},
				Ontology: body,
			}
			state.builtinPkgProvider.set(slug, req, &oms.BuiltinPackageMetadata{
				Slug:            slug,
				Name:            slug,
				Version:         version,
				OntologyAPIName: ontologyAPIName,
				ObjectTypeCount: 1,
			})
			return nil
		},
	)

	// --- Given: convenience installer used by Update + Uninstall --------

	sc.Given(
		`^the operator has installed the built-in package "([^"]+)"$`,
		func(slug string) error {
			if err := postInstallBuiltin(state, slug, ""); err != nil {
				return err
			}
			if state.lastPackageResponse.statusCode != http.StatusCreated {
				return fmt.Errorf(
					"prerequisite install for %q expected 201, got %d; body=%s",
					slug, state.lastPackageResponse.statusCode,
					state.lastPackageResponse.body,
				)
			}
			return nil
		},
	)

	// --- When: drive the install endpoint -------------------------------

	sc.When(
		`^the operator installs the built-in package "([^"]+)"$`,
		func(slug string) error { return postInstallBuiltin(state, slug, "") },
	)

	sc.When(
		`^the operator installs the built-in package "([^"]+)" with onConflict "([^"]+)"$`,
		func(slug, onConflict string) error { return postInstallBuiltin(state, slug, onConflict) },
	)

	// --- When: drive the uninstall endpoint -----------------------------

	sc.When(
		`^the operator uninstalls the package "([^"]+)"$`,
		func(name string) error {
			path := fmt.Sprintf("/api/v2/pkg/%s", name)
			req := httptest.NewRequest(http.MethodDelete, path, nil)
			rr := httptest.NewRecorder()
			state.pkgRouter.ServeHTTP(rr, req)
			state.lastPackageResponse = &packageHTTPResult{
				statusCode: rr.Code,
				body:       rr.Body.Bytes(),
			}
			return nil
		},
	)

	// --- Then: HTTP status layer ----------------------------------------

	sc.Then(`^the install response status is (\d+)$`, func(want int) error {
		return assertPackageStatus(state, want)
	})

	sc.Then(`^the uninstall response status is (\d+)$`, func(want int) error {
		return assertPackageStatus(state, want)
	})

	// --- Then: response body layer --------------------------------------

	sc.Then(
		`^the install response says version "([^"]+)" was applied to ontology "([^"]+)"$`,
		func(version, ontologyAPIName string) error {
			if state.lastPackageResponse == nil {
				return errors.New("no install response captured")
			}
			var resp oms.PackageInstallResponse
			if err := json.Unmarshal(state.lastPackageResponse.body, &resp); err != nil {
				return fmt.Errorf("decode install response: %w; body=%s", err, state.lastPackageResponse.body)
			}
			if resp.Version != version {
				return fmt.Errorf("response.version = %q, want %q", resp.Version, version)
			}
			if resp.Ontology != ontologyAPIName {
				return fmt.Errorf("response.ontology = %q, want %q", resp.Ontology, ontologyAPIName)
			}
			return nil
		},
	)

	// --- Then: installed_packages PG row layer --------------------------

	sc.Then(
		`^the installed_packages row "([^"]+)" exists with version "([^"]+)" and enabled (true|false)$`,
		func(name, version, enabledLit string) error {
			row, err := state.pkgStore.GetInstalledPackage(context.Background(), name)
			if err != nil {
				return fmt.Errorf("GetInstalledPackage(%s): %w", name, err)
			}
			if row.Version != version {
				return fmt.Errorf("installed_packages[%s].version = %q, want %q",
					name, row.Version, version)
			}
			wantEnabled := enabledLit == "true"
			if row.Enabled != wantEnabled {
				return fmt.Errorf("installed_packages[%s].enabled = %v, want %v",
					name, row.Enabled, wantEnabled)
			}
			return nil
		},
	)

	sc.Then(
		`^no installed_packages row exists for name "([^"]+)"$`,
		func(name string) error {
			_, err := state.pkgStore.GetInstalledPackage(context.Background(), name)
			if err == nil {
				return fmt.Errorf("expected ErrInstalledPackageNotFound for %q, got row", name)
			}
			if !errors.Is(err, oms.ErrInstalledPackageNotFound) {
				return fmt.Errorf("GetInstalledPackage(%s): want ErrInstalledPackageNotFound, got %v",
					name, err)
			}
			return nil
		},
	)

	sc.Then(
		`^exactly (\d+) installed_packages rows? exists? for name "([^"]+)"$`,
		func(want int, name string) error {
			var got int
			err := state.pg.Pool.QueryRow(context.Background(),
				`SELECT COUNT(*) FROM installed_packages WHERE name = $1`, name,
			).Scan(&got)
			if err != nil {
				return fmt.Errorf("count installed_packages: %w", err)
			}
			if got != want {
				return fmt.Errorf("installed_packages rows for %q = %d, want %d", name, got, want)
			}
			return nil
		},
	)

	// --- Then: ontology side-effect layer -------------------------------

	sc.Then(
		`^the ontology "([^"]+)" has object type "([^"]+)"$`,
		func(ontologyAPIName, objectTypeAPIName string) error {
			ctx := context.Background()
			ont, err := state.repo.GetOntology(ctx, ontologyAPIName)
			if err != nil {
				return fmt.Errorf("GetOntology(%s): %w", ontologyAPIName, err)
			}
			ot, err := state.repo.GetObjectTypeByAPIName(ctx, ont.RID, objectTypeAPIName)
			if err != nil {
				return fmt.Errorf("GetObjectTypeByAPIName(%s/%s): %w",
					ontologyAPIName, objectTypeAPIName, err)
			}
			if ot.APIName != objectTypeAPIName {
				return fmt.Errorf("imported object type apiName = %q, want %q",
					ot.APIName, objectTypeAPIName)
			}
			return nil
		},
	)
}

// --- helpers --------------------------------------------------------

// buildSyntheticOntologyExport assembles a minimal OntologyExport JSON
// envelope with the named ontology + a single ObjectType. Properties are
// intentionally empty so the import path stays exercise-of-handler rather
// than exercise-of-property-schema; the existing pkg/oms unit suite covers
// property-level import behaviour in handlers_import_test.go.
func buildSyntheticOntologyExport(ontologyAPIName, objectTypeAPIName string) (json.RawMessage, error) {
	export := oms.OntologyExport{
		Ontology: oms.Ontology{
			APIName:     ontologyAPIName,
			DisplayName: ontologyAPIName,
			Description: "synthetic ontology body for BDD package install",
		},
		ObjectTypes: []oms.ObjectType{
			{
				APIName:     objectTypeAPIName,
				DisplayName: objectTypeAPIName,
				PrimaryKey:  "id",
				PrimaryKeys: []string{"id"},
				Status:      "ACTIVE",
				Visibility:  "PROMINENT",
			},
		},
	}
	raw, err := json.Marshal(export)
	if err != nil {
		return nil, fmt.Errorf("marshal synthetic OntologyExport: %w", err)
	}
	return raw, nil
}

// postInstallBuiltin issues POST /api/v2/pkg/builtin/{slug}/install
// against the BDD chi router, optionally supplying onConflict in the
// body. The InstallBuiltinPackage handler accepts an empty body when
// ContentLength is 0, so onConflict="" means "use the server default".
func postInstallBuiltin(state *suiteState, slug, onConflict string) error {
	path := fmt.Sprintf("/api/v2/pkg/builtin/%s/install", slug)
	var reqBody *bytes.Buffer
	contentLength := 0
	if onConflict != "" {
		buf, err := json.Marshal(oms.BuiltinInstallRequest{OnConflict: onConflict})
		if err != nil {
			return fmt.Errorf("marshal builtin install body: %w", err)
		}
		reqBody = bytes.NewBuffer(buf)
		contentLength = reqBody.Len()
	} else {
		reqBody = &bytes.Buffer{}
	}
	req := httptest.NewRequest(http.MethodPost, path, reqBody)
	req.ContentLength = int64(contentLength)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	state.pkgRouter.ServeHTTP(rr, req)
	state.lastPackageResponse = &packageHTTPResult{
		statusCode: rr.Code,
		body:       rr.Body.Bytes(),
	}
	return nil
}

func assertPackageStatus(state *suiteState, want int) error {
	if state.lastPackageResponse == nil {
		return errors.New("no package response captured")
	}
	if state.lastPackageResponse.statusCode != want {
		return fmt.Errorf("package response status = %d, want %d; body=%s",
			state.lastPackageResponse.statusCode, want,
			state.lastPackageResponse.body)
	}
	return nil
}

// --- BDD-local BuiltinPackageProvider stub --------------------------

// bddBuiltinPackageProvider is the in-process stand-in for
// cmd/server/builtin_packages.go. Step definitions register synthetic
// (slug → request, metadata) entries on the fly so the BDD does not
// have to drag the embedded examples/packages tree or pkg/weavepkg
// into the test binary.
type bddBuiltinPackageProvider struct {
	mu       sync.Mutex
	entries  map[string]bddBuiltinEntry
	metadata []oms.BuiltinPackageMetadata
}

type bddBuiltinEntry struct {
	req  *oms.PackageInstallRequest
	meta oms.BuiltinPackageMetadata
}

func newBDDBuiltinPackageProvider() *bddBuiltinPackageProvider {
	return &bddBuiltinPackageProvider{entries: map[string]bddBuiltinEntry{}}
}

// set registers or replaces one slug. Re-setting a slug overwrites the
// previous entry (used by the Update-Version scenario, which seeds the
// same slug with a bumped version after the initial install).
func (p *bddBuiltinPackageProvider) set(slug string, req *oms.PackageInstallRequest, meta *oms.BuiltinPackageMetadata) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if meta == nil {
		meta = &oms.BuiltinPackageMetadata{Slug: slug}
	}
	p.entries[slug] = bddBuiltinEntry{req: req, meta: *meta}
	p.rebuildMetadata()
}

func (p *bddBuiltinPackageProvider) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries = map[string]bddBuiltinEntry{}
	p.metadata = nil
}

func (p *bddBuiltinPackageProvider) rebuildMetadata() {
	p.metadata = p.metadata[:0]
	for _, entry := range p.entries {
		p.metadata = append(p.metadata, entry.meta)
	}
}

func (p *bddBuiltinPackageProvider) List(_ context.Context) []oms.BuiltinPackageMetadata {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]oms.BuiltinPackageMetadata, len(p.metadata))
	copy(out, p.metadata)
	return out
}

func (p *bddBuiltinPackageProvider) Get(_ context.Context, slug string) (*oms.PackageInstallRequest, *oms.BuiltinPackageMetadata, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.entries[slug]
	if !ok {
		return nil, nil, false
	}
	// Clone the install request so the in-handler tweak to OnConflict
	// (see handlers_pkg_builtin.go:127) does not bleed between scenarios.
	clone := *entry.req
	if len(clone.Ontology) > 0 {
		buf := make([]byte, len(clone.Ontology))
		copy(buf, clone.Ontology)
		clone.Ontology = buf
	}
	metaClone := entry.meta
	return &clone, &metaClone, true
}
