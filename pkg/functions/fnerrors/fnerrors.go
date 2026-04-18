// Package fnerrors declares the sentinel errors callers of the Function
// runtime use to distinguish resource-limit violations from generic
// execution failures. It intentionally imports nothing from the rest of
// pkg/functions (which transitively pulls in pkg/oss and pkg/oms) so that
// upstream layers such as pkg/oms can import these sentinels without
// creating an import cycle.
package fnerrors

import "errors"

// ErrTimeout is returned when a function exceeds its CPU execution budget
// (goja interrupt triggered by the context deadline). Handlers translate
// this to HTTP 408.
var ErrTimeout = errors.New("function execution timeout")

// ErrMemoryLimit is returned when a function's heap allocation crosses the
// configured MaxMemoryBytes budget. Handlers translate this to HTTP 429.
var ErrMemoryLimit = errors.New("function memory limit exceeded")
