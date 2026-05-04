// Package examplepkgs embeds the source material for the three built-in
// example packages exposed under the Marketplace UI's "Built-in" tab
// (US-414).
//
// Each subdirectory under examples/packages/ holds one package described by
// a manifest.json + ontology.json pair. The server loads the embedded FS
// once at boot and surfaces the catalog through GET /api/v2/pkg/builtin
// + POST /api/v2/pkg/builtin/{name}/install.
package examplepkgs

import "embed"

// FS is the embedded filesystem rooted at examples/packages/. Consumers
// walk it via fs.ReadDir / fs.ReadFile to discover packages and load
// their JSON payloads.
//
//go:embed all:northwind all:chinook all:iot-demo
var FS embed.FS
