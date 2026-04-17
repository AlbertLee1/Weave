package developer

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrApplicationNotFound is returned by lookup methods when the requested
// row does not exist.
var ErrApplicationNotFound = errors.New("application not found")

// ApplicationRepository is the persistence interface for the applications
// table. It is consumed by the developer-console HTTP handlers and, later,
// by the OAuth token endpoints (US-142).
type ApplicationRepository interface {
	Create(ctx context.Context, app *Application) error
	GetByID(ctx context.Context, id string) (*Application, error)
	GetByClientID(ctx context.Context, clientID string) (*Application, error)
	ListByUser(ctx context.Context, userID string) ([]*Application, error)
	Delete(ctx context.Context, id string) error
}

// PGApplicationRepository is the Postgres-backed ApplicationRepository.
type PGApplicationRepository struct {
	pool *pgxpool.Pool
}

// NewPGApplicationRepository wraps a pgx pool as an ApplicationRepository.
func NewPGApplicationRepository(pool *pgxpool.Pool) *PGApplicationRepository {
	return &PGApplicationRepository{pool: pool}
}

const applicationColumns = `id, name, description, client_id, client_secret_hash, redirect_uris, scopes, created_by, created_at, updated_at`

// Create inserts a new application row. The caller is responsible for
// populating ClientID + ClientSecretHash; ID/CreatedAt/UpdatedAt are
// populated from the DB defaults and copied back onto the supplied record.
func (r *PGApplicationRepository) Create(ctx context.Context, app *Application) error {
	if app == nil {
		return errors.New("application: nil record")
	}
	if app.Name == "" || app.ClientID == "" || len(app.ClientSecretHash) == 0 || app.CreatedBy == "" {
		return errors.New("application: name, client_id, client_secret_hash and created_by are required")
	}
	redirects := app.RedirectURIs
	if redirects == nil {
		redirects = []string{}
	}
	scopes := app.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return err
	}
	row := r.pool.QueryRow(ctx,
		`INSERT INTO applications (name, description, client_id, client_secret_hash, redirect_uris, scopes, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, created_at, updated_at`,
		app.Name, app.Description, app.ClientID, app.ClientSecretHash, redirects, scopesJSON, app.CreatedBy)
	return row.Scan(&app.ID, &app.CreatedAt, &app.UpdatedAt)
}

func scanApplication(row pgx.Row) (*Application, error) {
	app := &Application{}
	var redirects []string
	var scopesJSON []byte
	err := row.Scan(
		&app.ID,
		&app.Name,
		&app.Description,
		&app.ClientID,
		&app.ClientSecretHash,
		&redirects,
		&scopesJSON,
		&app.CreatedBy,
		&app.CreatedAt,
		&app.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if redirects == nil {
		redirects = []string{}
	}
	app.RedirectURIs = redirects
	var scopes []string
	if len(scopesJSON) > 0 {
		if err := json.Unmarshal(scopesJSON, &scopes); err != nil {
			return nil, err
		}
	}
	if scopes == nil {
		scopes = []string{}
	}
	app.Scopes = scopes
	return app, nil
}

// GetByID returns the application with the given primary key.
func (r *PGApplicationRepository) GetByID(ctx context.Context, id string) (*Application, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+applicationColumns+` FROM applications WHERE id = $1`, id)
	app, err := scanApplication(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrApplicationNotFound
		}
		return nil, err
	}
	return app, nil
}

// GetByClientID looks up an application by its public OAuth client_id. Used
// by the token endpoint (US-142) to resolve an inbound client_id → row
// before verifying the submitted client_secret.
func (r *PGApplicationRepository) GetByClientID(ctx context.Context, clientID string) (*Application, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+applicationColumns+` FROM applications WHERE client_id = $1`, clientID)
	app, err := scanApplication(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrApplicationNotFound
		}
		return nil, err
	}
	return app, nil
}

// ListByUser returns the caller's applications, newest first.
func (r *PGApplicationRepository) ListByUser(ctx context.Context, userID string) ([]*Application, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+applicationColumns+` FROM applications WHERE created_by = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Application
	for rows.Next() {
		app, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, app)
	}
	return out, rows.Err()
}

// Delete removes the application row. Hard delete is fine: OAuth tokens
// issued against a deleted app become unusable as soon as the token
// endpoint's GetByClientID lookup misses.
func (r *PGApplicationRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM applications WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrApplicationNotFound
	}
	return nil
}

// Compile-time assertion that PGApplicationRepository satisfies the
// interface. Exists so that renames on the interface trigger a build break
// instead of a subtle test failure.
var _ ApplicationRepository = (*PGApplicationRepository)(nil)
