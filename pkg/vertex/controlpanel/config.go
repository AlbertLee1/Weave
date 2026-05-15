// Package controlpanel implements VTX-015 — the Vertex Control Panel, a
// small admin-managed configuration surface that holds operator-tunable knobs
// (default time window, polling interval, search-around limits, missing-data
// warning threshold).
//
// The package exposes:
//
//   - Config: the wire-format value type returned by GET and accepted by PUT.
//   - Store: a tiny CRUD interface so handlers / tests / the PG store all
//     interchange. Get-on-empty must return DefaultConfig so callers never
//     have to special-case the bootstrap state.
//   - MemStore: in-memory Store for tests and degraded-mode boots.
//   - Handler: chi-mountable HTTP surface at /api/vertex/v1/admin/control-panel.
//     GET is public (every authenticated client needs to read the knobs);
//     PUT requires the admin role.
package controlpanel

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Config is the wire-format value type for the Vertex Control Panel. All
// fields are non-negative integers; the JSON tags match the BDD acceptance
// names verbatim so the contract is stable across clients.
type Config struct {
	DefaultWindowDays       int `json:"defaultWindowDays"`
	PollingIntervalSec      int `json:"pollingIntervalSec"`
	SearchAroundMaxNodes    int `json:"searchAroundMaxNodes"`
	SearchAroundMaxDepth    int `json:"searchAroundMaxDepth"`
	MissingDataWarningHours int `json:"missingDataWarningHours"`
}

// DefaultConfig returns the canonical defaults from VTX-015 BDD acceptance 1.
// Callers MUST treat this as the source of truth — drift here is a contract
// change.
func DefaultConfig() Config {
	return Config{
		DefaultWindowDays:       30,
		PollingIntervalSec:      5,
		SearchAroundMaxNodes:    200,
		SearchAroundMaxDepth:    3,
		MissingDataWarningHours: 24,
	}
}

// Validate returns nil when every field is a strictly positive integer. The
// store rejects writes that fail this check so the bad value never reaches
// any subsequent Get.
func (c Config) Validate() error {
	if c.DefaultWindowDays <= 0 {
		return fmt.Errorf("defaultWindowDays must be > 0 (got %d)", c.DefaultWindowDays)
	}
	if c.PollingIntervalSec <= 0 {
		return fmt.Errorf("pollingIntervalSec must be > 0 (got %d)", c.PollingIntervalSec)
	}
	if c.SearchAroundMaxNodes <= 0 {
		return fmt.Errorf("searchAroundMaxNodes must be > 0 (got %d)", c.SearchAroundMaxNodes)
	}
	if c.SearchAroundMaxDepth <= 0 {
		return fmt.Errorf("searchAroundMaxDepth must be > 0 (got %d)", c.SearchAroundMaxDepth)
	}
	if c.MissingDataWarningHours <= 0 {
		return fmt.Errorf("missingDataWarningHours must be > 0 (got %d)", c.MissingDataWarningHours)
	}
	return nil
}

// ErrInvalidConfig is returned by Store.Set when a Config fails Validate. The
// handler maps this to 400 INVALID_ARGUMENT so callers can distinguish "your
// values are bad" from "we couldn't reach the DB".
var ErrInvalidConfig = errors.New("invalid control panel config")

// Store is the persistence interface the Handler depends on. Get on an empty
// store must return DefaultConfig (not an error), so the bootstrap flow
// trivially produces the BDD defaults.
type Store interface {
	Get(ctx context.Context) (Config, error)
	Set(ctx context.Context, c Config) error
}

// MemStore is the in-memory Store used by tests and degraded-mode boots.
type MemStore struct {
	mu  sync.Mutex
	set bool
	cfg Config
}

// NewMemStore returns an empty MemStore. Get on a fresh instance returns
// DefaultConfig (not zero-Config).
func NewMemStore() *MemStore {
	return &MemStore{}
}

func (s *MemStore) Get(ctx context.Context) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.set {
		return DefaultConfig(), nil
	}
	return s.cfg, nil
}

func (s *MemStore) Set(ctx context.Context, c Config) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = c
	s.set = true
	return nil
}

var _ Store = (*MemStore)(nil)
