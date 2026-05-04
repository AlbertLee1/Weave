package oms

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
)

// BuiltinPackageMetadata is one entry in the Marketplace UI's "Built-in"
// catalog (US-414). It is a thin projection over the package source data
// embedded into the server binary — the full ontology body is omitted on
// the list endpoint and only materialised at install time.
type BuiltinPackageMetadata struct {
	Slug            string              `json:"slug"`
	Name            string              `json:"name"`
	Version         string              `json:"version"`
	OntologyAPIName string              `json:"ontologyApiName"`
	Author          string              `json:"author,omitempty"`
	License         string              `json:"license,omitempty"`
	Description     string              `json:"description,omitempty"`
	MinWeaveVersion string              `json:"minWeaveVersion,omitempty"`
	Dependencies    []PackageDependency `json:"dependencies,omitempty"`
	ObjectTypeCount int                 `json:"objectTypeCount"`
	LinkTypeCount   int                 `json:"linkTypeCount"`
	ActionTypeCount int                 `json:"actionTypeCount"`
	FunctionCount   int                 `json:"functionCount"`
	MigrationCount  int                 `json:"migrationCount"`
}

// BuiltinPackageProvider is the read-side surface the OMS handler uses to
// list the embedded catalog and look one up at install time. It is wired
// from cmd/server/main.go where the embedded FS is loaded once at boot.
//
// Kept narrow on purpose so degraded-mode (test) routers can stub it with
// a static slice without dragging the loader package in.
type BuiltinPackageProvider interface {
	List(ctx context.Context) []BuiltinPackageMetadata
	Get(ctx context.Context, slug string) (*PackageInstallRequest, *BuiltinPackageMetadata, bool)
}

// SetBuiltinPackageProvider wires the embedded catalog source. When unset
// the list endpoint returns an empty data array (NOT 503) so degraded-mode
// boots still surface the marketplace UI cleanly; the install endpoint
// returns 404 BuiltinPackageNotFound for any slug.
func (h *OMSHandler) SetBuiltinPackageProvider(p BuiltinPackageProvider) {
	h.builtinPackageProvider = p
}

// BuiltinPackageProvider returns the wired provider (or nil) so the route
// table can decide whether to register the listing endpoint.
func (h *OMSHandler) BuiltinPackages() BuiltinPackageProvider {
	return h.builtinPackageProvider
}

// BuiltinPackageListResponse is the wire shape of GET /api/v2/pkg/builtin.
type BuiltinPackageListResponse struct {
	Data []BuiltinPackageMetadata `json:"data"`
}

// ListBuiltinPackages handles GET /api/v2/pkg/builtin.
func (h *OMSHandler) ListBuiltinPackages(w http.ResponseWriter, r *http.Request) {
	if h.builtinPackageProvider == nil {
		httputil.WriteJSON(w, http.StatusOK, BuiltinPackageListResponse{Data: []BuiltinPackageMetadata{}})
		return
	}
	rows := h.builtinPackageProvider.List(r.Context())
	if rows == nil {
		rows = []BuiltinPackageMetadata{}
	}
	httputil.WriteJSON(w, http.StatusOK, BuiltinPackageListResponse{Data: rows})
}

// BuiltinInstallRequest is the body of POST /api/v2/pkg/builtin/{slug}/install.
// All fields are optional: the slug supplied in the path drives the catalog
// lookup, and onConflict defaults to "fail" (matching the JSON-bodied
// /api/v2/pkg/install endpoint).
type BuiltinInstallRequest struct {
	OnConflict string `json:"onConflict,omitempty"`
}

// InstallBuiltinPackage handles POST /api/v2/pkg/builtin/{slug}/install.
//
// The slug is the directory name from the embedded examples/packages/ tree
// (e.g. "northwind"). The handler resolves the package, optionally applies
// the supplied onConflict knob, then routes through the same in-process
// install path as the JSON-bodied /api/v2/pkg/install endpoint.
func (h *OMSHandler) InstallBuiltinPackage(w http.ResponseWriter, r *http.Request) {
	if h.builtinPackageProvider == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("BuiltinPackagesNotConfigured", nil))
		return
	}
	slug := chi.URLParam(r, "slug")
	if strings.TrimSpace(slug) == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:slug", map[string]string{
			"parameter": "slug",
			"reason":    "slug is required",
		}))
		return
	}

	// Body is optional — POST without a body picks up onConflict=fail. We
	// allow EOF / empty body cleanly so the SPA fetch can call this
	// without serialising a body.
	var body BuiltinInstallRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, errEmptyBody) {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
				"reason": "invalid JSON",
			}))
			return
		}
	}

	req, _, ok := h.builtinPackageProvider.Get(r.Context(), slug)
	if !ok {
		apierror.WriteJSON(w, apierror.NewNotFound("BuiltinPackageNotFound", map[string]string{
			"slug": slug,
		}))
		return
	}
	if strings.TrimSpace(body.OnConflict) != "" {
		req.OnConflict = body.OnConflict
	}

	resp, status, apiErr := h.installPackage(r.Context(), req)
	if apiErr != nil {
		apierror.WriteJSON(w, apiErr)
		return
	}
	httputil.WriteJSON(w, status, *resp)
}

// errEmptyBody is sentinel for "request body was empty" — allowed for the
// builtin install flow because the body is fully optional.
var errEmptyBody = errors.New("empty body")
