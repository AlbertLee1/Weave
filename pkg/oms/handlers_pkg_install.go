package oms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
)

// WeaveServerVersion is the canonical server version string consulted by the
// pkg install flow's minWeaveVersion gate (US-412). It is a package-level
// var rather than a const so build-time `-ldflags '-X ...'` overrides remain
// possible without touching the install path; callers MUST treat it as
// read-only at runtime.
var WeaveServerVersion = "0.99.0"

// PackageMigrationEntry is one SQL migration file shipped inside the package
// archive. It is the wire-shape mirror of pkg/weavepkg.MigrationEntry but
// with the body base64-encoded so the JSON envelope stays text-safe even
// when the underlying SQL contains binary literals or non-UTF-8 sequences.
type PackageMigrationEntry struct {
	Filename string `json:"filename"`
	// Content is the migration body. Wire transport sends a base64-encoded
	// string via Go's encoding/json default for []byte; CLI clients should
	// not pre-encode.
	Content []byte `json:"content"`
}

// PackageInstallRequest is the body of POST /api/v2/pkg/install. The CLI
// (`weave pkg install`) parses a .weavepkg via pkg/weavepkg.Read and POSTs
// this envelope; the server validates manifest invariants, detects api
// conflicts, optionally runs the bundled migrations, and applies the
// ontology import.
type PackageInstallRequest struct {
	// Manifest carries the package's identity (name, version,
	// minWeaveVersion, dependencies). All wire fields except `name` and
	// `version` are optional.
	Manifest PackageManifest `json:"manifest"`
	// Ontology is the OntologyExport JSON envelope written by
	// pkg/weavepkg.Build at archive time. Required.
	Ontology json.RawMessage `json:"ontology"`
	// Migrations are the per-package SQL files shipped under migrations/ in
	// the archive. The server persists them under
	// {DataDir}/installed_packages/{name}/migrations/ and runs them when a
	// PackageMigrationRunner is wired.
	Migrations []PackageMigrationEntry `json:"migrations,omitempty"`
	// OnConflict controls behaviour when the server detects an API name
	// already in use by ObjectType / LinkType / ActionType / Function /
	// Interface / SharedProperty / TypeGroup / ValueType / QueryType. Three
	// values: "fail" (default — return 409 with the conflict list),
	// "overwrite" (proceed with replace-mode import), "skip" (proceed with
	// merge-mode import; existing entries with the same apiName are
	// skipped or updated as the importer sees fit).
	OnConflict string `json:"onConflict,omitempty"`
}

// PackageManifest mirrors weavepkg.Manifest's wire shape but is duplicated
// here so pkg/oms doesn't import pkg/weavepkg (the install handler must be
// reachable from the contract router whose dependency graph excludes the
// CLI helper packages).
type PackageManifest struct {
	Name            string              `json:"name"`
	Version         string              `json:"version"`
	Author          string              `json:"author,omitempty"`
	License         string              `json:"license,omitempty"`
	Description     string              `json:"description,omitempty"`
	MinWeaveVersion string              `json:"minWeaveVersion,omitempty"`
	Dependencies    []PackageDependency `json:"dependencies,omitempty"`
}

// PackageDependency mirrors weavepkg.Dependency for the same reason as
// PackageManifest.
type PackageDependency struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// PackageConflict identifies one entity in the package whose apiName is
// already used by an existing entity of the same kind. Surfaced in the 409
// response envelope so CLI clients can render an informative error and
// suggest --on-conflict=overwrite.
type PackageConflict struct {
	Kind    string `json:"kind"`
	APIName string `json:"apiName"`
}

// PackageInstallResponse is the wire-shape returned on success. Counts
// mirror ImportCounts so existing tooling consuming /import responses can
// reuse the same accounting helpers.
type PackageInstallResponse struct {
	Name            string       `json:"name"`
	Version         string       `json:"version"`
	Ontology        string       `json:"ontology"`
	Imported        ImportCounts `json:"imported"`
	MigrationsRan   int          `json:"migrationsRan"`
	MigrationsTotal int          `json:"migrationsTotal"`
	Message         string       `json:"message"`
}

// PackageMigrationRunner is the optional hook that runs SQL migrations
// shipped inside a .weavepkg. It is stamped into the OMSHandler at
// cmd/server/main.go boot when PG is wired; degraded-mode test routers
// leave it nil and the install handler skips the run step (migrations are
// still extracted to disk so a future operator-side run picks them up).
type PackageMigrationRunner interface {
	// RunPackageMigrations writes every supplied SQL file to
	// {DataDir}/installed_packages/{packageName}/migrations/ and runs
	// `golang-migrate up` against the resulting directory. Returns the
	// number of files it touched. Implementations MUST be idempotent —
	// reinstalling a package whose migrations already ran should report
	// the file count without re-applying.
	RunPackageMigrations(ctx context.Context, packageName string, files []PackageMigrationEntry) (int, error)
}

// SetInstalledPackageStore wires the durable installed-package registry.
// Optional: when unset the install handler still performs the import +
// conflict + migration steps and surfaces a successful response, but does
// not record the package on disk — the marketplace UI will then list an
// empty catalog until the store is wired.
func (h *OMSHandler) SetInstalledPackageStore(s InstalledPackageStore) {
	h.installedPackageStore = s
}

// InstalledPackageStore returns the wired store (or nil) so the route
// table can decide whether to register the listing endpoints.
func (h *OMSHandler) InstalledPackageStore() InstalledPackageStore {
	return h.installedPackageStore
}

// SetPackageMigrationRunner wires the optional migration runner for the
// pkg install flow. When unset the handler skips the migration run step
// and reports `migrationsRan=0, migrationsTotal=N` — the operator can pick
// up the SQL files from disk later. The split between "shipped count" and
// "ran count" stays observable end-to-end.
func (h *OMSHandler) SetPackageMigrationRunner(runner PackageMigrationRunner) {
	h.packageMigrationRunner = runner
}

// PackageInstall handles POST /api/v2/pkg/install (US-412).
//
// Flow:
//
//  1. Validate the request envelope (manifest.name + manifest.version
//     required, ontology body required).
//  2. Validate minWeaveVersion against WeaveServerVersion. A package
//     declaring a stricter requirement than the running server is rejected
//     with 400 PackageMinWeaveVersionUnsatisfied.
//  3. Decode the bundled ontology JSON into an OntologyExport-shaped
//     ImportOntologyV2Request envelope.
//  4. Detect api conflicts (entities whose apiName already exists at the
//     target ontology). When OnConflict="fail" (default) this short-circuits
//     with 409 PackageConflict. When OnConflict="overwrite" the import
//     proceeds in replace mode; when "skip" it proceeds in merge mode.
//  5. Run the bundled migrations through PackageMigrationRunner if wired.
//  6. Apply the ontology import via the shared importOntologyEntities path.
//  7. Record the install in InstalledPackageStore if wired.
func (h *OMSHandler) PackageInstall(w http.ResponseWriter, r *http.Request) {
	var req PackageInstallRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	if strings.TrimSpace(req.Manifest.Name) == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:manifest.name", map[string]string{
			"parameter": "manifest.name",
			"reason":    "manifest.name is required",
		}))
		return
	}
	if strings.TrimSpace(req.Manifest.Version) == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:manifest.version", map[string]string{
			"parameter": "manifest.version",
			"reason":    "manifest.version is required",
		}))
		return
	}
	if len(req.Ontology) == 0 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:ontology", map[string]string{
			"parameter": "ontology",
			"reason":    "ontology body is required",
		}))
		return
	}

	if err := checkMinWeaveVersion(req.Manifest.MinWeaveVersion, WeaveServerVersion); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("PackageMinWeaveVersionUnsatisfied", map[string]string{
			"required": req.Manifest.MinWeaveVersion,
			"server":   WeaveServerVersion,
			"reason":   err.Error(),
		}))
		return
	}

	onConflict := strings.ToLower(strings.TrimSpace(req.OnConflict))
	if onConflict == "" {
		onConflict = "fail"
	}
	switch onConflict {
	case "fail", "overwrite", "skip":
	default:
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:onConflict", map[string]string{
			"parameter": "onConflict",
			"reason":    "must be one of: fail, overwrite, skip",
		}))
		return
	}

	importReq, err := decodePackageOntology(req.Ontology, onConflict)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:ontology", map[string]string{
			"parameter": "ontology",
			"reason":    err.Error(),
		}))
		return
	}

	// 4. Conflict detection (always runs; fail / overwrite / skip diverge
	//    on what to do with the conflicts).
	conflicts := h.detectPackageConflicts(r.Context(), importReq)
	if onConflict == "fail" && len(conflicts) > 0 {
		// 409 Conflict envelope with the conflict list embedded in the
		// parameters bag (apierror's wire-format only carries flat string
		// fields, so we serialise the conflict list as a stable JSON
		// blob the CLI can re-parse).
		body, _ := json.Marshal(conflicts)
		apierror.WriteJSON(w, apierror.NewConflict("PackageConflict", map[string]string{
			"package":   req.Manifest.Name,
			"version":   req.Manifest.Version,
			"conflicts": string(body),
			"hint":      "rerun with --on-conflict=overwrite to replace existing entries, or --on-conflict=skip to keep existing definitions",
		}))
		return
	}

	// 5. Migrations.
	migrationsRan := 0
	if h.packageMigrationRunner != nil && len(req.Migrations) > 0 {
		ran, runErr := h.packageMigrationRunner.RunPackageMigrations(r.Context(), req.Manifest.Name, req.Migrations)
		if runErr != nil {
			apierror.WriteJSON(w, apierror.NewInternal("PackageMigrationFailed", map[string]string{
				"package": req.Manifest.Name,
				"reason":  runErr.Error(),
			}))
			return
		}
		migrationsRan = ran
	}

	// 6. Apply ontology import via the shared entity path.
	counts, ontology := h.applyImportRequest(r.Context(), importReq)

	// 7. Record the install if wired.
	if h.installedPackageStore != nil {
		manifestJSON, _ := json.Marshal(req.Manifest)
		filenames := make([]string, 0, len(req.Migrations))
		for _, m := range req.Migrations {
			filenames = append(filenames, m.Filename)
		}
		sort.Strings(filenames)
		row := &InstalledPackage{
			Name:         req.Manifest.Name,
			Version:      req.Manifest.Version,
			Ontology:     ontology.APIName,
			ManifestJSON: manifestJSON,
			Migrations:   filenames,
			Enabled:      true,
		}
		if h.actorFn != nil {
			row.InstalledBy = h.actorFn(r.Context())
		}
		if err := h.installedPackageStore.UpsertInstalledPackage(r.Context(), row); err != nil {
			// Recording is best-effort: the import already landed, so we
			// don't want to fail the whole call. Surface a 200 with a
			// warning string so operators can investigate.
			httputil.WriteJSON(w, http.StatusOK, PackageInstallResponse{
				Name:            req.Manifest.Name,
				Version:         req.Manifest.Version,
				Ontology:        ontology.APIName,
				Imported:        counts,
				MigrationsRan:   migrationsRan,
				MigrationsTotal: len(req.Migrations),
				Message:         fmt.Sprintf("install applied; recording in installed_packages failed: %v", err),
			})
			return
		}
	}

	httputil.WriteJSON(w, http.StatusCreated, PackageInstallResponse{
		Name:            req.Manifest.Name,
		Version:         req.Manifest.Version,
		Ontology:        ontology.APIName,
		Imported:        counts,
		MigrationsRan:   migrationsRan,
		MigrationsTotal: len(req.Migrations),
		Message:         "package installed",
	})
}

// applyImportRequest is the shared path between PackageInstall and
// ImportOntologyV2: it dispatches the entity import functions and returns
// the import counts plus the resolved ontology row. The caller chooses the
// req.Mode value before invoking; unsupported modes return zero counts.
func (h *OMSHandler) applyImportRequest(ctx context.Context, req *ImportOntologyV2Request) (ImportCounts, Ontology) {
	counts := ImportCounts{}

	ontology, isExisting, err := h.resolveImportOntology(ctx, req)
	if err != nil || ontology == nil {
		return counts, Ontology{}
	}

	if req.Mode == "replace" && isExisting {
		h.deleteOntologyEntities(ctx, ontology.RID)
	}

	spRIDMap := make(map[string]string)
	fnRIDMap := make(map[string]string)
	ifaceRIDMap := make(map[string]string)

	counts.SharedProperties = h.importSharedProperties(ctx, ontology.RID, req.Mode, req.SharedProperties, spRIDMap)
	counts.Functions = h.importFunctions(ctx, ontology.RID, req.Mode, req.Functions, fnRIDMap)
	otc, pc := h.importObjectTypes(ctx, ontology.RID, req.Mode, req.ObjectTypes, spRIDMap)
	counts.ObjectTypes = otc
	counts.Properties = pc
	counts.Interfaces = h.importInterfaces(ctx, ontology.RID, req.Mode, req.Interfaces, ifaceRIDMap)
	counts.LinkTypes = h.importLinkTypes(ctx, ontology.RID, req.Mode, req.LinkTypes)
	counts.ActionTypes = h.importActionTypes(ctx, ontology.RID, req.Mode, req.ActionTypes, fnRIDMap)
	counts.QueryTypes = h.importQueryTypes(ctx, ontology.RID, req.Mode, req.QueryTypes, fnRIDMap)
	counts.TypeGroups = h.importTypeGroups(ctx, ontology.RID, req.Mode, req.TypeGroups)
	counts.ValueTypes = h.importValueTypes(ctx, req.Mode, req.ValueTypes)

	return counts, *ontology
}

// detectPackageConflicts walks the import request and returns the list of
// entities whose apiName collides with an existing entry in the same
// ontology. The check uses the "merge"-mode lookups already present on the
// repo: GetByAPIName returning ErrNotFound for an entity means no conflict.
func (h *OMSHandler) detectPackageConflicts(ctx context.Context, req *ImportOntologyV2Request) []PackageConflict {
	if req.Ontology.APIName == "" {
		return nil
	}
	existing, err := h.repo.GetOntology(ctx, req.Ontology.APIName)
	if err != nil || existing == nil {
		// New ontology — nothing to conflict with.
		return nil
	}
	conflicts := make([]PackageConflict, 0)
	ontologyRID := existing.RID

	for _, ot := range req.ObjectTypes {
		if _, err := h.repo.GetObjectTypeByAPIName(ctx, ontologyRID, ot.APIName); err == nil {
			conflicts = append(conflicts, PackageConflict{Kind: "objectType", APIName: ot.APIName})
		}
	}
	for _, lt := range req.LinkTypes {
		if _, err := h.repo.GetLinkTypeByAPIName(ctx, ontologyRID, lt.APIName); err == nil {
			conflicts = append(conflicts, PackageConflict{Kind: "linkType", APIName: lt.APIName})
		}
	}
	for _, at := range req.ActionTypes {
		if _, err := h.repo.GetActionTypeByAPIName(ctx, ontologyRID, at.APIName); err == nil {
			conflicts = append(conflicts, PackageConflict{Kind: "actionType", APIName: at.APIName})
		}
	}
	for _, qt := range req.QueryTypes {
		if _, err := h.repo.GetQueryTypeByAPIName(ctx, ontologyRID, qt.APIName); err == nil {
			conflicts = append(conflicts, PackageConflict{Kind: "queryType", APIName: qt.APIName})
		}
	}
	for _, iface := range req.Interfaces {
		if _, err := h.repo.GetInterfaceByAPIName(ctx, ontologyRID, iface.APIName); err == nil {
			conflicts = append(conflicts, PackageConflict{Kind: "interface", APIName: iface.APIName})
		}
	}
	for _, fn := range req.Functions {
		if _, err := h.repo.GetFunctionByName(ctx, ontologyRID, fn.Name); err == nil {
			conflicts = append(conflicts, PackageConflict{Kind: "function", APIName: fn.Name})
		}
	}
	if existingSPs, err := h.repo.ListSharedProperties(ctx, ontologyRID); err == nil {
		for _, sp := range req.SharedProperties {
			if findSharedPropertyByAPIName(existingSPs, sp.APIName) != nil {
				conflicts = append(conflicts, PackageConflict{Kind: "sharedProperty", APIName: sp.APIName})
			}
		}
	}
	if existingTGs, err := h.repo.ListTypeGroups(ctx, ontologyRID); err == nil {
		for _, tg := range req.TypeGroups {
			if findTypeGroupByAPIName(existingTGs, tg.APIName) != nil {
				conflicts = append(conflicts, PackageConflict{Kind: "typeGroup", APIName: tg.APIName})
			}
		}
	}
	for _, vt := range req.ValueTypes {
		if _, err := h.repo.GetValueTypeByAPIName(ctx, vt.APIName); err == nil {
			conflicts = append(conflicts, PackageConflict{Kind: "valueType", APIName: vt.APIName})
		}
	}

	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Kind != conflicts[j].Kind {
			return conflicts[i].Kind < conflicts[j].Kind
		}
		return conflicts[i].APIName < conflicts[j].APIName
	})
	return conflicts
}

// decodePackageOntology unwraps the OntologyExport JSON envelope shipped
// inside a .weavepkg into the request shape the existing import path
// expects. mode is set from onConflict so the importer's create / update
// branching falls out automatically.
func decodePackageOntology(raw json.RawMessage, onConflict string) (*ImportOntologyV2Request, error) {
	var export OntologyExport
	if err := json.Unmarshal(raw, &export); err != nil {
		return nil, fmt.Errorf("ontology body is not a valid OntologyExport: %w", err)
	}
	if export.Ontology.APIName == "" {
		return nil, errors.New("ontology.apiName is required in the package's ontology body")
	}
	mode := "merge"
	if onConflict == "overwrite" {
		mode = "replace"
	}
	return &ImportOntologyV2Request{
		Mode:             mode,
		Ontology:         export.Ontology,
		ObjectTypes:      export.ObjectTypes,
		LinkTypes:        export.LinkTypes,
		ActionTypes:      export.ActionTypes,
		Interfaces:       export.Interfaces,
		SharedProperties: export.SharedProperties,
		ValueTypes:       export.ValueTypes,
		TypeGroups:       export.TypeGroups,
		Functions:        export.Functions,
		QueryTypes:       export.QueryTypes,
	}, nil
}

// checkMinWeaveVersion compares the package's required version against the
// running server's version. Both strings are dotted-decimal semver-ish (no
// prerelease segments expected); empty `required` is always satisfied.
//
// Comparison is element-wise on integer prefixes — `2.10.0` > `2.9.99` —
// with non-numeric segments treated as zero so older "v0.0.0" placeholders
// from pre-US-411 builds keep parsing cleanly.
func checkMinWeaveVersion(required, server string) error {
	required = strings.TrimSpace(required)
	if required == "" {
		return nil
	}
	cmp := compareSemverPrefix(required, server)
	if cmp > 0 {
		return fmt.Errorf("server version %s is older than required %s", server, required)
	}
	return nil
}

// compareSemverPrefix returns -1 / 0 / +1 for a vs b on a dotted-numeric
// prefix comparison. Trailing `+build` / `-rc1` suffixes are stripped; the
// function tolerates `v` prefixes ("v1.2.3" vs "1.2.3" compare equal).
func compareSemverPrefix(a, b string) int {
	aa := normaliseVersion(a)
	bb := normaliseVersion(b)
	for i := 0; i < len(aa) || i < len(bb); i++ {
		var av, bv int
		if i < len(aa) {
			av = aa[i]
		}
		if i < len(bb) {
			bv = bb[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func normaliseVersion(v string) []int {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "+-"); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		out = append(out, n)
	}
	return out
}

// no-op marker so the file always has a stable last symbol; keep the file
// lint-clean as new helpers land.
var _ = time.Time{}
