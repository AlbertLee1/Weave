package controlpanel

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore is the PostgreSQL-backed Store built on the single-row
// vertex_control_panel table from migration 000204.
type PGStore struct {
	pool *pgxpool.Pool
}

// NewPGStore wires a PGStore over an existing pgx pool.
func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

// Get returns the single configuration row. When no row has been written yet
// we return DefaultConfig — matching the BDD semantics where an
// unconfigured installation answers GET with the canonical defaults.
func (s *PGStore) Get(ctx context.Context) (Config, error) {
	cfg := Config{}
	err := s.pool.QueryRow(ctx, `
		SELECT default_window_days,
		       polling_interval_sec,
		       search_around_max_nodes,
		       search_around_max_depth,
		       missing_data_warning_hours
		FROM vertex_control_panel
		WHERE id = 1
	`).Scan(
		&cfg.DefaultWindowDays,
		&cfg.PollingIntervalSec,
		&cfg.SearchAroundMaxNodes,
		&cfg.SearchAroundMaxDepth,
		&cfg.MissingDataWarningHours,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DefaultConfig(), nil
		}
		return Config{}, fmt.Errorf("get control panel: %w", err)
	}
	return cfg, nil
}

// Set upserts the configuration row. Validation is run first so an invalid
// Config never reaches the database; on success the row is written with
// updated_at = NOW() so observers can ordinarily sort by last-changed.
func (s *PGStore) Set(ctx context.Context, c Config) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO vertex_control_panel (
			id,
			default_window_days,
			polling_interval_sec,
			search_around_max_nodes,
			search_around_max_depth,
			missing_data_warning_hours,
			updated_at
		) VALUES (1, $1, $2, $3, $4, $5, NOW())
		ON CONFLICT (id) DO UPDATE SET
			default_window_days         = EXCLUDED.default_window_days,
			polling_interval_sec        = EXCLUDED.polling_interval_sec,
			search_around_max_nodes     = EXCLUDED.search_around_max_nodes,
			search_around_max_depth     = EXCLUDED.search_around_max_depth,
			missing_data_warning_hours  = EXCLUDED.missing_data_warning_hours,
			updated_at                  = NOW()
	`,
		c.DefaultWindowDays,
		c.PollingIntervalSec,
		c.SearchAroundMaxNodes,
		c.SearchAroundMaxDepth,
		c.MissingDataWarningHours,
	)
	if err != nil {
		return fmt.Errorf("set control panel: %w", err)
	}
	return nil
}

var _ Store = (*PGStore)(nil)
