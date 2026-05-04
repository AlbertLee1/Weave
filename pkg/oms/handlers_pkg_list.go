package oms

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
)

// PackageListResponse is the wire shape of GET /api/v2/pkg.
type PackageListResponse struct {
	Data []InstalledPackage `json:"data"`
}

// ListInstalledPackages handles GET /api/v2/pkg.
//
// Returns the rows persisted by the install flow. When the
// InstalledPackageStore is unwired the response is an empty data array (NOT
// 503) so degraded-mode test routers can boot the marketplace UI without
// dragging the durable store through every mock.
func (h *OMSHandler) ListInstalledPackages(w http.ResponseWriter, r *http.Request) {
	if h.installedPackageStore == nil {
		httputil.WriteJSON(w, http.StatusOK, PackageListResponse{Data: []InstalledPackage{}})
		return
	}
	rows, err := h.installedPackageStore.ListInstalledPackages(r.Context())
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListInstalledPackagesFailed", nil))
		return
	}
	if rows == nil {
		rows = []InstalledPackage{}
	}
	httputil.WriteJSON(w, http.StatusOK, PackageListResponse{Data: rows})
}

// GetInstalledPackage handles GET /api/v2/pkg/{name}.
func (h *OMSHandler) GetInstalledPackage(w http.ResponseWriter, r *http.Request) {
	if h.installedPackageStore == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("InstalledPackagesNotConfigured", nil))
		return
	}
	name := chi.URLParam(r, "name")
	row, err := h.installedPackageStore.GetInstalledPackage(r.Context(), name)
	if err != nil {
		if errors.Is(err, ErrInstalledPackageNotFound) || errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("InstalledPackageNotFound", map[string]string{"name": name}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetInstalledPackageFailed", nil))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, row)
}

// PackageEnableRequest is the wire-body for the enable / disable toggle.
type PackageEnableRequest struct {
	Enabled bool `json:"enabled"`
}

// SetInstalledPackageEnabled handles POST /api/v2/pkg/{name}/enabled.
func (h *OMSHandler) SetInstalledPackageEnabled(w http.ResponseWriter, r *http.Request) {
	if h.installedPackageStore == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("InstalledPackagesNotConfigured", nil))
		return
	}
	name := chi.URLParam(r, "name")
	var req PackageEnableRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}
	if err := h.installedPackageStore.SetInstalledPackageEnabled(r.Context(), name, req.Enabled); err != nil {
		if errors.Is(err, ErrInstalledPackageNotFound) || errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("InstalledPackageNotFound", map[string]string{"name": name}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("SetInstalledPackageEnabledFailed", nil))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"name": name, "enabled": req.Enabled})
}

// DeleteInstalledPackage handles DELETE /api/v2/pkg/{name}. The on-disk
// migrations + ontology entities are NOT touched — uninstall in the strict
// sense (drop tables + remove ontology) is a separate operator workflow.
// This call only removes the registry row so the marketplace UI hides it.
func (h *OMSHandler) DeleteInstalledPackage(w http.ResponseWriter, r *http.Request) {
	if h.installedPackageStore == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("InstalledPackagesNotConfigured", nil))
		return
	}
	name := chi.URLParam(r, "name")
	if err := h.installedPackageStore.DeleteInstalledPackage(r.Context(), name); err != nil {
		if errors.Is(err, ErrInstalledPackageNotFound) || errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("InstalledPackageNotFound", map[string]string{"name": name}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DeleteInstalledPackageFailed", nil))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
