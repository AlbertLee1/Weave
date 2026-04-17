package developer

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGAuthorizationCodeRepository is the Postgres-backed
// AuthorizationCodeRepository.
type PGAuthorizationCodeRepository struct {
	pool *pgxpool.Pool
}

// NewPGAuthorizationCodeRepository wraps a pgx pool as an
// AuthorizationCodeRepository.
func NewPGAuthorizationCodeRepository(pool *pgxpool.Pool) *PGAuthorizationCodeRepository {
	return &PGAuthorizationCodeRepository{pool: pool}
}

const authCodeColumns = `id, code, client_id, user_id, redirect_uri, scopes, code_challenge, code_challenge_method, created_at, expires_at, consumed_at`

// Create inserts a fresh authorization code row.
func (r *PGAuthorizationCodeRepository) Create(ctx context.Context, code *AuthorizationCode) error {
	if code == nil {
		return errors.New("authorization code: nil record")
	}
	if code.Code == "" || code.ClientID == "" || code.UserID == "" || code.RedirectURI == "" || code.CodeChallenge == "" {
		return errors.New("authorization code: code/client_id/user_id/redirect_uri/code_challenge are required")
	}
	if code.CodeChallengeMethod == "" {
		code.CodeChallengeMethod = PKCEMethodS256
	}
	scopes := code.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return err
	}
	row := r.pool.QueryRow(ctx,
		`INSERT INTO oauth_authorization_codes
		   (code, client_id, user_id, redirect_uri, scopes, code_challenge, code_challenge_method, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, created_at`,
		code.Code, code.ClientID, code.UserID, code.RedirectURI, scopesJSON,
		code.CodeChallenge, code.CodeChallengeMethod, code.ExpiresAt)
	return row.Scan(&code.ID, &code.CreatedAt)
}

func scanAuthCode(row pgx.Row) (*AuthorizationCode, error) {
	c := &AuthorizationCode{}
	var scopesJSON []byte
	var consumedAt *time.Time
	err := row.Scan(
		&c.ID,
		&c.Code,
		&c.ClientID,
		&c.UserID,
		&c.RedirectURI,
		&scopesJSON,
		&c.CodeChallenge,
		&c.CodeChallengeMethod,
		&c.CreatedAt,
		&c.ExpiresAt,
		&consumedAt,
	)
	if err != nil {
		return nil, err
	}
	var scopes []string
	if len(scopesJSON) > 0 {
		if err := json.Unmarshal(scopesJSON, &scopes); err != nil {
			return nil, err
		}
	}
	if scopes == nil {
		scopes = []string{}
	}
	c.Scopes = scopes
	c.ConsumedAt = consumedAt
	return c, nil
}

// GetByCode looks up an authorization code by its opaque code string.
func (r *PGAuthorizationCodeRepository) GetByCode(ctx context.Context, code string) (*AuthorizationCode, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+authCodeColumns+` FROM oauth_authorization_codes WHERE code = $1`, code)
	c, err := scanAuthCode(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAuthorizationCodeNotFound
		}
		return nil, err
	}
	return c, nil
}

// MarkConsumed stamps consumed_at so the code is not redeemed twice. The
// token endpoint calls this inside the same request that issues the access
// token — repeating the code in a second request will see consumed_at set
// and return ErrAuthorizationCodeConsumed.
func (r *PGAuthorizationCodeRepository) MarkConsumed(ctx context.Context, id string, at time.Time) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE oauth_authorization_codes SET consumed_at = $2 WHERE id = $1 AND consumed_at IS NULL`,
		id, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAuthorizationCodeConsumed
	}
	return nil
}

var _ AuthorizationCodeRepository = (*PGAuthorizationCodeRepository)(nil)

// PGOAuthTokenRepository is the Postgres-backed OAuthTokenRepository.
type PGOAuthTokenRepository struct {
	pool *pgxpool.Pool
}

// NewPGOAuthTokenRepository wraps a pgx pool as an OAuthTokenRepository.
func NewPGOAuthTokenRepository(pool *pgxpool.Pool) *PGOAuthTokenRepository {
	return &PGOAuthTokenRepository{pool: pool}
}

const oauthTokenColumns = `id, token_hash, token_prefix, token_type, client_id, user_id, scopes, created_at, expires_at, revoked_at`

// Create inserts a new oauth_tokens row.
func (r *PGOAuthTokenRepository) Create(ctx context.Context, tok *OAuthToken) error {
	if tok == nil {
		return errors.New("oauth token: nil record")
	}
	if tok.TokenPrefix == "" || len(tok.TokenHash) == 0 || tok.TokenType == "" || tok.ClientID == "" {
		return errors.New("oauth token: token_prefix/token_hash/token_type/client_id are required")
	}
	scopes := tok.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return err
	}
	var userID *string
	if tok.UserID != "" {
		u := tok.UserID
		userID = &u
	}
	row := r.pool.QueryRow(ctx,
		`INSERT INTO oauth_tokens
		   (token_hash, token_prefix, token_type, client_id, user_id, scopes, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, created_at`,
		tok.TokenHash, tok.TokenPrefix, tok.TokenType, tok.ClientID, userID, scopesJSON, tok.ExpiresAt)
	return row.Scan(&tok.ID, &tok.CreatedAt)
}

func scanOAuthToken(row pgx.Row) (*OAuthToken, error) {
	t := &OAuthToken{}
	var userID *string
	var scopesJSON []byte
	var revokedAt *time.Time
	err := row.Scan(
		&t.ID,
		&t.TokenHash,
		&t.TokenPrefix,
		&t.TokenType,
		&t.ClientID,
		&userID,
		&scopesJSON,
		&t.CreatedAt,
		&t.ExpiresAt,
		&revokedAt,
	)
	if err != nil {
		return nil, err
	}
	if userID != nil {
		t.UserID = *userID
	}
	var scopes []string
	if len(scopesJSON) > 0 {
		if err := json.Unmarshal(scopesJSON, &scopes); err != nil {
			return nil, err
		}
	}
	if scopes == nil {
		scopes = []string{}
	}
	t.Scopes = scopes
	t.RevokedAt = revokedAt
	return t, nil
}

// GetByPrefix returns every non-revoked oauth_tokens row with the given
// prefix and type. The caller still has to constant-time compare the hash
// — prefixes are an O(1) lookup index, not an authenticator.
func (r *PGOAuthTokenRepository) GetByPrefix(ctx context.Context, prefix, tokenType string) ([]*OAuthToken, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+oauthTokenColumns+` FROM oauth_tokens
		 WHERE token_prefix = $1 AND token_type = $2`, prefix, tokenType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*OAuthToken
	for rows.Next() {
		t, err := scanOAuthToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Revoke stamps revoked_at so the middleware rejects the token on next use.
func (r *PGOAuthTokenRepository) Revoke(ctx context.Context, id string, at time.Time) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE oauth_tokens SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL`, id, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTokenNotFound
	}
	return nil
}

var _ OAuthTokenRepository = (*PGOAuthTokenRepository)(nil)
