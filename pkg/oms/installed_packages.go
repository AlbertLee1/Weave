package oms

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// InstalledPackage records one .weavepkg archive that was installed via the
// pkg install API (US-412). The marketplace UI (US-413, US-454) lists rows
// from this table and toggles `Enabled` for soft-disable; uninstall deletes
// the row outright. Only one installation per package name is tracked at a
// time — installing a new version over an existing row is upsert semantics.
type InstalledPackage struct {
	ID           int64           `json:"id"`
	Name         string          `json:"name"`
	Version      string          `json:"version"`
	Ontology     string          `json:"ontology"`
	ManifestJSON json.RawMessage `json:"manifest"`
	Migrations   []string        `json:"migrations"`
	Enabled      bool            `json:"enabled"`
	InstalledBy  string          `json:"installedBy,omitempty"`
	InstalledAt  time.Time       `json:"installedAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

// InstalledPackageStore is the narrow durable surface for the pkg install
// flow. The store is OPTIONAL: degraded-mode bootstraps (no PG) leave it
// nil, and the install handler falls back to no-op recording while still
// applying the ontology import + conflict checks. Listing endpoints (used
// by US-413's marketplace UI) report an empty list when the store is nil.
//
// Defined here (not on Repository) for the same reason as LineageStore /
// ColumnLineageStore — degraded-mode test routers should not have to
// cascade-stub it on top of every Repository mock.
type InstalledPackageStore interface {
	// UpsertInstalledPackage inserts a new row keyed by Name or updates the
	// existing row when one is present (so reinstalling a package replaces
	// its version + manifest in place rather than appending). The ID +
	// timestamps are back-filled on the supplied pointer.
	UpsertInstalledPackage(ctx context.Context, pkg *InstalledPackage) error
	// GetInstalledPackage looks up by Name; returns ErrNotFound when no
	// such row exists.
	GetInstalledPackage(ctx context.Context, name string) (*InstalledPackage, error)
	// ListInstalledPackages returns every installed package, newest first.
	ListInstalledPackages(ctx context.Context) ([]InstalledPackage, error)
	// SetInstalledPackageEnabled toggles the enabled flag without rewriting
	// the rest of the row. ErrNotFound when no such row exists.
	SetInstalledPackageEnabled(ctx context.Context, name string, enabled bool) error
	// DeleteInstalledPackage removes the row. Returns ErrNotFound when the
	// name doesn't match a row.
	DeleteInstalledPackage(ctx context.Context, name string) error
}

// ErrInstalledPackageNotFound is returned by InstalledPackageStore methods
// when the requested package name doesn't match a row. Callers can use
// errors.Is against this OR against ErrNotFound — both succeed.
var ErrInstalledPackageNotFound = errors.New("oms: installed package not found")
